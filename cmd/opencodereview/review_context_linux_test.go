package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLoadReviewContextRejectsFIFOPromptly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := loadReviewContext(path)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected FIFO review context to be rejected")
		}
		if !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("loadReviewContext blocked while opening a FIFO")
	}
}
