// Package sessionlog serves POST /api/session-log. There is no DB (CLAUDE.md
// rule 2): the whole payload is written to stdout as a structured log line.
// This is analytics, not a user feature, so it always returns 200 even on
// decode failure — a logging problem must never break the experience (spec §7).
package sessionlog

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"girugi/internal/httpx"
)

type request struct {
	SessionID string          `json:"session_id"`
	Scenario  string          `json:"scenario"`
	Payload   json.RawMessage `json:"payload"`
}

type Handler struct{}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("session-log decode failed", "err", err)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	slog.Info("session_complete",
		"session_id", req.SessionID,
		"scenario", req.Scenario,
		"payload", string(req.Payload),
	)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
