package lifecycle

import "sync"

// LoopRunner provides a reusable start/stop lifecycle for background loops.
// It guarantees idempotent start/stop and waits for loop exit on stop.
type LoopRunner struct {
	mu      sync.RWMutex
	wg      sync.WaitGroup
	running bool
	stopCh  chan struct{}
}

func NewLoopRunner() *LoopRunner {
	return &LoopRunner{}
}

func (r *LoopRunner) Start(loop func(stop <-chan struct{})) bool {
	if loop == nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return false
	}

	stopCh := make(chan struct{})
	r.stopCh = stopCh
	r.running = true
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		loop(stopCh)
	}()
	return true
}

func (r *LoopRunner) Stop() bool {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return false
	}
	stopCh := r.stopCh
	r.stopCh = nil
	r.running = false
	close(stopCh)
	r.mu.Unlock()

	r.wg.Wait()
	return true
}

func (r *LoopRunner) Running() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}
