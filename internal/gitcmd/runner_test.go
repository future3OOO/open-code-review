package gitcmd

import (
	"context"
	"errors"
	"testing"
)

func TestCommandKilledByContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := errors.New("signal: killed")

	if !commandKilledByContext(ctx, err, -1) {
		t.Fatal("expected context-killed command to be classified")
	}
	if commandKilledByContext(ctx, err, 1) {
		t.Fatal("exit status 1 must not be masked as context cancellation")
	}
	if commandKilledByContext(context.Background(), err, -1) {
		t.Fatal("active context must not be classified as context-killed")
	}
	if commandKilledByContext(ctx, nil, -1) {
		t.Fatal("nil error must not be classified as context-killed")
	}
}
