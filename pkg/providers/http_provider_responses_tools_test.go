package providers

import "testing"

func TestBuildResponsesTools_IncludesFunctionAndBuiltinTools(t *testing.T) {
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:       "read_file",
				Parameters: map[string]interface{}{"type": "object"},
			},
		},
		{
			Type:       "web_search",
			Parameters: map[string]interface{}{"search_context_size": "high"},
		},
	}
	options := map[string]interface{}{
		"responses_tools": []interface{}{
			map[string]interface{}{
				"type":             "file_search",
				"vector_store_ids": []string{"vs_123"},
			},
		},
	}

	got := buildResponsesTools(tools, options)
	if len(got) != 3 {
		t.Fatalf("expected 3 tools, got %#v", got)
	}
	if got[0]["type"] != "function" || got[0]["name"] != "read_file" {
		t.Fatalf("expected function tool in first slot, got %#v", got[0])
	}
	if got[1]["type"] != "web_search" {
		t.Fatalf("expected web_search tool in second slot, got %#v", got[1])
	}
	if got[2]["type"] != "file_search" {
		t.Fatalf("expected file_search tool from options, got %#v", got[2])
	}
}

func TestResponsesMessageContent_SupportsImageAndFileByID(t *testing.T) {
	msg := Message{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "input_image", FileID: "file_img_1", Detail: "high"},
			{Type: "input_file", FileID: "file_doc_1", Filename: "doc.pdf"},
		},
	}

	content := responsesMessageContent(msg)
	if len(content) != 2 {
		t.Fatalf("expected two content items, got %#v", content)
	}
	if content[0]["type"] != "input_image" || content[0]["file_id"] != "file_img_1" {
		t.Fatalf("expected input_image by file_id, got %#v", content[0])
	}
	if content[1]["type"] != "input_file" || content[1]["file_id"] != "file_doc_1" {
		t.Fatalf("expected input_file by file_id, got %#v", content[1])
	}
}
