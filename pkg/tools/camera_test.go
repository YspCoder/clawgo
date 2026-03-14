package tools

import (
	"context"
	"strings"
	"testing"
)

func TestCameraToolParsesFilenameArg(t *testing.T) {
	t.Parallel()

	tool := NewCameraTool(t.TempDir())
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"filename": "custom.jpg",
	})
	if err == nil {
		t.Fatal("expected camera access to fail in test environment")
	}
	if !strings.Contains(err.Error(), "/dev/video0") && !strings.Contains(err.Error(), "camera device") {
		t.Fatalf("unexpected error: %v", err)
	}
}
