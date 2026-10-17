// Package csrf melindungi request yang mengubah data (POST, PUT, PATCH,
// DELETE) dengan token CSRF memakai nosurf.
//
// Token dikirim lewat field form "csrf_token" atau header "X-CSRF-Token"
// (untuk AJAX dari Alpine.js). Selain token, nosurf juga memeriksa header
// Origin/Referer agar sama dengan host aplikasi.
package csrf

import (
	"html/template"
	"log/slog"
	"net/http"

	"github.com/justinas/nosurf"
)

// Nama cookie, field form, dan header token.
const (
	CookieName    = "akra_csrf"
	FormFieldName = nosurf.FormFieldName
	HeaderName    = nosurf.HeaderName
)

const forbiddenPage = `<!doctype html>
<html lang="id">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Permintaan ditolak · AKRA</title></head>
<body>
<main>
<h1>Permintaan ditolak</h1>
<p>Sesi formulir sudah tidak berlaku. Muat ulang halaman lalu coba lagi.</p>
<p><a href="/">Kembali ke beranda</a></p>
</main>
</body>
</html>
`

// Middleware mengembalikan middleware CSRF. secure bernilai true di luar
// development: cookie diberi atribut Secure dan origin aplikasi dianggap
// https (TLS diterminasi di proxy Railway).
//
// Pasang sebelum (di luar) LoadAndSave milik scs agar token sudah tersedia
// saat halaman dirender. Jangan pasang di route health check: nosurf
// menulis cookie di setiap request.
func Middleware(secure bool, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := nosurf.New(next)
		h.SetBaseCookie(http.Cookie{
			Name:     CookieName,
			Path:     "/",
			MaxAge:   nosurf.MaxAge,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
		h.SetIsTLSFunc(func(*http.Request) bool { return secure })
		h.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.Warn("csrf check failed", "method", r.Method, "path", r.URL.Path, "reason", nosurf.Reason(r))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(forbiddenPage))
		}))
		return h
	}
}

// Token mengembalikan token CSRF untuk request r. Kosong jika r tidak
// melewati Middleware.
func Token(r *http.Request) string {
	return nosurf.Token(r)
}

// Field mengembalikan input hidden berisi token CSRF untuk disisipkan ke form.
func Field(r *http.Request) template.HTML {
	return template.HTML(`<input type="hidden" name="` + FormFieldName + `" value="` +
		template.HTMLEscapeString(Token(r)) + `">`)
}

// FuncMap mengembalikan fungsi template {{ csrfField }} dan {{ csrfToken }}
// untuk request r. Template di-parse dengan PlaceholderFuncs, lalu
// fungsi ini dipasang per request lewat Clone().Funcs(...).
func FuncMap(r *http.Request) template.FuncMap {
	return template.FuncMap{
		"csrfField": func() template.HTML { return Field(r) },
		"csrfToken": func() string { return Token(r) },
	}
}

// PlaceholderFuncs dipakai saat parse template agar nama fungsi CSRF dikenal.
// Memanggilnya tanpa FuncMap(r) membuat render gagal, bukan diam-diam
// menghasilkan form tanpa token.
var PlaceholderFuncs = template.FuncMap{
	"csrfField": func() (template.HTML, error) { return "", errNoRequest },
	"csrfToken": func() (string, error) { return "", errNoRequest },
}

type csrfError string

func (e csrfError) Error() string { return string(e) }

const errNoRequest = csrfError("csrf: template rendered without csrf.FuncMap(r)")
