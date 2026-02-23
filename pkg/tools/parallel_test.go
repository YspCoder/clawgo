package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type basicTool struct {
	name    string
	execute func(ctx context.Context, args map[string]interface{}) (string, error)
}

func (t *basicTool) Name() string {
	return t.name
}

func (t *basicTool) Description() string {
	return "test tool"
}

func (t *basicTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

func (t *basicTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return t.execute(ctx, args)
}

type safeTool struct {
	*basicTool
}

func (t *safeTool) ParallelSafe() bool {
	return true
}

type concurrencyTool struct {
	name    string
	delay   time.Duration
	current int32
	max     int32
}

func (t *concurrencyTool) Name() string {
	return t.name
}

func (t *concurrencyTool) Description() string {
	return "concurrency test tool"
}

func (t *concurrencyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

func (t *concurrencyTool) ParallelSafe() bool {
	return true
}

func (t *concurrencyTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	current := atomic.AddInt32(&t.current, 1)
	for {
		max := atomic.LoadInt32(&t.max)
		if current <= max {
			break
		}
		if atomic.CompareAndSwapInt32(&t.max, max, current) {
			break
		}
	}
	time.Sleep(t.delay)
	atomic.AddInt32(&t.current, -1)
	return "ok", nil
}

type conflictTool struct {
	name      string
	delay     time.Duration
	mu        sync.Mutex
	active    map[string]bool
	conflicts int32
}

func (t *conflictTool) Name() string {
	return t.name
}

func (t *conflictTool) Description() string {
	return "resource conflict test tool"
}

func (t *conflictTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

func (t *conflictTool) ParallelSafe() bool {
	return true
}

func (t *conflictTool) ResourceKeys(args map[string]interface{}) []string {
	key, _ := args["key"].(string)
	if key == "" {
		return nil
	}
	return []string{key}
}

func (t *conflictTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	key, _ := args["key"].(string)
	if key == "" {
		return "", errors.New("missing key")
	}
	defer func() {
		t.mu.Lock()
		delete(t.active, key)
		t.mu.Unlock()
	}()

	t.mu.Lock()
	if t.active == nil {
		t.active = make(map[string]bool)
	}
	if t.active[key] {
		atomic.AddInt32(&t.conflicts, 1)
	}
	t.active[key] = true
	t.mu.Unlock()

	time.Sleep(t.delay)
	return "ok", nil
}

func TestParallelToolStableOrdering(t *testing.T) {
	registry := NewToolRegistry()
	tool := &safeTool{&basicTool{
		name: "echo",
		execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
			delay := 0 * time.Millisecond
			switch v := args["delay"].(type) {
			case int:
				delay = time.Duration(v) * time.Millisecond
			case float64:
				delay = time.Duration(v) * time.Millisecond
			}
			if delay > 0 {
				time.Sleep(delay)
			}
			value, _ := args["value"].(string)
			return value, nil
		},
	}}
	registry.Register(tool)

	parallel := NewParallelTool(registry, 3, nil)
	calls := []interface{}{
		map[string]interface{}{
			"tool":      "echo",
			"arguments": map[string]interface{}{"value": "first", "delay": 40},
			"id":        "first",
		},
		map[string]interface{}{
			"tool":      "echo",
			"arguments": map[string]interface{}{"value": "second", "delay": 10},
			"id":        "second",
		},
		map[string]interface{}{
			"tool":      "echo",
			"arguments": map[string]interface{}{"value": "third", "delay": 20},
			"id":        "third",
		},
	}

	output, err := parallel.Execute(context.Background(), map[string]interface{}{"calls": calls})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	firstIdx := strings.Index(output, "Result for first")
	secondIdx := strings.Index(output, "Result for second")
	thirdIdx := strings.Index(output, "Result for third")
	if firstIdx == -1 || secondIdx == -1 || thirdIdx == -1 {
		t.Fatalf("missing result markers in output: %s", output)
	}
	if !(firstIdx < secondIdx && secondIdx < thirdIdx) {
		t.Fatalf("results not in call order: %s", output)
	}
}

func TestParallelToolErrorFormatting(t *testing.T) {
	registry := NewToolRegistry()
	tool := &safeTool{&basicTool{
		name: "fail",
		execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "", errors.New("boom")
		},
	}}
	registry.Register(tool)

	parallel := NewParallelTool(registry, 2, nil)
	calls := []interface{}{
		map[string]interface{}{
			"tool":      "fail",
			"arguments": map[string]interface{}{},
			"id":        "err",
		},
	}

	output, err := parallel.Execute(context.Background(), map[string]interface{}{"calls": calls})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "Error: boom") {
		t.Fatalf("expected formatted error, got: %s", output)
	}
}

func TestParallelToolConcurrencyLimit(t *testing.T) {
	registry := NewToolRegistry()
	tool := &concurrencyTool{name: "sleep", delay: 25 * time.Millisecond}
	registry.Register(tool)

	parallel := NewParallelTool(registry, 2, nil)
	calls := make([]interface{}, 5)
	for i := 0; i < len(calls); i++ {
		calls[i] = map[string]interface{}{
			"tool":      "sleep",
			"arguments": map[string]interface{}{},
			"id":        "call",
		}
	}

	_, err := parallel.Execute(context.Background(), map[string]interface{}{"calls": calls})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if max := atomic.LoadInt32(&tool.max); max > 2 {
		t.Fatalf("expected max concurrency <= 2, got %d", max)
	}
}

func TestParallelToolResourceBatching(t *testing.T) {
	registry := NewToolRegistry()
	tool := &conflictTool{name: "resource", delay: 30 * time.Millisecond}
	registry.Register(tool)

	parallel := NewParallelTool(registry, 3, nil)
	calls := []interface{}{
		map[string]interface{}{
			"tool":      "resource",
			"arguments": map[string]interface{}{"key": "alpha"},
			"id":        "first",
		},
		map[string]interface{}{
			"tool":      "resource",
			"arguments": map[string]interface{}{"key": "beta"},
			"id":        "second",
		},
		map[string]interface{}{
			"tool":      "resource",
			"arguments": map[string]interface{}{"key": "alpha"},
			"id":        "third",
		},
	}

	_, err := parallel.Execute(context.Background(), map[string]interface{}{"calls": calls})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conflicts := atomic.LoadInt32(&tool.conflicts); conflicts > 0 {
		t.Fatalf("expected no resource conflicts, got %d", conflicts)
	}
}
