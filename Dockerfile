# Multi-stage: build a static binary, ship it on distroless (spec §9).
FROM golang:1.24 AS build
WORKDIR /src

# Cache module downloads. go.sum may not exist yet (stdlib-only), so glob it.
COPY go.* ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /app ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /app
# Prompts are read at runtime (CLAUDE.md rule 7) — omitting them breaks the app.
COPY prompts /prompts
ENV PORT=8080 PROMPTS_DIR=/prompts
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app"]
