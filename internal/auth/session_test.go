package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
)

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			return c
		}
	}
	t.Fatalf("response has no %s cookie", SessionCookieName)
	return nil
}

func get(h http.Handler, path string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSessionCookieAttributes(t *testing.T) {
	for _, secure := range []bool{false, true} {
		sm := NewSessionManager(memstore.NewWithCleanupInterval(0), secure, slog.New(slog.NewTextHandler(io.Discard, nil)))
		h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sm.Put(r.Context(), "k", "v") }))

		c := sessionCookie(t, get(h, "/", nil))
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure != secure {
			t.Errorf("secure=%v: cookie = %+v, want HttpOnly, SameSite=Lax, Path=/, Secure=%v", secure, c, secure)
		}
		if c.MaxAge <= 0 {
			t.Errorf("secure=%v: cookie MaxAge = %d, want a persistent cookie", secure, c.MaxAge)
		}
	}
}

func TestLogin_StoresIdentityAndRenewsToken(t *testing.T) {
	h, store := newTestRouterWithSessions(t)

	before := sessionCookie(t, get(h, "/test/anon", nil))

	rec := postLogin(h, "sd-harapan", "budi@sekolah.id", "benar-123", before)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", rec.Code)
	}
	after := sessionCookie(t, rec)

	if after.Value == before.Value {
		t.Error("session token did not change after login")
	}
	if _, found, _ := store.Find(before.Value); found {
		t.Error("pre-login session token is still valid after login")
	}

	if got := get(h, "/test/whoami", after).Body.String(); got != "u1|s1|guru" {
		t.Errorf("session identity = %q, want %q", got, "u1|s1|guru")
	}
}

func TestLogin_FailureDoesNotCreateSession(t *testing.T) {
	h, _ := newTestRouterWithSessions(t)

	rec := postLogin(h, "sd-harapan", "budi@sekolah.id", "salah")
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			t.Errorf("failed login set a session cookie: %+v", c)
		}
	}
}

func TestLogout(t *testing.T) {
	h, store := newTestRouterWithSessions(t)
	c := sessionCookie(t, postLogin(h, "sd-harapan", "budi@sekolah.id", "benar-123"))

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d, Location = %q; want 303 to /login", rec.Code, rec.Header().Get("Location"))
	}
	if _, found, _ := store.Find(c.Value); found {
		t.Error("session still exists in the store after logout")
	}
	if cleared := sessionCookie(t, rec); cleared.MaxAge >= 0 {
		t.Errorf("logout cookie MaxAge = %d, want negative (expired)", cleared.MaxAge)
	}
}

func TestLogout_GetNotAllowed(t *testing.T) {
	h, _ := newTestRouterWithSessions(t)
	if rec := get(h, "/logout", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /logout status = %d, want 405", rec.Code)
	}
}
