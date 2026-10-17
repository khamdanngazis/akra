package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"testing"

	"github.com/alexedwards/scs/pgxstore"

	"akra/internal/platform/database"
)

// TestPasswordReset_Postgres menjalankan alur reset ke database sungguhan,
// termasuk sesi di pgxstore. Butuh TEST_DATABASE_URL.
func TestPasswordReset_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const schoolCode = "test-auth-reset"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM login_attempts WHERE school_code = $1`, schoolCode)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE school_id IN (SELECT id FROM schools WHERE code = $1)`, schoolCode)
		_, _ = pool.Exec(ctx, `DELETE FROM schools WHERE code = $1`, schoolCode)
	}
	cleanup()
	t.Cleanup(cleanup)

	hasher := NewHasher(testParams)
	oldHash, _ := hasher.HashPassword("Lama-12345")
	var budiID, aniID string
	err = pool.QueryRow(ctx, `
		WITH s AS (INSERT INTO schools (name, code) VALUES ('Sekolah Reset', $1) RETURNING id)
		INSERT INTO users (school_id, email, password_hash, full_name)
		SELECT id, e, $2, 'Uji' FROM s, unnest(ARRAY['budi@sekolah.id', 'ani@sekolah.id']) AS e
		RETURNING id`, schoolCode, oldHash).Scan(&budiID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT u.id FROM users u JOIN schools s ON s.id = u.school_id WHERE s.code = $1 AND u.email = 'ani@sekolah.id'`, schoolCode).Scan(&aniID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT u.id FROM users u JOIN schools s ON s.id = u.school_id WHERE s.code = $1 AND u.email = 'budi@sekolah.id'`, schoolCode).Scan(&budiID); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sm := NewSessionManager(pgxstore.NewWithCleanupInterval(pool, 0), false, logger)
	sessionOf := func(userID string) string {
		h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sm.Put(r.Context(), sessionKeyUserID, userID)
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		return rec.Result().Cookies()[0].Value
	}
	budiSessions := []string{sessionOf(budiID), sessionOf(budiID)}
	aniSession := sessionOf(aniID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE token = ANY($1)`, append(budiSessions, aniSession))
	})

	repo := NewRepository(pool)
	mailer := &fakeMailer{}
	svc := NewResetService(repo, hasher, mailer, NewSessionRevoker(sm), "http://localhost:8080", logger)
	request := func() string {
		t.Helper()
		before := len(mailer.sent)
		if err := svc.RequestReset(ctx, schoolCode, "budi@sekolah.id"); err != nil {
			t.Fatal(err)
		}
		if len(mailer.sent) != before+1 {
			t.Fatal("no reset email sent")
		}
		return tokenFromLink(t, linkFromMessage(t, mailer.sent[len(mailer.sent)-1]))
	}

	token := request()

	t.Run("token stored as SHA-256 hash", func(t *testing.T) {
		raw, _ := base64.RawURLEncoding.DecodeString(token)
		var stored []byte
		if err := pool.QueryRow(ctx, `SELECT token_hash FROM password_reset_tokens WHERE user_id = $1`, budiID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		want := sha256.Sum256(raw)
		if !bytes.Equal(stored, want[:]) || bytes.Equal(stored, raw) || bytes.Contains(stored, []byte(token)) {
			t.Errorf("stored token_hash %x is not SHA-256 of the token", stored)
		}
	})

	t.Run("reset changes password and revokes sessions", func(t *testing.T) {
		if err := svc.ResetPassword(ctx, token, "Baru-12345"); err != nil {
			t.Fatalf("ResetPassword() error = %v", err)
		}

		loginSvc, err := NewService(repo, hasher)
		if err != nil {
			t.Fatal(err)
		}
		ip := netip.MustParseAddr("192.0.2.77")
		if _, err := loginSvc.Authenticate(ctx, schoolCode, "budi@sekolah.id", "Lama-12345", ip); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("old password: error = %v, want ErrInvalidCredentials", err)
		}
		if _, err := loginSvc.Authenticate(ctx, schoolCode, "budi@sekolah.id", "Baru-12345", ip); err != nil {
			t.Errorf("new password: error = %v, want nil", err)
		}

		var budiLeft, aniLeft int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token = ANY($1)`, budiSessions).Scan(&budiLeft)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token = $1`, aniSession).Scan(&aniLeft)
		if budiLeft != 0 {
			t.Errorf("%d sessions of the reset user remain, want 0", budiLeft)
		}
		if aniLeft != 1 {
			t.Errorf("another user's session was removed")
		}
	})

	t.Run("used token is rejected", func(t *testing.T) {
		if err := svc.ResetPassword(ctx, token, "Lagi-12345"); !errors.Is(err, ErrInvalidResetToken) {
			t.Errorf("error = %v, want ErrInvalidResetToken", err)
		}
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		expired := request()
		hash, _ := hashResetToken(expired)
		if _, err := pool.Exec(ctx, `UPDATE password_reset_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, hash); err != nil {
			t.Fatal(err)
		}
		if err := svc.CheckToken(ctx, expired); !errors.Is(err, ErrInvalidResetToken) {
			t.Errorf("CheckToken() error = %v, want ErrInvalidResetToken", err)
		}
		if err := svc.ResetPassword(ctx, expired, "Lagi-12345"); !errors.Is(err, ErrInvalidResetToken) {
			t.Errorf("ResetPassword() error = %v, want ErrInvalidResetToken", err)
		}
	})

	t.Run("expiry is 30 minutes", func(t *testing.T) {
		var minutes float64
		err := pool.QueryRow(ctx, `SELECT extract(epoch FROM expires_at - created_at) / 60 FROM password_reset_tokens WHERE user_id = $1 ORDER BY created_at LIMIT 1`, budiID).Scan(&minutes)
		if err != nil || minutes != 30 {
			t.Errorf("expires_at - created_at = %v minutes (err %v), want 30", minutes, err)
		}
	})

	t.Run("older tokens die with a successful reset", func(t *testing.T) {
		_, _ = pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, budiID)
		first, second := request(), request()
		if err := svc.ResetPassword(ctx, second, "Kedua-12345"); err != nil {
			t.Fatal(err)
		}
		if err := svc.CheckToken(ctx, first); !errors.Is(err, ErrInvalidResetToken) {
			t.Errorf("older token: error = %v, want ErrInvalidResetToken", err)
		}
	})
}
