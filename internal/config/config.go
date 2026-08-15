// Package config loads runtime configuration from the environment (spec §9).
// No DATABASE_URL: this service has no DB (CLAUDE.md rule 2).
package config

import "os"

type Config struct {
	Host          string
	Port          string
	Provider      string // "openai" (default) | "anthropic"
	OpenAIKey     string
	OpenAIBaseURL string // optional: custom OpenAI-compatible endpoint
	AnthropicKey  string
	Model         string
	AllowedOrigin string
	PromptsDir    string
}

func Load() Config {
	provider := envOr("LLM_PROVIDER", "openai") // AI part's prompts are tuned on GPT-4o
	return Config{
		Host:          os.Getenv("HOST"), // empty = bind all interfaces (correct for containers)
		Port:          envOr("PORT", "8080"),
		Provider:      provider,
		OpenAIKey:     os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL: os.Getenv("OPENAI_BASE_URL"),
		AnthropicKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:         modelFor(provider),
		AllowedOrigin: envOr("ALLOWED_ORIGIN", "*"), // "*" for local dev; set the real origin in prod
		PromptsDir:    envOr("PROMPTS_DIR", "./prompts"),
	}
}

// modelFor returns MODEL if set, else a provider-appropriate default.
func modelFor(provider string) string {
	if m := os.Getenv("MODEL"); m != "" {
		return m
	}
	if provider == "anthropic" {
		return "claude-haiku-4-5-20251001"
	}
	return "gpt-4o"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
