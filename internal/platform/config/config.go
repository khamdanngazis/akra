// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Valid values for APP_ENV.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config holds the runtime configuration of the application.
type Config struct {
	Port        string
	DatabaseURL string
	DBMaxConns  int32
	AppEnv      string
}

// Load reads the configuration from environment variables and validates it.
func Load() (Config, error) {
	cfg := Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		AppEnv:      getEnv("APP_ENV", EnvDevelopment),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: environment variable DATABASE_URL is required")
	}

	maxConns, err := strconv.ParseInt(getEnv("DB_MAX_CONNS", "10"), 10, 32)
	if err != nil || maxConns < 1 {
		return Config{}, fmt.Errorf("config: DB_MAX_CONNS must be a positive integer, got %q", os.Getenv("DB_MAX_CONNS"))
	}
	cfg.DBMaxConns = int32(maxConns)

	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		return Config{}, fmt.Errorf("config: invalid PORT %q", cfg.Port)
	}

	switch cfg.AppEnv {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		return Config{}, fmt.Errorf("config: invalid APP_ENV %q (allowed: development, staging, production)", cfg.AppEnv)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
