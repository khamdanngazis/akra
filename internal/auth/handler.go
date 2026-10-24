package auth

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

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
	// SchoolName terisi jika sekolah sudah diketahui (link /s/{kode} atau
	// cookie sekolah terakhir): form menampilkan nama sekolah, bukan kolom kode.
	SchoolName string
	Email      string
	Next       string
	Error      string
	Flash      string
}

// SchoolCookieName adalah cookie "sekolah terakhir" di perangkat ini. Isinya
// hanya kode sekolah (bukan rahasia), terpisah dari sesi, dan tidak dihapus
// saat logout, sehingga login berikutnya tidak perlu mengetik kode lagi.
const SchoolCookieName = "akra_school"

const schoolCookieMaxAge = 365 * 24 * 60 * 60

// SchoolLoginPath mengembalikan link login sekolah, mis. /s/sd-harapan.
func SchoolLoginPath(code string) string { return "/s/" + code }

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
	// Link login per sekolah: kode sudah terisi, mis. dari QR atau grup WA.
	r.Get("/s/{code}", h.showSchoolLogin)
	// Logout hanya lewat POST agar tidak bisa dipicu dari link atau <img>.
	r.Post("/logout", h.logout)
}

// showLogin menampilkan form login. Kode sekolah diisi dari cookie sekolah
// terakhir; ?ganti=1 ("Ganti sekolah") menghapus cookie itu.
func (h *Handler) showLogin(w http.ResponseWriter, r *http.Request) {
	code := ""
	if r.URL.Query().Get("ganti") != "" {
		h.clearSchoolCookie(w)
	} else if c, err := r.Cookie(SchoolCookieName); err == nil {
		code = c.Value
	}
	h.showLoginFor(w, r, code)
}

// showSchoolLogin menampilkan form login dengan sekolah dari link. Kode yang
// tidak dikenal menampilkan form biasa dengan kolom kode kosong, tanpa pesan
// "sekolah tidak ditemukan".
func (h *Handler) showSchoolLogin(w http.ResponseWriter, r *http.Request) {
	h.showLoginFor(w, r, chi.URLParam(r, "code"))
}

func (h *Handler) showLoginFor(w http.ResponseWriter, r *http.Request, code string) {
	next := SafeNext(r.URL.Query().Get("next"))
	// Pengguna yang sudah login tidak perlu melihat form lagi.
	if id, ok := identityFromSession(r, h.sessions); ok {
		http.Redirect(w, r, LandingPath(id.Roles, next), http.StatusSeeOther)
		return
	}
	page := loginPage{Next: next, Flash: PopFlash(r.Context(), h.sessions)}
	if code != "" {
		h.fillSchool(r, &page, code)
	}
	h.render.Render(w, r, http.StatusOK, loginPageName, page)
}

// fillSchool mengisi kode dan nama sekolah jika kode itu ada.
func (h *Handler) fillSchool(r *http.Request, page *loginPage, code string) {
	name, err := h.svc.SchoolName(r.Context(), code)
	switch {
	case err == nil:
		page.SchoolCode, page.SchoolName = NormalizeSchoolCode(code), name
	case !errors.Is(err, ErrNotFound):
		h.logger.Error("find school for login", "error", err)
	}
}

func (h *Handler) setSchoolCookie(w http.ResponseWriter, code string) {
	http.SetCookie(w, &http.Cookie{Name: SchoolCookieName, Value: code, Path: "/", MaxAge: schoolCookieMaxAge,
		HttpOnly: true, Secure: h.sessions.Cookie.Secure, SameSite: http.SameSiteLaxMode})
}

func (h *Handler) clearSchoolCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SchoolCookieName, Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.sessions.Cookie.Secure, SameSite: http.SameSiteLaxMode})
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
		code := NormalizeSchoolCode(page.SchoolCode)
		h.sessions.Put(r.Context(), sessionKeySchoolCode, code)
		h.setSchoolCookie(w, code)
		http.Redirect(w, r, LandingPath(user.Roles, page.Next), http.StatusSeeOther)
	case errors.Is(err, ErrInvalidCredentials):
		page.Error = msgInvalidCredentials
		// Form dari link sekolah tetap menampilkan nama sekolahnya.
		if r.PostForm.Get("school_fixed") != "" {
			h.fillSchool(r, &page, page.SchoolCode)
		}
		h.render.Render(w, r, http.StatusUnauthorized, loginPageName, page)
	case errors.Is(err, ErrTooManyAttempts):
		h.logger.Warn("login locked out", "school_code", NormalizeSchoolCode(page.SchoolCode), "ip", ip.String())
		if r.PostForm.Get("school_fixed") != "" {
			h.fillSchool(r, &page, page.SchoolCode)
		}
		page.Error = msgTooManyAttempts
		h.render.Render(w, r, http.StatusTooManyRequests, loginPageName, page)
	default:
		// err tidak pernah berisi password.
		h.logger.Error("login failed", "error", err)
		page.Error = msgInternalError
		h.render.Render(w, r, http.StatusInternalServerError, loginPageName, page)
	}
}

// logout kembali ke link login sekolah pengguna (/s/{kode}); sesi lama tanpa
// kode sekolah kembali ke /login.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	to := "/login"
	if code := h.sessions.GetString(r.Context(), sessionKeySchoolCode); ValidSchoolCode(code) {
		to = SchoolLoginPath(code)
	}
	if err := h.sessions.Destroy(r.Context()); err != nil {
		h.logger.Error("destroy session", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	// Sesi baru yang hanya berisi pesan flash, tanpa identitas.
	SetFlash(r.Context(), h.sessions, msgLoggedOut)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// ClientIP mengembalikan IP klien request, untuk dicatat di audit.
func ClientIP(r *http.Request) netip.Addr { return clientIP(r) }

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

// PortalPath adalah halaman awal portal orang tua.
const PortalPath = "/portal"

// LandingPath menentukan tujuan setelah login. Akun orang tua hanya boleh
// ke portal, jadi next di luar /portal diganti PortalPath.
func LandingPath(roles []string, next string) string {
	if slices.Contains(roles, RoleOrangTua) && next != PortalPath && !strings.HasPrefix(next, PortalPath+"/") {
		return PortalPath
	}
	return next
}
