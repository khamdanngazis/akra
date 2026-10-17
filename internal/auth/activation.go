package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/mail"
)

// ActivationTokenTTL adalah masa berlaku tautan aktivasi akun.
const ActivationTokenTTL = 7 * 24 * time.Hour

// ActionAccountActivate adalah aksi audit saat akun orang tua diaktivasi.
const ActionAccountActivate = "guardian_account.activate"

// ErrInvalidActivationToken dikembalikan untuk token yang tidak dikenal,
// sudah dipakai, kedaluwarsa, atau akunnya sudah aktif.
var ErrInvalidActivationToken = errors.New("auth: invalid or expired activation token")

// Invitee adalah penerima undangan aktivasi akun.
type Invitee struct {
	UserID     string
	Email      string
	FullName   string
	SchoolName string
	SchoolCode string
}

// ActivationService menerbitkan dan memproses tautan aktivasi akun yang
// dibuat tanpa password (akun orang tua).
type ActivationService struct {
	db      database.DB
	hasher  *Hasher
	mailer  mail.Mailer
	baseURL string
	logger  *slog.Logger
}

// NewActivationService mengembalikan ActivationService.
func NewActivationService(db database.DB, hasher *Hasher, mailer mail.Mailer, baseURL string, logger *slog.Logger) *ActivationService {
	return &ActivationService{db: db, hasher: hasher, mailer: mailer, baseURL: baseURL, logger: logger}
}

// IssueToken membuat token aktivasi baru untuk userID di dalam transaksi
// tx. Token lama yang belum dipakai ikut dibatalkan, jadi hanya tautan
// terbaru yang berlaku.
func (s *ActivationService) IssueToken(ctx context.Context, tx pgx.Tx, userID string) (string, error) {
	token, hash, err := newResetToken()
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE activation_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
		return "", fmt.Errorf("auth: revoke activation tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO activation_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + make_interval(secs => $3))`, userID, hash, ActivationTokenTTL.Seconds()); err != nil {
		return "", fmt.Errorf("auth: create activation token: %w", err)
	}
	return token, nil
}

// SendInvite mengirim email berisi tautan aktivasi. Dipanggil setelah
// transaksi pembuatan akun selesai.
func (s *ActivationService) SendInvite(ctx context.Context, inv Invitee, token string) error {
	link := s.baseURL + "/aktivasi?token=" + token
	return s.mailer.Send(ctx, mail.Message{
		To:      inv.Email,
		Subject: "Aktivasi akun portal orang tua AKRA",
		Text: fmt.Sprintf(`Halo %s,

%s membuatkan akun portal orang tua AKRA untuk Anda. Lewat portal ini Anda bisa melihat data anak, absensi, dan tagihan.

Buka tautan berikut untuk membuat password:
%s

Tautan berlaku %d hari dan hanya bisa dipakai sekali.

Setelah itu, masuk di %s/login dengan:
  Kode sekolah : %s
  Email        : %s

Jika Anda merasa tidak terkait dengan sekolah ini, abaikan email ini.

Salam,
AKRA
`, inv.FullName, inv.SchoolName, link, int(ActivationTokenTTL.Hours()/24), s.baseURL, inv.SchoolCode, inv.Email),
	})
}

// ActivationTarget adalah akun yang akan diaktivasi lewat token.
type ActivationTarget struct {
	UserID     string
	SchoolID   string
	SchoolCode string
	Email      string
	FullName   string
}

// Token bisa dipakai jika belum dipakai, belum kedaluwarsa, dan akunnya
// belum pernah diaktivasi (password masih kosong).
const usableActivationSQL = `
FROM activation_tokens t
JOIN users u ON u.id = t.user_id
JOIN schools s ON s.id = u.school_id
WHERE t.token_hash = $1 AND t.used_at IS NULL AND t.expires_at > now()
  AND NOT u.is_active AND u.password_hash = ''`

// CheckToken mengembalikan akun pemilik token, atau ErrInvalidActivationToken.
func (s *ActivationService) CheckToken(ctx context.Context, token string) (ActivationTarget, error) {
	hash, ok := hashResetToken(token)
	if !ok {
		return ActivationTarget{}, ErrInvalidActivationToken
	}
	return findActivationTarget(ctx, s.db, hash, false)
}

func findActivationTarget(ctx context.Context, q database.DBTX, hash []byte, lock bool) (ActivationTarget, error) {
	sql := `SELECT u.id, u.school_id, s.code, u.email, u.full_name ` + usableActivationSQL
	if lock {
		sql += ` FOR UPDATE OF t, u`
	}
	var t ActivationTarget
	err := q.QueryRow(ctx, sql, hash).Scan(&t.UserID, &t.SchoolID, &t.SchoolCode, &t.Email, &t.FullName)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActivationTarget{}, ErrInvalidActivationToken
	}
	if err != nil {
		return ActivationTarget{}, fmt.Errorf("auth: find activation token: %w", err)
	}
	return t, nil
}

// Activate memasang password pertama dan mengaktifkan akun pemilik token,
// dalam satu transaksi beserta auditnya. Semua token aktivasi akun itu
// tidak berlaku lagi setelahnya.
func (s *ActivationService) Activate(ctx context.Context, token, password string) (ActivationTarget, error) {
	if err := ValidatePassword(password); err != nil {
		return ActivationTarget{}, err
	}
	hash, ok := hashResetToken(token)
	if !ok {
		return ActivationTarget{}, ErrInvalidActivationToken
	}
	// Periksa token dulu agar token palsu tidak memicu hashing yang mahal.
	if _, err := findActivationTarget(ctx, s.db, hash, false); err != nil {
		return ActivationTarget{}, err
	}
	passwordHash, err := s.hasher.HashPassword(password)
	if err != nil {
		return ActivationTarget{}, err
	}

	var target ActivationTarget
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, err := findActivationTarget(ctx, tx, hash, true)
		if err != nil {
			return err
		}
		target = t
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, is_active = true, updated_at = now() WHERE id = $1`,
			t.UserID, passwordHash); err != nil {
			return fmt.Errorf("auth: activate user: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE activation_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, t.UserID); err != nil {
			return fmt.Errorf("auth: use activation token: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: t.SchoolID, UserID: t.UserID,
			Action: ActionAccountActivate, EntityType: "user", EntityID: t.UserID,
			Before: map[string]any{"is_active": false}, After: map[string]any{"is_active": true},
		})
	})
	if err != nil {
		return ActivationTarget{}, err
	}
	s.logger.InfoContext(ctx, "account activated", "user_id", target.UserID)
	return target, nil
}

// Pesan halaman aktivasi.
const (
	activationPage         = "auth/aktivasi"
	msgActivationInvalid   = "Tautan aktivasi tidak berlaku, sudah dipakai, atau sudah kedaluwarsa. Minta sekolah mengirim ulang tautan aktivasi."
	msgActivationCompleted = "Akun Anda sudah aktif. Silakan masuk dengan kode sekolah %s dan email %s."
)

type activationView struct {
	Token      string
	TokenValid bool
	FullName   string
	Email      string
	SchoolCode string
	Error      string
	Errors     map[string]string
	Flash      string
}

// ActivationHandler melayani halaman aktivasi akun.
type ActivationHandler struct {
	svc      *ActivationService
	sessions *scs.SessionManager
	render   Renderer
	logger   *slog.Logger
}

// NewActivationHandler mengembalikan ActivationHandler. Route-nya harus
// dibungkus CSRF dan sessions.LoadAndSave.
func NewActivationHandler(svc *ActivationService, sessions *scs.SessionManager, render Renderer, logger *slog.Logger) *ActivationHandler {
	return &ActivationHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route aktivasi ke r.
//
// Peran: publik (tanpa login).
func (h *ActivationHandler) Routes(r chi.Router) {
	r.Get("/aktivasi", h.show)
	r.Post("/aktivasi", h.submit)
}

func (h *ActivationHandler) show(w http.ResponseWriter, r *http.Request) {
	noLeakHeaders(w)
	token := r.URL.Query().Get("token")
	t, err := h.svc.CheckToken(r.Context(), token)
	switch {
	case err == nil:
		h.render.Render(w, r, http.StatusOK, activationPage, activationView{Token: token, TokenValid: true, FullName: t.FullName, Email: t.Email, SchoolCode: t.SchoolCode})
	case errors.Is(err, ErrInvalidActivationToken):
		h.render.Render(w, r, http.StatusBadRequest, activationPage, activationView{Error: msgActivationInvalid})
	default:
		h.logger.ErrorContext(r.Context(), "check activation token", "error", err)
		h.render.Render(w, r, http.StatusInternalServerError, activationPage, activationView{Error: msgInternalError})
	}
}

func (h *ActivationHandler) submit(w http.ResponseWriter, r *http.Request) {
	noLeakHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, maxResetFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	token := r.PostForm.Get("token")
	password := r.PostForm.Get("password")

	reshow := func(status int, errs map[string]string) {
		view := activationView{Token: token, TokenValid: true, Errors: errs}
		if t, err := h.svc.CheckToken(r.Context(), token); err == nil {
			view.FullName, view.Email, view.SchoolCode = t.FullName, t.Email, t.SchoolCode
		}
		h.render.Render(w, r, status, activationPage, view)
	}
	if password != r.PostForm.Get("password_confirm") {
		reshow(http.StatusBadRequest, map[string]string{"password_confirm": msgPasswordMismatch})
		return
	}

	t, err := h.svc.Activate(r.Context(), token, password)
	switch {
	case err == nil:
		SetFlash(r.Context(), h.sessions, fmt.Sprintf(msgActivationCompleted, t.SchoolCode, t.Email))
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	case errors.Is(err, ErrPasswordTooShort):
		reshow(http.StatusBadRequest, map[string]string{"password": msgPasswordTooShort})
	case errors.Is(err, ErrPasswordTooLong):
		reshow(http.StatusBadRequest, map[string]string{"password": msgPasswordTooLong})
	case errors.Is(err, ErrInvalidActivationToken):
		h.render.Render(w, r, http.StatusBadRequest, activationPage, activationView{Error: msgActivationInvalid})
	default:
		h.logger.ErrorContext(r.Context(), "activate account", "error", err)
		h.render.Render(w, r, http.StatusInternalServerError, activationPage, activationView{Error: msgInternalError})
	}
}
