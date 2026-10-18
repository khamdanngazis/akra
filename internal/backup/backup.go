// Package backup membuat backup database harian ke R2 dan memulihkannya
// (T62). pg_dump dan pg_restore dijalankan sebagai proses terpisah; DSN
// diteruskan lewat variabel lingkungan PG*, tidak pernah lewat argumen
// proses atau log.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/clock"
	"akra/internal/platform/storage"
)

const (
	// Prefix adalah awalan kunci backup di R2.
	Prefix = "backups/"
	// FileName adalah nama berkas backup di setiap folder tanggal.
	FileName = "schoolos.dump"
	// DefaultRetention adalah jumlah backup harian yang disimpan.
	DefaultRetention = 14
)

// Store adalah penyimpanan objek yang dibutuhkan backup (dipenuhi
// *storage.R2).
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]storage.Object, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// ErrNoBackup: belum ada backup di R2.
var ErrNoBackup = errors.New("backup: no backup found")

// ErrTargetNotEmpty: database tujuan pemulihan sudah berisi tabel.
var ErrTargetNotEmpty = errors.New("backup: target database is not empty")

// Key mengembalikan kunci backup untuk tanggal day (Asia/Jakarta), mis.
// "backups/2026-10-18/schoolos.dump".
func Key(day time.Time) string {
	return Prefix + day.In(clock.Jakarta).Format(time.DateOnly) + "/" + FileName
}

var sslmodeRe = regexp.MustCompile(`sslmode=([a-z-]+)`)

// pgEnv mengubah DSN menjadi variabel lingkungan libpq (PGHOST, PGPASSWORD,
// dst.) sehingga DSN tidak muncul di argumen proses (terlihat di ps) atau
// pesan kesalahan.
func pgEnv(dsn string) ([]string, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		// Pesan asli bisa memuat DSN; jangan diteruskan.
		return nil, errors.New("backup: invalid database URL")
	}
	env := []string{
		"PGHOST=" + cfg.Host,
		fmt.Sprintf("PGPORT=%d", cfg.Port),
		"PGUSER=" + cfg.User,
		"PGPASSWORD=" + cfg.Password,
		"PGDATABASE=" + cfg.Database,
		"PGCONNECT_TIMEOUT=30",
	}
	sslmode := "prefer"
	if m := sslmodeRe.FindStringSubmatch(dsn); m != nil {
		sslmode = m[1]
	}
	return append(env, "PGSSLMODE="+sslmode), nil
}

// runPG menjalankan program pg_dump/pg_restore dengan env koneksi dsn.
// stderr dikembalikan dalam pesan kesalahan setelah password disamarkan.
func runPG(ctx context.Context, dsn, prog string, args []string, stdin io.Reader, stdout io.Writer) error {
	env, err := pgEnv(dsn)
	if err != nil {
		return err
	}
	cfg, _ := pgconn.ParseConfig(dsn)
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Env = append(baseEnv(), env...)
	cmd.Stdin, cmd.Stdout = stdin, stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if cfg != nil && cfg.Password != "" {
			msg = strings.ReplaceAll(msg, cfg.Password, "***")
		}
		return fmt.Errorf("backup: %s failed: %v: %s", prog, err, msg)
	}
	return nil
}

// baseEnv adalah lingkungan proses tanpa variabel yang memuat DSN atau
// kredensial R2.
func baseEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "PG") || strings.HasPrefix(name, "R2_") || strings.Contains(name, "DATABASE_URL") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Result adalah ringkasan satu backup.
type Result struct {
	Key      string
	Size     int64
	Duration time.Duration
	Deleted  []string // backup lama yang dihapus (retensi)
}

// Run membuat backup format custom (pg_dump -Fc) dari dsn, mengunggahnya ke
// Key(now), lalu menghapus backup lama sehingga tersisa retention backup
// harian terbaru.
func Run(ctx context.Context, store Store, dsn string, now time.Time, retention int, logger *slog.Logger) (Result, error) {
	start := time.Now()
	res := Result{Key: Key(now)}
	f, err := os.CreateTemp("", "akra-backup-*.dump")
	if err != nil {
		return res, fmt.Errorf("backup: temp file: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	// --no-owner/--no-privileges: dump bisa dipulihkan ke database dengan
	// nama peran berbeda (mis. instance Postgres lain di Railway).
	if err := runPG(ctx, dsn, "pg_dump", []string{"--format=custom", "--no-owner", "--no-privileges", "--compress=6"}, nil, f); err != nil {
		return res, err
	}
	info, err := f.Stat()
	if err != nil {
		return res, err
	}
	res.Size = info.Size()
	if res.Size == 0 {
		return res, errors.New("backup: pg_dump produced an empty file")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return res, err
	}
	if err := store.Put(ctx, res.Key, f, res.Size, "application/octet-stream"); err != nil {
		return res, err
	}
	logger.Info("backup uploaded", "key", res.Key, "bytes", res.Size)

	if res.Deleted, err = applyRetention(ctx, store, retention); err != nil {
		return res, err
	}
	res.Duration = time.Since(start)
	return res, nil
}

// applyRetention menyisakan retention folder tanggal terbaru di Prefix dan
// menghapus objek di folder yang lebih lama.
func applyRetention(ctx context.Context, store Store, retention int) ([]string, error) {
	if retention < 1 {
		return nil, fmt.Errorf("backup: retention must be at least 1, got %d", retention)
	}
	objs, err := store.List(ctx, Prefix)
	if err != nil {
		return nil, err
	}
	days := map[string]bool{}
	for _, o := range objs {
		if d := dayOf(o.Key); d != "" {
			days[d] = true
		}
	}
	sorted := make([]string, 0, len(days))
	for d := range days {
		sorted = append(sorted, d)
	}
	slices.Sort(sorted)
	if len(sorted) <= retention {
		return nil, nil
	}
	old := map[string]bool{}
	for _, d := range sorted[:len(sorted)-retention] {
		old[d] = true
	}
	var deleted []string
	for _, o := range objs {
		if old[dayOf(o.Key)] {
			if err := store.Delete(ctx, o.Key); err != nil {
				return deleted, err
			}
			deleted = append(deleted, o.Key)
		}
	}
	return deleted, nil
}

// dayOf mengembalikan folder tanggal kunci backup ("2026-10-18"), atau ""
// jika kunci bukan backup.
func dayOf(key string) string {
	rest, ok := strings.CutPrefix(key, Prefix)
	if !ok {
		return ""
	}
	day, _, ok := strings.Cut(rest, "/")
	if !ok {
		return ""
	}
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return ""
	}
	return day
}

// Latest mengembalikan kunci backup terbaru.
func Latest(ctx context.Context, store Store) (string, error) {
	objs, err := store.List(ctx, Prefix)
	if err != nil {
		return "", err
	}
	var keys []string
	for _, o := range objs {
		if dayOf(o.Key) != "" && path.Base(o.Key) == FileName {
			keys = append(keys, o.Key)
		}
	}
	if len(keys) == 0 {
		return "", ErrNoBackup
	}
	slices.Sort(keys)
	return keys[len(keys)-1], nil
}
