package config

import (
	"strings"
	"testing"
)

func TestLoad_Lengkap(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("APP_ENV", "production")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Config{Port: "9000", DatabaseURL: "postgres://localhost/akra", AppEnv: EnvProduction}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoad_DatabaseURLKosong(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("APP_ENV", "development")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("pesan error tidak menyebut DATABASE_URL: %v", err)
	}
}

func TestLoad_Default(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("APP_ENV", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.AppEnv != EnvDevelopment {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, EnvDevelopment)
	}
}

func TestLoad_NilaiTidakValid(t *testing.T) {
	tests := []struct {
		name, port, appEnv, wantInErr string
	}{
		{"port bukan angka", "abc", "development", "PORT"},
		{"port di luar rentang", "70000", "development", "PORT"},
		{"app env tidak dikenal", "8080", "prod", "APP_ENV"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("DATABASE_URL", "postgres://localhost/akra")
			t.Setenv("APP_ENV", tt.appEnv)

			_, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("pesan error tidak menyebut %s: %v", tt.wantInErr, err)
			}
		})
	}
}
