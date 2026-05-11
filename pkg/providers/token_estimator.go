package providers

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	estimateMessageOverheadTokens = 4
	estimateNameOverheadTokens    = 1
	estimateToolCallOverhead      = 8
	estimateToolDefOverhead       = 10
	estimateImageLowTokens        = 85
	estimateImageHighTokens       = 255
	estimateFileTokens            = 120
)

// EstimateOpenAICompatRequestTokens estimates prompt tokens for an OpenAI-compatible
// chat request without calling an upstream tokenizer. It intentionally errs a bit
// high for structured fields so compaction triggers before providers reject a prompt.
func EstimateOpenAICompatRequestTokens(body map[string]interface{}) (int, error) {
	if body == nil {
		return 1, nil
	}
	count := 0
	if model := strings.TrimSpace(asEstimateString(body["model"])); model != "" {
		count += estimateTextTokens(model)
	}
	count += estimateOpenAICompatMessages(body["messages"])
	count += estimateOpenAICompatTools(body["tools"])
	count += estimateReasoningTokens(body)
	count += estimateGenericOptions(body)
	if count < 1 {
		count = 1
	}
	return count, nil
}

// EstimatePromptTokens estimates tokens directly from provider-native message and
// tool structures. Providers with native count APIs should still prefer those.
func EstimatePromptTokens(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) int {
	body := map[string]interface{}{
		"model":    model,
		"messages": openAICompatMessages(messages),
	}
	if len(tools) > 0 {
		body["tools"] = openAICompatTools(tools)
	}
	for key, value := range options {
		body[key] = value
	}
	count, err := EstimateOpenAICompatRequestTokens(body)
	if err != nil {
		return 1
	}
	return count
}

func estimateOpenAICompatMessages(raw interface{}) int {
	messages, ok := raw.([]map[string]interface{})
	if !ok {
		if arr, ok := raw.([]interface{}); ok {
			total := 0
			for _, item := range arr {
				if msg, ok := item.(map[string]interface{}); ok {
					total += estimateOpenAICompatMessage(msg)
				}
			}
			return total
		}
		return estimateJSONTokens(raw)
	}
	total := 0
	for _, msg := range messages {
		total += estimateOpenAICompatMessage(msg)
	}
	return total
}

func estimateOpenAICompatMessage(msg map[string]interface{}) int {
	if msg == nil {
		return 0
	}
	total := estimateMessageOverheadTokens
	total += estimateTextTokens(asEstimateString(msg["role"]))
	if name := strings.TrimSpace(asEstimateString(msg["name"])); name != "" {
		total += estimateNameOverheadTokens + estimateTextTokens(name)
	}
	if toolCallID := strings.TrimSpace(asEstimateString(msg["tool_call_id"])); toolCallID != "" {
		total += estimateTextTokens(toolCallID)
	}
	total += estimateContentTokens(msg["content"])
	total += estimateToolCalls(msg["tool_calls"])
	return total
}

func estimateContentTokens(content interface{}) int {
	switch v := content.(type) {
	case nil:
		return 0
	case string:
		return estimateTextTokens(v)
	case []map[string]interface{}:
		total := 0
		for _, part := range v {
			total += estimateContentPartTokens(part)
		}
		return total
	case []interface{}:
		total := 0
		for _, raw := range v {
			if part, ok := raw.(map[string]interface{}); ok {
				total += estimateContentPartTokens(part)
				continue
			}
			total += estimateJSONTokens(raw)
		}
		return total
	default:
		return estimateJSONTokens(v)
	}
}

func estimateContentPartTokens(part map[string]interface{}) int {
	if part == nil {
		return 0
	}
	typ := strings.ToLower(strings.TrimSpace(asEstimateString(part["type"])))
	switch typ {
	case "text", "input_text":
		return estimateTextTokens(asEstimateString(part["text"]))
	case "image_url", "input_image":
		detail := strings.ToLower(strings.TrimSpace(asEstimateString(part["detail"])))
		if detail == "" {
			if image, ok := part["image_url"].(map[string]interface{}); ok {
				detail = strings.ToLower(strings.TrimSpace(asEstimateString(image["detail"])))
			}
		}
		if detail == "low" {
			return estimateImageLowTokens
		}
		return estimateImageHighTokens
	case "input_file", "file":
		return estimateFileTokens + estimateJSONTokens(part)
	default:
		if text := strings.TrimSpace(asEstimateString(part["text"])); text != "" {
			return estimateTextTokens(text)
		}
		return estimateJSONTokens(part)
	}
}

func estimateToolCalls(raw interface{}) int {
	calls, ok := raw.([]map[string]interface{})
	if !ok {
		if arr, ok := raw.([]interface{}); ok {
			total := 0
			for _, item := range arr {
				if call, ok := item.(map[string]interface{}); ok {
					total += estimateToolCall(call)
				}
			}
			return total
		}
		return 0
	}
	total := 0
	for _, call := range calls {
		total += estimateToolCall(call)
	}
	return total
}

func estimateToolCall(call map[string]interface{}) int {
	if call == nil {
		return 0
	}
	total := estimateToolCallOverhead
	total += estimateTextTokens(asEstimateString(call["id"]))
	total += estimateTextTokens(asEstimateString(call["type"]))
	if fn, ok := call["function"].(map[string]interface{}); ok {
		total += estimateTextTokens(asEstimateString(fn["name"]))
		total += estimateTextTokens(asEstimateString(fn["arguments"]))
	}
	total += estimateTextTokens(asEstimateString(call["name"]))
	total += estimateJSONTokens(call["arguments"])
	return total
}

func estimateOpenAICompatTools(raw interface{}) int {
	tools, ok := raw.([]map[string]interface{})
	if !ok {
		if arr, ok := raw.([]interface{}); ok {
			total := 0
			for _, item := range arr {
				if tool, ok := item.(map[string]interface{}); ok {
					total += estimateToolDefinition(tool)
				}
			}
			return total
		}
		return 0
	}
	total := 0
	for _, tool := range tools {
		total += estimateToolDefinition(tool)
	}
	return total
}

func estimateToolDefinition(tool map[string]interface{}) int {
	if tool == nil {
		return 0
	}
	total := estimateToolDefOverhead + estimateTextTokens(asEstimateString(tool["type"]))
	if fn, ok := tool["function"].(map[string]interface{}); ok {
		total += estimateTextTokens(asEstimateString(fn["name"]))
		total += estimateTextTokens(asEstimateString(fn["description"]))
		total += estimateJSONTokens(fn["parameters"])
		if strict, ok := fn["strict"]; ok {
			total += estimateJSONTokens(strict)
		}
		return total
	}
	total += estimateTextTokens(asEstimateString(tool["name"]))
	total += estimateTextTokens(asEstimateString(tool["description"]))
	total += estimateJSONTokens(tool["parameters"])
	return total
}

func estimateReasoningTokens(body map[string]interface{}) int {
	total := 0
	if effort := strings.ToLower(strings.TrimSpace(asEstimateString(body["reasoning_effort"]))); effort != "" {
		total += estimateTextTokens(effort)
		switch effort {
		case "minimal", "low":
			total += 32
		case "medium", "auto":
			total += 96
		case "high":
			total += 192
		}
	}
	for _, key := range []string{"reasoning", "chat_template_kwargs"} {
		if value, ok := body[key]; ok {
			total += estimateJSONTokens(value)
		}
	}
	return total
}

func estimateGenericOptions(body map[string]interface{}) int {
	total := 0
	for _, key := range []string{"tool_choice", "parallel_tool_calls", "response_format"} {
		if value, ok := body[key]; ok {
			total += estimateJSONTokens(value)
		}
	}
	return total
}

func estimateJSONTokens(value interface{}) int {
	if value == nil {
		return 0
	}
	data, err := json.Marshal(value)
	if err != nil {
		return estimateTextTokens(fmt.Sprintf("%v", value))
	}
	return estimateTextTokens(string(data))
}

func estimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	runes := utf8.RuneCountInString(text)
	ascii := 0
	han := 0
	other := 0
	for _, r := range text {
		switch {
		case r <= unicode.MaxASCII:
			ascii++
		case unicode.Is(unicode.Han, r):
			han++
		default:
			other++
		}
	}
	asciiTokens := int(math.Ceil(float64(ascii) / 4.0))
	otherTokens := int(math.Ceil(float64(other) / 2.0))
	total := asciiTokens + han + otherTokens
	if total < 1 && runes > 0 {
		return 1
	}
	return total
}

func asEstimateString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
