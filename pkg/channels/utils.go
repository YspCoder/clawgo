package channels

import (
	"context"
	"errors"
	"sync"

	"clawgo/pkg/logger"
)

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 0 {
		return ""
	}
	return s[:maxLen]
}

func safeCloseSignal(v interface{}) {
	ch, ok := v.(chan struct{})
	if !ok || ch == nil {
		return
	}
	defer func() { _ = recover() }()
	close(ch)
}

type cancelGuard struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (g *cancelGuard) set(cancel context.CancelFunc) {
	g.mu.Lock()
	g.cancel = cancel
	g.mu.Unlock()
}

func (g *cancelGuard) cancelAndClear() {
	g.mu.Lock()
	cancel := g.cancel
	g.cancel = nil
	g.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func runChannelTask(name, taskName string, task func() error, onFailure func(error)) {
	go func() {
		if err := task(); err != nil {
			if errors.Is(err, context.Canceled) {
				logger.InfoCF(name, taskName+" stopped", map[string]interface{}{
					"reason": "context canceled",
				})
				return
			}
			logger.ErrorCF(name, taskName+" failed", map[string]interface{}{
				logger.FieldError: err.Error(),
			})
			if onFailure != nil {
				onFailure(err)
			}
		}
	}()
}
