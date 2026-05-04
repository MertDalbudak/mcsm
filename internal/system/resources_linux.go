//go:build linux

package system

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SampleResources reads /proc/meminfo, /proc/loadavg, and statfs() the
// data directory. CPU usage requires two samples (200ms apart) to
// compute %used since /proc/stat is cumulative.
func SampleResources(dataDir string) (Resources, error) {
	r := Resources{
		CPU: CPU{Cores: runtime.NumCPU()},
	}

	if mem, err := readMeminfo(); err == nil {
		r.Mem = mem
	}
	if l, err := readLoadavg(); err == nil {
		r.Load = l
	}
	if used, err := readCPUUsage(); err == nil {
		r.CPU.UsedP = used
	}
	if d, err := statfsAt(dataDir); err == nil {
		r.Disk = []Disk{d}
	}
	return r, nil
}

func readMeminfo() (Memory, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Memory{}, err
	}
	defer f.Close()
	var total, avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = parseKB(line)
		}
		if total != 0 && avail != 0 {
			break
		}
	}
	used := total - avail
	pct := 0.0
	if total > 0 {
		pct = float64(used) * 100 / float64(total)
	}
	return Memory{TotalBytes: total, UsedBytes: used, UsedP: pct}, nil
}

func parseKB(line string) uint64 {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0
	}
	n, _ := strconv.ParseUint(parts[1], 10, 64)
	return n * 1024
}

func readLoadavg() (Load, error) {
	body, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return Load{}, err
	}
	parts := strings.Fields(string(body))
	if len(parts) < 3 {
		return Load{}, nil
	}
	one, _ := strconv.ParseFloat(parts[0], 64)
	five, _ := strconv.ParseFloat(parts[1], 64)
	fifteen, _ := strconv.ParseFloat(parts[2], 64)
	return Load{One: one, Five: five, Fifteen: fifteen}, nil
}

// readCPUUsage takes two snapshots ~200ms apart and returns busy %.
func readCPUUsage() (float64, error) {
	a, err := readCPUTimes()
	if err != nil {
		return 0, err
	}
	time.Sleep(200 * time.Millisecond)
	b, err := readCPUTimes()
	if err != nil {
		return 0, err
	}
	totalA := a.user + a.nice + a.system + a.idle + a.iowait + a.irq + a.softirq + a.steal
	totalB := b.user + b.nice + b.system + b.idle + b.iowait + b.irq + b.softirq + b.steal
	idleA := a.idle + a.iowait
	idleB := b.idle + b.iowait
	totalDelta := totalB - totalA
	if totalDelta == 0 {
		return 0, nil
	}
	idleDelta := idleB - idleA
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta), nil
}

type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func readCPUTimes() (cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuTimes{}, sc.Err()
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 9 {
		return cpuTimes{}, nil
	}
	atoi := func(s string) uint64 { n, _ := strconv.ParseUint(s, 10, 64); return n }
	return cpuTimes{
		user:    atoi(fields[1]),
		nice:    atoi(fields[2]),
		system:  atoi(fields[3]),
		idle:    atoi(fields[4]),
		iowait:  atoi(fields[5]),
		irq:     atoi(fields[6]),
		softirq: atoi(fields[7]),
		steal:   atoi(fields[8]),
	}, nil
}

func statfsAt(path string) (Disk, error) {
	if path == "" {
		path = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Disk{}, err
	}
	bs := uint64(st.Bsize)
	total := uint64(st.Blocks) * bs
	free := uint64(st.Bavail) * bs
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) * 100 / float64(total)
	}
	return Disk{Mount: path, TotalBytes: total, UsedBytes: used, UsedP: pct}, nil
}
