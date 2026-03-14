package tools

import "testing"

func TestMapObjectArgReturnsEmptyMapForMissingValue(t *testing.T) {
	t.Parallel()

	got := MapObjectArg(nil, "arguments")
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty map, got %#v", got)
	}
}

func TestMapObjectArgReturnsObjectValue(t *testing.T) {
	t.Parallel()

	got := MapObjectArg(map[string]interface{}{
		"arguments": map[string]interface{}{"path": "README.md"},
	}, "arguments")
	if got["path"] != "README.md" {
		t.Fatalf("unexpected object arg: %#v", got)
	}
}
