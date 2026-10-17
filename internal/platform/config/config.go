// Package config memuat konfigurasi aplikasi dari environment variable.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	DBMaxConns  int32
	AppEnv      string

	// BaseURL adalah URL publik aplikasi tanpa garis miring di akhir, mis.
	// "https://app.akra.id". Dipakai untuk tautan di email; tidak pernah
	// diambil dari header Host request.
	BaseURL string

	// WebDir, jika diisi, membuat template dan aset dibaca dari disk (mis.
	// "web") alih-alih dari binary. Hanya untuk development.
	WebDir string

	// Parameter biaya argon2id untuk hash password baru. Default mengikuti
	// batas minimum OWASP (19 MiB, 2 iterasi, 1 lane).
	Argon2MemoryKiB   uint32
	Argon2Iterations  uint32
	Argon2Parallelism uint8
}

// Load membaca konfigurasi dari environment variable lalu memvalidasinya.
func Load() (Config, error) {
	cfg := Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		AppEnv:      getEnv("APP_ENV", EnvDevelopment),
		WebDir:      os.Getenv("WEB_DIR"),
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

	cfg.BaseURL = strings.TrimRight(os.Getenv("APP_BASE_URL"), "/")
	if cfg.BaseURL == "" {
		if cfg.AppEnv != EnvDevelopment {
			return Config{}, fmt.Errorf("config: environment variable APP_BASE_URL is required when APP_ENV=%s", cfg.AppEnv)
		}
		cfg.BaseURL = "http://localhost:" + cfg.Port
	}
	if u, err := url.Parse(cfg.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		return Config{}, fmt.Errorf("config: APP_BASE_URL must be an absolute http(s) URL without a path, got %q", cfg.BaseURL)
	}

	if cfg.WebDir != "" && cfg.AppEnv != EnvDevelopment {
		return Config{}, fmt.Errorf("config: WEB_DIR is only allowed when APP_ENV=development")
	}

	return cfg, nil
}

// positiveInt membaca key sebagai bilangan bulat positif yang muat dalam
// signed integer bitSize bit, dan mengembalikan fallback jika key kosong.
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
