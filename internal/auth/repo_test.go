package auth

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"testing"

	"akra/internal/platform/database"
)

// TestPostgresRepository_Login menjalankan alur login ke database sungguhan.
// Butuh TEST_DATABASE_URL yang menunjuk ke database yang sudah dimigrasi.
func TestPostgresRepository_Login(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const schoolCode = "test-auth-repo"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM login_attempts WHERE school_code = $1`, schoolCode)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE school_id IN (SELECT id FROM schools WHERE code = $1)`, schoolCode)
		_, _ = pool.Exec(ctx, `DELETE FROM schools WHERE code = $1`, schoolCode)
	}
	cleanup()
	t.Cleanup(cleanup)

	h := NewHasher(testParams)
	hash, err := h.HashPassword("benar-123")
	if err != nil {
		t.Fatal(err)
	}
	var userID string
	err = pool.QueryRow(ctx, `
		WITH s AS (INSERT INTO schools (name, code) VALUES ('Sekolah Uji', $1) RETURNING id),
		     u AS (INSERT INTO users (school_id, email, password_hash, full_name)
		           SELECT id, 'budi@sekolah.id', $2, 'Budi' FROM s RETURNING id),
		     ur AS (INSERT INTO user_roles (user_id, role_id)
		            SELECT u.id, r.id FROM u, roles r WHERE r.code IN ('guru', 'bendahara'))
		SELECT id FROM u`, schoolCode, hash).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(pool)
	svc, err := NewService(repo, h)
	if err != nil {
		t.Fatal(err)
	}
	ip := netip.MustParseAddr("192.0.2.200")

	t.Run("stored hash is argon2id", func(t *testing.T) {
		var stored string
		if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if len(stored) < 10 || stored[:10] != "$argon2id$" {
			t.Errorf("password_hash = %q, want $argon2id$ prefix", stored)
		}
	})

	t.Run("correct password with mixed-case email", func(t *testing.T) {
		u, err := svc.Authenticate(ctx, "Test-Auth-Repo", "BUDI@Sekolah.id", "benar-123", ip)
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}
		if u.ID != userID || len(u.Roles) != 2 || u.Roles[0] != "bendahara" || u.Roles[1] != "guru" {
			t.Errorf("user = %+v, want id %s with roles [bendahara guru]", u, userID)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		if _, err := repo.FindUserForLogin(ctx, schoolCode, "siapa@sekolah.id"); !errors.Is(err, ErrNotFound) {
			t.Errorf("FindUserForLogin() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("sixth attempt is locked even with the correct password", func(t *testing.T) {
		for i := range MaxFailuresPerEmail {
			if _, err := svc.Authenticate(ctx, schoolCode, "budi@sekolah.id", "salah", ip); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("failure %d: error = %v", i+1, err)
			}
		}
		if _, err := svc.Authenticate(ctx, schoolCode, "budi@sekolah.id", "benar-123", ip); !errors.Is(err, ErrTooManyAttempts) {
			t.Errorf("error = %v, want ErrTooManyAttempts", err)
		}
	})
}
