package embeddings

import (
	"context"
	"errors"
	"testing"
)

func TestMockClientEmbed_Default(t *testing.T) {
	m := &MockClient{}
	vecs, err := m.Embed(context.Background(), []string{"a", "b"}, "model")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("len(vecs) = %d, want 2", len(vecs))
	}
	if len(vecs[0]) != 1024 || len(vecs[1]) != 1024 {
		t.Fatalf("vector dimensions = %d,%d, want 1024,1024", len(vecs[0]), len(vecs[1]))
	}
}

func TestMockClientEmbed_Custom(t *testing.T) {
	wantErr := errors.New("boom")
	m := &MockClient{
		EmbedFunc: func(_ context.Context, texts []string, model string) ([][]float32, error) {
			if model != "m" || len(texts) != 1 || texts[0] != "x" {
				t.Fatalf("unexpected args: texts=%v model=%q", texts, model)
			}
			return nil, wantErr
		},
	}
	_, err := m.Embed(context.Background(), []string{"x"}, "m")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Embed() error = %v, want %v", err, wantErr)
	}
}
