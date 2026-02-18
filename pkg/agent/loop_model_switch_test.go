package agent

import "testing"

func TestApplyRuntimeModelConfig_ProxyFallbacks(t *testing.T) {
	al := &AgentLoop{proxyFallbacks: []string{"old-proxy"}}
	al.applyRuntimeModelConfig("agents.defaults.proxy_fallbacks", []interface{}{"backup-a", "", "backup-b"})
	if len(al.proxyFallbacks) != 2 {
		t.Fatalf("expected 2 fallbacks, got %d: %v", len(al.proxyFallbacks), al.proxyFallbacks)
	}
	if al.proxyFallbacks[0] != "backup-a" || al.proxyFallbacks[1] != "backup-b" {
		t.Fatalf("unexpected fallbacks: %v", al.proxyFallbacks)
	}
}

func TestParseStringList_StringValue(t *testing.T) {
	out := parseStringList("backup-a")
	if len(out) != 1 || out[0] != "backup-a" {
		t.Fatalf("unexpected parse result: %v", out)
	}
}
