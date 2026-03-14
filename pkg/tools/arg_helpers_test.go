package tools

import "testing"

func TestMapBoolArgParsesStringValues(t *testing.T) {
	t.Parallel()

	got, ok := MapBoolArg(map[string]interface{}{"enabled": "true"}, "enabled")
	if !ok || !got {
		t.Fatalf("expected string true to parse, got %v %v", got, ok)
	}
}

func TestMapIntArgParsesStringValues(t *testing.T) {
	t.Parallel()

	if got := MapIntArg(map[string]interface{}{"limit": "25"}, "limit", 5); got != 25 {
		t.Fatalf("expected parsed int, got %d", got)
	}
}

func TestMapStringListArgParsesCommaSeparatedValues(t *testing.T) {
	t.Parallel()

	got := MapStringListArg(map[string]interface{}{"tools": "shell, sessions, shell"}, "tools")
	if len(got) != 2 || got[0] != "shell" || got[1] != "sessions" {
		t.Fatalf("unexpected string list: %#v", got)
	}
}
