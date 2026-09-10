package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           string
	Workspace      string
	InternalToken  string
	MaxUploadBytes int64
	TaskLease      time.Duration
	DefaultProfile string
}

func Load() Config {
	return Config{
		Port:           env("APP_PORT", "8080"),
		Workspace:      env("WORKSPACE", "/workspace"),
		InternalToken:  mustEnv("INTERNAL_TOKEN"),
		MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 4<<30),
		TaskLease:      time.Duration(envInt64("TASK_LEASE_SECONDS", 45)) * time.Second,
		DefaultProfile: env("DEFAULT_PROFILE", "balanced"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("missing required environment variable: " + key)
	}
	return v
}

func envInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
