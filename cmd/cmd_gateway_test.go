package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigFileFingerprintSameContentIgnoresTouch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	first, err := readConfigFileFingerprint(path)
	if err != nil {
		t.Fatalf("read first fingerprint: %v", err)
	}

	target := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, target, target); err != nil {
		t.Fatalf("touch file: %v", err)
	}
	second, err := readConfigFileFingerprint(path)
	if err != nil {
		t.Fatalf("read second fingerprint: %v", err)
	}

	if first.sameContent(second) != true {
		t.Fatalf("expected touch to keep content fingerprint unchanged")
	}
	if first.ModUnixNano == second.ModUnixNano {
		t.Fatalf("expected touch to change mod time")
	}
}

func TestGatewayConfigWatcherReloadOnContentChangeWithDebounce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var reloadCalls atomic.Int32
	stop := startGatewayConfigWatcher(ctx, path, 150*time.Millisecond, 30*time.Millisecond, func() error {
		reloadCalls.Add(1)
		return nil
	})
	defer stop()

	time.Sleep(120 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"a":2}`), 0644); err != nil {
		t.Fatalf("write changed file #1: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"a":3}`), 0644); err != nil {
		t.Fatalf("write changed file #2: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"a":4}`), 0644); err != nil {
		t.Fatalf("write changed file #3: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for reloadCalls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := reloadCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one reload after debounced content changes, got %d", got)
	}

	time.Sleep(300 * time.Millisecond)
	if got := reloadCalls.Load(); got != 1 {
		t.Fatalf("expected no extra reload after debounce settles, got %d", got)
	}
}

func TestGatewayConfigWatcherTouchDoesNotReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var reloadCalls atomic.Int32
	stop := startGatewayConfigWatcher(ctx, path, 120*time.Millisecond, 30*time.Millisecond, func() error {
		reloadCalls.Add(1)
		return nil
	})
	defer stop()

	time.Sleep(120 * time.Millisecond)
	target := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, target, target); err != nil {
		t.Fatalf("touch file: %v", err)
	}

	time.Sleep(400 * time.Millisecond)
	if got := reloadCalls.Load(); got != 0 {
		t.Fatalf("expected touch-only update to skip reload, got %d", got)
	}
}
