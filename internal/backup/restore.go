package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Restore mengunduh backup key dari store dan memulihkannya ke database
// targetDSN, yang harus kosong (belum ada tabel di skema public). Jika key
// kosong, backup terbaru dipakai. Mengembalikan kunci yang dipulihkan.
func Restore(ctx context.Context, store Store, key, targetDSN string) (string, error) {
	if key == "" {
		var err error
		if key, err = Latest(ctx, store); err != nil {
			return "", err
		}
	}
	if err := checkEmpty(ctx, targetDSN); err != nil {
		return key, err
	}
	f, err := os.CreateTemp("", "akra-restore-*.dump")
	if err != nil {
		return key, fmt.Errorf("backup: temp file: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	body, err := store.Get(ctx, key)
	if err != nil {
		return key, err
	}
	_, err = io.Copy(f, body)
	body.Close()
	if err != nil {
		return key, fmt.Errorf("backup: download %s: %w", key, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return key, err
	}
	// --single-transaction: gagal di tengah berarti tidak ada yang tersisa.
	args := []string{"--no-owner", "--no-privileges", "--exit-on-error", "--single-transaction"}
	if err := runPG(ctx, targetDSN, "pg_restore", append(args, "--dbname="+mustDB(targetDSN)), f, io.Discard); err != nil {
		return key, err
	}
	return key, nil
}

// mustDB mengembalikan nama database dari DSN untuk --dbname (bukan DSN
// lengkap, agar password tidak masuk argumen proses). Koneksi lainnya dari
// variabel PG*.
func mustDB(dsn string) string {
	env, _ := pgEnv(dsn)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PGDATABASE="); ok {
			return v
		}
	}
	return ""
}

func checkEmpty(ctx context.Context, dsn string) error {
	conn, err := connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&n); err != nil {
		return fmt.Errorf("backup: check target: %w", err)
	}
	if n > 0 {
		return ErrTargetNotEmpty
	}
	return nil
}

func connect(ctx context.Context, dsn string) (*pgx.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		// Pesan pgx bisa memuat host dan user, tetapi tidak password.
		return nil, fmt.Errorf("backup: connect: %w", err)
	}
	return conn, nil
}

// CountTables adalah tabel utama yang dibandingkan setelah pemulihan.
var CountTables = []string{
	"schools", "users", "academic_years", "classes", "students", "enrollments", "guardians",
	"fee_types", "invoices", "invoice_items", "payments", "payment_voids", "payment_proofs",
	"attendance_sessions", "attendance_records", "announcements", "audit_logs", "import_batches",
}

// Counts mengembalikan jumlah baris CountTables dan versi migrasi.
func Counts(ctx context.Context, dsn string) (map[string]int64, error) {
	conn, err := connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	out := map[string]int64{}
	for _, t := range CountTables {
		var n int64
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{t}.Sanitize()).Scan(&n); err != nil {
			return nil, fmt.Errorf("backup: count %s: %w", t, err)
		}
		out[t] = n
	}
	var version int64
	if err := conn.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		return nil, fmt.Errorf("backup: migration version: %w", err)
	}
	out["schema_migrations.version"] = version
	return out, nil
}
