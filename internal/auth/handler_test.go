package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/platform/render"
)

// testRenderer memakai template asli agar uji handler ikut memeriksa template.
func testRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	rd, err := render.New(os.DirFS("../../web/templates"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return rd
}

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	h, _ := newTestRouterWithSessions(t)
	return h
}

func newTestRouterWithSessions(t *testing.T) (http.Handler, *memstore.MemStore) {
	t.Helper()
	svc, _ := newTestService(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := memstore.NewWithCleanupInterval(0)
	sm := NewSessionManager(store, false, logger)

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	NewHandler(svc, sm, testRenderer(t), logger).Routes(r)
	// Route bantu untuk uji: membuat sesi anonim dan membaca isi sesi.
	r.Get("/test/anon", func(w http.ResponseWriter, r *http.Request) { sm.Put(r.Context(), "visited", true) })
	r.Get("/test/whoami", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, sm.GetString(r.Context(), sessionKeyUserID)+"|"+
			sm.GetString(r.Context(), sessionKeySchoolID)+"|"+
			strings.Join(sm.Get(r.Context(), sessionKeyRoles).([]string), ","))
	})
	return r, store
}

func postLogin(h http.Handler, school, email, password string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	return postLoginForm(h, url.Values{"school_code": {school}, "email": {email}, "password": {password}}, cookies...)
}

func postLoginForm(h http.Handler, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
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

// alertText mengembalikan pesan error yang tampil di halaman login.
func alertText(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `role="alert">`)
	if !ok {
		t.Fatal("login page has no error message")
	}
	msg, _, _ := strings.Cut(rest, "</")
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

func TestSubmitLogin_RedirectsToNext(t *testing.T) {
	cases := map[string]string{
		"/kelas?tab=2":       "/kelas?tab=2",
		"https://contoh.com": "/",
		"//contoh.com/phish": "/",
		"":                   "/",
	}
	for next, want := range cases {
		form := url.Values{"school_code": {"sd-harapan"}, "email": {"budi@sekolah.id"}, "password": {"benar-123"}, "next": {next}}
		rec := postLoginForm(newTestRouter(t), form)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("next=%q: status = %d, Location = %q; want 303 to %q", next, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

func TestLoginPage_CarriesSafeNext(t *testing.T) {
	h := newTestRouter(t)

	body := get(h, "/login?next=%2Fkelas%3Ftab%3D2", nil).Body.String()
	if !strings.Contains(body, `name="next" value="/kelas?tab=2"`) {
		t.Errorf("login form does not carry next; body:\n%s", body)
	}
	body = get(h, "/login?next=https%3A%2F%2Fcontoh.com", nil).Body.String()
	if !strings.Contains(body, `name="next" value="/"`) {
		t.Error("login form carries an unsafe next")
	}
}

func TestLoginPage_AlreadyLoggedIn(t *testing.T) {
	h := newTestRouter(t)
	c := sessionCookie(t, postLogin(h, "sd-harapan", "budi@sekolah.id", "benar-123"))

	rec := get(h, "/login?next=%2Fkelas", c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/kelas" {
		t.Errorf("status = %d, Location = %q; want 303 to /kelas", rec.Code, rec.Header().Get("Location"))
	}
}
