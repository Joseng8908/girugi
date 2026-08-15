package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const openAIURL = "https://api.openai.com/v1/chat/completions"

// OpenAI is a Client backed by the OpenAI Chat Completions API. The AI part's
// prompts are tuned on GPT-4o, so this is the default provider.
type OpenAI struct {
	apiKey  string
	model   string
	baseURL string // overridable in tests
	http    *http.Client
}

// NewOpenAI builds an OpenAI client. baseURL is for OpenAI-compatible endpoints
// (proxy/gateway given by the AI part); empty falls back to the official API.
func NewOpenAI(apiKey, model, baseURL string) *OpenAI {
	if baseURL == "" {
		baseURL = openAIURL
	}
	return &OpenAI{apiKey: apiKey, model: model, baseURL: baseURL, http: &http.Client{}}
}

type openAIReq struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

// responseFormat forces the model to emit a valid JSON object at the API level
// (json_object mode). This removes the "returns plain text" failure mode that no
// amount of prompt tuning fully fixes; all our prompts already demand JSON.
type responseFormat struct {
	Type string `json:"type"` // "json_object"
}

type openAIRes struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete calls the Chat Completions API. Unlike Anthropic, OpenAI has no
// separate system field — the system prompt is prepended as a role:"system"
// message. 20s timeout per attempt, one immediate retry (spec §3).
func (o *OpenAI) Complete(ctx context.Context, system string, msgs []Message) (string, error) {
	if o.apiKey == "" {
		return "", fmt.Errorf("llm: OPENAI_API_KEY not set")
	}
	all := make([]Message, 0, len(msgs)+1)
	if system != "" {
		all = append(all, Message{Role: "system", Content: system})
	}
	all = append(all, msgs...)

	body, err := json.Marshal(openAIReq{
		Model:          o.model,
		Messages:       all,
		MaxTokens:      maxTokens,
		ResponseFormat: &responseFormat{Type: "json_object"},
	})
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}

	var lastErr error
	for range 2 { // one immediate retry
		text, err := o.call(ctx, body)
		if err == nil {
			return text, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("llm: complete failed: %w", lastErr)
}

func (o *OpenAI) call(ctx context.Context, body []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(req)
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

	var out openAIRes
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: empty choices")
	}
	return out.Choices[0].Message.Content, nil
}
