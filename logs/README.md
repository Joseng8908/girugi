# logs

`scripts/smoke.sh` / 시나리오 러너로 실서버(실제 gpt-4o)에 쏜 **E2E 트랜스크립트** 보관.
각 파일 = 요청 메시지 + 응답 전문(reply/intent/disclose) + report 2종.

- `scenario-YYYYMMDD-HHMM.log` — cmf_outsourcing 4페르소나 + prototype_revision 3라운드 + 리포트.
- 참고 기준: OpenAI `response_format=json_object` + 병합 프롬프트 기준 **폴백 0**.

> API 키·시크릿은 포함되지 않음(대화 내용만). 서버 기동 로그는 stdout(여기 아님).
