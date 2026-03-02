package main

import (
	"context"
	"testing"
	"time"

	"clawgo/pkg/bus"
	"clawgo/pkg/cron"
)

func TestNormalizeCronTargetChatID(t *testing.T) {
	if got := normalizeCronTargetChatID("telegram", "telegram:12345"); got != "12345" {
		t.Fatalf("expected 12345, got %q", got)
	}
	if got := normalizeCronTargetChatID("telegram", "12345"); got != "12345" {
		t.Fatalf("expected unchanged chat id, got %q", got)
	}
}

func TestDispatchCronJob_DeliversTargetedMessageEvenWhenDeliverFalse(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	status := dispatchCronJob(mb, &cron.CronJob{
		ID: "job-1",
		Payload: cron.CronPayload{
			Message: "time to sleep",
			Deliver: false,
			Channel: "telegram",
			To:      "telegram:5988738763",
		},
	})

	if status != "delivered_targeted" {
		t.Fatalf("unexpected status: %s", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound message")
	}
	if out.Channel != "telegram" || out.ChatID != "5988738763" || out.Content != "time to sleep" {
		t.Fatalf("unexpected outbound: %#v", out)
	}
}

func TestDispatchCronJob_FallsBackToSystemInboundWithoutTarget(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	status := dispatchCronJob(mb, &cron.CronJob{ID: "job-2", Payload: cron.CronPayload{Message: "tick"}})
	if status != "scheduled" {
		t.Fatalf("unexpected status: %s", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message")
	}
	if in.Channel != "system" || in.ChatID != "internal:cron" {
		t.Fatalf("unexpected inbound: %#v", in)
	}
}
