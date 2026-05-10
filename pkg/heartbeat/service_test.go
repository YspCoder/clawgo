package heartbeat

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartDisabledDoesNotTriggerHeartbeat(t *testing.T) {
	var calls int64
	hs := NewHeartbeatService(t.TempDir(), func(prompt string) (string, error) {
		atomic.AddInt64(&calls, 1)
		return "", nil
	}, 1, false, "")
	hs.interval = time.Millisecond

	if err := hs.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	hs.Stop()

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Fatalf("heartbeat calls = %d, want 0", got)
	}
}

func TestEnabledStartCallsCallbackAndStopPreventsMoreCalls(t *testing.T) {
	var calls int64
	hs := NewHeartbeatService(t.TempDir(), func(prompt string) (string, error) {
		atomic.AddInt64(&calls, 1)
		return "", nil
	}, 1, true, "")
	hs.interval = 5 * time.Millisecond

	if err := hs.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	waitForHeartbeatCalls(t, &calls, 1)
	hs.Stop()
	afterStop := atomic.LoadInt64(&calls)
	time.Sleep(20 * time.Millisecond)

	if got := atomic.LoadInt64(&calls); got != afterStop {
		t.Fatalf("heartbeat calls after Stop = %d, want %d", got, afterStop)
	}
}

func TestBuildPromptUsesTemplateAndSkipsEmptyMarkdown(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "AGENTS.md"), "# Policy\nheartbeat_ack_token: ACK_OK\n")
	writeFile(t, filepath.Join(workspace, "HEARTBEAT.md"), "# Heartbeat\n\n## Notes\n")

	hs := NewHeartbeatService(workspace, nil, 1, true, "Custom template")
	prompt := hs.buildPrompt()

	for _, want := range []string{"Custom template", "Current time:", "## AGENTS.md", "heartbeat_ack_token: ACK_OK", "## HEARTBEAT.md"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "## Notes") {
		t.Fatalf("prompt included effectively empty HEARTBEAT.md content:\n%s", prompt)
	}
}

func TestBuildPromptDefaultMentionsAckToken(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "AGENTS.md"), "- heartbeat_ack_token: `ACK_DONE`\n")
	writeFile(t, filepath.Join(workspace, "HEARTBEAT.md"), "Take a tiny action.\n")

	hs := NewHeartbeatService(workspace, nil, 1, true, "")
	prompt := hs.buildPrompt()

	if !strings.Contains(prompt, "return ACK_DONE") {
		t.Fatalf("default prompt did not include ack token:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Take a tiny action.") {
		t.Fatalf("prompt did not include heartbeat notes:\n%s", prompt)
	}
}

func TestIsEffectivelyEmptyMarkdown(t *testing.T) {
	if !isEffectivelyEmptyMarkdown("# Title\n\n## Empty\n") {
		t.Fatal("heading-only markdown should be treated as empty")
	}
	if isEffectivelyEmptyMarkdown("# Title\n\nDo the thing.\n") {
		t.Fatal("markdown with body text should not be treated as empty")
	}
}

func TestHeartbeatAckTokenFromText(t *testing.T) {
	got := heartbeatAckTokenFromText("# Runtime\n- heartbeat_ack_token: \"ACK-123\"\n")
	if got != "ACK-123" {
		t.Fatalf("ack token = %q, want ACK-123", got)
	}
	if got := heartbeatAckTokenFromText("heartbeat: none"); got != "" {
		t.Fatalf("ack token = %q, want empty", got)
	}
}

func waitForHeartbeatCalls(t *testing.T, calls *int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(calls) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("heartbeat calls = %d, want at least %d", atomic.LoadInt64(calls), want)
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
