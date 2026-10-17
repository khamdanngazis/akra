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

	// argon2id cost parameters for new password hashes. The defaults follow
	// the OWASP minimum (19 MiB, 2 iterations, 1 lane).
	Argon2MemoryKiB   uint32
	Argon2Iterations  uint32
	Argon2Parallelism uint8
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

	maxConns, err := positiveInt("DB_MAX_CONNS", 10, 32)
	if err != nil {
		return Config{}, err
	}
	cfg.DBMaxConns = int32(maxConns)

	memory, err := positiveInt("ARGON2_MEMORY_KIB", 19*1024, 32)
	if err != nil {
		return Config{}, err
	}
	iterations, err := positiveInt("ARGON2_ITERATIONS", 2, 32)
	if err != nil {
		return Config{}, err
	}
	parallelism, err := positiveInt("ARGON2_PARALLELISM", 1, 8)
	if err != nil {
		return Config{}, err
	}
	cfg.Argon2MemoryKiB = uint32(memory)
	cfg.Argon2Iterations = uint32(iterations)
	cfg.Argon2Parallelism = uint8(parallelism)

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

// positiveInt parses key as a positive integer that fits in a signed integer
// of bitSize bits, returning fallback when key is unset.
func positiveInt(key string, fallback int64, bitSize int) (int64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseInt(raw, 10, bitSize)
	if err != nil || v < 1 {
		return 0, fmt.Errorf("config: %s must be a positive integer, got %q", key, raw)
	}
	return v, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
