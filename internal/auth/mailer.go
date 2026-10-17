package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// ErrMailerNotConfigured dikembalikan selama layanan email production belum
// dipilih (keputusan terbuka di doc/tasks_schoolos/00-index.md).
var ErrMailerNotConfigured = errors.New("auth: mailer is not configured")

// PasswordResetEmail adalah isi email reset password.
type PasswordResetEmail struct {
	To         string
	FullName   string
	SchoolName string
	Link       string
	ExpiresIn  time.Duration
}

// Mailer mengirim email ke pengguna.
type Mailer interface {
	SendPasswordReset(ctx context.Context, msg PasswordResetEmail) error
}

// NewMailer memilih Mailer sesuai lingkungan: LogMailer di development dan
// staging, UnconfiguredMailer di production sampai layanan email dipilih.
func NewMailer(appEnv string, logger *slog.Logger) Mailer {
	if appEnv == "production" {
		return UnconfiguredMailer{}
	}
	return LogMailer{logger: logger}
}

// LogMailer tidak mengirim email, hanya menulis tautan ke log. Tautan reset
// adalah rahasia, jadi jangan dipakai di production.
type LogMailer struct {
	logger *slog.Logger
}

// SendPasswordReset menulis tautan reset ke log.
func (m LogMailer) SendPasswordReset(ctx context.Context, msg PasswordResetEmail) error {
	m.logger.WarnContext(ctx, "password reset link written to log instead of email (development/staging only)",
		"to", msg.To, "link", msg.Link, "expires_in", msg.ExpiresIn.String())
	return nil
}

// UnconfiguredMailer selalu gagal dengan ErrMailerNotConfigured.
type UnconfiguredMailer struct{}

// SendPasswordReset selalu mengembalikan ErrMailerNotConfigured.
func (UnconfiguredMailer) SendPasswordReset(context.Context, PasswordResetEmail) error {
	return ErrMailerNotConfigured
}
