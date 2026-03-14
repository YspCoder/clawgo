package bus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMessageBusPublishAfterCloseDoesNotPanic(t *testing.T) {
	t.Parallel()

	mb := NewMessageBus()
	mb.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mb.PublishInbound(InboundMessage{Channel: "test"})
		mb.PublishOutbound(OutboundMessage{Channel: "test"})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish after close blocked")
	}
}

func TestMessageBusCloseWhilePublishingDoesNotPanic(t *testing.T) {
	t.Parallel()

	mb := NewMessageBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			if _, ok := mb.ConsumeInbound(ctx); !ok {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				mb.PublishInbound(InboundMessage{Channel: "test"})
				mb.PublishOutbound(OutboundMessage{Channel: "test"})
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	mb.Close()
	cancel()
	wg.Wait()
}
