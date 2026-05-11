package providers

import "testing"

func TestEstimatePromptTokensCountsMessagesToolsAndToolCalls(t *testing.T) {
	tools := []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionDefinition{
			Name:        "lookup_weather",
			Description: "Look up weather by city.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
				"required": []string{"city"},
			},
		},
	}}
	messages := []Message{
		{Role: "system", Content: "You are concise."},
		{Role: "user", Content: "北京天气怎么样"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: &FunctionCall{
					Name:      "lookup_weather",
					Arguments: `{"city":"北京"}`,
				},
			}},
		},
	}

	withoutTools := EstimatePromptTokens(messages, nil, "qwen-max", nil)
	withTools := EstimatePromptTokens(messages, tools, "qwen-max", map[string]interface{}{"reasoning_effort": "medium"})

	if withoutTools <= 0 {
		t.Fatalf("withoutTools = %d, want positive estimate", withoutTools)
	}
	if withTools <= withoutTools {
		t.Fatalf("withTools = %d, want > withoutTools %d", withTools, withoutTools)
	}
}

func TestEstimateOpenAICompatRequestTokensCountsMultimodalParts(t *testing.T) {
	base := NewHTTPProvider("openai", "token", "https://example.com/v1", "gpt-5", false, "api_key", 5, nil)
	textOnly := base.buildOpenAICompatChatRequest([]Message{{
		Role:    "user",
		Content: "look",
	}}, nil, "gpt-5", nil)
	withImage := base.buildOpenAICompatChatRequest([]Message{{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "input_text", Text: "look"},
			{Type: "input_image", ImageURL: "https://example.com/cat.png", Detail: "high"},
		},
	}}, nil, "gpt-5", nil)

	textCount, err := EstimateOpenAICompatRequestTokens(textOnly)
	if err != nil {
		t.Fatalf("text estimate error: %v", err)
	}
	imageCount, err := EstimateOpenAICompatRequestTokens(withImage)
	if err != nil {
		t.Fatalf("image estimate error: %v", err)
	}
	if imageCount < textCount+estimateImageHighTokens {
		t.Fatalf("imageCount = %d, textCount = %d, want image overhead", imageCount, textCount)
	}
}
