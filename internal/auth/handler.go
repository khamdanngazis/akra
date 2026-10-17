package auth

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
)

const maxLoginFormBytes = 16 << 10

// Pesan yang tampil ke pengguna.
const (
	msgInvalidCredentials = "Kode sekolah, email, atau password salah."
	msgTooManyAttempts    = "Terlalu banyak percobaan masuk. Coba lagi beberapa menit lagi."
	msgInternalError      = "Terjadi kesalahan pada server. Silakan coba lagi."
	msgLoggedOut          = "Anda telah keluar."
)

// Renderer merender halaman dari web/templates.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, status int, name string, data any)
}

const loginPageName = "auth/login"

type loginPage struct {
	SchoolCode string
	Email      string
	Next       string
	Error      string
	Flash      string
}

// Handler melayani halaman login dan logout.
type Handler struct {
	svc      *Service
	sessions *scs.SessionManager
	render   Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus dibungkus
// sessions.LoadAndSave.
func NewHandler(svc *Service, sessions *scs.SessionManager, render Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route login dan logout ke r.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/login", h.showLogin)
	r.Post("/login", h.submitLogin)
	// Logout hanya lewat POST agar tidak bisa dipicu dari link atau <img>.
	r.Post("/logout", h.logout)
}

func (h *Handler) showLogin(w http.ResponseWriter, r *http.Request) {
	next := SafeNext(r.URL.Query().Get("next"))
	// Pengguna yang sudah login tidak perlu melihat form lagi.
	if _, ok := identityFromSession(r, h.sessions); ok {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	h.render.Render(w, r, http.StatusOK, loginPageName, loginPage{Next: next, Flash: PopFlash(r.Context(), h.sessions)})
}

func (h *Handler) submitLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	page := loginPage{
		SchoolCode: r.PostForm.Get("school_code"),
		Email:      r.PostForm.Get("email"),
		Next:       SafeNext(r.PostForm.Get("next")),
	}
	ip := clientIP(r)

	user, err := h.svc.Authenticate(r.Context(), page.SchoolCode, page.Email, r.PostForm.Get("password"), ip)
	switch {
	case err == nil:
		if err := startSession(r.Context(), h.sessions, user); err != nil {
			h.logger.Error("start session", "error", err)
			page.Error = msgInternalError
			h.render.Render(w, r, http.StatusInternalServerError, loginPageName, page)
			return
		}
		http.Redirect(w, r, page.Next, http.StatusSeeOther)
	case errors.Is(err, ErrInvalidCredentials):
		page.Error = msgInvalidCredentials
		h.render.Render(w, r, http.StatusUnauthorized, loginPageName, page)
	case errors.Is(err, ErrTooManyAttempts):
		h.logger.Warn("login locked out", "school_code", NormalizeSchoolCode(page.SchoolCode), "ip", ip.String())
		page.Error = msgTooManyAttempts
		h.render.Render(w, r, http.StatusTooManyRequests, loginPageName, page)
	default:
		// err tidak pernah berisi password.
		h.logger.Error("login failed", "error", err)
		page.Error = msgInternalError
		h.render.Render(w, r, http.StatusInternalServerError, loginPageName, page)
	}
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.sessions.Destroy(r.Context()); err != nil {
		h.logger.Error("destroy session", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	// Sesi baru yang hanya berisi pesan flash, tanpa identitas.
	SetFlash(r.Context(), h.sessions, msgLoggedOut)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// clientIP mengembalikan alamat peer dari koneksi. Di belakang proxy,
// httpserver.ClientIP (CLIENT_IP_HEADER) sudah mengganti RemoteAddr dengan IP
// klien sebelum request sampai di sini.
func clientIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap()
}
