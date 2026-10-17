package config

import (
	"strings"
	"testing"
)

func TestLoad_AllSet(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("DB_MAX_CONNS", "20")
	t.Setenv("APP_ENV", "production")
	t.Setenv("ARGON2_MEMORY_KIB", "65536")
	t.Setenv("ARGON2_ITERATIONS", "3")
	t.Setenv("ARGON2_PARALLELISM", "4")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Config{
		Port: "9000", DatabaseURL: "postgres://localhost/akra", DBMaxConns: 20, AppEnv: EnvProduction,
		Argon2MemoryKiB: 65536, Argon2Iterations: 3, Argon2Parallelism: 4,
	}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("APP_ENV", "development")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error message does not mention DATABASE_URL: %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("ARGON2_MEMORY_KIB", "")
	t.Setenv("ARGON2_ITERATIONS", "")
	t.Setenv("ARGON2_PARALLELISM", "")
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("DB_MAX_CONNS", "")
	t.Setenv("APP_ENV", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.DBMaxConns != 10 {
		t.Errorf("DBMaxConns = %d, want 10", cfg.DBMaxConns)
	}
	if cfg.Argon2MemoryKiB != 19456 || cfg.Argon2Iterations != 2 || cfg.Argon2Parallelism != 1 {
		t.Errorf("argon2 params = (%d, %d, %d), want (19456, 2, 1)",
			cfg.Argon2MemoryKiB, cfg.Argon2Iterations, cfg.Argon2Parallelism)
	}
	if cfg.AppEnv != EnvDevelopment {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, EnvDevelopment)
	}
}

func TestLoad_InvalidValues(t *testing.T) {
	tests := []struct {
		name, port, maxConns, appEnv, wantInErr string
	}{
		{"non-numeric port", "abc", "10", "development", "PORT"},
		{"port out of range", "70000", "10", "development", "PORT"},
		{"unknown app env", "8080", "10", "prod", "APP_ENV"},
		{"non-numeric max conns", "8080", "many", "development", "DB_MAX_CONNS"},
		{"zero max conns", "8080", "0", "development", "DB_MAX_CONNS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("DATABASE_URL", "postgres://localhost/akra")
			t.Setenv("DB_MAX_CONNS", tt.maxConns)
			t.Setenv("APP_ENV", tt.appEnv)

			_, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error message does not mention %s: %v", tt.wantInErr, err)
			}
		})
	}
}

func TestLoad_WebDirOnlyInDevelopment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("WEB_DIR", "web")

	t.Setenv("APP_ENV", "development")
	if cfg, err := Load(); err != nil || cfg.WebDir != "web" {
		t.Errorf("development: Load() = %+v, %v; want WebDir \"web\"", cfg, err)
	}

	t.Setenv("APP_ENV", "production")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "WEB_DIR") {
		t.Errorf("production with WEB_DIR: error = %v, want error mentioning WEB_DIR", err)
	}
}
