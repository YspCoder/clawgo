package tools

import (
	"strings"
	"testing"
)

func TestFormatMemoryLine(t *testing.T) {
	got := formatMemoryLine("remember this", "high", "user", []string{"preference", "lang"})
	if got == "" {
		t.Fatal("empty formatted line")
	}
	if want := "importance=high"; !strings.Contains(got, want) {
		t.Fatalf("expected %q in %q", want, got)
	}
	if want := "source=user"; !strings.Contains(got, want) {
		t.Fatalf("expected %q in %q", want, got)
	}
}

func TestNormalizeImportance(t *testing.T) {
	if normalizeImportance("HIGH") != "high" {
		t.Fatal("expected high")
	}
	if normalizeImportance("unknown") != "medium" {
		t.Fatal("expected medium fallback")
	}
}
