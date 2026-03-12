package providers

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeProviderDisablesThinkingWhenToolChoiceForced(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)
	body := p.requestBody([]Message{{Role: "user", Content: "hi"}}, []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:        "lookup",
			Description: "Lookup data",
			Parameters: map[string]interface{}{
				"type": "object",
			},
		},
	}}, "claude-sonnet", map[string]interface{}{
		"tool_choice": "any",
		"thinking": map[string]interface{}{
			"type": "enabled",
		},
	}, false)

	if _, ok := body["thinking"]; ok {
		t.Fatalf("expected thinking to be removed when tool_choice forces tool use, got %#v", body["thinking"])
	}
	toolChoice := mapFromAny(body["tool_choice"])
	if got := asString(toolChoice["type"]); got != "any" {
		t.Fatalf("expected tool_choice to remain any, got %#v", toolChoice)
	}
}

func TestClaudeToolChoiceSupportsRequiredAndFunctionForms(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)

	requiredBody := p.requestBody([]Message{{Role: "user", Content: "hi"}}, []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:       "lookup",
			Parameters: map[string]interface{}{"type": "object"},
		},
	}}, "claude-sonnet", map[string]interface{}{
		"tool_choice": "required",
	}, false)
	requiredChoice := mapFromAny(requiredBody["tool_choice"])
	if got := asString(requiredChoice["type"]); got != "any" {
		t.Fatalf("expected required -> any, got %#v", requiredChoice)
	}

	functionBody := p.requestBody([]Message{{Role: "user", Content: "hi"}}, []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:       "lookup",
			Parameters: map[string]interface{}{"type": "object"},
		},
	}}, "claude-sonnet", map[string]interface{}{
		"tool_choice": map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": "lookup",
			},
		},
	}, false)
	functionChoice := mapFromAny(functionBody["tool_choice"])
	if got := asString(functionChoice["type"]); got != "tool" || asString(functionChoice["name"]) != "lookup" {
		t.Fatalf("expected function choice -> tool lookup, got %#v", functionChoice)
	}

	mapRequiredBody := p.requestBody([]Message{{Role: "user", Content: "hi"}}, nil, "claude-sonnet", map[string]interface{}{
		"tool_choice": map[string]interface{}{"type": "required"},
	}, false)
	mapRequiredChoice := mapFromAny(mapRequiredBody["tool_choice"])
	if got := asString(mapRequiredChoice["type"]); got != "any" {
		t.Fatalf("expected map required -> any, got %#v", mapRequiredChoice)
	}

	noneBody := p.requestBody([]Message{{Role: "user", Content: "hi"}}, nil, "claude-sonnet", map[string]interface{}{
		"tool_choice": "none",
	}, false)
	if _, ok := noneBody["tool_choice"]; ok {
		t.Fatalf("expected string none tool_choice to be omitted, got %#v", noneBody["tool_choice"])
	}

	noneMapBody := p.requestBody([]Message{{Role: "user", Content: "hi"}}, nil, "claude-sonnet", map[string]interface{}{
		"tool_choice": map[string]interface{}{"type": "none"},
	}, false)
	if _, ok := noneMapBody["tool_choice"]; ok {
		t.Fatalf("expected none tool_choice to be omitted, got %#v", noneMapBody["tool_choice"])
	}
}

func TestReadClaudeBodyDecodesGzip(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(`{"ok":true}`)); err != nil {
		t.Fatalf("gzip write failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close failed: %v", err)
	}

	body, err := readClaudeBody(io.NopCloser(bytes.NewReader(compressed.Bytes())), "gzip")
	if err != nil {
		t.Fatalf("readClaudeBody failed: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("unexpected decoded body: %s", string(body))
	}
}

func TestClaudeCacheControlInjectionAndLimit(t *testing.T) {
	body := map[string]interface{}{
		"tools": []map[string]interface{}{
			{"name": "t1"},
			{"name": "t2"},
		},
		"system": []map[string]interface{}{
			{"type": "text", "text": "s1"},
			{"type": "text", "text": "s2"},
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": []map[string]interface{}{{"type": "text", "text": "u1"}}},
			{"role": "assistant", "content": []map[string]interface{}{{"type": "text", "text": "a1"}}},
			{"role": "user", "content": []map[string]interface{}{{"type": "text", "text": "u2"}}},
		},
	}
	body = ensureClaudeCacheControl(body)
	if _, ok := body["tools"].([]map[string]interface{})[1]["cache_control"]; !ok {
		t.Fatalf("expected last tool cache_control")
	}
	if _, ok := body["system"].([]map[string]interface{})[1]["cache_control"]; !ok {
		t.Fatalf("expected last system cache_control")
	}
	msgs := body["messages"].([]map[string]interface{})
	content := msgs[0]["content"].([]map[string]interface{})
	if _, ok := content[0]["cache_control"]; !ok {
		t.Fatalf("expected second-to-last user message cache_control")
	}

	blocks := claudeCacheBlocks(body)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 cache blocks, got %d", len(blocks))
	}
}

func TestClaudeNormalizeCacheControlTTL(t *testing.T) {
	body := map[string]interface{}{
		"tools": []map[string]interface{}{
			{"name": "t1", "cache_control": map[string]interface{}{"type": "ephemeral", "ttl": "1h"}},
			{"name": "t2", "cache_control": map[string]interface{}{"type": "ephemeral"}},
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": []map[string]interface{}{{"type": "text", "text": "u1", "cache_control": map[string]interface{}{"type": "ephemeral", "ttl": "1h"}}}},
		},
	}
	body = normalizeClaudeCacheControlTTL(body)
	tools := body["tools"].([]map[string]interface{})
	if got := asString(mapFromAny(tools[0]["cache_control"])["ttl"]); got != "1h" {
		t.Fatalf("expected first ttl preserved, got %q", got)
	}
	msgs := body["messages"].([]map[string]interface{})
	content := msgs[0]["content"].([]map[string]interface{})
	if _, ok := mapFromAny(content[0]["cache_control"])["ttl"]; ok {
		t.Fatalf("expected later ttl removed after default block")
	}
}

func TestClaudeToolPrefixHelpers(t *testing.T) {
	body := map[string]interface{}{
		"tools": []map[string]interface{}{
			{"type": "web_search_20250305", "name": "web_search"},
			{"name": "Read"},
		},
		"tool_choice": map[string]interface{}{"type": "tool", "name": "Read"},
		"messages": []map[string]interface{}{
			{"role": "assistant", "content": []map[string]interface{}{
				{"type": "tool_use", "name": "Read", "id": "t1", "input": map[string]interface{}{}},
				{"type": "tool_reference", "tool_name": "abc"},
			}},
			{"role": "user", "content": []map[string]interface{}{
				{"type": "tool_result", "tool_use_id": "t1", "content": []map[string]interface{}{
					{"type": "tool_reference", "tool_name": "nested"},
				}},
			}},
		},
	}
	prefixed := applyClaudeToolPrefixToBody(body, "proxy_")
	tools := prefixed["tools"].([]map[string]interface{})
	if got := asString(tools[0]["name"]); got != "web_search" {
		t.Fatalf("builtin tool should not be prefixed, got %q", got)
	}
	if got := asString(tools[1]["name"]); got != "proxy_Read" {
		t.Fatalf("custom tool should be prefixed, got %q", got)
	}
	toolChoice := mapFromAny(prefixed["tool_choice"])
	if got := asString(toolChoice["name"]); got != "proxy_Read" {
		t.Fatalf("tool_choice should be prefixed, got %q", got)
	}
	msgs := prefixed["messages"].([]map[string]interface{})
	assistantContent := msgs[0]["content"].([]map[string]interface{})
	if got := asString(assistantContent[0]["name"]); got != "proxy_Read" {
		t.Fatalf("tool_use should be prefixed, got %q", got)
	}
	if got := asString(assistantContent[1]["tool_name"]); got != "proxy_abc" {
		t.Fatalf("tool_reference should be prefixed, got %q", got)
	}
	userContent := msgs[1]["content"].([]map[string]interface{})
	nested := userContent[0]["content"].([]map[string]interface{})
	if got := asString(nested[0]["tool_name"]); got != "proxy_nested" {
		t.Fatalf("nested tool_reference should be prefixed, got %q", got)
	}

	raw := []byte(`{"content":[{"type":"tool_use","name":"proxy_Read"},{"type":"tool_reference","tool_name":"proxy_abc"},{"type":"tool_result","content":[{"type":"tool_reference","tool_name":"proxy_nested"}]}]}`)
	stripped := stripClaudeToolPrefixFromResponse(raw, "proxy_")
	if !bytes.Contains(stripped, []byte(`"name":"Read"`)) || !bytes.Contains(stripped, []byte(`"tool_name":"abc"`)) || !bytes.Contains(stripped, []byte(`"tool_name":"nested"`)) {
		t.Fatalf("expected stripped response, got %s", string(stripped))
	}

	line := []byte(`{"content_block":{"type":"tool_reference","tool_name":"proxy_abc"}}`)
	out := stripClaudeToolPrefixFromStreamLine(line, "proxy_")
	if !bytes.Contains(out, []byte(`"tool_name":"abc"`)) {
		t.Fatalf("expected stripped stream line, got %s", string(out))
	}

	sseLine := []byte(`data: {"content_block":{"type":"tool_reference","tool_name":"proxy_sse"}}`)
	sseOut := stripClaudeToolPrefixFromStreamLine(sseLine, "proxy_")
	if !bytes.HasPrefix(sseOut, []byte("data: ")) || !bytes.Contains(sseOut, []byte(`"tool_name":"sse"`)) {
		t.Fatalf("expected stripped SSE stream line, got %s", string(sseOut))
	}
}

func TestClaudeSystemBlocksAreEnriched(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)
	body := p.requestBody([]Message{
		{Role: "system", Content: "System one"},
		{Role: "developer", Content: "System two"},
		{Role: "user", Content: "hi"},
	}, nil, "claude-sonnet", nil, false)

	system, ok := body["system"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected system blocks array, got %#v", body["system"])
	}
	if len(system) < 4 {
		t.Fatalf("expected enriched system blocks, got %#v", system)
	}
	if got := asString(system[0]["text"]); !strings.HasPrefix(got, "x-anthropic-billing-header:") {
		t.Fatalf("expected billing header block, got %q", got)
	}
	if got := asString(system[1]["text"]); got != "You are a Claude agent, built on Anthropic's Claude Agent SDK." {
		t.Fatalf("expected agent block, got %q", got)
	}
	if got := asString(system[2]["text"]); got != "System one" {
		t.Fatalf("expected first user system block, got %q", got)
	}
	if got := asString(system[3]["text"]); got != "System two" {
		t.Fatalf("expected second user system block, got %q", got)
	}
}

func TestClaudeSystemBlocksIncludeContentPartsText(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)
	body := p.requestBody([]Message{
		{
			Role: "system",
			ContentParts: []MessageContentPart{
				{Type: "text", Text: "Alpha"},
				{Type: "text", Text: "Beta"},
			},
		},
		{Role: "user", Content: "hi"},
	}, nil, "claude-sonnet", nil, false)

	system := body["system"].([]map[string]interface{})
	if got := asString(system[2]["text"]); got != "Alpha\nBeta" {
		t.Fatalf("expected content parts joined into system text, got %q", got)
	}
}

func TestClaudeSystemBlocksSupportStrictMode(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)
	body := p.requestBody([]Message{
		{Role: "system", Content: "System one"},
		{Role: "developer", Content: "System two"},
		{Role: "user", Content: "hi"},
	}, nil, "claude-sonnet", map[string]interface{}{
		"claude_strict_system": true,
	}, false)

	system, ok := body["system"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected system blocks array, got %#v", body["system"])
	}
	if len(system) != 2 {
		t.Fatalf("expected strict mode to keep only billing+agent blocks, got %#v", system)
	}
	if got := asString(system[0]["text"]); !strings.HasPrefix(got, "x-anthropic-billing-header:") {
		t.Fatalf("expected billing header block, got %q", got)
	}
	if got := asString(system[1]["text"]); got != "You are a Claude agent, built on Anthropic's Claude Agent SDK." {
		t.Fatalf("expected agent block, got %q", got)
	}
}

func TestClaudeRequestBodyMapsImageAndFileContentParts(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)
	body := p.requestBody([]Message{{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "text", Text: "look"},
			{Type: "input_image", ImageURL: "data:image/png;base64,AAAA"},
			{Type: "input_image", ImageURL: "https://example.com/a.png"},
			{Type: "input_file", FileData: "data:application/pdf;base64,BBBB"},
		},
	}}, nil, "claude-sonnet", nil, false)

	msgs := body["messages"].([]map[string]interface{})
	content := msgs[0]["content"].([]map[string]interface{})
	if got := asString(content[0]["type"]); got != "text" || asString(content[0]["text"]) != "look" {
		t.Fatalf("expected text part preserved, got %#v", content[0])
	}
	imageBase64 := mapFromAny(content[1]["source"])
	if got := asString(content[1]["type"]); got != "image" {
		t.Fatalf("expected image part, got %#v", content[1])
	}
	if got := asString(imageBase64["type"]); got != "base64" || asString(imageBase64["media_type"]) != "image/png" || asString(imageBase64["data"]) != "AAAA" {
		t.Fatalf("expected base64 image source, got %#v", imageBase64)
	}
	imageURL := mapFromAny(content[2]["source"])
	if got := asString(imageURL["type"]); got != "url" || asString(imageURL["url"]) != "https://example.com/a.png" {
		t.Fatalf("expected url image source, got %#v", imageURL)
	}
	doc := mapFromAny(content[3]["source"])
	if got := asString(content[3]["type"]); got != "document" {
		t.Fatalf("expected document part, got %#v", content[3])
	}
	if got := asString(doc["type"]); got != "base64" || asString(doc["media_type"]) != "application/pdf" || asString(doc["data"]) != "BBBB" {
		t.Fatalf("expected base64 document source, got %#v", doc)
	}
}

func TestClaudeRequestBodyKeepsSingleTextAsString(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)

	body := p.requestBody([]Message{{Role: "user", Content: "hello"}}, nil, "claude-sonnet", nil, false)
	msgs := body["messages"].([]map[string]interface{})
	if got := msgs[0]["content"]; got != "hello" {
		t.Fatalf("expected plain string content, got %#v", got)
	}

	partsBody := p.requestBody([]Message{{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "text", Text: "hello"},
		},
	}}, nil, "claude-sonnet", nil, false)
	partsMsgs := partsBody["messages"].([]map[string]interface{})
	if got := partsMsgs[0]["content"]; got != "hello" {
		t.Fatalf("expected single text content part to collapse to string, got %#v", got)
	}

	assistantBody := p.requestBody([]Message{{Role: "assistant", Content: "done"}}, nil, "claude-sonnet", nil, false)
	assistantMsgs := assistantBody["messages"].([]map[string]interface{})
	if got := assistantMsgs[0]["content"]; got != "done" {
		t.Fatalf("expected assistant single text to collapse to string, got %#v", got)
	}

	assistantWithTool := p.requestBody([]Message{{
		Role:    "assistant",
		Content: "done",
		ToolCalls: []ToolCall{{
			ID:   "call_1",
			Name: "lookup",
			Function: &FunctionCall{
				Name:      "lookup",
				Arguments: `{"q":"x"}`,
			},
		}},
	}}, nil, "claude-sonnet", nil, false)
	assistantWithToolMsgs := assistantWithTool["messages"].([]map[string]interface{})
	if _, ok := assistantWithToolMsgs[0]["content"].([]map[string]interface{}); !ok {
		t.Fatalf("expected assistant content with tools to remain structured array, got %#v", assistantWithToolMsgs[0]["content"])
	}
}

func TestClaudeRequestBodyMapsToolResultContentParts(t *testing.T) {
	p := NewClaudeProvider("claude", "", "", "claude-sonnet", false, "oauth", 0, nil)
	body := p.requestBody([]Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Name: "lookup",
				Function: &FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"x"}`,
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			ContentParts: []MessageContentPart{
				{Type: "text", Text: "done"},
				{Type: "input_image", ImageURL: "data:image/png;base64,AAAA"},
				{Type: "input_file", FileData: "data:application/pdf;base64,BBBB"},
			},
		},
	}, nil, "claude-sonnet", nil, false)

	msgs := body["messages"].([]map[string]interface{})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %#v", msgs)
	}
	toolResult := msgs[1]["content"].([]map[string]interface{})[0]
	resultContent := mustMapSlice(t, toolResult["content"])
	if got := asString(resultContent[0]["type"]); got != "text" || asString(resultContent[0]["text"]) != "done" {
		t.Fatalf("expected text tool result part, got %#v", resultContent[0])
	}
	if got := asString(resultContent[1]["type"]); got != "image" {
		t.Fatalf("expected image tool result part, got %#v", resultContent[1])
	}
	if got := asString(resultContent[2]["type"]); got != "document" {
		t.Fatalf("expected document tool result part, got %#v", resultContent[2])
	}
}

func TestClaudeProviderCountTokens(t *testing.T) {
	var requestBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages/count_tokens" {
			t.Fatalf("expected /v1/messages/count_tokens, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("expected application/json accept header, got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"input_tokens":321}`))
	}))
	defer server.Close()

	p := NewClaudeProvider("claude", "sk-ant-oat-test", server.URL, "claude-sonnet", false, "bearer", 0, nil)
	usage, err := p.CountTokens(t.Context(), []Message{{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "text", Text: "count this"},
			{Type: "input_image", ImageURL: "data:image/png;base64,AAAA"},
		},
	}}, nil, "claude-sonnet", map[string]interface{}{
		"max_tokens": int64(128),
	})
	if err != nil {
		t.Fatalf("CountTokens error: %v", err)
	}
	if usage == nil || usage.PromptTokens != 321 || usage.TotalTokens != 321 || usage.CompletionTokens != 0 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
	if _, ok := requestBody["stream"]; ok {
		t.Fatalf("did not expect stream in count_tokens request: %#v", requestBody)
	}
	if _, ok := requestBody["max_tokens"]; ok {
		t.Fatalf("did not expect max_tokens in count_tokens request: %#v", requestBody)
	}
	msgs := mustMapSlice(t, requestBody["messages"])
	content := mustMapSlice(t, msgs[0]["content"])
	if got := asString(content[1]["type"]); got != "image" {
		t.Fatalf("expected image content in count_tokens request, got %#v", content[1])
	}
}

func TestApplyClaudeCompatHeadersUsesDynamicStainlessValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	applyClaudeCompatHeaders(req, authAttempt{kind: "oauth", token: "tok"}, false)
	if got := req.Header.Get("X-Stainless-Arch"); got != claudeStainlessArch() {
		t.Fatalf("expected dynamic arch %q, got %q", claudeStainlessArch(), got)
	}
	if got := req.Header.Get("X-Stainless-Os"); got != claudeStainlessOS() {
		t.Fatalf("expected dynamic os %q, got %q", claudeStainlessOS(), got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("expected bearer auth, got %q", got)
	}
	if req.Header.Get("x-api-key") != "" {
		t.Fatalf("did not expect x-api-key for oauth attempt")
	}
}

func TestApplyClaudeCompatHeadersUsesIdentityEncodingForStream(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	applyClaudeCompatHeaders(req, authAttempt{kind: "api_key", token: "tok"}, true)
	if got := req.Header.Get("Accept-Encoding"); got != "identity" {
		t.Fatalf("expected identity accept-encoding for stream, got %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "tok" {
		t.Fatalf("expected x-api-key for anthropic api key request, got %q", got)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("did not expect Authorization header for anthropic api_key request")
	}
}

func TestApplyClaudeBetaHeadersAddsContext1MWhenEnabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	applyClaudeBetaHeaders(req, map[string]interface{}{
		"claude_1m": true,
	}, []string{"custom-beta"})
	got := req.Header.Get("Anthropic-Beta")
	if !strings.Contains(got, "context-1m-2025-08-07") {
		t.Fatalf("expected context-1m beta, got %q", got)
	}
	if !strings.Contains(got, "custom-beta") {
		t.Fatalf("expected custom beta, got %q", got)
	}
}

func TestClaudeStreamStateMergesUsageAcrossEvents(t *testing.T) {
	state := &claudeStreamState{}
	state.consume([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":12}}}`))
	delta := state.consume([]byte(`{"type":"content_block_start","content_block":{"type":"text","text":"he"}}`))
	if delta != "he" {
		t.Fatalf("expected initial text delta, got %q", delta)
	}
	state.consume([]byte(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"llo"}}`))
	state.consume([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`))
	final := state.finalBody()

	resp, err := parseClaudeResponse(final)
	if err != nil {
		t.Fatalf("parse final body: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("expected merged content, got %q", resp.Content)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 7 || resp.Usage.TotalTokens != 19 {
		t.Fatalf("expected merged usage, got %#v", resp.Usage)
	}
	if resp.FinishReason != "end_turn" {
		t.Fatalf("expected finish reason, got %q", resp.FinishReason)
	}
}

func TestClaudeStreamStateMergesToolUseInputAcrossEvents(t *testing.T) {
	state := &claudeStreamState{}
	state.consume([]byte(`{"type":"content_block_start","content_block":{"type":"tool_use","id":"tool_1","name":"lookup","input":{"a":"b"}}}`))
	state.consume([]byte(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":",\"c\":1}"}}`))
	state.consume([]byte(`{"type":"content_block_stop"}`))
	state.consume([]byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`))

	final := state.finalBody()
	resp, err := parseClaudeResponse(final)
	if err != nil {
		t.Fatalf("parse final body: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("expected tool name lookup, got %#v", resp.ToolCalls[0])
	}
	if resp.ToolCalls[0].Function == nil || resp.ToolCalls[0].Function.Arguments != `{"a":"b","c":1}` {
		t.Fatalf("expected merged arguments, got %#v", resp.ToolCalls[0].Function)
	}
	if resp.FinishReason != "tool_use" {
		t.Fatalf("expected finish reason tool_use, got %q", resp.FinishReason)
	}
}

func TestClaudeStreamStateReadsMessageStartContent(t *testing.T) {
	state := &claudeStreamState{}
	state.consume([]byte(`{"type":"message_start","message":{"content":[{"type":"text","text":"hello"},{"type":"tool_use","id":"tool_1","name":"lookup","input":{"x":1}}],"usage":{"input_tokens":3}}}`))
	state.consume([]byte(`{"type":"message_stop"}`))

	resp, err := parseClaudeResponse(state.finalBody())
	if err != nil {
		t.Fatalf("parse final body: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("expected content from message_start, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("expected tool call from message_start, got %#v", resp.ToolCalls)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 3 {
		t.Fatalf("expected usage from message_start, got %#v", resp.Usage)
	}
}

func TestClaudeStreamStateDedupesMessageStartAndContentBlocks(t *testing.T) {
	state := &claudeStreamState{}
	state.consume([]byte(`{"type":"message_start","message":{"content":[{"type":"text","text":"hello"}]}}`))
	if delta := state.consume([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"he"}}`)); delta != "" {
		t.Fatalf("expected no duplicate delta from content_block_start, got %q", delta)
	}
	if delta := state.consume([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}`)); delta != "" {
		t.Fatalf("expected no duplicate delta from content_block_delta, got %q", delta)
	}
	state.consume([]byte(`{"type":"message_stop"}`))

	resp, err := parseClaudeResponse(state.finalBody())
	if err != nil {
		t.Fatalf("parse final body: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("expected deduped content hello, got %q", resp.Content)
	}
}

func TestClaudeStreamStatePreservesMessageStartToolUseAcrossDuplicateBlocks(t *testing.T) {
	state := &claudeStreamState{}
	state.consume([]byte(`{"type":"message_start","message":{"content":[{"type":"tool_use","id":"tool_1","name":"lookup","input":{"x":1}}]}}`))
	state.consume([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"lookup","input":{}}}`))
	state.consume([]byte(`{"type":"content_block_stop","index":0}`))
	state.consume([]byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`))

	resp, err := parseClaudeResponse(state.finalBody())
	if err != nil {
		t.Fatalf("parse final body: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Function == nil || resp.ToolCalls[0].Function.Arguments != `{"x":1}` {
		t.Fatalf("expected original tool arguments preserved, got %#v", resp.ToolCalls[0].Function)
	}
	if resp.FinishReason != "tool_use" {
		t.Fatalf("expected finish reason tool_use, got %q", resp.FinishReason)
	}
}

func TestClaudeExtractBetasFromPayload(t *testing.T) {
	payload := map[string]interface{}{
		"model": "claude-sonnet",
		"betas": []interface{}{"context-1m-2025-08-07", "custom-beta"},
	}
	betas, out := extractClaudeBetasFromPayload(payload)
	if len(betas) != 2 {
		t.Fatalf("expected 2 betas, got %#v", betas)
	}
	if _, ok := out["betas"]; ok {
		t.Fatalf("expected betas removed from payload, got %#v", out)
	}
}

func mustMapSlice(t *testing.T, value interface{}) []map[string]interface{} {
	t.Helper()
	switch typed := value.(type) {
	case []map[string]interface{}:
		return typed
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			obj := mapFromAny(item)
			if len(obj) > 0 {
				out = append(out, obj)
			}
		}
		return out
	default:
		t.Fatalf("expected map slice, got %#v", value)
		return nil
	}
}

func TestClaudeStainlessMappings(t *testing.T) {
	if runtime.GOOS == "darwin" && claudeStainlessOS() != "MacOS" {
		t.Fatalf("expected darwin -> MacOS, got %q", claudeStainlessOS())
	}
	if runtime.GOARCH == "amd64" && claudeStainlessArch() != "x64" {
		t.Fatalf("expected amd64 -> x64, got %q", claudeStainlessArch())
	}
}

func TestClaudeCacheControlLimitPreservesLastTool(t *testing.T) {
	body := map[string]interface{}{
		"tools": []map[string]interface{}{
			{"name": "t1", "cache_control": map[string]interface{}{"type": "ephemeral"}},
			{"name": "t2", "cache_control": map[string]interface{}{"type": "ephemeral"}},
		},
		"system": []map[string]interface{}{
			{"type": "text", "text": "s1", "cache_control": map[string]interface{}{"type": "ephemeral"}},
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": []map[string]interface{}{{"type": "text", "text": "u1", "cache_control": map[string]interface{}{"type": "ephemeral"}}}},
			{"role": "user", "content": []map[string]interface{}{{"type": "text", "text": "u2", "cache_control": map[string]interface{}{"type": "ephemeral"}}}},
		},
	}
	body = enforceClaudeCacheControlLimit(body, 4)
	tools := body["tools"].([]map[string]interface{})
	if _, ok := tools[0]["cache_control"]; ok {
		t.Fatalf("expected non-last tool cache_control removed first")
	}
	if _, ok := tools[1]["cache_control"]; !ok {
		t.Fatalf("expected last tool cache_control preserved")
	}
	if got := len(claudeCacheBlocks(body)); got != 4 {
		t.Fatalf("expected cache blocks capped at 4, got %d", got)
	}
}
