package agent

import (
	"os"
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/providers"
)

func TestInjectResponsesMediaParts(t *testing.T) {
	msgs := []providers.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "look"},
	}
	media := []string{"https://example.com/a.png"}
	items := []bus.MediaItem{
		{Type: "image", Ref: "file_img_1"},
		{Type: "document", Ref: "file_doc_1"},
	}

	got := injectResponsesMediaParts(msgs, media, items)
	if len(got) != 2 {
		t.Fatalf("unexpected messages length: %#v", got)
	}
	parts := got[1].ContentParts
	if len(parts) != 4 {
		t.Fatalf("expected 4 content parts, got %#v", parts)
	}
	if parts[0].Type != "input_text" || parts[0].Text != "look" {
		t.Fatalf("expected first part to preserve input text, got %#v", parts[0])
	}
	if parts[1].Type != "input_image" || parts[1].ImageURL == "" {
		t.Fatalf("expected media URL as input_image, got %#v", parts[1])
	}
	if parts[2].Type != "input_image" || parts[2].FileID != "file_img_1" {
		t.Fatalf("expected image media item mapped to file_id, got %#v", parts[2])
	}
	if parts[3].Type != "input_file" || parts[3].FileID != "file_doc_1" {
		t.Fatalf("expected document media item mapped to input_file file_id, got %#v", parts[3])
	}
}

func TestBuildResponsesOptionsFromEnv(t *testing.T) {
	t.Setenv("CLAWGO_RESPONSES_WEB_SEARCH", "1")
	t.Setenv("CLAWGO_RESPONSES_WEB_SEARCH_CONTEXT_SIZE", "high")
	t.Setenv("CLAWGO_RESPONSES_FILE_SEARCH_VECTOR_STORE_IDS", "vs_1,vs_2")
	t.Setenv("CLAWGO_RESPONSES_FILE_SEARCH_MAX_NUM_RESULTS", "8")
	t.Setenv("CLAWGO_RESPONSES_INCLUDE", "output_text,tool_calls")
	t.Setenv("CLAWGO_RESPONSES_STREAM_INCLUDE_USAGE", "1")

	opts := buildResponsesOptions(1024, 0.2)
	if opts["max_tokens"] != int64(1024) {
		t.Fatalf("max_tokens mismatch: %#v", opts["max_tokens"])
	}
	if opts["temperature"] != 0.2 {
		t.Fatalf("temperature mismatch: %#v", opts["temperature"])
	}
	toolsRaw, ok := opts["responses_tools"].([]map[string]interface{})
	if !ok || len(toolsRaw) != 2 {
		t.Fatalf("expected two built-in response tools, got %#v", opts["responses_tools"])
	}
	if toolsRaw[0]["type"] != "web_search" {
		t.Fatalf("expected web_search tool first, got %#v", toolsRaw[0])
	}
	if toolsRaw[1]["type"] != "file_search" {
		t.Fatalf("expected file_search tool second, got %#v", toolsRaw[1])
	}
	if _, ok := opts["responses_include"]; !ok {
		t.Fatalf("expected responses_include in options")
	}
	if _, ok := opts["responses_stream_options"]; !ok {
		t.Fatalf("expected responses_stream_options in options")
	}

	// keep linter happy for unused os import when build tags differ
	_ = os.Getenv("CLAWGO_RESPONSES_WEB_SEARCH")
}
