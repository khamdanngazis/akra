package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
)

// newMiddlewareRouter memasang RequireLogin di depan /protected, yang
// menuliskan Identity dari context. /test/login membuat sesi dengan nilai
// yang diberikan lewat query.
func newMiddlewareRouter(t *testing.T) (http.Handler, *scs.SessionManager) {
	t.Helper()
	sm := NewSessionManager(memstore.NewWithCleanupInterval(0), false, slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Get("/test/login", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sm.Put(r.Context(), sessionKeyUserID, q.Get("user"))
		sm.Put(r.Context(), sessionKeySchoolID, q.Get("school"))
		sm.Put(r.Context(), sessionKeyRoles, q["role"])
	})
	r.Group(func(r chi.Router) {
		r.Use(RequireLogin(sm))
		r.HandleFunc("/protected", func(w http.ResponseWriter, r *http.Request) {
			id, ok := CurrentUser(r.Context())
			if !ok {
				t.Error("CurrentUser() ok = false inside RequireLogin")
			}
			_, _ = io.WriteString(w, id.UserID+"|"+id.SchoolID)
			if !slices.Equal(id.Roles, []string{"guru", "bendahara"}) {
				t.Errorf("Identity.Roles = %v", id.Roles)
			}
		})
	})
	return r, sm
}

func TestRequireLogin_NoSession(t *testing.T) {
	h, _ := newMiddlewareRouter(t)

	rec := get(h, "/protected?tab=2", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc, want := rec.Header().Get("Location"), "/login?next=%2Fprotected%3Ftab%3D2"; loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

func TestRequireLogin_NoSessionPost(t *testing.T) {
	h, _ := newMiddlewareRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Errorf("status = %d, Location = %q; want 302 to /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRequireLogin_WithSession(t *testing.T) {
	h, _ := newMiddlewareRouter(t)
	c := sessionCookie(t, get(h, "/test/login?user=u1&school=s1&role=guru&role=bendahara", nil))

	rec := get(h, "/protected", c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "u1|s1" {
		t.Errorf("identity = %q, want %q", got, "u1|s1")
	}
}

func TestRequireLogin_IncompleteSession(t *testing.T) {
	h, _ := newMiddlewareRouter(t)
	c := sessionCookie(t, get(h, "/test/login?user=u1", nil)) // tanpa school_id

	if rec := get(h, "/protected", c); rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 for a session without school_id", rec.Code)
	}
}

func TestCurrentUser_WithoutMiddleware(t *testing.T) {
	if _, ok := CurrentUser(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Error("CurrentUser() ok = true without RequireLogin")
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"":                        "/",
		"/":                       "/",
		"/kelas/7a?tab=siswa":     "/kelas/7a?tab=siswa",
		"/a/b#bagian":             "/a/b#bagian",
		"https://contoh.com":      "/",
		"http://contoh.com/login": "/",
		"//contoh.com":            "/",
		"///contoh.com":           "/",
		`/\contoh.com`:            "/",
		`/a\b`:                    "/",
		"javascript:alert(1)":     "/",
		"contoh.com":              "/",
		"/\tcontoh.com":           "/",
		"/a\r\nSet-Cookie: x=1":   "/",
	}
	for in, want := range cases {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
