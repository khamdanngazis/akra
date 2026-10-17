package auth

import (
	"errors"
	"html/template"
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
)

// TODO(T11): ganti dengan web/templates/auth/login.html yang dirender lewat
// internal/platform/render, dan tambahkan {{ csrfField }} di T09.
var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="id">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Masuk · AKRA</title>
</head>
<body>
<main>
<h1>Masuk ke AKRA</h1>
{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/login">
<input type="hidden" name="next" value="{{.Next}}">
<p><label>Kode sekolah<br><input name="school_code" value="{{.SchoolCode}}" autocomplete="organization" required></label></p>
<p><label>Email<br><input type="email" name="email" value="{{.Email}}" autocomplete="username" required></label></p>
<p><label>Password<br><input type="password" name="password" autocomplete="current-password" required></label></p>
<p><button type="submit">Masuk</button></p>
</form>
</main>
</body>
</html>
`))

type loginPage struct {
	SchoolCode string
	Email      string
	Next       string
	Error      string
}

// Handler melayani halaman login dan logout.
type Handler struct {
	svc      *Service
	sessions *scs.SessionManager
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus dibungkus
// sessions.LoadAndSave.
func NewHandler(svc *Service, sessions *scs.SessionManager, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, sessions: sessions, logger: logger}
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
	h.render(w, http.StatusOK, loginPage{Next: next})
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
			h.render(w, http.StatusInternalServerError, page)
			return
		}
		http.Redirect(w, r, page.Next, http.StatusSeeOther)
	case errors.Is(err, ErrInvalidCredentials):
		page.Error = msgInvalidCredentials
		h.render(w, http.StatusUnauthorized, page)
	case errors.Is(err, ErrTooManyAttempts):
		h.logger.Warn("login locked out", "school_code", NormalizeSchoolCode(page.SchoolCode), "ip", ip.String())
		page.Error = msgTooManyAttempts
		h.render(w, http.StatusTooManyRequests, page)
	default:
		// err tidak pernah berisi password.
		h.logger.Error("login failed", "error", err)
		page.Error = msgInternalError
		h.render(w, http.StatusInternalServerError, page)
	}
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.sessions.Destroy(r.Context()); err != nil {
		h.logger.Error("destroy session", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) render(w http.ResponseWriter, status int, page loginPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := loginTmpl.Execute(w, page); err != nil {
		h.logger.Error("render login page", "error", err)
	}
}

// clientIP mengembalikan alamat peer dari koneksi.
//
// TODO(T14): di belakang proxy Railway, RemoteAddr adalah alamat proxy. Baca IP
// klien dari header proxy hanya setelah konfigurasi proxy tepercaya diketahui,
// karena mempercayai X-Forwarded-For begitu saja memungkinkan penyerang
// melewati lockout per IP.
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
