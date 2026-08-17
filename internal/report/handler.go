// Package report serves POST /api/report: it turns pre-assigned findings into
// natural sentences (spec §5). The frontend already did the categorization;
// neither the server nor the LLM re-classifies. The report must always render,
// so every failure path falls back to fixed sentences and returns 200.
package report

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"girugi/internal/domain"
	"girugi/internal/httpx"
	"girugi/internal/jsonx"
	"girugi/internal/llm"
)

// PromptLoader loads a report prompt by name (path without .md).
type PromptLoader interface {
	Load(name string) (string, error)
}

// Handler serves POST /api/report.
type Handler struct {
	LLM      llm.Client
	Prompts  PromptLoader
	Fallback *Fallback
}

type Finding struct {
	Code     string         `json:"code"`
	Evidence map[string]any `json:"evidence"`
}

type ActionLog struct {
	Seq    int    `json:"seq"`
	T      int64  `json:"t"`
	Type   string `json:"type"`
	Actor  string `json:"actor,omitempty"`
	Target string `json:"target,omitempty"`
	Intent string `json:"intent,omitempty"`
}

type RoundEntry struct {
	Round     string `json:"round"`
	Choice    any    `json:"choice"`
	Reasoning string `json:"reasoning"`
}

type SelfEval struct {
	Q1 int    `json:"q1"`
	Q2 int    `json:"q2"`
	Q3 int    `json:"q3"`
	Q4 string `json:"q4"`
}

type request struct {
	Scenario          string      `json:"scenario"`
	StrengthsFindings []Finding   `json:"strengths_findings"`
	CautionsFindings  []Finding   `json:"cautions_findings"`
	MissedFindings    []Finding   `json:"missed_findings"`
	ActionLogs        []ActionLog `json:"action_logs"`

	// cmf_outsourcing
	Scores map[string]int `json:"scores"`
	Branch string         `json:"branch"`

	// prototype_revision
	InitialIntent      string       `json:"initial_intent"`
	FinalIntentSummary string       `json:"final_intent_summary"`
	RoundSummary       []RoundEntry `json:"round_summary"`
	SelfEval           *SelfEval    `json:"self_eval"`
}

// resS2 — session 2 (cmf_outsourcing): work overview + three sentence buckets +
// job meaning (wireframe sections 1/3/4/6; issue #14).
type resS2 struct {
	WorkOverview string   `json:"work_overview"`
	Strengths    []string `json:"strengths"`
	Cautions     []string `json:"cautions"`
	Missed       []string `json:"missed"`
	JobMeaning   string   `json:"job_meaning"`
}

// resS1 — session 1 (prototype_revision): five narrative sections.
type resS1 struct {
	InitialDirection string `json:"initial_direction"`
	WorkJourney      string `json:"work_journey"`
	KeyChanges       string `json:"key_changes"`
	CompetencyRecord string `json:"competency_record"`
	JobMeaning       string `json:"job_meaning"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}
	if !domain.ValidScenario(req.Scenario) {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_scenario"})
		return
	}

	// Unknown finding codes are logged but not rejected — the report must always
	// render (issue #10, BACKEND_SPEC §5).
	logUnknownFindings(req)

	if req.Scenario == domain.ScenarioPrototype {
		httpx.WriteJSON(w, http.StatusOK, h.completeS1(r, req))
		return
	}

	out := h.completeS2(r, req)
	out.Strengths = capN(out.Strengths, 2)
	out.Cautions = capN(out.Cautions, 2)
	out.Missed = capN(out.Missed, 3)
	httpx.WriteJSON(w, http.StatusOK, out)
}

// completeS2 renders the session-2 report, falling back to fixed sentences on
// any prompt/LLM/parse failure (always 200).
func (h *Handler) completeS2(r *http.Request, req request) resS2 {
	raw, ok := h.complete(r, "report", req)
	if !ok {
		return h.Fallback.forS2(req)
	}
	var out resS2
	if err := json.Unmarshal([]byte(jsonx.Extract(raw)), &out); err != nil {
		slog.Warn("report parse fallback", "scenario", req.Scenario)
		return h.Fallback.forS2(req)
	}
	// work_overview / job_meaning must never be blank (they are their own report
	// sections); backfill from the fixed defaults if the model omitted them.
	if out.WorkOverview == "" {
		out.WorkOverview = h.Fallback.S2Default.WorkOverview
	}
	if out.JobMeaning == "" {
		out.JobMeaning = h.Fallback.S2Default.JobMeaning
	}
	return out
}

// completeS1 renders the session-1 narrative report, falling back to the fixed
// _s1_default block when the LLM output is missing or unusable.
func (h *Handler) completeS1(r *http.Request, req request) resS1 {
	raw, ok := h.complete(r, "s1_report", req)
	if !ok {
		return h.Fallback.S1Default
	}
	var out resS1
	if err := json.Unmarshal([]byte(jsonx.Extract(raw)), &out); err != nil || out.InitialDirection == "" {
		slog.Warn("report parse fallback", "scenario", req.Scenario)
		return h.Fallback.S1Default
	}
	return out
}

// complete loads the prompt and calls the LLM. ok=false signals the caller to
// use its fallback.
func (h *Handler) complete(r *http.Request, promptName string, req request) (string, bool) {
	system, err := h.Prompts.Load(promptName)
	if err != nil {
		slog.Error("report prompt load failed", "prompt", promptName, "err", err)
		return "", false
	}
	raw, err := h.LLM.Complete(r.Context(), system, []llm.Message{{Role: "user", Content: mustJSON(req)}})
	if err != nil {
		slog.Warn("report llm unavailable, using fallback", "err", err)
		return "", false
	}
	return raw, true
}

func mustJSON(req request) string {
	b, _ := json.Marshal(req)
	return string(b)
}

// logUnknownFindings warns on finding codes not in the reference whitelist so a
// frontend typo / undefined code is not lost silently (issue #10).
func logUnknownFindings(req request) {
	check := func(bucket string, fs []Finding) {
		for _, f := range fs {
			if !domain.ValidFindingCode(req.Scenario, f.Code) {
				slog.Warn("report: unknown finding code (kept, not rejected)",
					"scenario", req.Scenario, "bucket", bucket, "code", f.Code)
			}
		}
	}
	check("strengths", req.StrengthsFindings)
	check("cautions", req.CautionsFindings)
	check("missed", req.MissedFindings)
}

func capN(s []string, n int) []string {
	if s == nil {
		return []string{}
	}
	if len(s) > n {
		return s[:n]
	}
	return s
}
