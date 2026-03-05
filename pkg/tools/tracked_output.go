package tools

import (
	"bytes"
	"sync"
	"sync/atomic"
)

// trackedOutput is a thread-safe writer+buffer pair used by command progress checks.
type trackedOutput struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	size atomic.Int64
}

func (t *trackedOutput) Write(p []byte) (int, error) {
	if t == nil {
		return 0, nil
	}
	t.mu.Lock()
	n, err := t.buf.Write(p)
	t.mu.Unlock()
	if n > 0 {
		t.size.Add(int64(n))
	}
	return n, err
}

func (t *trackedOutput) Len() int {
	if t == nil {
		return 0
	}
	return int(t.size.Load())
}

func (t *trackedOutput) String() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}
