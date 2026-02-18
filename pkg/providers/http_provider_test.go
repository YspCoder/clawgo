package providers

import (
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestMapChatCompletionResponse_CompatFunctionCallXML(t *testing.T) {
	resp := mapChatCompletionResponse(&openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "stop",
				Message: openai.ChatCompletionMessage{
					Content: "I need to check the current state and understand what was last worked on before proceeding.\n\n<function_call><invoke><toolname>exec</toolname><parameters><command>cd /root/clawgo && git status</command></parameters></invoke></function_call>\n\n<function_call><invoke><tool_name>read_file</tool_name><parameters><path>/root/.clawgo/workspace/memory/MEMORY.md</path></parameters></invoke></function_call>",
				},
			},
		},
	})

	if resp == nil {
		t.Fatalf("expected response")
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}

	if resp.ToolCalls[0].Name != "exec" {
		t.Fatalf("expected first tool exec, got %q", resp.ToolCalls[0].Name)
	}
	if got, ok := resp.ToolCalls[0].Arguments["command"].(string); !ok || got == "" {
		t.Fatalf("expected first tool command arg, got %#v", resp.ToolCalls[0].Arguments)
	}

	if resp.ToolCalls[1].Name != "read_file" {
		t.Fatalf("expected second tool read_file, got %q", resp.ToolCalls[1].Name)
	}
	if got, ok := resp.ToolCalls[1].Arguments["path"].(string); !ok || got == "" {
		t.Fatalf("expected second tool path arg, got %#v", resp.ToolCalls[1].Arguments)
	}

	if resp.Content == "" {
		t.Fatalf("expected non-empty cleaned content")
	}
	if containsFunctionCallMarkup(resp.Content) {
		t.Fatalf("expected function call markup removed from content, got %q", resp.Content)
	}
}

func TestNormalizeAPIBase_CompatibilityPaths(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://localhost:8080/v1/chat/completions", "http://localhost:8080/v1"},
		{"http://localhost:8080/v1/chat", "http://localhost:8080/v1"},
		{"http://localhost:8080/v1/responses", "http://localhost:8080/v1"},
		{"http://localhost:8080/v1", "http://localhost:8080/v1"},
	}

	for _, tt := range tests {
		got := normalizeAPIBase(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeAPIBase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseCompatFunctionCalls_NoMarkup(t *testing.T) {
	calls, cleaned := parseCompatFunctionCalls("hello")
	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
	if cleaned != "hello" {
		t.Fatalf("expected content unchanged, got %q", cleaned)
	}
}

func containsFunctionCallMarkup(s string) bool {
	return len(s) > 0 && (strings.Contains(s, "<function_call>") || strings.Contains(s, "</function_call>"))
}
