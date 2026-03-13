//go:build !omit_telegram

package channels

import (
	"strings"
	"testing"
)

func TestMarkdownToTelegramHTMLFormatsChineseAndInlineMarkup(t *testing.T) {
	got := markdownToTelegramHTML("中文 **加粗** *斜体* `代码`")
	if strings.Contains(got, "鈹") || strings.Contains(got, "鈥") {
		t.Fatalf("unexpected mojibake in output: %q", got)
	}
	if !strings.Contains(got, "中文 <b>加粗</b> <i>斜体</i> <code>代码</code>") {
		t.Fatalf("unexpected formatted output: %q", got)
	}
}

func TestMarkdownToTelegramHTMLFormatsQuoteAndListsWithoutMojibake(t *testing.T) {
	input := "> 引用\n- 列表\n* 另一项\n1. 有序"
	got := markdownToTelegramHTML(input)

	if strings.Contains(got, "鈹") || strings.Contains(got, "鈥") {
		t.Fatalf("unexpected mojibake in output: %q", got)
	}
	if !strings.Contains(got, "&gt; 引用") {
		t.Fatalf("expected escaped quote marker, got %q", got)
	}
	if !strings.Contains(got, "• 列表") || !strings.Contains(got, "• 另一项") {
		t.Fatalf("expected bullet list markers, got %q", got)
	}
	if !strings.Contains(got, "1. 有序") {
		t.Fatalf("expected ordered list marker preserved, got %q", got)
	}
}

func TestRenderTelegramStreamChunksDoesNotInjectMojibake(t *testing.T) {
	chunks := renderTelegramStreamChunks("> 引用\n- 列表\n1. 有序\n中文内容")
	if len(chunks) == 0 {
		t.Fatal("expected stream chunks")
	}
	for _, chunk := range chunks {
		if strings.Contains(chunk.payload, "鈹") || strings.Contains(chunk.payload, "鈥") {
			t.Fatalf("unexpected mojibake chunk payload: %q", chunk.payload)
		}
	}
}

func TestShouldFlushTelegramStreamSnapshotRejectsUnclosedMarkdown(t *testing.T) {
	cases := []string{
		"中文 **加粗",
		"中文 *斜体",
		"中文 `代码",
		"```go\nfmt.Println(\"hi\")",
		"[链接](https://example.com",
	}

	for _, input := range cases {
		if shouldFlushTelegramStreamSnapshot(input) {
			t.Fatalf("expected unsafe snapshot to be rejected: %q", input)
		}
		if chunks := renderTelegramStreamChunks(input); len(chunks) != 0 {
			t.Fatalf("expected no chunks for unsafe snapshot %q, got %+v", input, chunks)
		}
	}
}

func TestShouldFlushTelegramStreamSnapshotAcceptsBalancedMarkdown(t *testing.T) {
	input := "> 引用\n- 列表\n1. 有序\n中文 **加粗** *斜体* `代码` [链接](https://example.com)"
	if !shouldFlushTelegramStreamSnapshot(input) {
		t.Fatalf("expected balanced snapshot to flush: %q", input)
	}
	chunks := renderTelegramStreamChunks(input)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks for balanced snapshot")
	}
	if chunks[0].parseMode != "" {
		t.Fatalf("expected auto-markdown stream to degrade to plain text, got mode=%q", chunks[0].parseMode)
	}
}

func TestRenderTelegramStreamChunksFinalizeRecoversRichFormatting(t *testing.T) {
	input := "> 引用\n- 列表\n中文 **加粗** *斜体* `代码` [链接](https://example.com)"
	chunks := renderTelegramStreamChunksWithFinalize(input, true)
	if len(chunks) == 0 {
		t.Fatalf("expected finalize chunks")
	}
	if chunks[0].parseMode != "HTML" {
		t.Fatalf("expected finalize chunk to use HTML, got %q", chunks[0].parseMode)
	}
	if !strings.Contains(chunks[0].payload, "<b>加粗</b>") {
		t.Fatalf("expected rich formatting restored, got %q", chunks[0].payload)
	}
}

func TestMarkdownToTelegramHTMLHandlesEdgeFormatting(t *testing.T) {
	input := "> 第一段引用\n> 第二段引用\n- 列表一\n  - 子项\n1. 有序项\n\n```go\nfmt.Println(\"hi\")\nfmt.Println(\"bye\")\n```\n[链接](https://example.com/path?q=1)"
	got := markdownToTelegramHTML(input)

	if !strings.Contains(got, "&gt; 第一段引用\n&gt; 第二段引用") {
		t.Fatalf("expected consecutive quote lines to stay stable, got %q", got)
	}
	if !strings.Contains(got, "• 列表一") || !strings.Contains(got, "• 子项") {
		t.Fatalf("expected nested list lines to normalize to bullets, got %q", got)
	}
	if !strings.Contains(got, "<pre><code>fmt.Println(\"hi\")\nfmt.Println(\"bye\")\n</code></pre>") {
		t.Fatalf("expected code block newlines preserved, got %q", got)
	}
	if !strings.Contains(got, `<a href="https://example.com/path?q=1">链接</a>`) {
		t.Fatalf("expected link conversion, got %q", got)
	}
}
