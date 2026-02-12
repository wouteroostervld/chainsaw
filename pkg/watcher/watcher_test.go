package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	w, err := New(nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer w.Close()

	if w.debounce != time.Second {
		t.Errorf("debounce = %v, want %v", w.debounce, time.Second)
	}
}

func TestWatch(t *testing.T) {
	tempDir := t.TempDir()

	w, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := w.Watch(tempDir); err != nil {
		t.Errorf("Watch failed: %v", err)
	}

	watched := w.Watched()
	if len(watched) != 1 {
		t.Errorf("expected 1 watched dir, got %d", len(watched))
	}

	// Watch same dir again - should be idempotent
	if err := w.Watch(tempDir); err != nil {
		t.Errorf("second Watch failed: %v", err)
	}

	if len(w.Watched()) != 1 {
		t.Error("watching same dir twice should not duplicate")
	}
}

func TestUnwatch(t *testing.T) {
	tempDir := t.TempDir()

	w, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Watch(tempDir)

	if err := w.Unwatch(tempDir); err != nil {
		t.Errorf("Unwatch failed: %v", err)
	}

	if len(w.Watched()) != 0 {
		t.Error("expected 0 watched dirs after unwatch")
	}
}

func TestFileChangeDetection(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")

	changes := make(chan string, 10)
	cfg := &Config{
		DebounceDelay: 50 * time.Millisecond,
		OnChange: func(path string) {
			changes <- path
		},
	}

	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := w.Watch(tempDir); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start watcher in background
	go w.Start(ctx)

	// Create file
	if err := os.WriteFile(testFile, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}

	// Wait for debounced event
	select {
	case path := <-changes:
		if path != testFile {
			t.Errorf("got change for %s, want %s", path, testFile)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for file change event")
	}
}

func TestDebounce(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")

	changes := make(chan string, 10)
	cfg := &Config{
		DebounceDelay: 100 * time.Millisecond,
		OnChange: func(path string) {
			changes <- path
		},
	}

	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Watch(tempDir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go w.Start(ctx)

	// Write multiple times rapidly
	for i := 0; i < 5; i++ {
		os.WriteFile(testFile, []byte(string(rune('a'+i))), 0600)
		time.Sleep(20 * time.Millisecond)
	}

	// Should only get ONE debounced event
	eventCount := 0
	timeout := time.After(300 * time.Millisecond)

loop:
	for {
		select {
		case <-changes:
			eventCount++
		case <-timeout:
			break loop
		}
	}

	if eventCount != 1 {
		t.Errorf("expected 1 debounced event, got %d", eventCount)
	}
}
