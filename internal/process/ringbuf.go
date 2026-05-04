package process

import (
	"bufio"
	"io"
	"sync"
	"time"
)

// LogLine is one captured line from the supervised process. We don't
// parse semantic fields here — that's logtail's job. Capture is purely
// for /logs?tail=N before the dedicated tailer is wired up.
type LogLine struct {
	At     time.Time `json:"at"`
	Stream string    `json:"stream"` // "stdout" | "stderr"
	Text   string    `json:"text"`
}

// RingBuffer is a fixed-capacity FIFO of recent LogLines. New lines push
// out the oldest. Goroutine-safe.
type RingBuffer struct {
	mu   sync.RWMutex
	data []LogLine
	head int // index of next write; len(data)==cap when full
	full bool
	cap  int
}

func newRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{
		data: make([]LogLine, capacity),
		cap:  capacity,
	}
}

func (r *RingBuffer) push(l LogLine) {
	r.mu.Lock()
	r.data[r.head] = l
	r.head = (r.head + 1) % r.cap
	if r.head == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Tail returns up to n most recent lines, oldest first. If n > capacity
// you get capacity. If the buffer is empty you get an empty slice.
func (r *RingBuffer) Tail(n int) []LogLine {
	r.mu.RLock()
	defer r.mu.RUnlock()
	have := r.cap
	if !r.full {
		have = r.head
	}
	if n > have {
		n = have
	}
	if n <= 0 {
		return nil
	}
	out := make([]LogLine, n)
	// Start at (head - n) mod cap.
	start := (r.head - n + r.cap) % r.cap
	for i := 0; i < n; i++ {
		out[i] = r.data[(start+i)%r.cap]
	}
	return out
}

// pump reads lines from r and pushes them into the buffer with the
// given stream label. Blocks until r returns an error (typically EOF
// when the process closes its stream).
func (rb *RingBuffer) pump(stream string, r io.Reader) {
	sc := bufio.NewScanner(r)
	// Allow long lines (default buffer is 64K which is fine for MC logs,
	// but Forge mod stack traces can be very long).
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		rb.push(LogLine{
			At:     time.Now().UTC(),
			Stream: stream,
			Text:   sc.Text(),
		})
	}
}
