// Package chat serves POST /api/chat: it generates a stakeholder reply and
// classifies the user's intent (spec §4). The disclose whitelist is enforced
// here in code, not by trusting the prompt.
package chat

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"girugi/internal/domain"
	"girugi/internal/httpx"
	"girugi/internal/jsonx"
	"girugi/internal/llm"
)

const (
	maxHistory     = 20  // keep only the most recent turns
	maxMessageRune = 300 // spec §4: message length cap
)

// PromptLoader loads a system prompt by name (path without .md).
type PromptLoader interface {
	Load(name string) (string, error)
}

// Handler serves POST /api/chat.
type Handler struct {
	LLM     llm.Client
	Prompts PromptLoader
}

type chatContext struct {
	Round           string `json:"round"`            // round1 | round2 | round3
	Choice          any    `json:"choice"`           // 1|2|3 or "confirm"|"revisit"
	CostAlternative string `json:"cost_alternative"` // round2 choice=3
	IntentSummary   string `json:"intent_summary"`   // round3 required
}

type request struct {
	Scenario string        `json:"scenario"`
	Persona  string        `json:"persona"`
	History  []llm.Message `json:"history"`
	Message  string        `json:"message"`
	Context  *chatContext  `json:"context"`
}

type response struct {
	Reply    string   `json:"reply"`
	Intent   string   `json:"intent"`
	Disclose []string `json:"disclose"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}

	if !domain.ValidScenario(req.Scenario) {
		httpx.WriteJSON(w, http.StatusBadRequest, errBody("invalid_scenario"))
		return
	}
	if !domain.ValidPersona(req.Scenario, req.Persona) {
		httpx.WriteJSON(w, http.StatusBadRequest, errBody("invalid_persona"))
		return
	}

	// cmf_outsourcing has no negotiation rounds, so context is ignored (spec §4).
	ctx := req.Context
	if req.Scenario == domain.ScenarioCMF {
		ctx = nil
	}

	msg := strings.TrimSpace(req.Message)
	if utf8.RuneCountInString(msg) > maxMessageRune {
		httpx.WriteJSON(w, http.StatusBadRequest, errBody("invalid_message"))
		return
	}
	// Without a round context the message carries the whole turn, so it must be
	// non-empty; with a context an empty message is allowed (e.g. round3).
	if ctx == nil && msg == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, errBody("invalid_message"))
		return
	}
	if ctx != nil && ctx.Round == "round3" && strings.TrimSpace(ctx.IntentSummary) == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, errBody("missing_intent_summary"))
		return
	}

	system, err := h.Prompts.Load(promptName(req.Scenario, req.Persona))
	if err != nil {
		slog.Error("prompt load failed", "scenario", req.Scenario, "persona", req.Persona, "err", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, errBody("internal"))
		return
	}

	// Keep only the most recent maxHistory turns (drop from the front).
	hist := req.History
	if len(hist) > maxHistory {
		hist = hist[len(hist)-maxHistory:]
	}
	msgs := append(append([]llm.Message{}, hist...), llm.Message{Role: "user", Content: buildUserTurn(msg, ctx)})

	raw, err := h.LLM.Complete(r.Context(), system, msgs)
	if err != nil {
		// Transport failure (timeout/5xx/missing key) — frontend shows a retry
		// button (spec §4). Do not log the conversation content.
		slog.Warn("llm unavailable", "scenario", req.Scenario, "persona", req.Persona, "err", err)
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":     "llm_unavailable",
			"retryable": true,
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, parseReply(req.Scenario, req.Persona, raw))
}

// promptName maps scenario+persona to the prompt file path (spec §0).
func promptName(scenario, persona string) string {
	if scenario == domain.ScenarioPrototype {
		return "s1_personas/" + persona
	}
	return "personas/" + persona
}

// buildUserTurn reproduces the exact JSON the AI part tuned against (spec §4).
// Empty fields are omitted; without a context the raw message is passed through.
func buildUserTurn(msg string, c *chatContext) string {
	if c == nil {
		return msg
	}
	m := map[string]any{"round": c.Round, "choice": c.Choice}
	if msg != "" {
		m["reasoning"] = msg
	}
	if c.CostAlternative != "" {
		m["cost_alternative"] = c.CostAlternative
	}
	if c.IntentSummary != "" {
		m["intent_summary"] = c.IntentSummary
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// parseReply parses the LLM output, falling back to raw text when the JSON is
// malformed or the required reply field is empty (spec §6 — never 500).
// disclose is filtered to the persona's whitelist and always emitted as an array.
func parseReply(scenario, persona, raw string) response {
	var out response
	extracted := jsonx.Extract(raw)
	if err := json.Unmarshal([]byte(extracted), &out); err != nil || strings.TrimSpace(out.Reply) == "" {
		slog.Warn("llm parse fallback", "scenario", scenario, "persona", persona)
		return response{Reply: fallbackReply(raw), Intent: "unknown", Disclose: []string{}}
	}
	out.Disclose = domain.FilterDisclose(scenario, persona, out.Disclose)
	if out.Disclose == nil {
		out.Disclose = []string{}
	}
	return out
}

// fallbackReply shows the model's text without fences/JSON noise so the chat can
// continue even when structured parsing fails.
func fallbackReply(raw string) string {
	return strings.TrimSpace(jsonx.StripFences(raw))
}

func errBody(code string) map[string]any {
	return map[string]any{"error": code}
}
