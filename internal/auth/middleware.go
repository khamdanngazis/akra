package auth

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/alexedwards/scs/v2"
)

// RequireLogin mengembalikan middleware yang mengalihkan request tanpa sesi
// login ke /login?next=<path>. Jika sesi valid, Identity disimpan ke context
// dan bisa dibaca dengan CurrentUser. Harus dipasang di dalam
// sm.LoadAndSave.
func RequireLogin(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identityFromSession(r, sm)
			if !ok {
				http.Redirect(w, r, loginURL(r), http.StatusFound)
				return
			}
			next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), id)))
		})
	}
}

// identityFromSession membaca identitas dari sesi. Sesi yang tidak lengkap
// dianggap belum login.
func identityFromSession(r *http.Request, sm *scs.SessionManager) (Identity, bool) {
	ctx := r.Context()
	id := Identity{
		UserID:   sm.GetString(ctx, sessionKeyUserID),
		SchoolID: sm.GetString(ctx, sessionKeySchoolID),
	}
	id.Roles, _ = sm.Get(ctx, sessionKeyRoles).([]string)
	if id.UserID == "" || id.SchoolID == "" {
		return Identity{}, false
	}
	return id, true
}

// loginURL mengembalikan URL halaman login dengan next berisi halaman yang
// diminta. next hanya diisi untuk GET dan HEAD, karena kembali ke URL POST
// lewat redirect tidak bermakna.
func loginURL(r *http.Request) string {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "/login"
	}
	return "/login?next=" + url.QueryEscape(r.URL.RequestURI())
}

// SafeNext mengembalikan raw jika raw adalah path lokal yang aman untuk
// redirect, dan "/" jika tidak. Ini mencegah open redirect: URL absolut
// ("https://contoh.com"), protocol-relative ("//contoh.com"), varian dengan
// backslash ("/\contoh.com"), dan karakter kontrol semuanya ditolak.
func SafeNext(raw string) string {
	if raw == "" || raw[0] != '/' {
		return "/"
	}
	if len(raw) > 1 && (raw[1] == '/' || raw[1] == '\\') {
		return "/"
	}
	if strings.ContainsFunc(raw, func(c rune) bool { return c < 0x20 || c == 0x7f || c == '\\' }) {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	return raw
}
