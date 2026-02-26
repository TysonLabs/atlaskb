package llm

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicClient struct {
	client anthropic.Client
	maxRetries int
}

func NewAnthropicClient(apiKey string) *AnthropicClient {
	return &AnthropicClient{
		client:     anthropic.NewClient(option.WithAPIKey(apiKey)),
		maxRetries: 3,
	}
}

func (c *AnthropicClient) Complete(ctx context.Context, model string, system string, messages []Message, maxTokens int) (*Response, error) {
	msgs := make([]anthropic.MessageParam, len(messages))
	for i, m := range messages {
		msgs[i] = anthropic.MessageParam{
			Role: anthropic.MessageParamRole(m.Role),
			Content: []anthropic.ContentBlockParamUnion{
				anthropic.NewTextBlock(m.Content),
			},
		}
	}

	var resp *anthropic.Message
	var err error

	for attempt := range c.maxRetries {
		resp, err = c.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: int64(maxTokens),
			System: []anthropic.TextBlockParam{
				{Text: system},
			},
			Messages: msgs,
		})
		if err == nil {
			break
		}

		if attempt < c.maxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed after %d retries: %w", c.maxRetries, err)
	}

	content := ""
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &Response{
		Content:      content,
		Model:        string(resp.Model),
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
		StopReason:   string(resp.StopReason),
	}, nil
}

func (c *AnthropicClient) CompleteStream(ctx context.Context, model string, system string, messages []Message, maxTokens int) (<-chan StreamChunk, error) {
	msgs := make([]anthropic.MessageParam, len(messages))
	for i, m := range messages {
		msgs[i] = anthropic.MessageParam{
			Role: anthropic.MessageParamRole(m.Role),
			Content: []anthropic.ContentBlockParamUnion{
				anthropic.NewTextBlock(m.Content),
			},
		}
	}

	stream := c.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: msgs,
	})

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)

		for stream.Next() {
			evt := stream.Current()
			switch evt.Type {
			case "content_block_delta":
				if evt.Delta.Type == "text_delta" {
					ch <- StreamChunk{Text: evt.Delta.Text}
				}
			case "message_stop":
				ch <- StreamChunk{Done: true}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- StreamChunk{Error: err}
		}
	}()

	return ch, nil
}
