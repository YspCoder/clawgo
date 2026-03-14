package configops

import (
	"reflect"
	"testing"
)

func TestNormalizeConfigPath(t *testing.T) {
	t.Parallel()

	if got := NormalizeConfigPath(".agents..enable."); got != "agents.enabled" {
		t.Fatalf("unexpected normalized path: %q", got)
	}
}

func TestParseConfigValueJSON(t *testing.T) {
	t.Parallel()

	got := ParseConfigValue(`{"enabled":true,"count":2}`)
	row, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map value, got %#v", got)
	}
	if enabled, _ := row["enabled"].(bool); !enabled {
		t.Fatalf("expected enabled=true, got %#v", row["enabled"])
	}
}

func TestSetMapValueByPathRejectsNilRoot(t *testing.T) {
	t.Parallel()

	if err := SetMapValueByPath(nil, "agents.main.enabled", true); err == nil {
		t.Fatal("expected error for nil root")
	}
}

func TestSetAndGetMapValueByPath(t *testing.T) {
	t.Parallel()

	root := map[string]interface{}{}
	if err := SetMapValueByPath(root, ".agents.main.enable.", true); err != nil {
		t.Fatalf("set value failed: %v", err)
	}
	got, ok := GetMapValueByPath(root, "agents.main.enabled")
	if !ok || !reflect.DeepEqual(got, true) {
		t.Fatalf("unexpected get result: %#v, %v", got, ok)
	}
}
