package report

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girugi/internal/llm"
)

type fakePrompt struct{ err error }

func (f fakePrompt) Load(name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "SYSTEM:" + name, nil
}

func testFallback() *Fallback {
	return &Fallback{
		Sentences: map[string]string{
			"budget_never_asked":            "예산 미확인",
			"limit_sample_missing_in_draft": "한도견본 누락",
			"asked_capability_before_final": "사전 확인함",
		},
		S1Default: resS1{
			InitialDirection: "기본 방향",
			WorkJourney:      "기본 여정",
			KeyChanges:       "기본 변화",
			CompetencyRecord: "기본 역량",
			JobMeaning:       "기본 의미",
		},
	}
}

func do(h *Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestReportSession2(t *testing.T) {
	t.Run("parse success caps arrays", func(t *testing.T) {
		fake := &llm.Fake{Reply: `{"strengths":["a","b","c"],"cautions":["x"],"missed":["m1","m2","m3","m4"]}`}
		h := &Handler{LLM: fake, Prompts: fakePrompt{}, Fallback: testFallback()}
		rec := do(h, `{"scenario":"cmf_outsourcing"}`)
		if rec.Code != 200 {
			t.Fatalf("status %d", rec.Code)
		}
		var out resS2
		json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Strengths) != 2 || len(out.Cautions) != 1 || len(out.Missed) != 3 {
			t.Fatalf("caps wrong: %+v", out)
		}
	})

	t.Run("broken json falls back to code sentences", func(t *testing.T) {
		fake := &llm.Fake{Reply: "그냥 평문"}
		h := &Handler{LLM: fake, Prompts: fakePrompt{}, Fallback: testFallback()}
		body := `{"scenario":"cmf_outsourcing",
			"cautions_findings":[{"code":"limit_sample_missing_in_draft"}],
			"missed_findings":[{"code":"budget_never_asked"}]}`
		rec := do(h, body)
		var out resS2
		json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Cautions) != 1 || out.Cautions[0] != "한도견본 누락" {
			t.Fatalf("cautions fallback wrong: %+v", out)
		}
		if len(out.Missed) != 1 || out.Missed[0] != "예산 미확인" {
			t.Fatalf("missed fallback wrong: %+v", out)
		}
	})

	t.Run("llm error still 200 with fallback", func(t *testing.T) {
		fake := &llm.Fake{Err: errors.New("timeout")}
		h := &Handler{LLM: fake, Prompts: fakePrompt{}, Fallback: testFallback()}
		rec := do(h, `{"scenario":"cmf_outsourcing","strengths_findings":[{"code":"asked_capability_before_final"}]}`)
		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var out resS2
		json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Strengths) != 1 || out.Strengths[0] != "사전 확인함" {
			t.Fatalf("expected fallback strength, got %+v", out)
		}
	})

	t.Run("empty arrays are emitted as [] not null", func(t *testing.T) {
		fake := &llm.Fake{Reply: `{"strengths":[],"cautions":[],"missed":[]}`}
		h := &Handler{LLM: fake, Prompts: fakePrompt{}, Fallback: testFallback()}
		rec := do(h, `{"scenario":"cmf_outsourcing"}`)
		if b := rec.Body.String(); !strings.Contains(b, `"strengths":[]`) {
			t.Fatalf("expected empty array, got %s", b)
		}
	})
}

func TestReportSession1(t *testing.T) {
	t.Run("parse success returns five sections", func(t *testing.T) {
		fake := &llm.Fake{Reply: `{"initial_direction":"d","work_journey":"j","key_changes":"k","competency_record":"c","job_meaning":"m"}`}
		h := &Handler{LLM: fake, Prompts: fakePrompt{}, Fallback: testFallback()}
		rec := do(h, `{"scenario":"prototype_revision"}`)
		var out resS1
		json.Unmarshal(rec.Body.Bytes(), &out)
		if out.InitialDirection != "d" || out.JobMeaning != "m" {
			t.Fatalf("unexpected: %+v", out)
		}
	})

	t.Run("broken json falls back to _s1_default", func(t *testing.T) {
		fake := &llm.Fake{Reply: "깨진 응답"}
		h := &Handler{LLM: fake, Prompts: fakePrompt{}, Fallback: testFallback()}
		rec := do(h, `{"scenario":"prototype_revision"}`)
		var out resS1
		json.Unmarshal(rec.Body.Bytes(), &out)
		if out.InitialDirection != "기본 방향" {
			t.Fatalf("expected s1 default, got %+v", out)
		}
	})
}

func TestReportInvalidScenario(t *testing.T) {
	h := &Handler{LLM: &llm.Fake{}, Prompts: fakePrompt{}, Fallback: testFallback()}
	if rec := do(h, `{"scenario":"nope"}`); rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
