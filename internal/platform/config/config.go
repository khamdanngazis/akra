// Package config memuat konfigurasi aplikasi dari environment variable.
package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"akra/internal/platform/pii"
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

	// RiverWorkers adalah jumlah job latar belakang yang berjalan bersamaan.
	RiverWorkers int

	// BaseURL adalah URL publik aplikasi tanpa garis miring di akhir, mis.
	// "https://app.akra.id". Dipakai untuk tautan di email; tidak pernah
	// diambil dari header Host request.
	BaseURL string

	// Pengiriman email. MailDriver "log" (default di luar production) hanya
	// menulis email ke log; "resend" mengirim lewat Resend HTTP API dan wajib
	// di production.
	MailDriver   string
	MailFrom     string
	ResendAPIKey string
	// GoogleMapsAPIKey (opsional) mengaktifkan "Pilih di peta" untuk titik
	// lokasi rumah (T112). Kunci browser: wajib dibatasi HTTP referrer dan
	// hanya Maps JavaScript API di Google Cloud.
	GoogleMapsAPIKey string

	// Penyimpanan file (bukti transfer). StorageDriver "local" (default di
	// luar production) menyimpan di disk StorageLocalDir; "r2" menyimpan di
	// bucket Cloudflare R2 privat dan wajib di production.
	StorageDriver     string
	StorageLocalDir   string
	R2AccountID       string
	R2Bucket          string
	R2AccessKeyID     string
	R2SecretAccessKey string

	// ClientIPHeader adalah header berisi IP klien yang diisi proxy, mis.
	// "X-Real-IP" di Railway. Kosong berarti memakai alamat koneksi.
	ClientIPHeader string

	// WebDir, jika diisi, membuat template dan aset dibaca dari disk (mis.
	// "web") alih-alih dari binary. Hanya untuk development.
	WebDir string

	// Kunci enkripsi data pribadi (T85b). PIIEncKeys berformat
	// "versi:base64,…" dan PIIIndexKey base64 32 byte. Wajib di staging dan
	// production; di development boleh kosong dan memakai pii.DevKeyring.
	PIIEncKeys  string
	PIIIndexKey string

	// ExamTokenKeys adalah kunci HMAC token attempt ujian (T139): daftar
	// kunci base64 (≥ 32 byte) dipisah koma; kunci pertama aktif, kunci
	// berikutnya tetap diterima selama rotasi. Wajib di staging dan
	// production; di development boleh kosong (kunci development).
	ExamTokenKeys string

	// Keandalan (T145). OpsToken melindungi /metrics dan /internal (kosong:
	// route itu 404). Alert ke Telegram (AlertTelegramToken + ChatID)
	// dan/atau email AlertEmail; tanpa keduanya alert hanya ditulis ke log.
	OpsToken            string
	AlertTelegramToken  string
	AlertTelegramChatID string
	AlertEmail          string
	// Kontak dukungan AKRA di pusat bantuan ujian dan panduan cetak (T154).
	// Semua opsional; tanpa kontak, sekolah diarahkan ke admin TU.
	SupportWhatsApp  string
	SupportEmail     string
	SupportHours     string // mis. "Senin–Jumat 07.00–16.00 WIB"
	SupportHoursPeak string // jam layanan musim PTS/PAS
	// ExamFaultSavePercent (0–100, hanya di luar production) membuat
	// sebagian simpan jawaban gagal 500 untuk menguji alert di staging.
	ExamFaultSavePercent int
	// ShutdownDrain: lama server tetap menerima request setelah SIGTERM
	// sambil /healthz menjawab 503, agar load balancer berhenti mengirim
	// request sebelum server berhenti. ShutdownTimeout: batas menunggu
	// request yang sedang berjalan selesai.
	ShutdownDrain   time.Duration
	ShutdownTimeout time.Duration

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

		ClientIPHeader: os.Getenv("CLIENT_IP_HEADER"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: environment variable DATABASE_URL is required")
	}

	maxConns, err := positiveInt("DB_MAX_CONNS", 10, 32)
	if err != nil {
		return Config{}, err
	}
	cfg.DBMaxConns = int32(maxConns)

	riverWorkers, err := positiveInt("RIVER_WORKERS", 4, 16)
	if err != nil {
		return Config{}, err
	}
	cfg.RiverWorkers = int(riverWorkers)

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

	cfg.MailDriver = getEnv("MAIL_DRIVER", "log")
	cfg.MailFrom = os.Getenv("MAIL_FROM")
	cfg.ResendAPIKey = os.Getenv("RESEND_API_KEY")
	cfg.GoogleMapsAPIKey = strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY"))
	switch cfg.MailDriver {
	case "log":
		if cfg.AppEnv == EnvProduction {
			return Config{}, fmt.Errorf("config: MAIL_DRIVER=log is not allowed in production (emails would be written to the log)")
		}
		if cfg.MailFrom == "" {
			cfg.MailFrom = "AKRA <no-reply@akra.local>"
		}
	case "resend":
		if cfg.ResendAPIKey == "" {
			return Config{}, fmt.Errorf("config: environment variable RESEND_API_KEY is required when MAIL_DRIVER=resend")
		}
		if cfg.MailFrom == "" {
			return Config{}, fmt.Errorf("config: environment variable MAIL_FROM is required when MAIL_DRIVER=resend")
		}
	default:
		return Config{}, fmt.Errorf("config: invalid MAIL_DRIVER %q (allowed: log, resend)", cfg.MailDriver)
	}
	if _, err := mail.ParseAddress(cfg.MailFrom); err != nil {
		return Config{}, fmt.Errorf("config: invalid MAIL_FROM %q: %w", cfg.MailFrom, err)
	}

	cfg.StorageDriver = getEnv("STORAGE_DRIVER", "local")
	switch cfg.StorageDriver {
	case "local":
		if cfg.AppEnv == EnvProduction {
			return Config{}, fmt.Errorf("config: STORAGE_DRIVER=local is not allowed in production (the container disk is not persistent)")
		}
		cfg.StorageLocalDir = getEnv("STORAGE_LOCAL_DIR", "data/storage")
	case "r2":
		cfg.R2AccountID = os.Getenv("R2_ACCOUNT_ID")
		cfg.R2Bucket = os.Getenv("R2_BUCKET")
		cfg.R2AccessKeyID = os.Getenv("R2_ACCESS_KEY_ID")
		cfg.R2SecretAccessKey = os.Getenv("R2_SECRET_ACCESS_KEY")
		for _, v := range []struct{ key, val string }{
			{"R2_ACCOUNT_ID", cfg.R2AccountID}, {"R2_BUCKET", cfg.R2Bucket},
			{"R2_ACCESS_KEY_ID", cfg.R2AccessKeyID}, {"R2_SECRET_ACCESS_KEY", cfg.R2SecretAccessKey},
		} {
			if v.val == "" {
				return Config{}, fmt.Errorf("config: environment variable %s is required when STORAGE_DRIVER=r2", v.key)
			}
		}
	default:
		return Config{}, fmt.Errorf("config: invalid STORAGE_DRIVER %q (allowed: local, r2)", cfg.StorageDriver)
	}

	cfg.PIIEncKeys = os.Getenv("PII_ENC_KEYS")
	cfg.PIIIndexKey = os.Getenv("PII_INDEX_KEY")
	if cfg.PIIEncKeys == "" && cfg.PIIIndexKey == "" {
		if cfg.AppEnv != EnvDevelopment {
			return Config{}, fmt.Errorf("config: environment variables PII_ENC_KEYS and PII_INDEX_KEY are required when APP_ENV=%s", cfg.AppEnv)
		}
	} else if _, err := pii.ParseKeyring(cfg.PIIEncKeys, cfg.PIIIndexKey); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	cfg.ExamTokenKeys = os.Getenv("EXAM_TOKEN_KEYS")
	if cfg.ExamTokenKeys == "" && cfg.AppEnv != EnvDevelopment {
		return Config{}, fmt.Errorf("config: environment variable EXAM_TOKEN_KEYS is required when APP_ENV=%s", cfg.AppEnv)
	}

	cfg.OpsToken = os.Getenv("OPS_TOKEN")
	if cfg.OpsToken != "" && len(cfg.OpsToken) < 24 {
		return Config{}, fmt.Errorf("config: OPS_TOKEN must be at least 24 characters")
	}
	cfg.AlertTelegramToken = os.Getenv("ALERT_TELEGRAM_TOKEN")
	cfg.AlertTelegramChatID = os.Getenv("ALERT_TELEGRAM_CHAT_ID")
	if (cfg.AlertTelegramToken == "") != (cfg.AlertTelegramChatID == "") {
		return Config{}, fmt.Errorf("config: ALERT_TELEGRAM_TOKEN and ALERT_TELEGRAM_CHAT_ID must be set together")
	}
	cfg.AlertEmail = os.Getenv("ALERT_EMAIL")
	if cfg.AlertEmail != "" {
		if _, err := mail.ParseAddress(cfg.AlertEmail); err != nil {
			return Config{}, fmt.Errorf("config: invalid ALERT_EMAIL %q", cfg.AlertEmail)
		}
	}
	cfg.SupportWhatsApp = strings.TrimSpace(os.Getenv("SUPPORT_WHATSAPP"))
	cfg.SupportEmail = strings.TrimSpace(os.Getenv("SUPPORT_EMAIL"))
	if cfg.SupportEmail != "" {
		if _, err := mail.ParseAddress(cfg.SupportEmail); err != nil {
			return Config{}, fmt.Errorf("config: invalid SUPPORT_EMAIL %q", cfg.SupportEmail)
		}
	}
	cfg.SupportHours = strings.TrimSpace(os.Getenv("SUPPORT_HOURS"))
	cfg.SupportHoursPeak = strings.TrimSpace(os.Getenv("SUPPORT_HOURS_PEAK"))
	if raw := os.Getenv("EXAM_FAULT_SAVE_PERCENT"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 || v > 100 {
			return Config{}, fmt.Errorf("config: EXAM_FAULT_SAVE_PERCENT must be 0-100, got %q", raw)
		}
		if v > 0 && cfg.AppEnv == EnvProduction {
			return Config{}, fmt.Errorf("config: EXAM_FAULT_SAVE_PERCENT is not allowed in production")
		}
		cfg.ExamFaultSavePercent = v
	}
	drain, err := seconds("SHUTDOWN_DRAIN_SECONDS", 0, 60)
	if err != nil {
		return Config{}, err
	}
	timeout, err := seconds("SHUTDOWN_TIMEOUT_SECONDS", 25, 120)
	if err != nil {
		return Config{}, err
	}
	cfg.ShutdownDrain, cfg.ShutdownTimeout = drain, timeout
	if timeout == 0 {
		cfg.ShutdownTimeout = 25 * time.Second
	}

	if cfg.WebDir != "" && cfg.AppEnv != EnvDevelopment {
		return Config{}, fmt.Errorf("config: WEB_DIR is only allowed when APP_ENV=development")
	}

	return cfg, nil
}

// Keyring membuat Keyring dari PIIEncKeys dan PIIIndexKey, atau kunci
// development jika keduanya kosong (hanya mungkin di development, karena
// Load menolak kondisi itu di lingkungan lain).
func (c Config) Keyring() (*pii.Keyring, error) {
	if c.PIIEncKeys == "" && c.PIIIndexKey == "" {
		return pii.DevKeyring(), nil
	}
	return pii.ParseKeyring(c.PIIEncKeys, c.PIIIndexKey)
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

// seconds membaca key sebagai jumlah detik 0..maxSec.
func seconds(key string, fallback, maxSec int) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return time.Duration(fallback) * time.Second, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 || v > maxSec {
		return 0, fmt.Errorf("config: %s must be 0-%d seconds, got %q", key, maxSec, raw)
	}
	return time.Duration(v) * time.Second, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
