package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeResetRepo menyimpan token di memori. Token dianggap selalu belum
// kedaluwarsa; kasus kedaluwarsa diuji di uji integrasi.
type fakeResetRepo struct {
	users  map[string]User // kunci: schoolCode + "|" + email
	tokens []fakeToken
}

type fakeToken struct {
	userID string
	hash   []byte
	used   bool
}

func (f *fakeResetRepo) FindActiveUserForReset(_ context.Context, schoolCode, email string) (User, error) {
	u, ok := f.users[schoolCode+"|"+email]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (f *fakeResetRepo) CountRecentResetTokens(_ context.Context, userID string, _ time.Duration) (int, error) {
	n := 0
	for _, t := range f.tokens {
		if t.userID == userID {
			n++
		}
	}
	return n, nil
}

func (f *fakeResetRepo) CreateResetToken(_ context.Context, userID string, hash []byte, _ time.Duration) error {
	f.tokens = append(f.tokens, fakeToken{userID: userID, hash: hash})
	return nil
}

func (f *fakeResetRepo) find(hash []byte) *fakeToken {
	for i := range f.tokens {
		if bytes.Equal(f.tokens[i].hash, hash) && !f.tokens[i].used {
			return &f.tokens[i]
		}
	}
	return nil
}

func (f *fakeResetRepo) CheckResetToken(_ context.Context, hash []byte) error {
	if f.find(hash) == nil {
		return ErrInvalidResetToken
	}
	return nil
}

func (f *fakeResetRepo) ResetPassword(_ context.Context, hash []byte, _ string) (string, error) {
	t := f.find(hash)
	if t == nil {
		return "", ErrInvalidResetToken
	}
	for i := range f.tokens {
		if f.tokens[i].userID == t.userID {
			f.tokens[i].used = true
		}
	}
	return t.userID, nil
}

type fakeMailer struct {
	sent []PasswordResetEmail
	err  error
}

func (m *fakeMailer) SendPasswordReset(_ context.Context, msg PasswordResetEmail) error {
	m.sent = append(m.sent, msg)
	return m.err
}

type fakeRevoker struct{ revoked []string }

func (r *fakeRevoker) RevokeUserSessions(_ context.Context, userID string) error {
	r.revoked = append(r.revoked, userID)
	return nil
}

func newTestResetService() (*ResetService, *fakeResetRepo, *fakeMailer, *fakeRevoker) {
	repo := &fakeResetRepo{users: map[string]User{
		"sd-harapan|budi@sekolah.id": {ID: "u1", SchoolID: "s1", SchoolName: "SD Harapan", Email: "budi@sekolah.id", FullName: "Budi"},
	}}
	mailer, revoker := &fakeMailer{}, &fakeRevoker{}
	svc := NewResetService(repo, NewHasher(testParams), mailer, revoker, "https://app.akra.id",
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, repo, mailer, revoker
}

// tokenFromLink mengambil parameter token dari tautan reset.
func tokenFromLink(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("token")
}

func TestRequestReset_RegisteredUser(t *testing.T) {
	svc, repo, mailer, _ := newTestResetService()

	if err := svc.RequestReset(context.Background(), " SD-Harapan ", "Budi@Sekolah.id"); err != nil {
		t.Fatalf("RequestReset() error = %v", err)
	}
	if len(mailer.sent) != 1 || len(repo.tokens) != 1 {
		t.Fatalf("sent %d emails and stored %d tokens, want 1 and 1", len(mailer.sent), len(repo.tokens))
	}
	msg := mailer.sent[0]
	if msg.To != "budi@sekolah.id" || msg.ExpiresIn != 30*time.Minute {
		t.Errorf("email = %+v", msg)
	}
	if !strings.HasPrefix(msg.Link, "https://app.akra.id/reset-password?token=") {
		t.Errorf("link = %q", msg.Link)
	}

	token := tokenFromLink(t, msg.Link)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token %q is not 32 random bytes in base64url", token)
	}
	want := sha256.Sum256(raw)
	if !bytes.Equal(repo.tokens[0].hash, want[:]) {
		t.Error("stored value is not the SHA-256 of the token")
	}
	if bytes.Contains(repo.tokens[0].hash, []byte(token)) || bytes.Equal(repo.tokens[0].hash, raw) {
		t.Error("stored value contains the plaintext token")
	}
}

func TestRequestReset_UnknownEmailLooksTheSame(t *testing.T) {
	svc, repo, mailer, _ := newTestResetService()

	for _, tc := range [][2]string{{"sd-harapan", "siapa@sekolah.id"}, {"sd-lain", "budi@sekolah.id"}} {
		if err := svc.RequestReset(context.Background(), tc[0], tc[1]); err != nil {
			t.Errorf("RequestReset(%q, %q) error = %v, want nil", tc[0], tc[1], err)
		}
	}
	if len(mailer.sent) != 0 || len(repo.tokens) != 0 {
		t.Errorf("unknown users: sent %d emails, stored %d tokens; want none", len(mailer.sent), len(repo.tokens))
	}
}

func TestRequestReset_Limit(t *testing.T) {
	svc, repo, mailer, _ := newTestResetService()

	for range MaxResetRequests + 2 {
		if err := svc.RequestReset(context.Background(), "sd-harapan", "budi@sekolah.id"); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.tokens) != MaxResetRequests || len(mailer.sent) != MaxResetRequests {
		t.Errorf("tokens = %d, emails = %d; want %d each", len(repo.tokens), len(mailer.sent), MaxResetRequests)
	}
}

func TestRequestReset_MailerErrorIsHidden(t *testing.T) {
	svc, _, mailer, _ := newTestResetService()
	mailer.err = ErrMailerNotConfigured

	if err := svc.RequestReset(context.Background(), "sd-harapan", "budi@sekolah.id"); err != nil {
		t.Errorf("RequestReset() error = %v, want nil so registered emails are not revealed", err)
	}
}

func TestResetPassword(t *testing.T) {
	svc, _, mailer, revoker := newTestResetService()
	ctx := context.Background()
	if err := svc.RequestReset(ctx, "sd-harapan", "budi@sekolah.id"); err != nil {
		t.Fatal(err)
	}
	token := tokenFromLink(t, mailer.sent[0].Link)

	if err := svc.CheckToken(ctx, token); err != nil {
		t.Fatalf("CheckToken() error = %v", err)
	}
	if err := svc.ResetPassword(ctx, token, "pendek"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: error = %v, want ErrPasswordTooShort", err)
	}
	if err := svc.ResetPassword(ctx, token, "Password-Baru-1"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}
	if len(revoker.revoked) != 1 || revoker.revoked[0] != "u1" {
		t.Errorf("revoked sessions for %v, want [u1]", revoker.revoked)
	}
	if err := svc.ResetPassword(ctx, token, "Password-Baru-2"); !errors.Is(err, ErrInvalidResetToken) {
		t.Errorf("reused token: error = %v, want ErrInvalidResetToken", err)
	}
}

func TestResetPassword_MalformedToken(t *testing.T) {
	svc, _, _, _ := newTestResetService()
	for _, token := range []string{"", "bukan-token", strings.Repeat("A", 43) + "!", base64.RawURLEncoding.EncodeToString([]byte("pendek"))} {
		if err := svc.ResetPassword(context.Background(), token, "Password-Baru-1"); !errors.Is(err, ErrInvalidResetToken) {
			t.Errorf("token %q: error = %v, want ErrInvalidResetToken", token, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := map[string]error{
		"1234567":                ErrPasswordTooShort,
		"12345678":               nil,
		"kata sandi panjang":     nil,
		"ééééééé":                ErrPasswordTooShort, // 7 karakter walau 14 byte
		strings.Repeat("a", 256): nil,
		strings.Repeat("a", 257): ErrPasswordTooLong,
	}
	for p, want := range cases {
		if got := ValidatePassword(p); !errors.Is(got, want) {
			t.Errorf("ValidatePassword(len %d) = %v, want %v", len(p), got, want)
		}
	}
}

func TestNewMailer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, ok := NewMailer("production", logger).(UnconfiguredMailer); !ok {
		t.Error("production mailer is not UnconfiguredMailer")
	}
	for _, env := range []string{"development", "staging"} {
		if _, ok := NewMailer(env, logger).(LogMailer); !ok {
			t.Errorf("%s mailer is not LogMailer", env)
		}
	}
	if err := (UnconfiguredMailer{}).SendPasswordReset(context.Background(), PasswordResetEmail{}); !errors.Is(err, ErrMailerNotConfigured) {
		t.Errorf("UnconfiguredMailer error = %v", err)
	}
}
