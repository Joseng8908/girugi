# 백엔드 구현 명세 v4

Claude Code 작업 지시서. `CLAUDE.md` 규칙을 먼저 읽을 것.
프론트와의 계약은 `API.md` 가 원본이며, 이 문서와 어긋나면 `API.md` 를 따른다.
AI 파트 프롬프트의 실제 계약은 `AI_INTERFACE_SPEC.md` 를 참조한다.

> **v3 → v4** (AI 인터페이스 스펙 반영)
> 1. intent enum을 페르소나별 실제 값으로 교체 — 세션1 `roundN_feedback` 계열 추가
> 2. 세션1 disclose 키 8개 확정 (더 이상 TODO 아님)
> 3. `context` 에 `cost_alternative` · `intent_summary` 추가
> 4. **`context` 조립 형식을 AI 파트 검증 형식(JSON 문자열)으로 확정** — 프롬프트 수정 불필요
> 5. finding 3배열 분리를 **세션1에도 적용** (문제가 관찰된 곳이 세션1이었음)
> 6. 세션1 `self_eval` 을 `{q1,q2,q3,q4}` 로 정정

---

## 0. 시나리오

| `scenario` | 관계자 | 리포트 | 프롬프트 경로 |
|---|---|---|---|
| `cmf_outsourcing` | `senior` `engineering` `purchasing` `design` | 3분류 | `prompts/personas/` · `prompts/report.md` |
| `prototype_revision` | `senior` `engineering` `design` | 5섹션 서술형 | `prompts/s1_personas/` · `prompts/s1_report.md` |

---

## 1. 엔드포인트

| 엔드포인트 | 역할 | LLM |
|---|---|---|
| `POST /api/chat` | 관계자 답변 생성 + 의도 분류 | O |
| `POST /api/report` | 배정된 finding을 문장으로 변환 | O |
| `POST /api/session-log` | stdout JSON 출력 | X |
| `GET /healthz` | 헬스체크 | X |

stateless. **DB 없음. 외부 의존성은 LLM API 하나. Go 표준 라이브러리만 사용.**

---

## 2. 디렉토리 구조

```
.
├── CLAUDE.md · API.md · go.mod · Dockerfile
├── cmd/server/main.go
├── internal/
│   ├── config/
│   ├── domain/          시나리오·페르소나·disclose 화이트리스트
│   ├── httpx/           CORS · 로깅 · panic 복구 · 바디 제한 · JSON 헬퍼
│   ├── jsonx/           extractJSONObject · stripFences
│   ├── llm/             client.go(interface + Anthropic) · fake.go
│   ├── prompt/          prompts/ 로더 (기동 시 1회)
│   ├── chat/
│   └── report/          handler.go · fallback.go
├── prompts/
│   ├── personas/        senior · engineering · purchasing · design
│   ├── s1_personas/     senior · engineering · design
│   ├── report.md · s1_report.md · fallback.json
└── scenarios/*.json     ※ 프론트 참조용. 백엔드는 읽지 않음
```

`prompts/` 는 AI·기획 담당 영역. 파일 수정에 코드 변경이 필요 없어야 한다.

---

## 3. 도메인 상수

```go
package domain

const (
    ScenarioCMF       = "cmf_outsourcing"
    ScenarioPrototype = "prototype_revision"
)

const (
    PersonaSenior      = "senior"
    PersonaEngineering = "engineering"
    PersonaPurchasing  = "purchasing"
    PersonaDesign      = "design"
)

var ScenarioPersonas = map[string][]string{
    ScenarioCMF:       {PersonaSenior, PersonaEngineering, PersonaPurchasing, PersonaDesign},
    ScenarioPrototype: {PersonaSenior, PersonaEngineering, PersonaDesign},
}

// [scenario][persona] → 허용 disclose 키
var DiscloseKeys = map[string]map[string][]string{
    ScenarioCMF: {
        PersonaSenior:      {"spec_format", "limit_sample", "vendor_criteria", "product_concept", "design_direction", "cmf_spec"},
        PersonaEngineering: {"inhouse_capability", "sheet_lead_time"},
        PersonaPurchasing:  {"budget_limit", "cost_impact", "part_cost_share"},
        PersonaDesign:      {"wood_material_intent", "concept_consistency_criteria",
                             "cmf_alternative_range"},
    },
    ScenarioPrototype: {
        PersonaEngineering: {"molding_cost_constraint", "part_consolidation_criteria",
                             "undercut_constraint", "min_thickness"},
        PersonaSenior:      {"belt_line_intent", "revision_guidance_criteria"},
        PersonaDesign:      {"user_requirement_keywords", "concept_direction"},
    },
}
```

**화이트리스트 필터링은 반드시 코드에서 수행한다.** 프롬프트 준수만 믿지 않는다.

---

## 4. `POST /api/chat`

```go
type ChatReq struct {
    Scenario string        `json:"scenario"`
    Persona  string        `json:"persona"`
    History  []llm.Message `json:"history"`
    Message  string        `json:"message"`
    Context  *ChatContext  `json:"context"`
}

type ChatContext struct {
    Round           string `json:"round"`             // round1 | round2 | round3
    Choice          any    `json:"choice"`            // 1|2|3 또는 "confirm"|"revisit"
    CostAlternative string `json:"cost_alternative"`  // round2 choice=3
    IntentSummary   string `json:"intent_summary"`    // round3 필수
}
```

### 검증

- `scenario` 미등록 → 400
- `persona` 가 `ScenarioPersonas[scenario]` 에 없음 → 400
- `context == nil` 이고 `message` 가 비었거나 300자 초과 → 400
- `context != nil` 이고 `message` 300자 초과 → 400 (빈 값 허용)
- `context.round == "round3"` 인데 `intent_summary` 비었음 → 400
- `scenario == cmf_outsourcing` 이면 `context` 무시

### ★ context 조립 — AI 파트 검증 형식 그대로

AI 파트는 아래 **JSON 문자열을 user 메시지로 넣는 전제**로 프롬프트를 튜닝하고
테스트를 통과시켰다. 서버는 그 형식을 그대로 재현한다. **프롬프트 수정 불필요.**

```go
// context != nil 일 때 user turn content
{"round":"round1","choice":1,"reasoning":"벨트라인을 살리면서 두 조각으로 줄이겠습니다"}
{"round":"round2","choice":3,"reasoning":"...","cost_alternative":"포장재 등급을 낮춰 상쇄"}
{"round":"round3","choice":"confirm","intent_summary":"벨트라인 무드는 그래픽으로 유지"}
```

- `reasoning` 에 `req.Message` 를 넣는다
- 빈 문자열 필드는 **생략**한다 (round3의 `reasoning` 등)
- `context == nil` 이면 `req.Message` 원문 그대로

```go
func buildUserTurn(msg string, c *ChatContext) string {
    if c == nil { return msg }
    m := map[string]any{"round": c.Round, "choice": c.Choice}
    if msg != "" { m["reasoning"] = msg }
    if c.CostAlternative != "" { m["cost_alternative"] = c.CostAlternative }
    if c.IntentSummary != "" { m["intent_summary"] = c.IntentSummary }
    b, _ := json.Marshal(m)
    return string(b)
}
```

### 처리 순서

```
검증 → history 뒤에서 20개 → buildUserTurn → 시스템 프롬프트 로드(§0 경로)
     → LLM 호출(20s, 재시도 1회) → 파싱+폴백(§6) → disclose 필터 → 200
```

### 응답

```go
type ChatRes struct {
    Reply    string   `json:"reply"`
    Intent   string   `json:"intent"`
    Disclose []string `json:"disclose"`
}
```

`intent` 는 서버가 검증하지 않고 그대로 통과시킨다 (enum은 `API.md` 참조).
파싱 폴백 시에만 `"unknown"` 으로 채운다.

### 에러

| 코드 | 상황 |
|---|---|
| 400 | 검증 실패 |
| 503 | LLM 호출 실패 → `{"error":"llm_unavailable","retryable":true}` |

**파싱 실패는 500이 아니라 200 + 폴백.**

---

## 5. `POST /api/report`

**카테고리 분류는 프론트에서 끝나 있다. 서버·LLM은 재분류하지 않는다.**

AI 파트 검증에서 `agreement_confirmed`(strengths) + `core_intent_abandoned`(cautions)
처럼 상충하는 finding이 함께 오면 LLM이 표를 무시하고 재분류하거나 finding을
누락시키는 문제가 재현됐다. 그래서 배열을 나눠 받는다. **세션1·2 모두 적용.**

```go
type ReportReq struct {
    Scenario          string      `json:"scenario"`
    StrengthsFindings []Finding   `json:"strengths_findings"`
    CautionsFindings  []Finding   `json:"cautions_findings"`
    MissedFindings    []Finding   `json:"missed_findings"`
    ActionLogs        []ActionLog `json:"action_logs"`

    // cmf_outsourcing
    Scores map[string]int `json:"scores"`
    Branch string         `json:"branch"`   // wood_dropped|sheet_wrap|outsourcing

    // prototype_revision
    InitialIntent      string       `json:"initial_intent"`
    FinalIntentSummary string       `json:"final_intent_summary"`
    RoundSummary       []RoundEntry `json:"round_summary"`
    SelfEval           *SelfEval    `json:"self_eval"`
}

type Finding struct {
    Code     string         `json:"code"`
    Evidence map[string]any `json:"evidence"`
}

type SelfEval struct {
    Q1 int    `json:"q1"`   // 흥미도 1~5
    Q2 int    `json:"q2"`   // 예상과의 차이 1~5
    Q3 int    `json:"q3"`   // 반복 수행 의향 1~5
    Q4 string `json:"q4"`   // 부담 지점, 서술형(선택)
}
```

### 응답

**세션2** — `{"work_overview","strengths":[],"cautions":[],"missed":[],"job_meaning"}`
strengths/cautions/missed 상한 2/2/3, 문장 60자 이내, 숫자 미포함.
`work_overview`(①)·`job_meaning`(⑥)은 서술 문자열, 항상 채움(빈 값이면 `_s2_default`로 백필). 이슈 #14.

**세션1** — `{"initial_direction","work_journey","key_changes","competency_record","job_meaning"}`
각 2~4문장 서술형, 점수·등급 미포함(라운드 서수는 허용).

### 폴백

`prompts/fallback.json` 에서 finding code별 고정 문장을 조회해 반환.

```json
{
  "budget_never_asked": "목표 예산을 확인하지 않아 비용 관점의 검토가 빠졌습니다.",
  "core_intent_abandoned": "최종 의도 요약에서 처음의 벨트라인 방향이 드러나지 않았습니다.",
  "_s1_default": {
    "initial_direction": "...", "work_journey": "...", "key_changes": "...",
    "competency_record": "...", "job_meaning": "..."
  }
}
```

세션1은 finding 문장을 조합할 수 없으므로 `_s1_default` 를 그대로 반환한다.
**리포트는 마지막 화면이므로 절대 비어서는 안 된다.**

> **finding code / intent 검증 정책 (issue #10).**
> - **finding code**: 정의되지 않은 code(= `domain.FindingCodes` 및 `fallback.json` 에 없음)는
>   400이 아니라 **`slog.Warn` 로깅 후 스킵**한다. 리포트는 항상 렌더링돼야 하므로 계약 위반
>   때문에 화면을 비우지 않는다. 참조용 화이트리스트는 `domain.FindingCodes`(API.md 기준).
> - **intent**: 서버는 검증하지 않고 그대로 통과시킨다(§4). 참고용이며 폴백 시에만 `"unknown"`.
>   이는 누락이 아니라 **의도된 설계**다. 참조 enum은 `domain.IntentValues`.

---

## 6. JSON 파싱 — `internal/jsonx`

```go
func Extract(raw string) string {
    s := stripFences(raw)        // ```json ... ``` 제거
    s = trimBeforeFirstBrace(s)  // 첫 '{' 이전 평문 제거   ★
    s = trimAfterLastBrace(s)    // 마지막 '}' 이후 제거
    return s
}
```

`trimBeforeFirstBrace` 가 핵심이다. AI 파트 검증에서 **"JSON 앞에 평문 답변을
중복 출력"** 하는 실패 유형이 반복 관찰됐고, 코드펜스 제거만으로는 못 잡는다.

파싱에 성공해도 필수 필드(`reply` 등)가 비어 있으면 폴백으로 간주한다.

---

## 7. `POST /api/session-log`

```go
slog.Info("session_complete", "session_id", req.SessionID,
    "scenario", req.Scenario, "payload", json.RawMessage(rawPayload))
```

payload를 검증하지 않는다. 실패해도 200을 반환한다.

---

## 8. LLM 클라이언트

`Client` 인터페이스 뒤에 provider 구현체. 표준 `net/http` 직접 호출, SDK 금지.
**기본 provider는 OpenAI(GPT-4o)** — AI 파트 프롬프트가 GPT-4o 기준으로 튜닝·검증됐다.
Anthropic 구현체도 유지하며 `LLM_PROVIDER` env로 전환한다. (전환 = 한 줄)

```go
type Client interface {
    Complete(ctx context.Context, system string, msgs []Message) (string, error)
}
type Message struct {
    Role, Content string
}
```

OpenAI (기본, `internal/llm/openai.go`):
```
POST https://api.openai.com/v1/chat/completions   # OPENAI_BASE_URL 로 override 가능(프록시/게이트웨이)
Authorization: Bearer {OPENAI_API_KEY}
{"model":"{MODEL}","max_tokens":1024,"messages":[{"role":"system",...}, ...]}
```
응답 `choices[0].message.content` 반환. **system은 별도 필드가 아니라 첫 메시지(role:"system")로 넣는다.**

Anthropic (`LLM_PROVIDER=anthropic`, `internal/llm/client.go`):
```
POST https://api.anthropic.com/v1/messages
x-api-key: {ANTHROPIC_API_KEY} · anthropic-version: 2023-06-01
{"model":"{MODEL}","max_tokens":1024,"system":"...","messages":[...]}
```
응답 `content[0].text` 반환.

> 두 provider 모두 20초 타임아웃 × 재시도 1회. 프롬프트가 GPT-4o 튜닝이라 기본은 OpenAI.
> Anthropic으로 바꾸면 JSON 준수율·톤이 달라질 수 있으니 `MODEL` 조정으로 대응한다.

---

## 9. 환경변수 · 미들웨어 · 컨테이너

| 변수 | 기본 | 필수 |
|---|---|---|
| `PORT` | 8080 | X |
| `LLM_PROVIDER` | `openai` | X (`openai`\|`anthropic`) |
| `OPENAI_API_KEY` | — | O(openai, 없으면 LLM만 503) |
| `OPENAI_BASE_URL` | (공식 API) | X (커스텀 OpenAI 호환 엔드포인트) |
| `ANTHROPIC_API_KEY` | — | O(anthropic) |
| `MODEL` | provider별(openai→`gpt-4o`, anthropic→`claude-haiku-4-5-20251001`) | X |
| `ALLOWED_ORIGIN` | — | O |
| `PROMPTS_DIR` | `./prompts` | X |

미들웨어: CORS(`ALLOWED_ORIGIN` 만) · 요청 로깅(**대화 내용 미출력**) ·
panic 복구 · 바디 1MB 제한.

```dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /app /app
COPY prompts /prompts        # ★ 빠뜨리면 런타임 프롬프트 로드 실패
ENV PORT=8080 PROMPTS_DIR=/prompts
EXPOSE 8080
ENTRYPOINT ["/app"]
```

`fly.toml` 은 만들지 않는다. **HTTPS 필수** — Vercel 프론트가 https라 http API는 차단된다.

---

## 10. 수용 기준

1. `go vet ./...` · `go test ./...` 통과
2. provider API 키(`OPENAI_API_KEY`) 없이 기동, `/healthz` 200 (LLM 호출만 503)
3. Fake 클라이언트 기준:
   - 정상 JSON / 코드펜스 / **JSON 앞 평문 접두사** → 모두 파싱 성공
   - 완전 파손 → 폴백, **200** 반환
   - `disclose` 정의 밖 키 → 필터링
   - `cmf_outsourcing` + `design` → 200
   - `prototype_revision` + `purchasing` → **400**
   - `context.round == round3` 인데 `intent_summary` 없음 → **400**
   - `context` 조립 결과가 §4 JSON 형식과 일치 (Fake 인자 검증)
   - `context != nil` 이고 `message` 빈 값 → 200 (round3 케이스)
4. `/api/report` 세션2 → 3키, 배열 상한 적용
5. `/api/report` 세션1 → 5키 서술형
6. 파싱 실패 시 `fallback.json` 문장 반환 (세션1은 `_s1_default`)
7. `/api/session-log` → stdout JSON + 200

---

## 11. 작업 순서

Step 1 을 끝내고 **실제 API 키로 파싱 실패율을 측정**한 뒤 다음으로 넘어갈 것.

1. `internal/{config,httpx,jsonx,domain,llm,prompt}` + `POST /api/chat`
   (세션2 `engineering` 만) → curl 검증 + 실패율 측정
2. 세션2 나머지 페르소나 3개
3. `POST /api/report` 세션2 + `fallback.json`
4. 세션1 페르소나 3개 + `context` 조립
5. `POST /api/report` 세션1
6. `POST /api/session-log` + Dockerfile + 로컬 `docker run` 검증

**세션1(Step 4~5)은 프론트 진도에 따라 후순위로 미룰 수 있다.**
서버 비용은 `scenario` 분기 하나뿐이다.

---

## 12. 미확정 — 코드 변경 없이 채워지는 것

| 항목 | 위치 |
|---|---|
| `budget_limit` 실제 금액 | `prompts/personas/purchasing.md` |
| 세션2 `design` 보유 정보 (현재 초안) | `prompts/personas/design.md` |
| 세션1 1차 협상 선택지 ③ 문구 | `prompts/s1_personas/engineering.md` |
| 납기 일수 · 시트지 단가 등 | `prompts/**/*.md` |
| fallback 문장 전체 | `prompts/fallback.json` |
| 자료함 문서 본문 | 프론트 레포 |

**코드 변경이 필요한 미확정 항목은 없다.** disclose 키는 전부 확정됐다.
