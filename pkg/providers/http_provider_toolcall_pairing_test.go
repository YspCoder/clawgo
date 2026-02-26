package providers

import "testing"

func TestToResponsesInputItemsWithState_DropsOrphanToolOutputs(t *testing.T) {
	pending := map[string]struct{}{}

	orphan := Message{Role: "tool", ToolCallID: "call-orphan", Content: "orphan output"}
	if got := toResponsesInputItemsWithState(orphan, pending); len(got) != 0 {
		t.Fatalf("expected orphan tool output to be dropped, got: %#v", got)
	}

	assistant := Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Name: "read",
			Arguments: map[string]interface{}{
				"path": "README.md",
			},
		}},
	}
	items := toResponsesInputItemsWithState(assistant, pending)
	if len(items) == 0 {
		t.Fatalf("assistant tool call should produce responses items")
	}
	if _, ok := pending["call-1"]; !ok {
		t.Fatalf("assistant tool call id should be tracked as pending")
	}

	matched := Message{Role: "tool", ToolCallID: "call-1", Content: "file content"}
	matchedItems := toResponsesInputItemsWithState(matched, pending)
	if len(matchedItems) != 1 {
		t.Fatalf("expected matched tool output item, got %#v", matchedItems)
	}
	if matchedItems[0]["type"] != "function_call_output" {
		t.Fatalf("expected function_call_output item, got %#v", matchedItems[0])
	}
	if _, ok := pending["call-1"]; ok {
		t.Fatalf("matched tool output should clear pending call id")
	}
}
