package providers

import (
	"testing"
	"time"
)

func TestBuildQwenChatRequestSupportsExtendedThinkingSuffixes(t *testing.T) {
	base := NewHTTPProvider("qwen", "token", qwenCompatBaseURL, "qwen-max", false, "oauth", 5*time.Second, nil)
	autoBody := buildQwenChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "qwen-max(-1)", nil, false)
	if got := autoBody["reasoning_effort"]; got != "auto" {
		t.Fatalf("reasoning_effort = %#v, want auto", got)
	}
	disableBody := buildQwenChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "qwen-max(0)", nil, false)
	thinking, _ := disableBody["thinking"].(map[string]interface{})
	if got := thinking["type"]; got != "disabled" {
		t.Fatalf("thinking.type = %#v, want disabled", got)
	}
	minimalBody := buildQwenChatRequest(base, []Message{{Role: "user", Content: "hi"}}, nil, "qwen-max(minimal)", nil, false)
	if got := minimalBody["reasoning_effort"]; got != "low" {
		t.Fatalf("reasoning_effort = %#v, want low", got)
	}
}
