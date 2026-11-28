package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/platform/database"
	"akra/internal/platform/module"
)

func TestParseFlags(t *testing.T) {
	o, err := parseFlags([]string{"-school-code", " SD-Uji ", "-email", "Admin@SD.id", "-name", " Admin "})
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}
	if o.schoolCode != "sd-uji" || o.email != "admin@sd.id" || o.fullName != "Admin" || o.role != "admin_tu" {
		t.Errorf("options = %+v", o)
	}

	if _, err := parseFlags([]string{"-email", "a@b.id"}); err == nil || !strings.Contains(err.Error(), "-name, -school-code") {
		t.Errorf("missing flags: error = %v", err)
	}
	if _, err := parseFlags([]string{"-school-code", "x", "-email", "a@b.id", "-name", "A", "-role", "superadmin"}); err == nil {
		t.Error("unknown role: error = nil")
	}
}

// TestRun_Postgres membuat sekolah dan admin di database sungguhan. Butuh
// TEST_DATABASE_URL.
func TestRun_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("APP_ENV", "development")
	t.Setenv("WEB_DIR", "")
	t.Setenv("MAIL_DRIVER", "")
	t.Setenv("ARGON2_MEMORY_KIB", "1024")
	t.Setenv("ARGON2_ITERATIONS", "1")

	ctx := context.Background()
	args := []string{"-school-code", "test-seed", "-school-name", "Sekolah Seed", "-email", "admin@seed.id", "-name", "Admin Seed"}
	cleanup := func() {
		pool := mustPool(t, dsn)
		defer pool.Close()
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE school_id IN (SELECT id FROM schools WHERE code = 'test-seed')`)
		_, _ = pool.Exec(ctx, `DELETE FROM schools WHERE code = 'test-seed'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	var out bytes.Buffer
	env := func(string) string { return "" }
	if err := run(ctx, args, env, &out); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	for _, want := range []string{"school created: Sekolah Seed (test-seed)", "user created: admin@seed.id (role admin_tu)", "generated password"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not contain %q:\n%s", want, out.String())
		}
	}

	pool := mustPool(t, dsn)
	defer pool.Close()
	var hash, role string
	err := pool.QueryRow(ctx, `
		SELECT u.password_hash, r.code FROM users u
		JOIN schools s ON s.id = u.school_id
		JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
		WHERE s.code = 'test-seed' AND u.email = 'admin@seed.id'`).Scan(&hash, &role)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || role != "admin_tu" {
		t.Errorf("hash prefix = %q, role = %q", hash[:10], role)
	}

	// Pengguna yang sudah ada tidak ditimpa.
	err = run(ctx, args, func(string) string { return "Password-Lain-1" }, &out)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second run: error = %v, want already exists", err)
	}
	var after string
	_ = pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = 'admin@seed.id'`).Scan(&after)
	if after != hash {
		t.Error("existing user's password was changed")
	}

	// Sekolah yang sudah ada dipakai ulang tanpa -school-name.
	out.Reset()
	if err := run(ctx, []string{"-school-code", "test-seed", "-email", "guru@seed.id", "-name", "Guru", "-role", "guru"}, func(string) string { return "Password-Guru-1" }, &out); err != nil {
		t.Fatalf("second user: error = %v", err)
	}
	if !strings.Contains(out.String(), "school exists: test-seed") || strings.Contains(out.String(), "generated password") {
		t.Errorf("output = %q", out.String())
	}
}

func mustPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := database.Connect(context.Background(), dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// Peran bendahara hanya bila modul keuangan atau pendaftaran aktif (T166).
func TestRun_TreasurerNeedsModule(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("APP_ENV", "development")
	t.Setenv("WEB_DIR", "")
	t.Setenv("MAIL_DRIVER", "")
	t.Setenv("ARGON2_MEMORY_KIB", "1024")
	t.Setenv("ARGON2_ITERATIONS", "1")
	ctx := context.Background()
	cleanup := func() {
		pool := mustPool(t, dsn)
		defer pool.Close()
		_, _ = pool.Exec(ctx, `DELETE FROM user_roles WHERE user_id IN (SELECT u.id FROM users u JOIN schools s ON s.id = u.school_id WHERE s.code = 'test-seed-role')`)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE school_id IN (SELECT id FROM schools WHERE code = 'test-seed-role')`)
		_, _ = pool.Exec(ctx, `DELETE FROM school_modules WHERE school_id IN (SELECT id FROM schools WHERE code = 'test-seed-role')`)
		_, _ = pool.Exec(ctx, `DELETE FROM schools WHERE code = 'test-seed-role'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	env := func(string) string { return "" }
	var out bytes.Buffer
	if err := run(ctx, []string{"-school-code", "test-seed-role", "-school-name", "Sekolah Peran", "-email", "admin@peran.id", "-name", "Admin"}, env, &out); err != nil {
		t.Fatal(err)
	}
	pool := mustPool(t, dsn)
	defer pool.Close()
	if _, err := pool.Exec(ctx, `INSERT INTO school_modules (school_id, module, enabled)
		SELECT id, m, false FROM schools, unnest(ARRAY['billing', 'admission']) m WHERE code = 'test-seed-role'`); err != nil {
		t.Fatal(err)
	}
	treasurer := []string{"-school-code", "test-seed-role", "-email", "bendahara@peran.id", "-name", "Bendahara", "-role", "bendahara"}
	if err := run(ctx, treasurer, env, &out); err == nil || !strings.Contains(err.Error(), "needs module") {
		t.Errorf("treasurer without billing and admission: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE school_modules SET enabled = true WHERE module = 'admission' AND school_id = (SELECT id FROM schools WHERE code = 'test-seed-role')`); err != nil {
		t.Fatal(err)
	}
	var sid string
	if err := pool.QueryRow(ctx, `SELECT id FROM schools WHERE code = 'test-seed-role'`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	module.Forget(sid) // diubah langsung di database, di luar module.Save
	if err := run(ctx, treasurer, env, &out); err != nil {
		t.Errorf("treasurer with admission: %v", err)
	}
}
