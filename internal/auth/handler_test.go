package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	svc, _ := newTestService(t)
	r := chi.NewRouter()
	NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes(r)
	return r
}

func postLogin(h http.Handler, school, email, password string) *httptest.ResponseRecorder {
	form := url.Values{"school_code": {school}, "email": {email}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.7:51000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginPage(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for _, field := range []string{`name="school_code"`, `name="email"`, `name="password"`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("login page is missing %s", field)
		}
	}
}

func TestSubmitLogin_Success(t *testing.T) {
	rec := postLogin(newTestRouter(t), "sd-harapan", "budi@sekolah.id", "benar-123")

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("status = %d, Location = %q; want 303 to /", rec.Code, rec.Header().Get("Location"))
	}
}

// alertText returns the error message shown on the login page.
func alertText(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `<p role="alert">`)
	if !ok {
		t.Fatal("login page has no error message")
	}
	msg, _, _ := strings.Cut(rest, "</p>")
	return msg
}

func TestSubmitLogin_SameResponseForWrongEmailAndPassword(t *testing.T) {
	const submitted = "Tebak-Pw-9137"
	wrongPassword := postLogin(newTestRouter(t), "sd-harapan", "budi@sekolah.id", submitted)
	wrongEmail := postLogin(newTestRouter(t), "sd-harapan", "siapa@sekolah.id", "benar-123")

	if wrongPassword.Code != http.StatusUnauthorized || wrongEmail.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d / %d, want 401 / 401", wrongPassword.Code, wrongEmail.Code)
	}
	a, b := alertText(t, wrongPassword.Body.String()), alertText(t, wrongEmail.Body.String())
	if a != b || a != msgInvalidCredentials {
		t.Errorf("messages = %q / %q, want both %q", a, b, msgInvalidCredentials)
	}
	if strings.Contains(wrongPassword.Body.String(), submitted) {
		t.Error("response echoes the submitted password")
	}
}

func TestSubmitLogin_LockedOut(t *testing.T) {
	h := newTestRouter(t)
	for range MaxFailuresPerEmail {
		postLogin(h, "sd-harapan", "budi@sekolah.id", "salah")
	}

	rec := postLogin(h, "sd-harapan", "budi@sekolah.id", "benar-123")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if msg := alertText(t, rec.Body.String()); !strings.Contains(msg, "Coba lagi beberapa menit lagi") {
		t.Errorf("message = %q, want it to mention trying again in a few minutes", msg)
	}
}

func TestClientIP(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7:51000":     "203.0.113.7",
		"[2001:db8::1]:443":     "2001:db8::1",
		"[::ffff:192.0.2.1]:80": "192.0.2.1",
		"garbage":               "invalid IP",
	}
	for remote, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		if got := clientIP(r).String(); got != want {
			t.Errorf("clientIP(%q) = %q, want %q", remote, got, want)
		}
	}
}
