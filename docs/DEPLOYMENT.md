# 배포 가이드

이미지 빌드 → 레지스트리 푸시 → 서버/클라우드에서 pull → env 주입 → 기동 → 검증.
배포 대상은 미정(BACKEND_SPEC §9). 이 문서는 레지스트리·런타임 무관하게 쓰도록 3가지 경로를 다룬다:
**A) docker run(단일 컨테이너)**, **B) docker compose**, **C) Kubernetes(프로덕션)**.

> 전제
> - 이 서버는 stateless. DB·볼륨 없음. `prompts/` 는 이미지에 포함됨.
> - 프론트가 HTTPS(Vercel)라 **프로덕션 API도 HTTPS 필수** — TLS는 리버스 프록시/인그레스/LB에서 종단.
> - 시크릿(`ANTHROPIC_API_KEY`)은 이미지·git에 넣지 않는다.

---

## 0. 환경변수 (런타임 주입)

| 변수 | 기본 | 필수 | 설명 |
|---|---|---|---|
| `PORT` | `8080` | X | 바인드 포트(이미지에 8080 설정됨) |
| `HOST` | (전체) | X | 비우면 모든 인터페이스 — 컨테이너 기본 |
| `LLM_PROVIDER` | `openai` | X | `openai` \| `anthropic`. 프롬프트가 GPT-4o 튜닝이라 기본 openai |
| `OPENAI_API_KEY` | — | **O(openai)** | 없으면 `/api/chat` 만 503, 서버는 기동 |
| `ANTHROPIC_API_KEY` | — | O(anthropic) | provider=anthropic일 때 |
| `MODEL` | provider별 | X | openai→`gpt-4o`, anthropic→`claude-haiku-4-5-20251001` |
| `ALLOWED_ORIGIN` | `*` | **O(프로덕션)** | 실제 프론트 오리진. `*` 금지 |
| `PROMPTS_DIR` | `/prompts` | X | 이미지가 `/prompts` 로 설정 |

---

## 1. 이미지 빌드

```bash
# 로컬 아키텍처로 빌드 (개발용)
docker build -t girugi:dev .
```

### ⚠️ 크로스 아키텍처 (Mac arm64 → 클라우드/서버 amd64)
Apple Silicon(arm64)에서 빌드한 이미지는 amd64 서버에서 안 뜬다. **타깃 아키텍처를 명시**한다:

```bash
# buildx로 amd64 이미지 빌드 (RHEL/대부분 클라우드 = linux/amd64)
docker buildx build --platform linux/amd64 -t girugi:latest --load .

# 멀티아키(둘 다) — 레지스트리로 바로 push 필요
docker buildx build --platform linux/amd64,linux/arm64 -t <IMAGE> --push .
```

빌드 자체 검증(선택): `go vet ./... && go test ./...` 는 이미지와 무관하게 CI에서.

---

## 2. 레지스트리에 푸시 ("허브에 올리기")

### 옵션 1: GHCR (GitHub Container Registry — repo와 같은 곳, 권장)

```bash
# gh 토큰으로 로그인 (이미 gh auth 돼 있으면 토큰 재사용)
echo $(gh auth token) | docker login ghcr.io -u Joseng8908 --password-stdin

IMAGE=ghcr.io/joseng8908/girugi        # 소유자는 소문자
docker tag girugi:latest $IMAGE:latest
docker tag girugi:latest $IMAGE:$(git rev-parse --short HEAD)   # 커밋 SHA 태그 권장
docker push $IMAGE:latest
docker push $IMAGE:$(git rev-parse --short HEAD)
```
> 푸시 후 GitHub 패키지에서 **visibility를 Public 또는 Private** 설정. Private면 서버에서 pull 시 로그인 필요.

### 옵션 2: Docker Hub

```bash
docker login -u <DOCKERHUB_ID>
IMAGE=<DOCKERHUB_ID>/girugi
docker tag girugi:latest $IMAGE:latest
docker push $IMAGE:latest
```

### 옵션 3: 클라우드 레지스트리 (GCP Artifact Registry 예)

```bash
gcloud auth configure-docker <REGION>-docker.pkg.dev
IMAGE=<REGION>-docker.pkg.dev/<PROJECT>/<REPO>/girugi
docker tag girugi:latest $IMAGE:latest
docker push $IMAGE:latest
```

> **태그 전략**: `latest` 는 편의용, 실제 배포는 **커밋 SHA 태그**로 고정(롤백·추적 가능).

---

## 3. 서버/클라우드에서 pull & 배포

### A) docker run (단일 컨테이너 — 가장 단순, Navix 홈서버)

```bash
docker pull ghcr.io/joseng8908/girugi:latest        # private면 먼저 docker login

docker run -d --name girugi --restart unless-stopped \
  -p 8080:8080 \
  -e OPENAI_API_KEY=sk-... \
  -e ALLOWED_ORIGIN=https://your-frontend.vercel.app \
  ghcr.io/joseng8908/girugi:latest
# Anthropic로 전환: -e LLM_PROVIDER=anthropic -e ANTHROPIC_API_KEY=sk-ant-...

# 포트만 바꾸려면 (이미지 재빌드 X)
#   -e PORT=9000 -p 9000:9000
```

시크릿을 CLI에 노출하기 싫으면 `--env-file`:
```bash
# .env (git에 커밋 금지)
docker run -d --name girugi --restart unless-stopped -p 8080:8080 \
  --env-file .env ghcr.io/joseng8908/girugi:latest
```

### B) docker compose

```yaml
# compose.yaml
services:
  girugi:
    image: ghcr.io/joseng8908/girugi:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    env_file: .env          # ANTHROPIC_API_KEY, ALLOWED_ORIGIN 등
    healthcheck:
      test: ["CMD", "/app", "-healthz"]   # 참고: 이미지에 shell 없음(distroless). 아래 주 참조
```
> distroless 이미지엔 shell·curl이 없어 컨테이너 내부 healthcheck가 어렵다.
> compose에선 healthcheck를 빼거나, 외부(리버스 프록시/모니터링)에서 `GET /healthz` 로 체크한다.

```bash
docker compose up -d
docker compose logs -f girugi
```

### C) Kubernetes (프로덕션 — K8s + Helm + ArgoCD)

**시크릿**
```bash
kubectl create secret generic girugi-secret \
  --from-literal=OPENAI_API_KEY=sk-...
```

**Deployment + Service** (`k8s/girugi.yaml`)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: girugi
spec:
  replicas: 1                     # stateless라 수평 확장 자유. 우선 1개
  selector:
    matchLabels: { app: girugi }
  template:
    metadata:
      labels: { app: girugi }
    spec:
      containers:
        - name: girugi
          image: ghcr.io/joseng8908/girugi:<COMMIT_SHA>
          ports:
            - containerPort: 8080
          env:
            - name: ALLOWED_ORIGIN
              value: "https://your-frontend.vercel.app"
            - name: OPENAI_API_KEY
              valueFrom:
                secretKeyRef: { name: girugi-secret, key: OPENAI_API_KEY }
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
            initialDelaySeconds: 2
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
            initialDelaySeconds: 5
          resources:
            requests: { cpu: "50m", memory: "32Mi" }
            limits:   { cpu: "500m", memory: "128Mi" }
          securityContext:
            runAsNonRoot: true      # 이미지가 nonroot 유저로 실행됨
            allowPrivilegeEscalation: false
      terminationGracePeriodSeconds: 15   # graceful shutdown(10s 드레인)과 맞춤
---
apiVersion: v1
kind: Service
metadata:
  name: girugi
spec:
  selector: { app: girugi }
  ports:
    - port: 80
      targetPort: 8080
```

```bash
kubectl apply -f k8s/girugi.yaml
kubectl rollout status deploy/girugi
```

**TLS/외부 노출** — Ingress(예: nginx-ingress + cert-manager)로 HTTPS 종단, `ALLOWED_ORIGIN` 과 도메인 일치.
**GitOps** — 위 매니페스트를 Helm 차트화해 ArgoCD가 git 상태를 클러스터에 동기화. 이미지 태그(SHA)만 바꿔 커밋하면 자동 롤아웃.

---

## 4. 배포 검증

```bash
# 헬스체크 (200 빈 바디)
curl -i https://api.your-domain/healthz

# chat 실호출 (JSON 형식·disclose 확인)
curl -s -X POST https://api.your-domain/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"scenario":"cmf_outsourcing","persona":"engineering","message":"목재 밴딩 사내 가능한가요?"}' | jq

# CORS 프리플라이트 (프론트 오리진 허용 확인)
curl -i -X OPTIONS https://api.your-domain/api/chat -H "Origin: https://your-frontend.vercel.app"
```

체크리스트:
- [ ] `/healthz` 200
- [ ] `/api/chat` 이 200 + `{reply,intent,disclose}` (503이면 `ANTHROPIC_API_KEY` 확인)
- [ ] `Access-Control-Allow-Origin` 이 프론트 오리진과 일치
- [ ] 로그가 stdout으로 나옴(`kubectl logs` / `docker logs`) — 대화 내용은 안 찍힘

---

## 5. 운영 노트

- **로깅**: slog가 stdout에 구조화 로그. 수집기(Loki/CloudWatch 등)로 tail. 요청 로그에 대화 내용은 안 남김.
- **graceful shutdown**: SIGTERM 시 10초 드레인. K8s `terminationGracePeriodSeconds >= 10` 유지.
- **무중단 배포**: 이미지 SHA 태그만 바꿔 롤아웃. replicas>=2 + rollingUpdate면 다운타임 0.
- **프롬프트 수정**: `prompts/` 가 이미지에 포함 → 문구만 바꿔도 **이미지 재빌드·재배포** 필요.
  자주 바뀌면 ConfigMap 볼륨 마운트 + `PROMPTS_DIR` 로 분리 검토(현재는 미적용).
- **모델 전환**: 파싱 실패율 높으면 `MODEL` env만 상향(코드·이미지 변경 불필요).
- **비용/타임아웃**: LLM 호출 20초 × 재시도 1회. LB/인그레스 타임아웃을 45초 이상으로.
