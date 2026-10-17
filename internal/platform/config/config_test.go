package config

import (
	"strings"
	"testing"
)

func TestLoad_AllSet(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("DB_MAX_CONNS", "20")
	t.Setenv("RIVER_WORKERS", "8")
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_BASE_URL", "https://app.akra.id/")
	t.Setenv("MAIL_DRIVER", "resend")
	t.Setenv("MAIL_FROM", "AKRA <no-reply@akra.id>")
	t.Setenv("RESEND_API_KEY", "re_test")
	t.Setenv("STORAGE_DRIVER", "r2")
	t.Setenv("R2_ACCOUNT_ID", "acc")
	t.Setenv("R2_BUCKET", "akra-private")
	t.Setenv("R2_ACCESS_KEY_ID", "key")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret")
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
		BaseURL: "https://app.akra.id", RiverWorkers: 8,
		MailDriver: "resend", MailFrom: "AKRA <no-reply@akra.id>", ResendAPIKey: "re_test",
		StorageDriver: "r2", R2AccountID: "acc", R2Bucket: "akra-private", R2AccessKeyID: "key", R2SecretAccessKey: "secret",
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
	t.Setenv("RIVER_WORKERS", "")
	t.Setenv("APP_BASE_URL", "")
	t.Setenv("MAIL_DRIVER", "")
	t.Setenv("MAIL_FROM", "")
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
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q, want http://localhost:8080", cfg.BaseURL)
	}
	if cfg.MailDriver != "log" || cfg.MailFrom != "AKRA <no-reply@akra.local>" {
		t.Errorf("mail = (%q, %q), want log driver with default sender", cfg.MailDriver, cfg.MailFrom)
	}
	if cfg.DBMaxConns != 10 {
		t.Errorf("DBMaxConns = %d, want 10", cfg.DBMaxConns)
	}
	if cfg.RiverWorkers != 4 {
		t.Errorf("RiverWorkers = %d, want 4", cfg.RiverWorkers)
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
	t.Setenv("APP_BASE_URL", "https://app.akra.id")
	t.Setenv("MAIL_DRIVER", "resend")
	t.Setenv("MAIL_FROM", "AKRA <no-reply@akra.id>")
	t.Setenv("RESEND_API_KEY", "re_test")
	setR2(t)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "WEB_DIR") {
		t.Errorf("production with WEB_DIR: error = %v, want error mentioning WEB_DIR", err)
	}
}

func TestLoad_BaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("WEB_DIR", "")

	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_BASE_URL", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APP_BASE_URL") {
		t.Errorf("staging without APP_BASE_URL: error = %v, want error", err)
	}

	for _, bad := range []string{"app.akra.id", "ftp://app.akra.id", "https://app.akra.id/sub", "https://"} {
		t.Setenv("APP_BASE_URL", bad)
		if _, err := Load(); err == nil {
			t.Errorf("APP_BASE_URL=%q: error = nil, want error", bad)
		}
	}
}

func TestLoad_Mail(t *testing.T) {
	cases := []struct {
		name, env, driver, from, key, wantInErr string
	}{
		{"log in production", "production", "log", "", "", "MAIL_DRIVER=log"},
		{"resend without key", "staging", "resend", "AKRA <a@akra.id>", "", "RESEND_API_KEY"},
		{"resend without sender", "staging", "resend", "", "re_x", "MAIL_FROM"},
		{"unknown driver", "development", "smtp", "", "", "MAIL_DRIVER"},
		{"invalid sender", "development", "log", "bukan email", "", "MAIL_FROM"},
		{"resend in production", "production", "resend", "AKRA <a@akra.id>", "re_x", ""},
		{"log in staging", "staging", "log", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/akra")
			t.Setenv("WEB_DIR", "")
			t.Setenv("APP_ENV", tc.env)
			t.Setenv("APP_BASE_URL", "https://app.akra.id")
			t.Setenv("MAIL_DRIVER", tc.driver)
			t.Setenv("MAIL_FROM", tc.from)
			t.Setenv("RESEND_API_KEY", tc.key)
			setR2(t)

			_, err := Load()
			if tc.wantInErr == "" {
				if err != nil {
					t.Errorf("Load() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("Load() error = %v, want error mentioning %s", err, tc.wantInErr)
			}
		})
	}
}

func TestLoad_RiverWorkers(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("APP_ENV", "development")
	t.Setenv("WEB_DIR", "")
	for _, bad := range []string{"0", "-1", "banyak"} {
		t.Setenv("RIVER_WORKERS", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RIVER_WORKERS") {
			t.Errorf("RIVER_WORKERS=%q: error = %v", bad, err)
		}
	}
	t.Setenv("RIVER_WORKERS", "2")
	if cfg, err := Load(); err != nil || cfg.RiverWorkers != 2 {
		t.Errorf("RIVER_WORKERS=2: %+v, %v", cfg.RiverWorkers, err)
	}
}

func TestLoad_Storage(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("APP_ENV", "development")
	t.Setenv("WEB_DIR", "")
	t.Setenv("STORAGE_DRIVER", "")
	t.Setenv("STORAGE_LOCAL_DIR", "")
	if cfg, err := Load(); err != nil || cfg.StorageDriver != "local" || cfg.StorageLocalDir != "data/storage" {
		t.Errorf("default storage = (%q, %q), %v", cfg.StorageDriver, cfg.StorageLocalDir, err)
	}
	t.Setenv("STORAGE_DRIVER", "s3")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STORAGE_DRIVER") {
		t.Errorf("unknown driver: error = %v", err)
	}
	t.Setenv("STORAGE_DRIVER", "r2")
	t.Setenv("R2_ACCOUNT_ID", "acc")
	t.Setenv("R2_BUCKET", "b")
	t.Setenv("R2_ACCESS_KEY_ID", "k")
	t.Setenv("R2_SECRET_ACCESS_KEY", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "R2_SECRET_ACCESS_KEY") {
		t.Errorf("missing secret: error = %v", err)
	}

	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_BASE_URL", "https://app.akra.id")
	t.Setenv("MAIL_DRIVER", "resend")
	t.Setenv("MAIL_FROM", "AKRA <no-reply@akra.id>")
	t.Setenv("RESEND_API_KEY", "re_test")
	t.Setenv("STORAGE_DRIVER", "local")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STORAGE_DRIVER=local") {
		t.Errorf("local in production: error = %v", err)
	}
}

// setR2 mengisi konfigurasi R2 yang wajib di production.
func setR2(t *testing.T) {
	t.Helper()
	t.Setenv("STORAGE_DRIVER", "r2")
	t.Setenv("R2_ACCOUNT_ID", "acc")
	t.Setenv("R2_BUCKET", "akra-private")
	t.Setenv("R2_ACCESS_KEY_ID", "key")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret")
}
