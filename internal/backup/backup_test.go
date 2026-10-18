package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/storage"
)

// memStore adalah Store di memori untuk uji.
type memStore struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func newMemStore() *memStore { return &memStore{objs: map[string][]byte{}} }

func (m *memStore) Put(_ context.Context, key string, body io.Reader, size int64, _ string) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return errors.New("size mismatch")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = b
	return nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *memStore) List(_ context.Context, prefix string) ([]storage.Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []storage.Object
	for k, v := range m.objs {
		if strings.HasPrefix(k, prefix) {
			out = append(out, storage.Object{Key: k, Size: int64(len(v))})
		}
	}
	return out, nil
}

func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var k []string
	for key := range m.objs {
		k = append(k, key)
	}
	slices.Sort(k)
	return k
}

func TestKey_UsesJakartaDate(t *testing.T) {
	// 18.00 UTC = 01.00 WIB hari berikutnya: jadwal harian 01.00 WIB.
	if got := Key(time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)); got != "backups/2026-10-19/schoolos.dump" {
		t.Errorf("Key = %q", got)
	}
}

func TestRetention_KeepsNewestDays(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range 17 {
		_ = s.Put(ctx, Prefix+day.AddDate(0, 0, i).Format(time.DateOnly)+"/"+FileName, strings.NewReader("x"), 1, "")
	}
	// Bukan backup: tidak boleh tersentuh.
	for _, k := range []string{"schools/s1/proofs/a.png", "backups/catatan.txt", "backups/bukan-tanggal/schoolos.dump"} {
		_ = s.Put(ctx, k, strings.NewReader("x"), 1, "")
	}
	deleted, err := applyRetention(ctx, s, 14)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(deleted)
	want := []string{"backups/2026-10-01/schoolos.dump", "backups/2026-10-02/schoolos.dump", "backups/2026-10-03/schoolos.dump"}
	if !slices.Equal(deleted, want) {
		t.Errorf("deleted = %v", deleted)
	}
	if n := len(s.keys()); n != 14+3 {
		t.Errorf("objects left = %d", n)
	}
	latest, err := Latest(ctx, s)
	if err != nil || latest != "backups/2026-10-17/schoolos.dump" {
		t.Errorf("Latest = %q, %v", latest, err)
	}
	if _, err := Latest(ctx, newMemStore()); !errors.Is(err, ErrNoBackup) {
		t.Errorf("empty store: %v", err)
	}
	if _, err := applyRetention(ctx, s, 0); err == nil {
		t.Error("retention 0 must be rejected")
	}
}

func TestPGEnv_NoDSNInArgsOrErrors(t *testing.T) {
	dsn := "postgres://akra:S3cret-Pass@db.internal:6543/akra_prod?sslmode=require"
	env, err := pgEnv(dsn)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"PGHOST=db.internal": true, "PGPORT=6543": true, "PGUSER=akra": true, "PGPASSWORD=S3cret-Pass": true,
		"PGDATABASE=akra_prod": true, "PGSSLMODE=require": true}
	for _, kv := range env {
		delete(want, kv)
	}
	if len(want) != 0 {
		t.Errorf("missing env %v in %v", want, env)
	}
	if mustDB(dsn) != "akra_prod" {
		t.Errorf("dbname = %q", mustDB(dsn))
	}
	if _, err := pgEnv("postgres://akra:S3cret-Pass@bad host:x/y"); err == nil || strings.Contains(err.Error(), "S3cret") {
		t.Errorf("invalid DSN error leaks: %v", err)
	}
	// Kegagalan pg_dump tidak memuat password.
	err = runPG(context.Background(), dsn, "sh", []string{"-c", `echo "password $PGPASSWORD rejected" >&2; exit 1`}, nil, io.Discard)
	if err == nil || strings.Contains(err.Error(), "S3cret-Pass") || !strings.Contains(err.Error(), "***") {
		t.Errorf("error = %v", err)
	}
	// Proses anak tidak mewarisi DATABASE_URL atau kredensial R2.
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("R2_SECRET_ACCESS_KEY", "r2-secret")
	var out bytes.Buffer
	if err := runPG(context.Background(), dsn, "env", nil, nil, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "DATABASE_URL") || strings.Contains(out.String(), "r2-secret") {
		t.Errorf("child env leaks: %s", out.String())
	}
}

// pgToolsMatch melaporkan apakah pg_dump dan pg_restore ada dan versinya
// tidak lebih lama dari server.
func pgToolsMatch(t *testing.T, conn *pgx.Conn) bool {
	t.Helper()
	var serverNum int
	if err := conn.QueryRow(context.Background(), `SELECT current_setting('server_version_num')::int`).Scan(&serverNum); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"pg_dump", "pg_restore"} {
		out, err := exec.Command(tool, "--version").Output()
		if err != nil {
			t.Logf("%s not found", tool)
			return false
		}
		m := regexp.MustCompile(`(\d+)\.`).FindStringSubmatch(string(out))
		if m == nil {
			return false
		}
		if major, _ := strconv.Atoi(m[1]); major < serverNum/10000 {
			t.Logf("%s %d is older than server %d", tool, major, serverNum/10000)
			return false
		}
	}
	return true
}

// Backup dari database uji dipulihkan ke database baru yang kosong, dan
// jumlah baris tabel utama sama.
func TestBackupAndRestore(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Didaftarkan lebih dulu agar ditutup setelah database uji dihapus.
	t.Cleanup(func() { admin.Close(ctx) })
	if !pgToolsMatch(t, admin) {
		t.Skip("pg_dump/pg_restore not available in a compatible version")
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name := "akra_restore_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Skipf("cannot create a database for the restore test: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	target := strings.Replace(dsn, "/"+cfg.Database+"?", "/"+name+"?", 1)
	if target == dsn {
		t.Skip("cannot derive target DSN")
	}

	store := newMemStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	res, err := Run(ctx, store, dsn, time.Now(), 14, logger)
	if err != nil {
		t.Fatal(err)
	}
	if res.Size == 0 || res.Key != Key(time.Now()) || len(store.keys()) != 1 {
		t.Fatalf("result = %+v, keys %v", res, store.keys())
	}
	if !bytes.HasPrefix(store.objs[res.Key], []byte("PGDMP")) {
		t.Error("backup is not a pg_dump custom-format archive")
	}

	restored, err := Restore(ctx, store, "", target)
	if err != nil {
		t.Fatal(err)
	}
	if restored != res.Key {
		t.Errorf("restored %q", restored)
	}
	src, err := Counts(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := Counts(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range src {
		if dst[k] != v {
			t.Errorf("%s: source %d, restored %d", k, v, dst[k])
		}
	}
	// Database yang sudah berisi ditolak.
	if _, err := Restore(ctx, store, "", target); !errors.Is(err, ErrTargetNotEmpty) {
		t.Errorf("restore into non-empty database: %v", err)
	}
}
