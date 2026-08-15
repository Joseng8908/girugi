#!/usr/bin/env bash
# Smoke test: exercises every endpoint + every persona against a running server.
# 실제 LLM 키가 설정된 서버에 쏘면 응답 품질까지, 키 없는 서버면 라우팅/폴백만 검증.
#
# Usage:
#   BASE_URL=http://localhost:8080 ./scripts/smoke.sh
#   BASE_URL=https://api.your-domain ./scripts/smoke.sh
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
pass=0; warn=0; fail=0

command -v jq >/dev/null || { echo "jq 필요 (brew install jq / dnf install jq)"; exit 2; }

hr(){ printf -- '--------------------------------------------------------\n'; }
section(){ hr; printf '### %s\n' "$1"; }

# chat <scenario> <persona> <message> [context-json]
chat() {
  local scenario="$1" persona="$2" msg="$3" ctx="${4:-null}"
  local payload body status
  payload=$(jq -nc --arg s "$scenario" --arg p "$persona" --arg m "$msg" --argjson c "$ctx" \
    '{scenario:$s, persona:$p, history:[], message:$m, context:$c}')
  body=$(curl -s -w $'\n%{http_code}' -X POST "$BASE_URL/api/chat" \
    -H 'Content-Type: application/json' -d "$payload")
  status="${body##*$'\n'}"; body="${body%$'\n'*}"
  if [[ "$status" == "200" ]]; then
    local reply intent disc
    reply=$(jq -r '.reply // ""' <<<"$body")
    intent=$(jq -r '.intent // ""' <<<"$body")
    disc=$(jq -c '.disclose // []' <<<"$body")
    if [[ -n "$reply" && "$intent" != "unknown" ]]; then
      printf '  PASS %-11s intent=%-22s disclose=%s\n' "$persona" "$intent" "$disc"; ((pass++))
    else
      printf '  WARN %-11s 폴백(JSON 파싱 실패) intent=%s\n' "$persona" "$intent"; ((warn++))
    fi
  elif [[ "$status" == "503" ]]; then
    printf '  WARN %-11s 503 — LLM 키 미설정/호출 실패\n' "$persona"; ((warn++))
  else
    printf '  FAIL %-11s status=%s body=%s\n' "$persona" "$status" "$body"; ((fail++))
  fi
}

# report <scenario> <payload-json> <expected-key>
report() {
  local scenario="$1" payload="$2" key="$3" body status
  body=$(curl -s -w $'\n%{http_code}' -X POST "$BASE_URL/api/report" \
    -H 'Content-Type: application/json' -d "$payload")
  status="${body##*$'\n'}"; body="${body%$'\n'*}"
  if [[ "$status" == "200" ]] && jq -e "has(\"$key\")" <<<"$body" >/dev/null 2>&1; then
    printf '  PASS %-18s %s\n' "$scenario" "$(jq -c . <<<"$body" | cut -c1-90)"; ((pass++))
  else
    printf '  FAIL %-18s status=%s body=%s\n' "$scenario" "$status" "$body"; ((fail++))
  fi
}

echo "BASE_URL=$BASE_URL"

section "GET /healthz"
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/healthz")
if [[ "$code" == "200" ]]; then echo "  PASS healthz 200"; ((pass++)); else echo "  FAIL healthz $code"; ((fail++)); fi

section "POST /api/chat — cmf_outsourcing (전 페르소나)"
chat cmf_outsourcing senior     "시방서 작성 기준이 어떻게 되나요?"
chat cmf_outsourcing engineering "B파트 목재 밴딩 사내에서 가능한가요?"
chat cmf_outsourcing purchasing  "목재 파트 예산 상한이 있나요?"
chat cmf_outsourcing design      "오크 원목을 꼭 유지해야 하나요?"

section "POST /api/chat — prototype_revision (협상 3라운드)"
chat prototype_revision engineering "벨트라인을 살리며 두 조각으로 줄이겠습니다" '{"round":"round1","choice":1}'
chat prototype_revision engineering "구배 각도를 조정하겠습니다" '{"round":"round2","choice":3,"cost_alternative":"포장재 등급을 낮춰 상쇄"}'
chat prototype_revision engineering "" '{"round":"round3","choice":"confirm","intent_summary":"벨트라인 무드는 그래픽으로 유지"}'
chat prototype_revision senior      "벨트라인 방향을 유지해도 될까요?"
chat prototype_revision design      "사용자 요구 핵심이 뭐였죠?"

section "POST /api/report"
report cmf_outsourcing "$(jq -nc '{scenario:"cmf_outsourcing",
  strengths_findings:[{code:"asked_capability_before_final",evidence:{}}],
  cautions_findings:[{code:"limit_sample_missing_in_draft",evidence:{}}],
  missed_findings:[{code:"budget_never_asked",evidence:{}}],
  action_logs:[], scores:{intent_delivery:2}, branch:"outsourcing"}')" strengths
report prototype_revision "$(jq -nc '{scenario:"prototype_revision",
  strengths_findings:[{code:"core_intent_kept",evidence:{}}],
  cautions_findings:[], missed_findings:[],
  action_logs:[], initial_intent:"벨트라인 3파츠", final_intent_summary:"2파츠+그래픽",
  round_summary:[{round:"round1",choice:1,reasoning:"단가"}],
  self_eval:{q1:4,q2:3,q3:4,q4:"반복 수정 부담"}}')" initial_direction

section "POST /api/session-log"
slog=$(curl -s -w $'\n%{http_code}' -X POST "$BASE_URL/api/session-log" \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"smoke-test","scenario":"cmf_outsourcing","payload":{"foo":"bar"}}')
scode="${slog##*$'\n'}"; sbody="${slog%$'\n'*}"
if [[ "$scode" == "200" ]] && jq -e '.ok == true' <<<"$sbody" >/dev/null 2>&1; then
  echo "  PASS session-log ok"; ((pass++)); else echo "  FAIL session-log $scode $sbody"; ((fail++)); fi

hr
printf 'RESULT  PASS=%d  WARN=%d  FAIL=%d\n' "$pass" "$warn" "$fail"
[[ "$warn" -gt 0 ]] && echo "(WARN=503 이면 LLM 키 미설정, WARN=폴백이면 프롬프트 JSON 준수 확인)"
[[ "$fail" -eq 0 ]] || exit 1
