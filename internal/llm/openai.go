package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

type OpenAIClient struct {
	baseURL           string
	apiKey            string
	http              *http.Client
	maxRetries        int
	contextWindowCache map[string]int
}

func NewOpenAIClient(baseURL, apiKey string) *OpenAIClient {
	baseURL = strings.TrimRight(baseURL, "/")
	return &OpenAIClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		http:       &http.Client{Timeout: 5 * time.Minute},
		maxRetries: 3,
	}
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	MaxTokens      int             `json:"max_tokens"`
	Stream         bool            `json:"stream"`
	StreamOptions  *streamOptions  `json:"stream_options,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type responseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *jsonSchemaSpec `json:"json_schema,omitempty"`
}

type jsonSchemaSpec struct {
	Name   string          `json:"name"`
	Strict string          `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage,omitempty"`
}

func (c *OpenAIClient) buildMessages(system string, messages []Message) []chatMessage {
	msgs := make([]chatMessage, 0, len(messages)+1)
	if system != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: system})
	}
	for _, m := range messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	return msgs
}

func (c *OpenAIClient) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	url := c.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

func (c *OpenAIClient) Complete(ctx context.Context, model string, system string, messages []Message, maxTokens int, schema *JSONSchema) (*Response, error) {
	reqBody := chatRequest{
		Model:     model,
		Messages:  c.buildMessages(system, messages),
		MaxTokens: maxTokens,
		Stream:    false,
	}

	if schema != nil {
		reqBody.ResponseFormat = &responseFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchemaSpec{
				Name:   schema.Name,
				Strict: "true",
				Schema: schema.Schema,
			},
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	var resp *http.Response
	for attempt := range c.maxRetries {
		req, err := c.newRequest(ctx, body)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		resp, err = c.http.Do(req)
		if err == nil && resp.StatusCode < 500 {
			break
		}
		if resp != nil {
			resp.Body.Close()
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
		return nil, fmt.Errorf("LLM API call failed after %d retries: %w", c.maxRetries, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("LLM API call failed after %d retries: no response", c.maxRetries)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM API returned no choices")
	}

	return &Response{
		Content:      chatResp.Choices[0].Message.Content,
		Model:        chatResp.Model,
		InputTokens:  chatResp.Usage.PromptTokens,
		OutputTokens: chatResp.Usage.CompletionTokens,
		StopReason:   chatResp.Choices[0].FinishReason,
	}, nil
}

func (c *OpenAIClient) CompleteStream(ctx context.Context, model string, system string, messages []Message, maxTokens int) (<-chan StreamChunk, error) {
	reqBody := chatRequest{
		Model:         model,
		Messages:      c.buildMessages(system, messages),
		MaxTokens:     maxTokens,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := c.newRequest(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var lastUsage *StreamUsage

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				ch <- StreamChunk{Done: true, Usage: lastUsage}
				return
			}

			var chunk chatStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if chunk.Usage != nil {
				lastUsage = &StreamUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
				}
			}

			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				ch <- StreamChunk{Text: chunk.Choices[0].Delta.Content}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err}
		}
	}()

	return ch, nil
}

func (c *OpenAIClient) GetContextWindow(ctx context.Context, model string) (int, error) {
	if v, ok := c.contextWindowCache[model]; ok {
		return v, nil
	}

	url := c.baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("parsing models response: %w", err)
	}

	for _, m := range result.Data {
		if m.MaxModelLen > 0 {
			if c.contextWindowCache == nil {
				c.contextWindowCache = make(map[string]int)
			}
			c.contextWindowCache[m.ID] = m.MaxModelLen
		}
	}

	if v, ok := c.contextWindowCache[model]; ok {
		return v, nil
	}

	// If exact model name not found, return the first model's context window
	if len(result.Data) > 0 && result.Data[0].MaxModelLen > 0 {
		if c.contextWindowCache == nil {
			c.contextWindowCache = make(map[string]int)
		}
		c.contextWindowCache[model] = result.Data[0].MaxModelLen
		return result.Data[0].MaxModelLen, nil
	}

	return 0, fmt.Errorf("model %q not found in /v1/models response", model)
}
