package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveConfigUsesOwnerOnlyPermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not reliable on windows")
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, DefaultConfig()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", got)
	}
}
