package channels

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/YspCoder/clawgo/pkg/logger"
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
				logger.InfoCF(name, logger.C0168, map[string]interface{}{
					"task_name": taskName,
					"reason":    "context canceled",
				})
				return
			}
			logger.ErrorCF(name, logger.C0169, map[string]interface{}{
				"task_name":       taskName,
				logger.FieldError: err.Error(),
			})
			if onFailure != nil {
				onFailure(err)
			}
		}
	}()
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
