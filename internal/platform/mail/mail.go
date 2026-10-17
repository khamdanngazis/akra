// Package mail mengirim email teks lewat driver yang dipilih di config:
// "log" (development, hanya ditulis ke log) atau "resend" (Resend HTTP API).
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Nama driver yang didukung (MAIL_DRIVER).
const (
	DriverLog    = "log"
	DriverResend = "resend"
)

// DefaultResendEndpoint adalah endpoint Resend untuk mengirim email.
const DefaultResendEndpoint = "https://api.resend.com/emails"

// Message adalah email teks sederhana.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Mailer mengirim email. Implementasi aman dipakai bersamaan.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Config adalah setelan pengiriman email.
type Config struct {
	Driver       string // DriverLog atau DriverResend
	From         string // mis. "AKRA <no-reply@akra.id>"
	ResendAPIKey string
}

// New memilih implementasi Mailer berdasarkan cfg.Driver.
func New(cfg Config, logger *slog.Logger) (Mailer, error) {
	switch cfg.Driver {
	case DriverLog:
		return &LogMailer{Logger: logger}, nil
	case DriverResend:
		if cfg.ResendAPIKey == "" {
			return nil, fmt.Errorf("mail: RESEND_API_KEY is required for driver %q", DriverResend)
		}
		return &ResendMailer{
			APIKey: cfg.ResendAPIKey,
			From:   cfg.From,
			Client: &http.Client{Timeout: 10 * time.Second},
		}, nil
	default:
		return nil, fmt.Errorf("mail: unknown driver %q (allowed: log, resend)", cfg.Driver)
	}
}

// LogMailer menulis email ke log alih-alih mengirimnya. Isi email (termasuk
// tautan rahasia seperti reset password) ikut tertulis, jadi jangan dipakai
// di production.
type LogMailer struct {
	Logger *slog.Logger
}

// Send menulis msg ke log dengan level WARN.
func (m *LogMailer) Send(ctx context.Context, msg Message) error {
	m.Logger.WarnContext(ctx, "email written to log instead of being sent (MAIL_DRIVER=log)",
		"to", msg.To, "subject", msg.Subject, "text", msg.Text)
	return nil
}

// ResendMailer mengirim email lewat Resend HTTP API (https://resend.com).
type ResendMailer struct {
	APIKey   string
	From     string
	Client   *http.Client
	Endpoint string // kosong berarti DefaultResendEndpoint
}

// Send mengirim msg lewat Resend. Status HTTP di luar 2xx menjadi error yang
// memuat potongan isi respons (tanpa API key).
func (m *ResendMailer) Send(ctx context.Context, msg Message) error {
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = DefaultResendEndpoint
	}
	body, err := json.Marshal(map[string]any{
		"from":    m.From,
		"to":      []string{msg.To},
		"subject": msg.Subject,
		"text":    msg.Text,
	})
	if err != nil {
		return fmt.Errorf("mail: encode resend request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mail: build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.Client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: resend: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("mail: resend: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}
