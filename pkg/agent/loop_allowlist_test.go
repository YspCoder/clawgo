package agent

import (
	"context"
	"testing"
)

func TestEnsureToolAllowedByContext(t *testing.T) {
	ctx := context.Background()

	if err := ensureToolAllowedByContext(ctx, "write_file", map[string]interface{}{}); err != nil {
		t.Fatalf("expected unrestricted context to allow tool, got: %v", err)
	}

	restricted := withToolAllowlistContext(ctx, []string{"read_file", "memory_search"})
	if err := ensureToolAllowedByContext(restricted, "read_file", map[string]interface{}{}); err != nil {
		t.Fatalf("expected allowed tool to pass, got: %v", err)
	}
	if err := ensureToolAllowedByContext(restricted, "write_file", map[string]interface{}{}); err == nil {
		t.Fatalf("expected disallowed tool to fail")
	}
}

func TestEnsureToolAllowedByContextParallelNested(t *testing.T) {
	restricted := withToolAllowlistContext(context.Background(), []string{"parallel", "read_file"})

	okArgs := map[string]interface{}{
		"calls": []interface{}{
			map[string]interface{}{"tool": "read_file", "arguments": map[string]interface{}{"path": "README.md"}},
		},
	}
	if err := ensureToolAllowedByContext(restricted, "parallel", okArgs); err != nil {
		t.Fatalf("expected parallel with allowed nested tool to pass, got: %v", err)
	}

	badArgs := map[string]interface{}{
		"calls": []interface{}{
			map[string]interface{}{"tool": "write_file", "arguments": map[string]interface{}{"path": "README.md", "content": "x"}},
		},
	}
	if err := ensureToolAllowedByContext(restricted, "parallel", badArgs); err == nil {
		t.Fatalf("expected parallel with disallowed nested tool to fail")
	}
}
