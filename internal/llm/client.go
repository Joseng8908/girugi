// Package llm wraps the Anthropic Messages API behind a small interface so
// handlers can be tested with a Fake (see fake.go). We call the HTTP API
// directly with net/http — no SDK (CLAUDE.md rule 3).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message is one turn of the conversation.
type Message struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// Client produces an assistant completion for a system prompt + message list.
type Client interface {
	Complete(ctx context.Context, system string, msgs []Message) (string, error)
}

const (
	apiURL         = "https://api.anthropic.com/v1/messages"
	apiVersion     = "2023-06-01"
	requestTimeout = 20 * time.Second
	maxTokens      = 1024
	maxRespBytes   = 1 << 20 // guard against a runaway response body
)

// Anthropic is the production Client.
type Anthropic struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewAnthropic(apiKey, model string) *Anthropic {
	return &Anthropic{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{}, // per-attempt timeout comes from the request context
	}
}

type reqBody struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
}

type resBody struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// Complete calls the API with a 20s timeout per attempt and retries once
// immediately on failure (spec §3). A missing API key surfaces as an error,
// which the handler translates to 503.
func (a *Anthropic) Complete(ctx context.Context, system string, msgs []Message) (string, error) {
	if a.apiKey == "" {
		return "", fmt.Errorf("llm: ANTHROPIC_API_KEY not set")
	}
	body, err := json.Marshal(reqBody{
		Model:     a.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  msgs,
	})
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		text, err := a.call(ctx, body)
		if err == nil {
			return text, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("llm: complete failed: %w", lastErr)
}

func (a *Anthropic) call(ctx context.Context, body []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm: api status %d: %s", resp.StatusCode, raw)
	}

	var out resBody
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if len(out.Content) == 0 {
		return "", fmt.Errorf("llm: empty content")
	}
	return out.Content[0].Text, nil
}
