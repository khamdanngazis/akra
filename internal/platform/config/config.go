// Package config memuat konfigurasi aplikasi dari environment variable.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Nilai yang valid untuk APP_ENV.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config berisi konfigurasi yang dibutuhkan saat aplikasi berjalan.
type Config struct {
	Port        string
	DatabaseURL string
	AppEnv      string
}

// Load membaca konfigurasi dari environment variable dan memvalidasinya.
func Load() (Config, error) {
	cfg := Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		AppEnv:      getEnv("APP_ENV", EnvDevelopment),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: environment variable DATABASE_URL wajib diisi")
	}

	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		return Config{}, fmt.Errorf("config: PORT tidak valid: %q", cfg.Port)
	}

	switch cfg.AppEnv {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		return Config{}, fmt.Errorf("config: APP_ENV tidak valid: %q (pilihan: development, staging, production)", cfg.AppEnv)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
