package channels

import (
	"context"
	"sync"
	"testing"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
)

type recordingChannel struct {
	mu   sync.Mutex
	sent []bus.OutboundMessage
}

func (r *recordingChannel) Name() string { return "test" }
func (r *recordingChannel) Start(ctx context.Context) error { return nil }
func (r *recordingChannel) Stop(ctx context.Context) error { return nil }
func (r *recordingChannel) IsRunning() bool { return true }
func (r *recordingChannel) IsAllowed(senderID string) bool { return true }
func (r *recordingChannel) HealthCheck(ctx context.Context) error { return nil }
func (r *recordingChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, msg)
	return nil
}
func (r *recordingChannel) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func TestDispatchOutbound_DeduplicatesRepeatedSend(t *testing.T) {
	mb := bus.NewMessageBus()
	mgr, err := NewManager(&config.Config{}, mb)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	rc := &recordingChannel{}
	mgr.RegisterChannel("test", rc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.dispatchOutbound(ctx)

	msg := bus.OutboundMessage{Channel: "test", ChatID: "c1", Content: "hello", Action: "send"}
	mb.PublishOutbound(msg)
	mb.PublishOutbound(msg)
	mb.PublishOutbound(msg)
	time.Sleep(200 * time.Millisecond)

	if got := rc.count(); got != 1 {
		t.Fatalf("expected 1 send after dedupe, got %d", got)
	}
}

func TestBaseChannel_HandleMessage_ContentHashFallbackDedupe(t *testing.T) {
	mb := bus.NewMessageBus()
	bc := NewBaseChannel("test", nil, mb, nil)
	meta1 := map[string]string{}
	meta2 := map[string]string{}

	bc.HandleMessage("u1", "c1", "same content", nil, meta1)
	bc.HandleMessage("u1", "c1", "same content", nil, meta2)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Fatalf("expected first inbound message")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if _, ok := mb.ConsumeInbound(ctx2); ok {
		t.Fatalf("expected duplicate inbound to be dropped")
	}
}


func TestDispatchOutbound_DifferentButtonsShouldNotDeduplicate(t *testing.T) {
	mb := bus.NewMessageBus()
	mgr, err := NewManager(&config.Config{}, mb)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	rc := &recordingChannel{}
	mgr.RegisterChannel("test", rc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.dispatchOutbound(ctx)

	msg1 := bus.OutboundMessage{Channel: "test", ChatID: "c1", Content: "choose", Action: "send", Buttons: [][]bus.Button{{{Text: "A", Data: "a"}}}}
	msg2 := bus.OutboundMessage{Channel: "test", ChatID: "c1", Content: "choose", Action: "send", Buttons: [][]bus.Button{{{Text: "B", Data: "b"}}}}
	mb.PublishOutbound(msg1)
	mb.PublishOutbound(msg2)
	time.Sleep(220 * time.Millisecond)

	if got := rc.count(); got != 2 {
		t.Fatalf("expected 2 sends when buttons differ, got %d", got)
	}
}
