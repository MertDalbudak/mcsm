// Package process supervises the Java child process that runs a
// Minecraft server. It captures stdout/stderr into a bounded ring
// buffer, exposes a channel that fires once on process exit, and
// supports graceful (SIGTERM) and forceful (SIGKILL) termination.
package process

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ExitInfo describes how a supervised process ended.
type ExitInfo struct {
	ExitCode int
	Signal   os.Signal
	Err      error
	At       time.Time
	// Reason is an mcsm-side classification: "normal", "killed", "crashed".
	Reason string
}

// Spec is what the supervisor needs to launch a process.
type Spec struct {
	Dir        string            // working directory (the server dir)
	Command    string            // executable, e.g. "java"
	Args       []string          // arguments
	Env        map[string]string // overlay on top of the parent's env
	LogCapBuf  int               // ring-buffer capacity in lines (default 2000)
}

// Process is a running supervised process.
type Process struct {
	cmd  *exec.Cmd
	logs *RingBuffer

	mu       sync.Mutex
	exited   bool
	exitInfo *ExitInfo

	exitCh chan ExitInfo // closed/sent-once once the process exits
}

// Start launches the process and begins capturing output. Returns
// immediately; use ExitChannel() to await the exit.
func Start(spec Spec) (*Process, error) {
	if spec.LogCapBuf <= 0 {
		spec.LogCapBuf = 2000
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = mergeEnv(os.Environ(), spec.Env)

	// Put the child in its own process group so SIGTERM to it doesn't
	// also hit mcsm, and so we can signal the whole tree at once.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	p := &Process{
		cmd:    cmd,
		logs:   newRingBuffer(spec.LogCapBuf),
		exitCh: make(chan ExitInfo, 1),
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	slog.Info("process: started",
		"pid", cmd.Process.Pid,
		"command", spec.Command,
		"dir", spec.Dir,
	)

	go p.logs.pump("stdout", stdout)
	go p.logs.pump("stderr", stderr)
	go p.waitAndPublish()
	return p, nil
}

// PID returns the child's process id.
func (p *Process) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Logs returns up to n most recent captured lines (oldest first).
func (p *Process) Logs(n int) []LogLine { return p.logs.Tail(n) }

// ExitChannel fires exactly once with the exit info, then is closed.
// Subsequent reads return the zero value.
func (p *Process) ExitChannel() <-chan ExitInfo { return p.exitCh }

// Exited returns (info, true) once the process has ended; otherwise
// (zero, false). Non-blocking.
func (p *Process) Exited() (ExitInfo, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exitInfo == nil {
		return ExitInfo{}, false
	}
	return *p.exitInfo, true
}

// Signal sends sig to the entire process group.
func (p *Process) Signal(sig syscall.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("process: not started")
	}
	pgid, err := syscall.Getpgid(p.cmd.Process.Pid)
	if err != nil {
		// Fall back to single-process signaling if pgid lookup fails.
		return p.cmd.Process.Signal(sig)
	}
	return syscall.Kill(-pgid, sig)
}

// Terminate SIGTERMs the process group, waits up to graceTimeout, then
// SIGKILLs if still alive. Safe to call multiple times.
func (p *Process) Terminate(ctx context.Context, graceTimeout time.Duration) error {
	if _, exited := p.Exited(); exited {
		return nil
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sigterm: %w", err)
	}
	timer := time.NewTimer(graceTimeout)
	defer timer.Stop()
	select {
	case <-p.exitCh:
		return nil
	case <-timer.C:
		slog.Warn("process: grace period elapsed, sending SIGKILL", "pid", p.PID())
		_ = p.Signal(syscall.SIGKILL)
	case <-ctx.Done():
		return ctx.Err()
	}
	// Wait for the actual exit after kill (bounded).
	select {
	case <-p.exitCh:
	case <-time.After(5 * time.Second):
		return errors.New("process: refused to die after SIGKILL")
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (p *Process) waitAndPublish() {
	err := p.cmd.Wait()
	info := ExitInfo{At: time.Now().UTC()}
	if err == nil {
		info.ExitCode = 0
		info.Reason = "normal"
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		ws, _ := exitErr.Sys().(syscall.WaitStatus)
		info.ExitCode = ws.ExitStatus()
		if ws.Signaled() {
			info.Signal = ws.Signal()
			info.Reason = "killed"
		} else if info.ExitCode == 0 {
			info.Reason = "normal"
		} else {
			info.Reason = "crashed"
		}
	} else {
		info.Err = err
		info.ExitCode = -1
		info.Reason = "crashed"
	}

	p.mu.Lock()
	p.exitInfo = &info
	p.exited = true
	p.mu.Unlock()

	slog.Info("process: exited",
		"pid", p.PID(),
		"exit_code", info.ExitCode,
		"signal", info.Signal,
		"reason", info.Reason,
	)
	// Non-blocking: capacity 1, single producer.
	p.exitCh <- info
	close(p.exitCh)
}

func mergeEnv(parent []string, overlay map[string]string) []string {
	if len(overlay) == 0 {
		return parent
	}
	out := append([]string(nil), parent...)
	for k, v := range overlay {
		out = append(out, k+"="+v)
	}
	return out
}
