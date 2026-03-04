package agent

import (
	"testing"

	"clawgo/pkg/bus"
	"clawgo/pkg/config"
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
	if len(parts) != 3 {
		t.Fatalf("expected 3 content parts, got %#v", parts)
	}
	if parts[0].Type != "input_text" || parts[0].Text != "look" {
		t.Fatalf("expected first part to preserve input text, got %#v", parts[0])
	}
	if parts[1].Type != "input_image" || parts[1].FileID != "file_img_1" {
		t.Fatalf("expected image media item mapped to file_id, got %#v", parts[1])
	}
	if parts[2].Type != "input_file" || parts[2].FileID != "file_doc_1" {
		t.Fatalf("expected document media item mapped to input_file file_id, got %#v", parts[2])
	}
}

func TestBuildResponsesOptionsFromConfig(t *testing.T) {
	al := &AgentLoop{
		providerNames: []string{"proxy"},
		providerResponses: map[string]config.ProviderResponsesConfig{
			"proxy": {
				WebSearchEnabled:         true,
				WebSearchContextSize:     "high",
				FileSearchVectorStoreIDs: []string{"vs_1", "vs_2"},
				FileSearchMaxNumResults:  8,
				Include:                  []string{"output_text", "tool_calls"},
				StreamIncludeUsage:       true,
			},
		},
	}

	opts := al.buildResponsesOptions("session-a", 1024, 0.2)
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
}

func TestInjectResponsesMediaParts_SkipsLocalPathsForResponsesContentParts(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "check local media"},
	}
	items := []bus.MediaItem{
		{Type: "image", Path: "/tmp/a.png"},
		{Type: "document", Path: "/tmp/a.pdf"},
	}
	got := injectResponsesMediaParts(msgs, nil, items)
	if len(got[0].ContentParts) != 1 {
		t.Fatalf("expected only input_text for local files, got %#v", got[0].ContentParts)
	}
	if got[0].ContentParts[0].Type != "input_text" {
		t.Fatalf("expected input_text only, got %#v", got[0].ContentParts[0])
	}
}
