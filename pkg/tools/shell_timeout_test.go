package tools

import (
	"testing"
	"time"
)

func TestIsHeavyCommand(t *testing.T) {
	tests := []struct {
		command string
		heavy   bool
	}{
		{command: "docker build -t app .", heavy: true},
		{command: "docker compose build api", heavy: true},
		{command: "go test ./...", heavy: true},
		{command: "npm run build", heavy: true},
		{command: "echo hello", heavy: false},
	}

	for _, tt := range tests {
		if got := isHeavyCommand(tt.command); got != tt.heavy {
			t.Fatalf("isHeavyCommand(%q)=%v want %v", tt.command, got, tt.heavy)
		}
	}
}

func TestCommandTickBase(t *testing.T) {
	light := (&ExecTool{}).commandTickBase("echo hello")
	heavy := (&ExecTool{}).commandTickBase("docker build -t app .")
	if heavy <= light {
		t.Fatalf("expected heavy command base tick > light, got heavy=%v light=%v", heavy, light)
	}
}

func TestNextCommandTick(t *testing.T) {
	base := 2 * time.Second
	t1 := nextCommandTick(base, 30*time.Second)
	t2 := nextCommandTick(base, 5*time.Minute)
	if t1 < base {
		t.Fatalf("tick should not shrink below base: %v", t1)
	}
	if t2 <= t1 {
		t.Fatalf("tick should grow with elapsed time: t1=%v t2=%v", t1, t2)
	}
	if t2 > 45*time.Second {
		t.Fatalf("tick should be capped, got %v", t2)
	}
}
