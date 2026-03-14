package tools

import (
	"context"
	"testing"
)

type captureAliasTool struct {
	args map[string]interface{}
}

func (t *captureAliasTool) Name() string        { return "capture" }
func (t *captureAliasTool) Description() string { return "capture" }
func (t *captureAliasTool) Parameters() map[string]interface{} {
	return map[string]interface{}{}
}
func (t *captureAliasTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	t.args = args
	return "ok", nil
}

func TestAliasToolExecuteDoesNotMutateCallerArgs(t *testing.T) {
	t.Parallel()

	base := &captureAliasTool{}
	tool := NewAliasTool("read", "", base, map[string]string{"file_path": "path"})
	original := map[string]interface{}{"file_path": "README.md"}
	if _, err := tool.Execute(context.Background(), original); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if _, ok := original["path"]; ok {
		t.Fatalf("caller args were mutated: %+v", original)
	}
	if got, _ := base.args["path"].(string); got != "README.md" {
		t.Fatalf("expected translated arg, got %+v", base.args)
	}
}
