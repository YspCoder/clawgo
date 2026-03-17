//go:build !omit_telegram

package channels

import (
	"strings"
	"testing"
)

func TestMarkdownToTelegramHTMLFormatsInlineMarkup(t *testing.T) {
	got := markdownToTelegramHTML("plain **bold** *italic* `code`")
	if !strings.Contains(got, "plain <b>bold</b> <i>italic</i> <code>code</code>") {
		t.Fatalf("unexpected formatted output: %q", got)
	}
}

func TestMarkdownToTelegramHTMLFormatsQuoteAndLists(t *testing.T) {
	input := "> quote\n- bullet\n* other\n1. ordered"
	got := markdownToTelegramHTML(input)

	if !strings.Contains(got, "&gt; quote") {
		t.Fatalf("expected escaped quote marker, got %q", got)
	}
	if !strings.Contains(got, "• bullet") || !strings.Contains(got, "• other") {
		t.Fatalf("expected bullet list markers, got %q", got)
	}
	if !strings.Contains(got, "1. ordered") {
		t.Fatalf("expected ordered list marker preserved, got %q", got)
	}
}

func TestRenderTelegramStreamChunksDoesNotInjectBrokenPayload(t *testing.T) {
	chunks := renderTelegramStreamChunksWithFinalize("> quote\n- bullet\n1. ordered\nplain text", false)
	if len(chunks) == 0 {
		t.Fatal("expected stream chunks")
	}
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.payload) == "" {
			t.Fatalf("unexpected empty chunk payload: %+v", chunk)
		}
	}
}

func TestShouldFlushTelegramStreamSnapshotRejectsUnclosedMarkdown(t *testing.T) {
	cases := []string{
		"text **bold",
		"text *italic",
		"text `code",
		"```go\nfmt.Println(\"hi\")",
		"[link](https://example.com",
	}

	for _, input := range cases {
		if shouldFlushTelegramStreamSnapshot(input) {
			t.Fatalf("expected unsafe snapshot to be rejected: %q", input)
		}
		if chunks := renderTelegramStreamChunksWithFinalize(input, false); len(chunks) != 0 {
			t.Fatalf("expected no chunks for unsafe snapshot %q, got %+v", input, chunks)
		}
	}
}

func TestShouldFlushTelegramStreamSnapshotAcceptsBalancedMarkdown(t *testing.T) {
	input := "> quote\n- bullet\n1. ordered\ntext **bold** *italic* `code` [link](https://example.com)"
	if !shouldFlushTelegramStreamSnapshot(input) {
		t.Fatalf("expected balanced snapshot to flush: %q", input)
	}
	chunks := renderTelegramStreamChunksWithFinalize(input, false)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks for balanced snapshot")
	}
	if chunks[0].parseMode != "" {
		t.Fatalf("expected auto-markdown stream to degrade to plain text, got mode=%q", chunks[0].parseMode)
	}
}

func TestRenderTelegramStreamChunksFinalizeRecoversRichFormatting(t *testing.T) {
	input := "> quote\n- bullet\ntext **bold** *italic* `code` [link](https://example.com)"
	chunks := renderTelegramStreamChunksWithFinalize(input, true)
	if len(chunks) == 0 {
		t.Fatalf("expected finalize chunks")
	}
	if chunks[0].parseMode != "HTML" {
		t.Fatalf("expected finalize chunk to use HTML, got %q", chunks[0].parseMode)
	}
	if !strings.Contains(chunks[0].payload, "<b>bold</b>") {
		t.Fatalf("expected rich formatting restored, got %q", chunks[0].payload)
	}
}

func TestMarkdownToTelegramHTMLHandlesEdgeFormatting(t *testing.T) {
	input := "> first quote\n> second quote\n- item one\n  - child item\n1. ordered\n\n```go\nfmt.Println(\"hi\")\nfmt.Println(\"bye\")\n```\n[link](https://example.com/path?q=1)"
	got := markdownToTelegramHTML(input)

	if !strings.Contains(got, "&gt; first quote\n&gt; second quote") {
		t.Fatalf("expected consecutive quote lines to stay stable, got %q", got)
	}
	if !strings.Contains(got, "• item one") || !strings.Contains(got, "• child item") {
		t.Fatalf("expected nested list lines to normalize to bullets, got %q", got)
	}
	if !strings.Contains(got, "<pre><code>fmt.Println(\"hi\")\nfmt.Println(\"bye\")\n</code></pre>") {
		t.Fatalf("expected code block newlines preserved, got %q", got)
	}
	if !strings.Contains(got, `<a href="https://example.com/path?q=1">link</a>`) {
		t.Fatalf("expected link conversion, got %q", got)
	}
}
