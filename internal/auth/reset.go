package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"
)

// Kebijakan reset password.
const (
	ResetTokenTTL = 30 * time.Minute
	// MaxResetRequests membatasi jumlah tautan reset per pengguna dalam
	// ResetTokenTTL, agar form tidak bisa dipakai untuk membanjiri email.
	MaxResetRequests  = 3
	MinPasswordLength = 8
	MaxPasswordLength = 256

	resetTokenBytes = 32
)

var (
	// ErrInvalidResetToken dikembalikan untuk token yang tidak dikenal,
	// sudah dipakai, kedaluwarsa, atau milik akun nonaktif.
	ErrInvalidResetToken = errors.New("auth: invalid or expired reset token")
	ErrPasswordTooShort  = errors.New("auth: password is too short")
	ErrPasswordTooLong   = errors.New("auth: password is too long")
)

// ResetRepository adalah akses data untuk reset password.
type ResetRepository interface {
	// FindActiveUserForReset mengembalikan ErrNotFound jika sekolah atau
	// pengguna tidak ada, atau pengguna nonaktif.
	FindActiveUserForReset(ctx context.Context, schoolCode, email string) (User, error)
	CountRecentResetTokens(ctx context.Context, userID string, window time.Duration) (int, error)
	CreateResetToken(ctx context.Context, userID string, tokenHash []byte, ttl time.Duration) error
	// CheckResetToken mengembalikan ErrInvalidResetToken jika token tidak
	// bisa dipakai.
	CheckResetToken(ctx context.Context, tokenHash []byte) error
	// ResetPassword, dalam satu transaksi, memastikan token masih berlaku,
	// mengganti hash password, dan menandai semua token pengguna sebagai
	// terpakai. Mengembalikan user_id pemilik token.
	ResetPassword(ctx context.Context, tokenHash []byte, passwordHash string) (userID string, err error)
}

// SessionRevoker menghapus semua sesi aktif milik seorang pengguna.
type SessionRevoker interface {
	RevokeUserSessions(ctx context.Context, userID string) error
}

// ResetService menjalankan alur lupa password.
type ResetService struct {
	repo     ResetRepository
	hasher   *Hasher
	mailer   Mailer
	sessions SessionRevoker
	baseURL  string
	logger   *slog.Logger
}

// NewResetService mengembalikan ResetService. baseURL adalah URL publik
// aplikasi tanpa garis miring di akhir.
func NewResetService(repo ResetRepository, hasher *Hasher, mailer Mailer, sessions SessionRevoker, baseURL string, logger *slog.Logger) *ResetService {
	return &ResetService{repo: repo, hasher: hasher, mailer: mailer, sessions: sessions, baseURL: baseURL, logger: logger}
}

// RequestReset membuat token dan mengirim tautan reset jika (schoolCode,
// email) milik pengguna aktif. Untuk pengguna yang tidak ada, nonaktif, atau
// sudah mencapai MaxResetRequests, RequestReset tetap mengembalikan nil
// agar pemanggil tidak bisa membedakannya. Gagal kirim email juga hanya
// dicatat di log karena alasan yang sama. Error hanya dikembalikan untuk
// kegagalan database.
func (s *ResetService) RequestReset(ctx context.Context, schoolCode, email string) error {
	schoolCode, email = NormalizeSchoolCode(schoolCode), NormalizeEmail(email)

	user, err := s.repo.FindActiveUserForReset(ctx, schoolCode, email)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: find user for reset: %w", err)
	}

	n, err := s.repo.CountRecentResetTokens(ctx, user.ID, ResetTokenTTL)
	if err != nil {
		return fmt.Errorf("auth: count reset tokens: %w", err)
	}
	if n >= MaxResetRequests {
		s.logger.WarnContext(ctx, "password reset request limit reached", "user_id", user.ID)
		return nil
	}

	token, hash, err := newResetToken()
	if err != nil {
		return err
	}
	if err := s.repo.CreateResetToken(ctx, user.ID, hash, ResetTokenTTL); err != nil {
		return fmt.Errorf("auth: create reset token: %w", err)
	}

	err = s.mailer.SendPasswordReset(ctx, PasswordResetEmail{
		To:         user.Email,
		FullName:   user.FullName,
		SchoolName: user.SchoolName,
		Link:       s.baseURL + "/reset-password?token=" + token,
		ExpiresIn:  ResetTokenTTL,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "send password reset email", "user_id", user.ID, "error", err)
	}
	return nil
}

// CheckToken mengembalikan ErrInvalidResetToken jika token tidak bisa
// dipakai. Dipakai untuk menampilkan form reset.
func (s *ResetService) CheckToken(ctx context.Context, token string) error {
	hash, ok := hashResetToken(token)
	if !ok {
		return ErrInvalidResetToken
	}
	return s.repo.CheckResetToken(ctx, hash)
}

// ResetPassword mengganti password pemilik token, menandai token sebagai
// terpakai, lalu menghapus semua sesi aktif pengguna tersebut.
func (s *ResetService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, ok := hashResetToken(token)
	if !ok {
		return ErrInvalidResetToken
	}
	// Periksa token dulu agar token palsu tidak memicu hashing argon2id yang mahal.
	if err := s.repo.CheckResetToken(ctx, hash); err != nil {
		return err
	}

	passwordHash, err := s.hasher.HashPassword(newPassword)
	if err != nil {
		return err
	}
	userID, err := s.repo.ResetPassword(ctx, hash, passwordHash)
	if err != nil {
		return err
	}

	if err := s.sessions.RevokeUserSessions(ctx, userID); err != nil {
		// Password sudah diganti; sesi lama tetap harus dicabut, jadi ini error.
		return fmt.Errorf("auth: revoke sessions after password reset: %w", err)
	}
	s.logger.InfoContext(ctx, "password reset completed", "user_id", userID)
	return nil
}

// ValidatePassword memeriksa aturan password baru.
func ValidatePassword(p string) error {
	switch n := utf8.RuneCountInString(p); {
	case n < MinPasswordLength:
		return ErrPasswordTooShort
	case len(p) > MaxPasswordLength:
		return ErrPasswordTooLong
	}
	return nil
}

// newResetToken membuat token acak (base64url, untuk tautan) dan SHA-256
// dari byte mentahnya (untuk database).
func newResetToken() (token string, hash []byte, err error) {
	raw := make([]byte, resetTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: generate reset token: %w", err)
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), sum[:], nil
}

// hashResetToken mengubah token dari tautan menjadi hash untuk dicari di
// database. ok bernilai false jika format token salah.
func hashResetToken(token string) (hash []byte, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != resetTokenBytes {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}
