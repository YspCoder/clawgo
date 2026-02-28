package autonomy

import "testing"

func TestShouldNotify_RespectsNotifyAllowChats(t *testing.T) {
	e := &Engine{opts: Options{
		DefaultNotifyChannel:        "telegram",
		DefaultNotifyChatID:         "chat-1",
		NotifyAllowChats:            []string{"chat-2", "chat-3"},
		NotifyCooldownSec:           1,
		NotifySameReasonCooldownSec: 1,
	}}
	if e.shouldNotify("k1", "") {
		t.Fatalf("expected notify to be blocked when chat not in allowlist")
	}

	e.opts.NotifyAllowChats = []string{"chat-1"}
	if !e.shouldNotify("k2", "") {
		t.Fatalf("expected notify to pass when chat in allowlist")
	}
}
