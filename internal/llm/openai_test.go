package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAIComplete verifies request shaping (system prepended, bearer auth,
// model) and response parsing against a stub server — no real API call.
func TestOpenAIComplete(t *testing.T) {
	var got openAIReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("auth = %q", auth)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
	}))
	defer srv.Close()

	c := &OpenAI{apiKey: "test-key", model: "gpt-4o", baseURL: srv.URL, http: srv.Client()}
	out, err := c.Complete(context.Background(), "SYS", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" {
		t.Fatalf("content = %q", out)
	}
	if got.Model != "gpt-4o" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.ResponseFormat == nil || got.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %+v, want json_object", got.ResponseFormat)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[0].Content != "SYS" {
		t.Fatalf("system message not prepended: %+v", got.Messages)
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "hi" {
		t.Fatalf("user message wrong: %+v", got.Messages[1])
	}
}

func TestOpenAINoKey(t *testing.T) {
	c := NewOpenAI("", "gpt-4o", "")
	if _, err := c.Complete(context.Background(), "s", nil); err == nil {
		t.Fatal("expected error without api key")
	}
}
