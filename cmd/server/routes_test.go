package main

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/csrf"
	"akra/internal/platform/render"
)

// emptyRepo adalah auth.Repository tanpa pengguna: setiap login gagal dengan
// ErrInvalidCredentials, cukup untuk membuktikan request sampai ke handler.
type emptyRepo struct{}

func (emptyRepo) FindUserForLogin(context.Context, string, string) (auth.UserCredentials, error) {
	return auth.UserCredentials{}, auth.ErrNotFound
}
func (emptyRepo) CountRecentFailures(context.Context, string, string, netip.Addr, time.Duration) (int, int, error) {
	return 0, 0, nil
}
func (emptyRepo) RecordLoginAttempt(context.Context, auth.LoginAttempt) error { return nil }

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	svc, err := auth.NewService(emptyRepo{}, auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}))
	if err != nil {
		t.Fatal(err)
	}
	rd, err := render.New(os.DirFS("../../web/templates"), logger)
	if err != nil {
		t.Fatal(err)
	}
	return newRouter(routerDeps{
		csrf:        csrf.Middleware(false, logger),
		sessions:    sessions,
		render:      rd,
		static:      os.DirFS("../../web/static"),
		authHandler: auth.NewHandler(svc, sessions, rd, logger),
	})
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

var tokenInForm = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// browser mensimulasikan satu browser: membuka /login untuk mendapat cookie
// dan token CSRF.
type browser struct {
	cookie *http.Cookie
	token  string
}

func openLoginPage(t *testing.T, h http.Handler) browser {
	t.Helper()
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/login", nil))
	c := cookieNamed(rec, csrf.CookieName)
	if c == nil {
		t.Fatalf("GET /login did not set the %s cookie", csrf.CookieName)
	}
	m := tokenInForm.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("login form has no csrf_token field")
	}
	return browser{cookie: c, token: m[1]}
}

func postForm(path string, form url.Values, b *browser) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Browser mengirim Origin pada POST; nosurf mencocokkannya dengan host.
	req.Header.Set("Origin", "http://example.com")
	if b != nil {
		req.AddCookie(b.cookie)
	}
	return req
}

func loginForm(token string) url.Values {
	return url.Values{"school_code": {"sd-x"}, "email": {"a@b.id"}, "password": {"x"}, "csrf_token": {token}}
}

func TestRouter_PublicRoutesDoNotRedirect(t *testing.T) {
	h := newTestRouter(t)
	for _, path := range []string{"/healthz", "/login"} {
		if rec := serve(h, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestRouter_HomeRequiresLogin(t *testing.T) {
	rec := serve(newTestRouter(t), httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?next=%2F" {
		t.Errorf("status = %d, Location = %q; want 302 to /login?next=%%2F", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCSRF_LoginWithoutToken(t *testing.T) {
	h := newTestRouter(t)
	b := openLoginPage(t, h)

	rec := serve(h, postForm("/login", loginForm(""), &b))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Permintaan ditolak") {
		t.Error("403 response is not the CSRF error page")
	}
}

func TestCSRF_LoginWithValidToken(t *testing.T) {
	h := newTestRouter(t)
	b := openLoginPage(t, h)

	// 401 berarti CSRF lolos dan handler login memproses kredensial.
	if rec := serve(h, postForm("/login", loginForm(b.token), &b)); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 from the login handler", rec.Code)
	}
}

func TestCSRF_TokenViaHeader(t *testing.T) {
	h := newTestRouter(t)
	b := openLoginPage(t, h)

	req := postForm("/login", loginForm(""), &b)
	req.Header.Set(csrf.HeaderName, b.token)
	if rec := serve(h, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 from the login handler", rec.Code)
	}
}

func TestCSRF_TokenFromAnotherSessionRejected(t *testing.T) {
	h := newTestRouter(t)
	victim := openLoginPage(t, h)
	attacker := openLoginPage(t, h)

	rec := serve(h, postForm("/login", loginForm(attacker.token), &victim))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestCSRF_CrossOriginRejected(t *testing.T) {
	h := newTestRouter(t)
	b := openLoginPage(t, h)

	req := postForm("/login", loginForm(b.token), &b)
	req.Header.Set("Origin", "https://jahat.example")
	if rec := serve(h, req); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestCSRF_HealthzUntouched(t *testing.T) {
	rec := serve(newTestRouter(t), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("/healthz set cookies: %v", rec.Result().Cookies())
	}
}

func TestCSRF_CookieAttributes(t *testing.T) {
	rec := serve(newTestRouter(t), httptest.NewRequest(http.MethodGet, "/login", nil))
	c := cookieNamed(rec, csrf.CookieName)
	if c == nil {
		t.Fatal("no CSRF cookie")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie = %+v, want HttpOnly, SameSite=Lax, Path=/", c)
	}
}

// TestCSRF_AllMutatingRoutesRequireToken menelusuri semua route dan
// memastikan setiap POST/PUT/PATCH/DELETE ditolak tanpa token. Route baru
// otomatis ikut diperiksa.
func TestCSRF_AllMutatingRoutesRequireToken(t *testing.T) {
	h := newTestRouter(t)
	mux, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	var checked int
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			return nil
		}
		checked++
		path := strings.ReplaceAll(route, "/*", "/x")
		req := httptest.NewRequest(method, path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		if rec := serve(h, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without CSRF token: status = %d, want 403", method, route, rec.Code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no mutating routes found")
	}
}

func TestRouter_Static(t *testing.T) {
	h := newTestRouter(t)
	for _, path := range []string{"/static/css/app.css", "/static/js/alpine.min.js", "/static/img/favicon.svg"} {
		if rec := serve(h, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
	for _, path := range []string{"/static/", "/static/css/", "/static/nope.css"} {
		if rec := serve(h, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, rec.Code)
		}
	}
}

// TestTemplates_NoExternalResources memastikan template dan CSS tidak
// memuat apa pun dari luar (CDN, Google Fonts, dan sejenisnya).
func TestTemplates_NoExternalResources(t *testing.T) {
	external := regexp.MustCompile(`(?i)(src|href)\s*=\s*"(https?:)?//|url\(\s*["']?(https?:)?//|@import`)
	for _, root := range []string{"../../web/templates", "../../web/static/css"} {
		err := fs.WalkDir(os.DirFS(root), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(os.DirFS(root), p)
			if err != nil {
				return err
			}
			if m := external.Find(b); m != nil {
				t.Errorf("%s/%s loads an external resource: %s", root, p, m)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
