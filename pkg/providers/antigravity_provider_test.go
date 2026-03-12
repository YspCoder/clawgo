package providers

import (
	"encoding/json"
	"testing"
)

func TestAntigravityBuildRequestBody(t *testing.T) {
	p := NewAntigravityProvider("openai", "", "", "gemini-2.5-pro", false, "oauth", 0, nil)
	body := p.buildRequestBody([]Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "calling tool",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Name: "lookup",
				Function: &FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"weather"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`},
	}, []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:        "lookup",
			Description: "Lookup data",
			Parameters: map[string]interface{}{
				"type": "object",
			},
		},
	}}, "gemini-2.5-pro", map[string]interface{}{
		"max_tokens":  256,
		"temperature": 0.2,
	}, &oauthSession{ProjectID: "demo-project"}, false)

	if got := body["project"]; got != "demo-project" {
		t.Fatalf("expected project id to be preserved, got %#v", got)
	}
	request := mapFromAny(body["request"])
	if system := asString(mapFromAny(request["systemInstruction"])["parts"].([]map[string]any)[0]["text"]); system != "You are helpful." {
		t.Fatalf("expected system instruction, got %q", system)
	}
	if got := len(request["contents"].([]map[string]any)); got != 3 {
		t.Fatalf("expected 3 content entries, got %d", got)
	}
	gen := mapFromAny(request["generationConfig"])
	if got := intValue(gen["maxOutputTokens"]); got != 256 {
		t.Fatalf("expected maxOutputTokens, got %#v", gen["maxOutputTokens"])
	}
	if got := gen["temperature"]; got != 0.2 {
		t.Fatalf("expected temperature, got %#v", got)
	}
}

func TestParseAntigravityResponse(t *testing.T) {
	raw := []byte(`{
		"response": {
			"candidates": [{
				"finishReason": "STOP",
				"content": {
					"parts": [
						{"text": "hello"},
						{"functionCall": {"id": "call_1", "name": "lookup", "args": {"q":"weather"}}}
					]
				}
			}],
			"usageMetadata": {
				"promptTokenCount": 11,
				"candidatesTokenCount": 7,
				"totalTokenCount": 18
			}
		}
	}`)
	resp, err := parseAntigravityResponse(raw)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("expected content, got %q", resp.Content)
	}
	if resp.FinishReason != "STOP" {
		t.Fatalf("expected finish reason passthrough, got %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("expected tool call, got %#v", resp.ToolCalls)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 18 {
		t.Fatalf("expected usage, got %#v", resp.Usage)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if got := asString(args["q"]); got != "weather" {
		t.Fatalf("expected tool args, got %#v", args)
	}
}
