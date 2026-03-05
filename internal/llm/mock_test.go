package llm

import (
	"context"
	"errors"
	"testing"
)

func TestMockClientComplete_Default(t *testing.T) {
	m := &MockClient{}
	resp, err := m.Complete(context.Background(), "model-a", "", nil, 0, nil)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Model != "model-a" || resp.Content != "{}" || resp.StopReason != "end_turn" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestMockClientComplete_Custom(t *testing.T) {
	wantErr := errors.New("custom")
	m := &MockClient{
		CompleteFunc: func(_ context.Context, model string, system string, messages []Message, maxTokens int, schema *JSONSchema) (*Response, error) {
			if model != "m" || system != "s" || maxTokens != 11 {
				t.Fatalf("unexpected args model=%q system=%q maxTokens=%d", model, system, maxTokens)
			}
			return nil, wantErr
		},
	}
	_, err := m.Complete(context.Background(), "m", "s", nil, 11, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Complete() error = %v, want %v", err, wantErr)
	}
}

func TestMockClientCompleteStream_DefaultAndContextWindow(t *testing.T) {
	m := &MockClient{}
	ch, err := m.CompleteStream(context.Background(), "m", "", nil, 0)
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}
	first, ok := <-ch
	if !ok || first.Text == "" {
		t.Fatalf("expected first text chunk, got %+v (ok=%v)", first, ok)
	}
	last, ok := <-ch
	if !ok || !last.Done {
		t.Fatalf("expected done chunk, got %+v (ok=%v)", last, ok)
	}
	if _, ok := <-ch; ok {
		t.Fatal("expected closed stream channel")
	}

	got, err := m.GetContextWindow(context.Background(), "any")
	if err != nil {
		t.Fatalf("GetContextWindow() error = %v", err)
	}
	if got != 32768 {
		t.Fatalf("GetContextWindow() = %d, want 32768", got)
	}
}

func TestMockClientCompleteStream_Custom(t *testing.T) {
	wantErr := errors.New("stream err")
	m := &MockClient{
		CompleteStreamFunc: func(_ context.Context, model string, system string, messages []Message, maxTokens int) (<-chan StreamChunk, error) {
			if model != "m" {
				t.Fatalf("unexpected model: %q", model)
			}
			return nil, wantErr
		},
	}
	_, err := m.CompleteStream(context.Background(), "m", "", nil, 0)
	if !errors.Is(err, wantErr) {
		t.Fatalf("CompleteStream() error = %v, want %v", err, wantErr)
	}
}
