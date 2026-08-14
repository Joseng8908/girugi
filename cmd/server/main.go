package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"girugi/internal/chat"
	"girugi/internal/config"
	"girugi/internal/httpx"
	"girugi/internal/llm"
	"girugi/internal/prompt"
)

func main() {
	cfg := config.Load()

	llmClient := llm.NewAnthropic(cfg.APIKey, cfg.Model)
	prompts := prompt.New(cfg.PromptsDir)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // 200 + empty body (API.md)
	})
	mux.Handle("POST /api/chat", &chat.Handler{LLM: llmClient, Prompts: prompts})

	handler := httpx.Chain(mux,
		httpx.Recover,
		httpx.Logger,
		httpx.CORS(cfg.AllowedOrigin),
		httpx.MaxBytes,
	)

	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		// WriteTimeout must exceed the LLM budget (20s × 2 attempts = 40s, spec §3)
		// or slow completions would be cut off mid-write.
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run the server and shut it down cleanly on SIGINT/SIGTERM so the
	// container/orchestrator can drain in-flight requests on redeploy.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("server starting", "addr", srv.Addr, "model", cfg.Model, "hasKey", cfg.APIKey != "")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received, draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("stopped")
}
