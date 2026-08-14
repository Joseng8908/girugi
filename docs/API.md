# API Reference (구현 기준)

프론트엔드 연동용 엔드포인트 레퍼런스. **현재 서버 구현 동작 기준**으로 작성됨.
계약 원본(필드 변경 규칙 포함)은 루트 [`API.md`](../API.md), 구현 명세는 [`BACKEND_SPEC_v4.md`](../BACKEND_SPEC_v4.md).

- Base URL: 로컬 `http://localhost:8080` / 배포 `https://TODO`
- 인증 없음. 모든 요청·응답 `application/json`.
- **서버는 상태를 저장하지 않습니다.** 매 요청에 필요한 정보를 모두 실어 보내세요.
- 프로덕션은 **HTTPS 필수** (Vercel 프론트가 http API를 차단).

## 시나리오 × 관계자

| `scenario` | 관계자(`persona`) | 리포트 |
|---|---|---|
| `cmf_outsourcing` | `senior` `engineering` `purchasing` `design` | strengths/cautions/missed |
| `prototype_revision` | `senior` `engineering` `design` | 5섹션 서술형 |

표에 없는 `scenario`×`persona` 조합은 **400**.

---

## POST /api/chat

관계자 답변 생성 + 의도 분류.

### 요청

| 필드 | 타입 | 필수 | 제약 |
|---|---|---|---|
| `scenario` | string | O | 위 표 |
| `persona` | string | O | 위 표 |
| `history` | array | O | 빈 배열 가능. 서버가 **뒤에서 20개**만 사용 |
| `history[].role` | string | O | `user` \| `assistant` |
| `history[].content` | string | O | |
| `message` | string | O | `context` 없으면 **1~300자**, 있으면 **0~300자** |
| `context` | object \| null | X | 세션1 협상 라운드에서만. 세션2는 항상 `null` |

```json
{
  "scenario": "cmf_outsourcing",
  "persona": "engineering",
  "history": [
    { "role": "user", "content": "시방서 초안 공유드립니다" },
    { "role": "assistant", "content": "확인했습니다." }
  ],
  "message": "B파트 목재 밴딩 사내에서 가능한가요?",
  "context": null
}
```

#### context (세션1 `prototype_revision` 전용)

`message` 에는 선택 **근거(reasoning)** 를 담습니다. 서버가 `context`+`message` 를
AI 파트 검증 포맷의 JSON으로 조립해 LLM에 전달합니다 — **프론트는 JSON 문자열을 직접 만들지 마세요.**

| `round` | `choice` | 추가 필드 | 비고 |
|---|---|---|---|
| `round1` | `1`\|`2`\|`3` | — | `message`(근거) 필수 |
| `round2` | `1`\|`2`\|`3` | `cost_alternative` (string, 선택) | choice=3일 때 노출 |
| `round3` | `"confirm"`\|`"revisit"` | `intent_summary` (string, **필수**) | `message` 비어도 됨 |

```json
{ "round": "round2", "choice": 3, "cost_alternative": "포장재 등급을 낮춰 상쇄" }
```

> `cmf_outsourcing` 은 `context` 를 무시합니다.

### 응답 200

```json
{ "reply": "3파츠로 가면 금형 비용이 초과합니다...", "intent": "round1_feedback", "disclose": ["molding_cost_constraint"] }
```

| 필드 | 설명 |
|---|---|
| `reply` | 화면에 표시할 답변 |
| `intent` | 의도 분류(참고용). 폴백 시 `"unknown"` |
| `disclose` | **이 답변으로 공개된 정보 키.** 자료함 잠금 해제에 사용. 빈 배열 가능. 목록 밖 키는 서버가 제거 |

### 에러

| 코드 | 바디 | 상황 |
|---|---|---|
| 400 | `{"error":"invalid_scenario"}` | 미등록 scenario |
| 400 | `{"error":"invalid_persona"}` | scenario에 없는 persona |
| 400 | `{"error":"invalid_message"}` | message 길이 위반 |
| 400 | `{"error":"missing_intent_summary"}` | round3인데 `intent_summary` 없음 |
| 503 | `{"error":"llm_unavailable","retryable":true}` | LLM 호출 실패 → **재시도 버튼** |

> LLM 응답 형식이 깨져도 서버가 폴백해 **200**(`intent:"unknown"`, `disclose:[]`)을 반환합니다.
> 대화는 진행되고 자료 해제만 안 됩니다.

### intent enum

**세션1 `prototype_revision`** — engineering: `round1_feedback` `round2_feedback` `round3_feedback` `manufacturing_capability` / senior: `design_guidance` `revisit_review` / design: `concept_feedback` `user_perspective`

**세션2 `cmf_outsourcing`** — senior: `spec_guidance` `vendor_guidance` / engineering: `manufacturing_capability` / purchasing: `cost_constraint` / design: `concept_feedback` `material_intent_guidance`

**공통** — `out_of_scope` `too_vague` `irrelevant` `other` `unknown`(서버 폴백 시에만)

### disclose 키 (자료함 매핑용)

**세션2 `cmf_outsourcing`**

| persona | 키 |
|---|---|
| `senior` | `spec_format` `limit_sample` `vendor_criteria` |
| `engineering` | `inhouse_capability` `sheet_lead_time` |
| `purchasing` | `budget_limit` `cost_impact` `part_cost_share` |
| `design` | `wood_material_intent` `concept_consistency_criteria` `cmf_alternative_range` |

**세션1 `prototype_revision`**

| persona | 키 |
|---|---|
| `engineering` | `molding_cost_constraint` `part_consolidation_criteria` `undercut_constraint` `min_thickness` |
| `senior` | `belt_line_intent` `revision_guidance_criteria` |
| `design` | `user_requirement_keywords` `concept_direction` |

> 키 이름은 확정. 세션2 `design` 3키의 **내용**은 기획 확정 대기지만 키는 안 바뀌므로 매핑은 지금 확정 가능.

---

## POST /api/report

배정된 finding을 자연스러운 문장으로 변환. **카테고리 분류는 프론트에서 끝난 상태로 보냅니다 — 서버·LLM은 재분류하지 않습니다.**
LLM/파싱 실패 시에도 **항상 200 + 고정 문장** (리포트는 마지막 화면이라 비면 안 됨).

### 요청 (공통)

```json
{
  "scenario": "cmf_outsourcing",
  "strengths_findings": [{ "code": "asked_capability_before_final", "evidence": { "ask_seq": 5 } }],
  "cautions_findings":  [{ "code": "limit_sample_missing_in_draft", "evidence": { "draft_seq": 7 } }],
  "missed_findings":    [{ "code": "budget_never_asked", "evidence": {} }],
  "action_logs": [ ... ]
}
```

**세션2 추가**: `scores` (object), `branch` (`wood_dropped`\|`sheet_wrap`\|`outsourcing`)
**세션1 추가**: `initial_intent`, `final_intent_summary`, `round_summary` (array), `self_eval` (`{q1,q2,q3,q4}`)

> finding code 전체 목록·기본 카테고리는 루트 [`API.md`](../API.md) 참조.

### 응답 200 — 세션2 `cmf_outsourcing`

```json
{ "strengths": ["..."], "cautions": ["..."], "missed": ["..."] }
```
상한 strengths 2 / cautions 2 / missed 3, 각 60자 이내, 숫자 미포함. 빈 배열 가능.

### 응답 200 — 세션1 `prototype_revision`

```json
{ "initial_direction": "...", "work_journey": "...", "key_changes": "...", "competency_record": "...", "job_meaning": "..." }
```
각 2~4문장 서술형. 점수·등급 미포함(라운드 서수는 허용).

### 에러

| 코드 | 상황 |
|---|---|
| 400 | `{"error":"invalid_scenario"}` |
| — | **503 없음.** LLM 실패도 200 + 폴백 |

---

## POST /api/session-log

체험 종료 시 상태 저장(분석용). **fire-and-forget 로 호출해도 됩니다.**

```json
{ "session_id": "클라이언트 생성 UUID v4", "scenario": "cmf_outsourcing", "payload": { ... } }
```

항상 **200** `{"ok":true}`. 서버는 payload를 검증하지 않고 stdout에 로깅만 합니다(DB 없음).

---

## GET /healthz

**200** + 빈 바디.

---

## action_logs 스키마 (프론트가 기록)

리포트 판정의 유일한 근거. `seq` 는 1부터 순차 증가.

```json
{"seq":1,"t":1699834200,"type":"doc_view","target":"spec_format"}
{"seq":2,"t":1699834260,"type":"ask","actor":"engineering","intent":"manufacturing_capability"}
{"seq":3,"t":1699834400,"type":"submit","target":"draft"}
{"seq":4,"t":1699834600,"type":"branch","target":"outsourcing"}
{"seq":5,"t":1699834700,"type":"choice","target":"round1:1"}
```

`type`: `doc_view` \| `ask` \| `submit` \| `branch` \| `revise` \| `choice`
`actor`: `ask` 일 때 persona / `target`: 자료 키 · `draft` · `final` · 분기값 · `round1:1` 등
