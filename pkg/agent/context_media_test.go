package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildUserContentParts_InlinesSmallFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	parts := buildUserContentParts("check", []string{filePath})
	if len(parts) < 2 {
		t.Fatalf("expected at least text + file parts, got %d", len(parts))
	}

	foundFile := false
	for _, p := range parts {
		if p.Type == "input_file" {
			foundFile = true
			if !strings.HasPrefix(p.FileData, "data:text/plain") {
				t.Fatalf("unexpected file data prefix: %q", p.FileData)
			}
		}
	}
	if !foundFile {
		t.Fatalf("expected input_file part")
	}
}

func TestBuildUserContentParts_SkipsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "big.bin")
	content := make([]byte, maxInlineMediaFileBytes+1)
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	parts := buildUserContentParts("check", []string{filePath})
	for _, p := range parts {
		if p.Type == "input_file" || p.Type == "input_image" {
			t.Fatalf("oversized attachment should not be inlined")
		}
	}

	foundNote := false
	for _, p := range parts {
		if p.Type == "input_text" && strings.Contains(p.Text, "too large and was not inlined") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Fatalf("expected oversize note in input_text part")
	}
}

func TestBuildUserContentParts_SkipsURLMedia(t *testing.T) {
	parts := buildUserContentParts("check", []string{"https://example.com/a.pdf"})

	for _, p := range parts {
		if p.Type == "input_file" || p.Type == "input_image" {
			t.Fatalf("url attachment should not be inlined")
		}
	}

	foundNote := false
	for _, p := range parts {
		if p.Type == "input_text" && strings.Contains(p.Text, "kept as URL only") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Fatalf("expected url note in input_text part")
	}
}
