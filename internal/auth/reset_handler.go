package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
)

const (
	forgotPasswordPage = "auth/lupa_password"
	resetPasswordPage  = "auth/reset_password"
	maxResetFormBytes  = 16 << 10
)

// Pesan yang tampil ke pengguna.
const (
	msgResetRequested     = "Jika kode sekolah dan email tersebut terdaftar, tautan untuk mengatur ulang password sudah dikirim. Tautan berlaku 30 menit."
	msgResetFieldsMissing = "Isi kode sekolah dan email."
	msgResetTokenInvalid  = "Tautan reset tidak berlaku, sudah dipakai, atau sudah kedaluwarsa. Silakan minta tautan baru."
	msgPasswordTooShort   = "Password minimal 8 karakter."
	msgPasswordTooLong    = "Password terlalu panjang."
	msgPasswordMismatch   = "Konfirmasi password tidak sama."
	msgPasswordChanged    = "Password berhasil diubah. Silakan masuk dengan password baru."
)

type forgotPasswordView struct {
	SchoolCode string
	Email      string
	Error      string
	Flash      string
}

type resetPasswordView struct {
	Token string
	// TokenValid false menampilkan pesan dan tautan untuk meminta ulang,
	// tanpa form.
	TokenValid bool
	Error      string
	Flash      string
}

// ResetHandler melayani halaman lupa password dan reset password.
type ResetHandler struct {
	svc      *ResetService
	sessions *scs.SessionManager
	render   Renderer
	logger   *slog.Logger
}

// NewResetHandler mengembalikan ResetHandler. Route-nya harus dibungkus
// CSRF dan sessions.LoadAndSave.
func NewResetHandler(svc *ResetService, sessions *scs.SessionManager, render Renderer, logger *slog.Logger) *ResetHandler {
	return &ResetHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route reset password ke r.
//
// Peran: publik (tanpa login).
func (h *ResetHandler) Routes(r chi.Router) {
	r.Get("/lupa-password", h.showForgot)
	r.Post("/lupa-password", h.submitForgot)
	r.Get("/reset-password", h.showReset)
	r.Post("/reset-password", h.submitReset)
}

func (h *ResetHandler) showForgot(w http.ResponseWriter, r *http.Request) {
	h.render.Render(w, r, http.StatusOK, forgotPasswordPage, forgotPasswordView{})
}

func (h *ResetHandler) submitForgot(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxResetFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	view := forgotPasswordView{SchoolCode: r.PostForm.Get("school_code"), Email: r.PostForm.Get("email")}
	if NormalizeSchoolCode(view.SchoolCode) == "" || NormalizeEmail(view.Email) == "" {
		view.Error = msgResetFieldsMissing
		h.render.Render(w, r, http.StatusBadRequest, forgotPasswordPage, view)
		return
	}

	if err := h.svc.RequestReset(r.Context(), view.SchoolCode, view.Email); err != nil {
		h.logger.ErrorContext(r.Context(), "request password reset", "error", err)
		view.Error = msgInternalError
		h.render.Render(w, r, http.StatusInternalServerError, forgotPasswordPage, view)
		return
	}
	// Respons sama persis untuk email terdaftar dan tidak terdaftar; isian
	// form tidak ditampilkan ulang.
	h.render.Render(w, r, http.StatusOK, forgotPasswordPage, forgotPasswordView{Flash: msgResetRequested})
}

func (h *ResetHandler) showReset(w http.ResponseWriter, r *http.Request) {
	noLeakHeaders(w)
	token := r.URL.Query().Get("token")
	err := h.svc.CheckToken(r.Context(), token)
	switch {
	case err == nil:
		h.render.Render(w, r, http.StatusOK, resetPasswordPage, resetPasswordView{Token: token, TokenValid: true})
	case errors.Is(err, ErrInvalidResetToken):
		h.render.Render(w, r, http.StatusBadRequest, resetPasswordPage, resetPasswordView{Error: msgResetTokenInvalid})
	default:
		h.logger.ErrorContext(r.Context(), "check reset token", "error", err)
		h.render.Render(w, r, http.StatusInternalServerError, resetPasswordPage, resetPasswordView{Error: msgInternalError})
	}
}

func (h *ResetHandler) submitReset(w http.ResponseWriter, r *http.Request) {
	noLeakHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, maxResetFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	token := r.PostForm.Get("token")
	password := r.PostForm.Get("password")
	view := resetPasswordView{Token: token, TokenValid: true}

	if password != r.PostForm.Get("password_confirm") {
		view.Error = msgPasswordMismatch
		h.render.Render(w, r, http.StatusBadRequest, resetPasswordPage, view)
		return
	}

	err := h.svc.ResetPassword(r.Context(), token, password)
	switch {
	case err == nil:
		SetFlash(r.Context(), h.sessions, msgPasswordChanged)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	case errors.Is(err, ErrPasswordTooShort):
		view.Error = msgPasswordTooShort
		h.render.Render(w, r, http.StatusBadRequest, resetPasswordPage, view)
	case errors.Is(err, ErrPasswordTooLong):
		view.Error = msgPasswordTooLong
		h.render.Render(w, r, http.StatusBadRequest, resetPasswordPage, view)
	case errors.Is(err, ErrInvalidResetToken):
		h.render.Render(w, r, http.StatusBadRequest, resetPasswordPage, resetPasswordView{Error: msgResetTokenInvalid})
	default:
		h.logger.ErrorContext(r.Context(), "reset password", "error", err)
		view.Error = msgInternalError
		h.render.Render(w, r, http.StatusInternalServerError, resetPasswordPage, view)
	}
}

// noLeakHeaders mencegah token di URL bocor lewat header Referer ke situs
// lain dan mencegah halaman disimpan di cache.
func noLeakHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}
