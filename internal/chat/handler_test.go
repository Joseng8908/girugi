package chat

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"girugi/internal/llm"
)

// fakePrompt returns a fixed system prompt so tests never touch disk.
type fakePrompt struct{ err error }

func (f fakePrompt) Load(name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "SYSTEM:" + name, nil
}

func do(h *Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) response {
	t.Helper()
	var out response
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body)
	}
	return out
}

func TestChatHandler(t *testing.T) {
	tests := []struct {
		name       string
		fake       *llm.Fake
		body       string
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:       "valid json parses",
			fake:       &llm.Fake{Reply: `{"reply":"사내 시트지 가능합니다","intent":"manufacturing_capability","disclose":["inhouse_capability"]}`},
			body:       `{"scenario":"cmf_outsourcing","persona":"engineering","message":"목재 밴딩 사내 가능한가요?","context":null}`,
			wantStatus: 200,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				got := decode(t, rec)
				if got.Intent != "manufacturing_capability" || len(got.Disclose) != 1 || got.Disclose[0] != "inhouse_capability" {
					t.Fatalf("unexpected: %+v", got)
				}
			},
		},
		{
			name:       "code fence stripped",
			fake:       &llm.Fake{Reply: "```json\n{\"reply\":\"ok\",\"intent\":\"other\",\"disclose\":[]}\n```"},
			body:       `{"scenario":"cmf_outsourcing","persona":"engineering","message":"안녕하세요"}`,
			wantStatus: 200,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if got := decode(t, rec); got.Reply != "ok" || got.Intent != "other" {
					t.Fatalf("unexpected: %+v", got)
				}
			},
		},
		{
			name:       "plain-text prefix before json still parses",
			fake:       &llm.Fake{Reply: "네, 답변드리면 아래와 같습니다.\n{\"reply\":\"12일 여유가 있습니다\",\"intent\":\"cost_constraint\",\"disclose\":[]}"},
			body:       `{"scenario":"cmf_outsourcing","persona":"purchasing","message":"납기 여유 있나요?"}`,
			wantStatus: 200,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if got := decode(t, rec); got.Reply != "12일 여유가 있습니다" || got.Intent != "cost_constraint" {
					t.Fatalf("expected extracted json, got %+v", got)
				}
			},
		},
		{
			name:       "fully broken json falls back, still 200",
			fake:       &llm.Fake{Reply: "그냥 평문 답변입니다"},
			body:       `{"scenario":"cmf_outsourcing","persona":"engineering","message":"안녕하세요"}`,
			wantStatus: 200,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				got := decode(t, rec)
				if got.Intent != "unknown" || got.Reply != "그냥 평문 답변입니다" || got.Disclose == nil {
					t.Fatalf("expected fallback with empty array disclose, got %+v", got)
				}
			},
		},
		{
			name:       "disclose out-of-set key filtered",
			fake:       &llm.Fake{Reply: `{"reply":"x","intent":"other","disclose":["inhouse_capability","budget_limit","made_up"]}`},
			body:       `{"scenario":"cmf_outsourcing","persona":"engineering","message":"안녕하세요"}`,
			wantStatus: 200,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				got := decode(t, rec)
				if len(got.Disclose) != 1 || got.Disclose[0] != "inhouse_capability" {
					t.Fatalf("expected only inhouse_capability, got %+v", got.Disclose)
				}
			},
		},
		{
			name:       "cmf + design is 200",
			fake:       &llm.Fake{Reply: `{"reply":"x","intent":"concept_feedback","disclose":[]}`},
			body:       `{"scenario":"cmf_outsourcing","persona":"design","message":"오크 유지 가능한가요?"}`,
			wantStatus: 200,
		},
		{
			name:       "prototype + purchasing is 400",
			fake:       &llm.Fake{Reply: "x"},
			body:       `{"scenario":"prototype_revision","persona":"purchasing","message":"안녕하세요"}`,
			wantStatus: 400,
		},
		{
			name:       "invalid scenario is 400",
			fake:       &llm.Fake{Reply: "x"},
			body:       `{"scenario":"nope","persona":"engineering","message":"안녕하세요"}`,
			wantStatus: 400,
		},
		{
			name:       "message over 300 runes is 400",
			fake:       &llm.Fake{Reply: "x"},
			body:       `{"scenario":"cmf_outsourcing","persona":"engineering","message":"` + strings.Repeat("가", 301) + `"}`,
			wantStatus: 400,
		},
		{
			name:       "round3 without intent_summary is 400",
			fake:       &llm.Fake{Reply: "x"},
			body:       `{"scenario":"prototype_revision","persona":"engineering","message":"","context":{"round":"round3","choice":"confirm"}}`,
			wantStatus: 400,
		},
		{
			name:       "context with empty message is 200 (round3)",
			fake:       &llm.Fake{Reply: `{"reply":"확정되었습니다","intent":"round3_feedback","disclose":[]}`},
			body:       `{"scenario":"prototype_revision","persona":"engineering","message":"","context":{"round":"round3","choice":"confirm","intent_summary":"벨트라인 무드는 그래픽으로 유지"}}`,
			wantStatus: 200,
		},
		{
			name:       "llm error is 503 retryable",
			fake:       &llm.Fake{Err: errors.New("timeout")},
			body:       `{"scenario":"cmf_outsourcing","persona":"engineering","message":"안녕하세요"}`,
			wantStatus: 503,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var body map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body["error"] != "llm_unavailable" || body["retryable"] != true {
					t.Fatalf("unexpected body: %v", body)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{LLM: tt.fake, Prompts: fakePrompt{}}
			rec := do(h, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.check != nil {
				tt.check(t, rec)
			}
		})
	}
}

// TestBuildUserTurn asserts the assembled context JSON matches the AI part's
// verified format (spec §4). json.Marshal sorts map keys, so fields come out
// alphabetically — the AI prompts were tuned to tolerate this.
func TestBuildUserTurn(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		ctx  *chatContext
		want string
	}{
		{
			name: "nil context passes message through",
			msg:  "목재 밴딩 가능한가요?",
			ctx:  nil,
			want: "목재 밴딩 가능한가요?",
		},
		{
			name: "round1 with reasoning",
			msg:  "벨트라인을 살리면서 두 조각으로 줄이겠습니다",
			ctx:  &chatContext{Round: "round1", Choice: float64(1)},
			want: `{"choice":1,"reasoning":"벨트라인을 살리면서 두 조각으로 줄이겠습니다","round":"round1"}`,
		},
		{
			name: "round2 choice 3 with cost_alternative",
			msg:  "구배 조정",
			ctx:  &chatContext{Round: "round2", Choice: float64(3), CostAlternative: "포장재 등급을 낮춰 상쇄"},
			want: `{"choice":3,"cost_alternative":"포장재 등급을 낮춰 상쇄","reasoning":"구배 조정","round":"round2"}`,
		},
		{
			name: "round3 confirm, empty message omits reasoning",
			msg:  "",
			ctx:  &chatContext{Round: "round3", Choice: "confirm", IntentSummary: "벨트라인 무드는 그래픽으로 유지"},
			want: `{"choice":"confirm","intent_summary":"벨트라인 무드는 그래픽으로 유지","round":"round3"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildUserTurn(tt.msg, tt.ctx); got != tt.want {
				t.Fatalf("buildUserTurn = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestContextSentToLLM verifies the assembled JSON actually reaches the LLM as
// the last user turn (Fake arg inspection, spec §10).
func TestContextSentToLLM(t *testing.T) {
	fake := &llm.Fake{Reply: `{"reply":"ok","intent":"round1_feedback","disclose":[]}`}
	h := &Handler{LLM: fake, Prompts: fakePrompt{}}

	do(h, `{"scenario":"prototype_revision","persona":"engineering","message":"제조 단가 우선","context":{"round":"round1","choice":1}}`)

	if len(fake.Msgs) == 0 {
		t.Fatal("no messages sent")
	}
	last := fake.Msgs[len(fake.Msgs)-1]
	want := `{"choice":1,"reasoning":"제조 단가 우선","round":"round1"}`
	if last.Content != want {
		t.Fatalf("last turn = %s, want %s", last.Content, want)
	}
}

// TestHistoryTruncated verifies history is capped to the last 20 turns.
func TestHistoryTruncated(t *testing.T) {
	fake := &llm.Fake{Reply: `{"reply":"x","intent":"other","disclose":[]}`}
	h := &Handler{LLM: fake, Prompts: fakePrompt{}}

	var sb strings.Builder
	sb.WriteString(`{"scenario":"cmf_outsourcing","persona":"engineering","history":[`)
	for i := range 25 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"role":"user","content":"m"}`)
	}
	sb.WriteString(`],"message":"질문"}`)

	do(h, sb.String())

	if len(fake.Msgs) != maxHistory+1 {
		t.Fatalf("expected %d msgs, got %d", maxHistory+1, len(fake.Msgs))
	}
	if last := fake.Msgs[len(fake.Msgs)-1]; last.Content != "질문" {
		t.Fatalf("last msg = %q", last.Content)
	}
}

var _ llm.Client = (*llm.Fake)(nil)
