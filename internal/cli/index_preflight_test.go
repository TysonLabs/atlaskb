package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
)

func TestPreflightCheckSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/v1/embeddings":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := captureStdout(func() error {
		return preflightCheck(context.Background(), srv.URL, srv.URL, "embed-model", nil)
	})
	if err != nil {
		t.Fatalf("preflightCheck() error = %v", err)
	}
	if !strings.Contains(out, "Checking LLM service") || !strings.Contains(out, "Checking embedding service") {
		t.Fatalf("expected preflight output, got: %q", out)
	}
}

func TestPreflightCheckLLMFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			http.Error(w, "nope", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := preflightCheck(context.Background(), srv.URL, srv.URL, "embed-model", []string{"phase2"})
	if err == nil || !strings.Contains(err.Error(), "LLM service returned") {
		t.Fatalf("preflightCheck() error = %v, want LLM status error", err)
	}
}

func TestPreflightCheckEmbeddingFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			http.Error(w, "bad embeddings", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := preflightCheck(context.Background(), srv.URL, srv.URL, "embed-model", []string{"embedding"})
	if err == nil || !strings.Contains(err.Error(), "embedding service returned") {
		t.Fatalf("preflightCheck() error = %v, want embedding status error", err)
	}
}

func TestPreflightCheckSkipsForPhase1Only(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := preflightCheck(context.Background(), srv.URL, srv.URL, "embed-model", []string{"phase1"}); err != nil {
		t.Fatalf("preflightCheck() error = %v", err)
	}
	if hits != 0 {
		t.Fatalf("expected no preflight HTTP calls, got %d", hits)
	}
}

func TestRunIndexPreflightFailure(t *testing.T) {
	oldCfg := cfg
	oldDryRun := indexDryRun
	oldPhases := indexPhases
	oldIndexConcurrency := indexConcurrency
	t.Cleanup(func() {
		cfg = oldCfg
		indexDryRun = oldDryRun
		indexPhases = oldPhases
		indexConcurrency = oldIndexConcurrency
	})

	cfg = config.DefaultConfig()
	cfg.LLM.BaseURL = "http://127.0.0.1:1"
	cfg.Embeddings.BaseURL = "http://127.0.0.1:1"
	indexDryRun = false
	indexPhases = nil
	indexConcurrency = 0

	cmd := &cobra.Command{}
	err := runIndex(cmd, []string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "preflight failed") {
		t.Fatalf("runIndex() error = %v, want preflight failure", err)
	}
}
