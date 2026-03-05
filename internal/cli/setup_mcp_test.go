package cli

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
)

func withClosedStdin(t *testing.T, fn func()) {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = old
		_ = r.Close()
	}()
	fn()
}

func TestConfirmRetryEOF(t *testing.T) {
	withClosedStdin(t, func() {
		if confirmRetry() {
			t.Fatalf("confirmRetry() expected false when stdin is closed")
		}
	})
}

func TestRunSetupReturnsErrorOnNonInteractiveInput(t *testing.T) {
	oldCfg := cfg
	oldCfgPath := cfgPath
	t.Cleanup(func() {
		cfg = oldCfg
		cfgPath = oldCfgPath
	})

	cfg = config.DefaultConfig()
	cfgPath = "/tmp/atlaskb-test-config.toml"

	withClosedStdin(t, func() {
		err := runSetup(&cobra.Command{Use: "setup"}, nil)
		if err == nil {
			t.Fatalf("runSetup expected non-interactive error, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "form") {
			t.Fatalf("runSetup error should mention form failure, got: %v", err)
		}
	})
}

func TestRunMCPCanceledContext(t *testing.T) {
	oldCfg := cfg
	oldPool := pool
	t.Cleanup(func() {
		cfg = oldCfg
		pool = oldPool
	})
	cfg = config.DefaultConfig()
	pool = nil

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := &cobra.Command{Use: "mcp"}
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() {
		done <- runMCP(cmd, nil)
	}()

	select {
	case <-time.After(2 * time.Second):
		t.Fatalf("runMCP blocked with canceled context")
	case <-done:
		// Any return is acceptable here; the test asserts no blocking behavior.
	}
}
