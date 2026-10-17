package auth

import (
	"errors"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"
)

const maxLoginFormBytes = 16 << 10

// User-facing messages (Indonesian UI).
const (
	msgInvalidCredentials = "Kode sekolah, email, atau password salah."
	msgTooManyAttempts    = "Terlalu banyak percobaan masuk. Coba lagi beberapa menit lagi."
	msgInternalError      = "Terjadi kesalahan pada server. Silakan coba lagi."
)

// TODO(T11): replace with web/templates/auth/login.html rendered through
// internal/platform/render, and add {{ csrfField }} in T09.
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
	Error      string
}

// Handler serves the login pages.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Routes registers the login routes on r.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/login", h.showLogin)
	r.Post("/login", h.submitLogin)
}

func (h *Handler) showLogin(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, loginPage{})
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
	}
	ip := clientIP(r)

	_, err := h.svc.Authenticate(r.Context(), page.SchoolCode, page.Email, r.PostForm.Get("password"), ip)
	switch {
	case err == nil:
		// TODO(T07): renew the session token and store user_id, school_id,
		// and roles in the session before redirecting.
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case errors.Is(err, ErrInvalidCredentials):
		page.Error = msgInvalidCredentials
		h.render(w, http.StatusUnauthorized, page)
	case errors.Is(err, ErrTooManyAttempts):
		h.logger.Warn("login locked out", "school_code", NormalizeSchoolCode(page.SchoolCode), "ip", ip.String())
		page.Error = msgTooManyAttempts
		h.render(w, http.StatusTooManyRequests, page)
	default:
		// err never contains the password.
		h.logger.Error("login failed", "error", err)
		page.Error = msgInternalError
		h.render(w, http.StatusInternalServerError, page)
	}
}

func (h *Handler) render(w http.ResponseWriter, status int, page loginPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := loginTmpl.Execute(w, page); err != nil {
		h.logger.Error("render login page", "error", err)
	}
}

// clientIP returns the peer address of the connection.
//
// TODO(T14): behind the Railway proxy RemoteAddr is the proxy; read the client
// IP from the proxy header only once the trusted proxy setup is known, since
// trusting X-Forwarded-For blindly lets attackers bypass the IP lockout.
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
