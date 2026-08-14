// Package config loads runtime configuration from the environment (spec §9).
// No DATABASE_URL: this service has no DB (CLAUDE.md rule 2).
package config

import "os"

type Config struct {
	Host          string
	Port          string
	APIKey        string
	Model         string
	AllowedOrigin string
	PromptsDir    string
}

func Load() Config {
	return Config{
		Host:          os.Getenv("HOST"), // empty = bind all interfaces (correct for containers)
		Port:          envOr("PORT", "8080"),
		APIKey:        os.Getenv("ANTHROPIC_API_KEY"),
		Model:         envOr("MODEL", "claude-haiku-4-5-20251001"),
		AllowedOrigin: envOr("ALLOWED_ORIGIN", "*"), // "*" for local dev; set the real origin in prod
		PromptsDir:    envOr("PROMPTS_DIR", "./prompts"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
