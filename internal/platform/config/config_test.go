package config

import (
	"os"
	"strings"
	"testing"
	"time"
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
	setPII(t)

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
		PIIEncKeys: os.Getenv("PII_ENC_KEYS"), PIIIndexKey: os.Getenv("PII_INDEX_KEY"),
		ExamTokenKeys: os.Getenv("EXAM_TOKEN_KEYS"), ShutdownTimeout: 25 * time.Second,
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
	setPII(t)
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
			setPII(t)

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

// setPII mengisi kunci data pribadi yang wajib di staging dan production.
func setPII(t *testing.T) {
	t.Helper()
	t.Setenv("PII_ENC_KEYS", "1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	t.Setenv("PII_INDEX_KEY", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	t.Setenv("EXAM_TOKEN_KEYS", "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ=")
}

func TestLoad_ExamTokenKeys(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("WEB_DIR", "")
	t.Setenv("APP_ENV", "development")
	t.Setenv("EXAM_TOKEN_KEYS", "")
	if _, err := Load(); err != nil {
		t.Fatalf("development without exam token keys: %v", err)
	}
	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_BASE_URL", "https://app.akra.id")
	setPII(t)
	t.Setenv("EXAM_TOKEN_KEYS", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EXAM_TOKEN_KEYS") {
		t.Errorf("staging without exam token keys: error = %v, want error mentioning EXAM_TOKEN_KEYS", err)
	}
}

func TestLoad_PIIKeys(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("WEB_DIR", "")
	t.Setenv("APP_ENV", "development")
	t.Setenv("PII_ENC_KEYS", "")
	t.Setenv("PII_INDEX_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("development without keys: %v", err)
	}
	if k, err := cfg.Keyring(); err != nil || k == nil {
		t.Errorf("development Keyring() = %v, %v; want dev keyring", k, err)
	}

	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_BASE_URL", "https://app.akra.id")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PII_ENC_KEYS") {
		t.Errorf("staging without keys: error = %v, want error mentioning PII_ENC_KEYS", err)
	}

	secret := "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
	for _, tc := range []struct{ enc, idx string }{
		{"1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", ""},                                                                                            // indeks kosong
		{"", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="},                                                                                              // kunci enkripsi kosong
		{"1:" + secret[:20], "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="},                                                                              // kunci terlalu pendek
		{"0:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="},                                                // versi 0
		{"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="},                                                  // tanpa versi
		{"1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=,1:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM="}, // versi ganda
		{"1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="},                                                // indeks sama dengan kunci enkripsi
	} {
		t.Setenv("PII_ENC_KEYS", tc.enc)
		t.Setenv("PII_INDEX_KEY", tc.idx)
		_, err := Load()
		if err == nil {
			t.Errorf("PII_ENC_KEYS=%q PII_INDEX_KEY=%q: error = nil", tc.enc, tc.idx)
			continue
		}
		if strings.Contains(err.Error(), "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=") || strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks key material: %v", err)
		}
	}

	t.Setenv("PII_ENC_KEYS", "1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=, 2:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=")
	t.Setenv("PII_INDEX_KEY", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	t.Setenv("EXAM_TOKEN_KEYS", "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ=")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("staging with keys: %v", err)
	}
	k, err := cfg.Keyring()
	if err != nil || k.ActiveVersion() != 2 {
		t.Errorf("Keyring() active = %v, %v; want version 2", k, err)
	}
}

func TestLoad_Ops(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/akra")
	t.Setenv("WEB_DIR", "")
	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_BASE_URL", "https://app.akra.id")
	setPII(t)
	t.Setenv("OPS_TOKEN", "rahasia-tim-akra-yang-panjang")
	t.Setenv("ALERT_TELEGRAM_TOKEN", "123:abc")
	t.Setenv("ALERT_TELEGRAM_CHAT_ID", "-100")
	t.Setenv("ALERT_EMAIL", "tim@akra.id")
	t.Setenv("EXAM_FAULT_SAVE_PERCENT", "20")
	t.Setenv("SHUTDOWN_DRAIN_SECONDS", "5")
	t.Setenv("SHUTDOWN_TIMEOUT_SECONDS", "30")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpsToken == "" || cfg.AlertTelegramChatID != "-100" || cfg.AlertEmail != "tim@akra.id" || cfg.ExamFaultSavePercent != 20 ||
		cfg.ShutdownDrain != 5*time.Second || cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ops config = %+v", cfg)
	}
	for _, c := range []struct{ key, val, want string }{
		{"OPS_TOKEN", "pendek", "OPS_TOKEN"},
		{"ALERT_TELEGRAM_CHAT_ID", "", "ALERT_TELEGRAM"},
		{"ALERT_EMAIL", "bukan email", "ALERT_EMAIL"},
		{"EXAM_FAULT_SAVE_PERCENT", "101", "EXAM_FAULT_SAVE_PERCENT"},
		{"SHUTDOWN_DRAIN_SECONDS", "61", "SHUTDOWN_DRAIN_SECONDS"},
	} {
		old := os.Getenv(c.key)
		t.Setenv(c.key, c.val)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s=%q: error = %v", c.key, c.val, err)
		}
		t.Setenv(c.key, old)
	}
	// Simulasi gangguan tidak boleh di production.
	t.Setenv("APP_ENV", "production")
	t.Setenv("MAIL_DRIVER", "resend")
	t.Setenv("MAIL_FROM", "AKRA <no-reply@akra.id>")
	t.Setenv("RESEND_API_KEY", "re_test")
	t.Setenv("STORAGE_DRIVER", "r2")
	t.Setenv("R2_ACCOUNT_ID", "acc")
	t.Setenv("R2_BUCKET", "b")
	t.Setenv("R2_ACCESS_KEY_ID", "k")
	t.Setenv("R2_SECRET_ACCESS_KEY", "s")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "not allowed in production") {
		t.Errorf("fault injection in production: %v", err)
	}
}
