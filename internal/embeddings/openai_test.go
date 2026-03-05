package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbedClient_Embed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization header = %q, want Bearer test-key", got)
		}

		var req openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "embed-model" || len(req.Input) != 2 {
			t.Fatalf("unexpected request payload: %+v", req)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float32{0.1, 0.2}},
				{"index": 1, "embedding": []float32{0.3, 0.4}},
			},
			"usage": map[string]any{"total_tokens": 8},
		})
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "test-key")
	vecs, err := c.Embed(context.Background(), []string{"a", "b"}, "embed-model")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("Embed() len = %d, want 2", len(vecs))
	}
	if vecs[0][0] != 0.1 || vecs[1][1] != 0.4 {
		t.Fatalf("unexpected embeddings: %+v", vecs)
	}
}

func TestOpenAIEmbedClient_EmbedEmpty(t *testing.T) {
	c := NewOpenAIClient("http://example.com", "")
	vecs, err := c.Embed(context.Background(), nil, "embed-model")
	if err != nil {
		t.Fatalf("Embed(empty) error = %v", err)
	}
	if vecs != nil {
		t.Fatalf("Embed(empty) = %+v, want nil", vecs)
	}
}

func TestOpenAIEmbedClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "")
	_, err := c.Embed(context.Background(), []string{"x"}, "embed-model")
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("Embed() error = %v, want status 400 error", err)
	}
}
