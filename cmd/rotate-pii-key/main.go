// Command akra-rotate-pii-key mengenkripsi ulang semua data pribadi dengan
// kunci aktif (versi tertinggi di PII_ENC_KEYS), setelah kunci baru
// ditambahkan (T85b).
//
// Langkah rotasi:
//  1. Tambah kunci baru di PII_ENC_KEYS, mis. "1:lama,2:baru", lalu deploy.
//     Data baru langsung memakai versi 2; data lama tetap terbaca.
//  2. Jalankan perintah ini sampai selesai.
//  3. Setelah memastikan tidak ada lagi data versi lama, hapus kunci lama
//     dari PII_ENC_KEYS dan deploy ulang.
//
// Koneksi dan kunci hanya dari environment: DATABASE_URL, PII_ENC_KEYS,
// PII_INDEX_KEY.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/students"
)

// piiColumns adalah semua kolom terenkripsi. Paket yang menambah kolom
// ciphertext wajib menambahkan daftarnya di sini.
func piiColumns() []pii.Column {
	return students.PIIColumns()
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("akra-rotate-pii-key failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("environment variable DATABASE_URL is required")
	}
	keys, err := pii.ParseKeyring(os.Getenv("PII_ENC_KEYS"), os.Getenv("PII_INDEX_KEY"))
	if err != nil {
		return err
	}
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		return err
	}
	defer pool.Close()

	logger.Info("rotating PII columns", "active_key_version", keys.ActiveVersion(), "columns", len(piiColumns()))
	for _, c := range piiColumns() {
		n, err := pii.Rotate(ctx, pool, keys, c, 500)
		if err != nil {
			return err
		}
		logger.Info("column rotated", "column", c.AADName, "rows", n)
	}
	return nil
}
