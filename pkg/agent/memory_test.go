package agent

import (
	"strings"
	"testing"
)

func TestTruncateMemoryTextRuneSafe(t *testing.T) {
	in := "你好世界这是一个测试"
	out := truncateMemoryText(in, 6)
	if strings.Contains(out, "�") {
		t.Fatalf("expected rune-safe truncation, got invalid rune replacement: %q", out)
	}
}

func TestCompressMemoryForPromptPrefersStructuredLines(t *testing.T) {
	in := `
# Long-term Memory

plain paragraph line 1
plain paragraph line 2

- bullet one
- bullet two

another paragraph
`
	out := compressMemoryForPrompt(in, 4, 200)
	if !strings.Contains(out, "# Long-term Memory") {
		t.Fatalf("expected heading in digest, got: %q", out)
	}
	if !strings.Contains(out, "- bullet one") {
		t.Fatalf("expected bullet in digest, got: %q", out)
	}
	if strings.Contains(out, "plain paragraph line 2") {
		t.Fatalf("expected paragraph compression to keep first line only, got: %q", out)
	}
}
