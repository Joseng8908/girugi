# 코드 읽는 순서 가이드

이 코드를 처음 읽거나 남에게 설명할 때의 권장 순서. **의존성 아래에서 위로** 읽는다 —
데이터·경계(seam)를 먼저 잡고, 마지막에 조립하는 `main.go` 를 본다.
(인프라 관점: DTO → 외부 I/O 경계 → 유틸 → 비즈니스 핸들러 → 부트스트랩)

---

## 1. 어휘 — `internal/domain/domain.go`
의존성 0. 서비스의 "명사"가 다 여기 있다.
- `ScenarioPersonas` — 시나리오별 관계자 화이트리스트
- `DiscloseKeys` — `[scenario][persona] → 공개 가능 키` (중첩 맵)
- `FilterDisclose()` — LLM이 뱉은 키 중 허용된 것만 남김(중복 제거 포함)

> 핵심: LLM이 없는 키를 지어내도 코드가 화이트리스트로 잘라낸다. 프롬프트 준수를 안 믿는다(CLAUDE.md 규칙 8).

## 2. 외부 경계 — `internal/llm/client.go` + `fake.go`
가장 중요한 seam.
- `Client` 인터페이스 = `Complete(ctx, system, msgs)` 하나
- `Anthropic` = `net/http` 로 Messages API 직접 호출(SDK 없음). 20초 타임아웃 × 재시도 1회
- `Fake` = 테스트용. 정한 문자열 반환 + 호출 인자 기록

> 외부 호출을 인터페이스 뒤에 숨겨 테스트에선 Fake로 교체. 실제 API를 때리는 테스트는 없다(비용·불안정).
> Go 인터페이스 기반 DI ≈ 자바 생성자 주입 + Mockito.

## 3. 파싱 방어 — `internal/jsonx/jsonx.go`
작고 독립적. **왜 있는지가 포인트.**
- `Extract()` = 코드펜스 제거 → 첫 `{` 앞 평문 제거 → 마지막 `}` 뒤 제거

> LLM이 코드펜스로 감싸거나 JSON 앞에 평문을 중복 출력하는 실패가 반복됨. 중괄호 기준으로 잘라낸다.

## 4. 잔가지 — `prompt/prompt.go`, `config/config.go`
- `prompt.Loader.Load()` = `prompts/{name}.md` 읽기. 프롬프트는 코드가 아니라 데이터라 런타임 로드.
- `config.Load()` = 환경변수 → `Config`. DB 관련 없음.

## 5. 공통 계층 — `internal/httpx/httpx.go`
- `WriteJSON` 헬퍼
- 미들웨어 4개: `Recover`(panic→500), `Logger`, `CORS`, `MaxBytes`(1MB)
- `Chain()` = 미들웨어를 바깥→안 순서로 감싸기

## 6. 🎯 핵심 흐름 — `internal/chat/handler.go`
가장 중요. `ServeHTTP` 를 위에서 아래로 읽으면 요청 생명주기가 그대로 보인다:

```
1. body 디코드
2. scenario 검증          → 400
3. persona 검증(조합)     → 400
4. cmf면 context 무시
5. message 길이 검증       → 400
6. round3 intent_summary  → 400
7. 프롬프트 로드(경로 분기: personas/ vs s1_personas/)
8. history 뒤 20개로 자르기
9. buildUserTurn()로 마지막 user turn 조립
10. LLM 호출             → 실패 시 503
11. parseReply(): jsonx.Extract → 언마샬 → 실패면 폴백(unknown)
12. FilterDisclose로 disclose 필터 → 200
```

> 꼭 볼 함수 2개
> - `buildUserTurn()` — 세션1 협상 라운드에서 `context`+`message` 를 AI가 검증한 JSON으로 재조립.
>   `map` 이라 키가 알파벳순 정렬됨(이슈 #7).
> - `parseReply()` — 절대 500 안 냄. 파싱 실패해도 원문을 reply에 넣고 200.

## 7. 두 번째 핸들러 — `internal/report/handler.go` + `fallback.go`
- `ServeHTTP` → scenario로 세션1/2 분기
- 세션2 = `{strengths,cautions,missed}` 상한 2/2/3
- 세션1 = 5섹션 서술형
- 폴백이 chat보다 강함: LLM 실패든 파싱 실패든 **전부 200 + 고정 문장**(`fallback.json`)
- `fallback.go` = `fallback.json` 로드 → finding code를 문장으로 매핑

## 8. 잔챙이 — `internal/sessionlog/handler.go`
가장 단순. stdout에 JSON 로그, 항상 200. DB 없음. 디코드 실패해도 200.

## 9. 🔌 조립 — `cmd/server/main.go`
맨 마지막에. 조각들이 어떻게 연결되는지:
- `config.Load()` → `llm.NewAnthropic()` → `prompt.New()` → `report.LoadFallback()`
- mux에 4개 라우트 등록 → `httpx.Chain()` 으로 미들웨어 감싸기
- graceful shutdown: SIGTERM 받으면 10초 드레인 후 종료(컨테이너 재배포 대비)

---

## 남 앞에서 설명할 때

**30초 요약**
> 상태 없는(stateless) Go API 서버. 엔드포인트 3개. 프론트가 LocalStorage에 상태를 다 갖고 매 요청에 실어보낸다.
> LLM은 판정을 안 하고 문장만 다듬는다. 핵심은 **LLM 응답이 깨져도 안 죽는 폴백 설계**와,
> **disclose 키를 코드에서 화이트리스트 필터링**하는 것.

**예상 Q&A**

| 질문 | 답변 |
|---|---|
| 왜 DB가 없어? | stateless. 세션 상태는 프론트 LocalStorage. 서버는 문장 생성만. session-log도 stdout. |
| LLM이 이상하게 답하면? | jsonx로 코드펜스·앞평문 제거 후 파싱 → 실패하면 폴백(chat: 원문+intent unknown / report: fallback.json). 500 안 냄. |
| chat는 503, report는 왜 200만? | chat 실패는 재시도 버튼. report는 마지막 화면이라 비면 안 됨. |
| disclose를 왜 코드에서 또 걸러? | 프롬프트가 없는 키를 지어낼 수 있어서. 프론트 자료함 해제 매핑이 깨지면 안 됨. |
| 프롬프트를 왜 파일로? | 기획·AI가 배포 없이 수정하게. 프롬프트는 코드가 아니라 데이터. |
| 테스트는 실제 LLM 부름? | 아니. `llm.Client` 를 `Fake`로 교체. 비용·불안정 때문에 실제 호출 테스트 없음. |
| 왜 SDK 안 쓰고 net/http 직접? | 의존성 최소화(표준 라이브러리만). Messages API가 단순. |
| 배포는? | Dockerfile 멀티스테이지(distroless), 설정 전부 env, graceful shutdown. → `docs/DEPLOYMENT.md` |
