package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"akra/internal/exam"
	"akra/internal/platform/database"
	"akra/internal/platform/mail"
	"akra/internal/platform/metrics"
)

// Nama endpoint peserta yang dicatat metrics (dipakai juga aturan alert).
const (
	EndpointLogin  = exam.EndpointLogin
	EndpointOpen   = exam.EndpointOpen
	EndpointStart  = exam.EndpointStart
	EndpointSave   = exam.EndpointSave
	EndpointSubmit = exam.EndpointSubmit
)

// Jenis kejadian (aturan alert).
const (
	RuleSaveErrors = "simpan_error"
	RuleSaveSlow   = "simpan_lambat"
	RuleAutoSubmit = "kumpul_otomatis_tertunda"
	RuleDBPool     = "koneksi_db_penuh"
)

var ruleTitles = map[string]string{
	RuleSaveErrors: "Simpan jawaban gagal > 1%",
	RuleSaveSlow:   "Simpan jawaban lambat (p95 > 1 detik)",
	RuleAutoSubmit: "Kumpul otomatis tertunda > 3 menit",
	RuleDBPool:     "Koneksi database > 80%",
}

// Ambang aturan alert (T145).
const (
	AlertWindow       = 2 * time.Minute
	AlertMinSamples   = 30 // simpan dalam jendela; di bawah ini tidak dinilai
	AlertErrorRate    = 0.01
	AlertSlowP95      = time.Second
	AlertAutoSubmit   = 3 * time.Minute
	AlertDBPool       = 0.8
	AlertQuiet        = 5 * time.Minute // kondisi baik selama ini → pulih
	AlertEvalInterval = 30 * time.Second
)

// Notifier mengirim pesan alert ke kanal tim.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// Telegram mengirim lewat Bot API.
type Telegram struct {
	Token, ChatID string
	Endpoint      string // default https://api.telegram.org
	Client        *http.Client
}

// Notify mengirim pesan ke chat.
func (t Telegram) Notify(ctx context.Context, text string) error {
	base := t.Endpoint
	if base == "" {
		base = "https://api.telegram.org"
	}
	body, _ := json.Marshal(map[string]any{"chat_id": t.ChatID, "text": text, "disable_web_page_preview": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+t.Token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ops: telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c := t.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := c.Do(req)
	if err != nil {
		// Jangan bocorkan token (ada di URL) ke log.
		return errors.New("ops: telegram send failed")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("ops: telegram status %d", res.StatusCode)
	}
	return nil
}

// Email mengirim lewat mailer aplikasi.
type Email struct {
	Mailer mail.Mailer
	To     string
}

// Notify mengirim email; baris pertama menjadi subjek.
func (e Email) Notify(ctx context.Context, text string) error {
	subject, _, _ := strings.Cut(text, "\n")
	return e.Mailer.Send(ctx, mail.Message{To: e.To, Subject: "[AKRA] " + subject, Text: text})
}

// Multi mengirim ke semua kanal; error pertama dikembalikan setelah semua
// dicoba.
type Multi []Notifier

// Notify mengirim ke semua kanal.
func (m Multi) Notify(ctx context.Context, text string) error {
	var first error
	for _, n := range m {
		if err := n.Notify(ctx, text); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// PoolStat adalah pemakaian pool koneksi database.
type PoolStat struct{ Acquired, Max int32 }

// Monitor menilai aturan alert secara berkala di setiap replika. Kejadian
// dibuka dan ditutup lewat database sehingga alert terkirim sekali.
type Monitor struct {
	DB       database.DB
	Metrics  *metrics.Registry
	Pool     func() PoolStat
	Notifier Notifier
	Env      string // staging/production, ditulis di pesan
	BaseURL  string
	Logger   *slog.Logger

	poolHigh int // penilaian berturut-turut dengan pool > 80%
}

// Check adalah hasil penilaian satu aturan.
type Check struct {
	Rule   string
	Bad    bool
	Detail string
}

// Evaluate menilai semua aturan pada now (tanpa menulis apa pun).
func (m *Monitor) Evaluate(ctx context.Context, now time.Time) ([]Check, error) {
	var out []Check
	w := m.Metrics.Recent(EndpointSave, AlertWindow)
	enough := w.Count >= AlertMinSamples
	out = append(out,
		Check{Rule: RuleSaveErrors, Bad: enough && w.ErrorRate() > AlertErrorRate,
			Detail: fmt.Sprintf("%d dari %d simpan gagal (%.1f%%) dalam 2 menit", w.Errors, w.Count, w.ErrorRate()*100)},
		Check{Rule: RuleSaveSlow, Bad: enough && w.P95 > AlertSlowP95,
			Detail: fmt.Sprintf("p95 simpan %s dari %d simpan dalam 2 menit", w.P95, w.Count)})
	n, oldest, err := exam.AutoSubmitBacklog(ctx, m.DB, now)
	if err != nil {
		return nil, err
	}
	out = append(out, Check{Rule: RuleAutoSubmit, Bad: oldest > AlertAutoSubmit,
		Detail: fmt.Sprintf("%d attempt melewati tenggat, tertua tertunda %s", n, oldest.Round(time.Second))})
	if m.Pool != nil {
		p := m.Pool()
		high := p.Max > 0 && float64(p.Acquired) > AlertDBPool*float64(p.Max)
		if high {
			m.poolHigh++
		} else {
			m.poolHigh = 0
		}
		// Dua penilaian berturut-turut (± 1 menit) agar lonjakan sesaat
		// tidak membangunkan tim.
		out = append(out, Check{Rule: RuleDBPool, Bad: m.poolHigh >= 2,
			Detail: fmt.Sprintf("%d dari %d koneksi terpakai", p.Acquired, p.Max)})
	}
	return out, nil
}

// Tick menilai aturan, membuka/menutup kejadian, dan mengirim alert.
func (m *Monitor) Tick(ctx context.Context, now time.Time) error {
	checks, err := m.Evaluate(ctx, now)
	if err != nil {
		return err
	}
	for _, c := range checks {
		if c.Bad {
			in, opened, err := OpenIncident(ctx, m.DB, c.Rule, c.Detail, now)
			if err != nil {
				return err
			}
			if opened {
				m.send(ctx, in.ID, fmt.Sprintf("🔴 %s\n%s\nLingkungan: %s\nRunbook: doc/runbook-ujian.md\nStatus: %s/status",
					KindTitle(c.Rule), c.Detail, m.Env, m.BaseURL), now)
			}
			continue
		}
		in, resolved, err := ResolveIncident(ctx, m.DB, c.Rule, AlertQuiet, now)
		if err != nil {
			return err
		}
		if resolved {
			m.send(ctx, in.ID, fmt.Sprintf("✅ Pulih: %s\nBerlangsung %s.\nLingkungan: %s", KindTitle(c.Rule),
				in.ResolvedAt.Sub(in.OpenedAt).Round(time.Second), m.Env), now)
		}
	}
	return nil
}

func (m *Monitor) send(ctx context.Context, id int64, text string, now time.Time) {
	m.Logger.ErrorContext(ctx, "ops alert", "incident_id", id, "text", text)
	if m.Notifier == nil {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.Notifier.Notify(sctx, text); err != nil {
		m.Logger.ErrorContext(ctx, "send ops alert", "incident_id", id, "error", err)
		return
	}
	if err := MarkNotified(ctx, m.DB, id, now); err != nil {
		m.Logger.ErrorContext(ctx, "mark alert notified", "incident_id", id, "error", err)
	}
}

// Run menilai aturan tiap AlertEvalInterval sampai ctx selesai.
func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(AlertEvalInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			if err := m.Tick(tctx, time.Now()); err != nil && ctx.Err() == nil {
				m.Logger.ErrorContext(ctx, "ops monitor tick", "error", err)
			}
			cancel()
		}
	}
}
