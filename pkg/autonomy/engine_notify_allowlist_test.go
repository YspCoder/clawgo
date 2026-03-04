package autonomy

import (
	"testing"
	"time"
)

func TestShouldNotify_RespectsNotifyAllowFrom(t *testing.T) {
	e := &Engine{opts: Options{
		DefaultNotifyChannel:        "telegram",
		DefaultNotifyChatID:         "chat-1",
		NotifyAllowFrom:             []string{"chat-2", "chat-3"},
		NotifyCooldownSec:           1,
		NotifySameReasonCooldownSec: 1,
	}, lastNotify: map[string]time.Time{}}
	if e.shouldNotify("k1", "") {
		t.Fatalf("expected notify to be blocked when chat not in allowlist")
	}

	e.opts.NotifyAllowFrom = []string{"chat-1"}
	if !e.shouldNotify("k2", "") {
		t.Fatalf("expected notify to pass when chat in allowlist")
	}
}
