# 런북 — AI 파트 산출물 받은 뒤 배포까지

AI 파트가 프롬프트(이슈 #1~#8)를 마무리하면 아래 순서로 진행. 대부분 명령 한 줄.

## 0. AI 파트에서 받을 것
- `OPENAI_API_KEY`
- **엔드포인트 URL** — 공식 OpenAI면 불필요(기본값). Azure/프록시 등 커스텀이면 `OPENAI_BASE_URL`
- **모델명** — 예: `gpt-4o` (다르면 `MODEL` 로 지정)
- GitHub 이슈 #1~#8 close 확인 (프롬프트/폴백 문구 확정)

> 커스텀 URL은 **OpenAI 호환(`/chat/completions`, `Bearer` 인증)** 이어야 이 클라이언트로 붙음.
> Azure OpenAI(api-key 헤더 + api-version)면 클라이언트 소폭 수정 필요 — 받은 형식 먼저 확인.

## 1~3. env 구성
```bash
cat > .env <<EOF
LLM_PROVIDER=openai
OPENAI_API_KEY=sk-...
MODEL=gpt-4o
# OPENAI_BASE_URL=https://...   # 커스텀 엔드포인트일 때만
ALLOWED_ORIGIN=http://localhost:3000
EOF
```

## 4~5. 로컬 기동 + 시나리오 스모크 테스트
```bash
# 서버 기동 (.env 로드)
set -a; source .env; set +a
go run ./cmd/server &        # 또는 docker run (아래 6번)

# 전 페르소나 chat + report + session-log 자동 호출
BASE_URL=http://localhost:8080 ./scripts/smoke.sh
```
기대치:
- `RESULT PASS=15 WARN=0 FAIL=0` 이면 완벽 (전 페르소나 JSON 준수 + disclose 정상)
- `WARN=503` → 키/URL 확인
- `WARN=폴백` → 해당 페르소나 프롬프트가 JSON 형식 안 지킴 → AI 파트에 피드백(이슈 재오픈)
- `FAIL` → 라우팅/서버 오류

## 6. 이미지화 (스모크 통과 후)
```bash
# ⚠️ 프론트/서버가 amd64면 크로스빌드 (Mac arm64에서)
docker buildx build --platform linux/amd64 -t ghcr.io/joseng8908/girugi:$(git rev-parse --short HEAD) --load .

# 컨테이너로 다시 스모크 (prompts 포함 확인)
docker run -d --name girugi -p 8080:8080 --env-file .env ghcr.io/joseng8908/girugi:$(git rev-parse --short HEAD)
BASE_URL=http://localhost:8080 ./scripts/smoke.sh
docker rm -f girugi

# 레지스트리 푸시 → 프론트에 이미지 태그 전달
echo $(gh auth token) | docker login ghcr.io -u Joseng8908 --password-stdin
docker push ghcr.io/joseng8908/girugi:$(git rev-parse --short HEAD)
```
자세한 배포는 [`DEPLOYMENT.md`](DEPLOYMENT.md).

## 7. (내일) 프론트 로컬 연동
- 프론트가 이미지 pull → `--env-file .env` 로 기동
- `ALLOWED_ORIGIN` 을 프론트 로컬 오리진(`http://localhost:3000`)으로
- 프론트에서 시나리오 처음부터 끝까지 1회 (chat 여러 턴 → 자료함 해제(disclose) → report)
- 문제 시 서버 로그(`docker logs girugi`) 확인 — 대화 내용은 안 찍히니 status/에러 위주
