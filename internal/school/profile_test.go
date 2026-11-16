package school

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/render"
	"akra/web"
)

// newProfileEnv memakai handlerEnv (dua sekolah dalam satu transaksi) dengan
// router profil sekolah. Route /_tz membaca zona waktu di sesi.
func newProfileEnv(t *testing.T) (*handlerEnv, *scs.SessionManager) {
	t.Helper()
	env := newHandlerEnv(t, false)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	h := NewProfileHandler(NewProfileService(env.tx), sm, rd, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			school, roles, _ := strings.Cut(r.Header.Get(identityHeader), "|")
			id := auth.Identity{UserID: "22222222-2222-2222-2222-222222222222", SchoolID: env.schoolID(school), Roles: strings.Split(roles, ",")}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	h.Routes(r)
	env.router = r
	return env, sm
}

func (e *handlerEnv) timezone(schoolID string) string {
	var tz string
	if err := e.tx.QueryRow(context.Background(), `SELECT timezone FROM schools WHERE id = $1`, schoolID).Scan(&tz); err != nil {
		e.t.Fatal(err)
	}
	return tz
}

func TestProfileTimezone(t *testing.T) {
	e, _ := newProfileEnv(t)

	if tz := e.timezone(e.schoolA); tz != "Asia/Jakarta" {
		t.Fatalf("default timezone = %s, want Asia/Jakarta", tz)
	}

	rec := e.do(http.MethodGet, "/pengaturan/sekolah/", principal, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "WITA") {
		t.Fatalf("principal GET = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `>Simpan</button>`) {
		t.Error("principal sees save button")
	}
	for _, who := range []string{principal, teacher, parent} {
		if rec := e.do(http.MethodPost, "/pengaturan/sekolah/", who, url.Values{"timezone": {"Asia/Jayapura"}}); rec.Code != http.StatusForbidden {
			t.Errorf("%s POST = %d, want 403", who, rec.Code)
		}
	}
	if rec := e.do(http.MethodGet, "/pengaturan/sekolah/", teacher, nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher GET = %d, want 403", rec.Code)
	}

	rec = e.do(http.MethodPost, "/pengaturan/sekolah/", admin, url.Values{"timezone": {"Europe/London"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid timezone = %d, want 422", rec.Code)
	}

	rec = e.do(http.MethodPost, "/pengaturan/sekolah/", admin, url.Values{"timezone": {"Asia/Jayapura"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin POST = %d, want 303", rec.Code)
	}
	if tz := e.timezone(e.schoolA); tz != "Asia/Jayapura" {
		t.Errorf("timezone = %s, want Asia/Jayapura", tz)
	}
	if tz := e.timezone(e.schoolB); tz != "Asia/Jakarta" {
		t.Errorf("other school timezone changed to %s", tz)
	}
	if n := e.auditCount(ActionSchoolTimezone); n != 1 {
		t.Errorf("audit = %d, want 1", n)
	}
	// Simpan ulang tanpa perubahan tidak menambah audit.
	e.do(http.MethodPost, "/pengaturan/sekolah/", admin, url.Values{"timezone": {"Asia/Jayapura"}})
	if n := e.auditCount(ActionSchoolTimezone); n != 1 {
		t.Errorf("audit after no-op = %d, want 1", n)
	}
	if rec := e.do(http.MethodGet, "/pengaturan/sekolah/", admin, nil); !strings.Contains(rec.Body.String(), " WIT</strong>") {
		t.Error("page does not show current time in WIT")
	}
}

func TestSetTimezoneInvalid(t *testing.T) {
	e, _ := newProfileEnv(t)
	_, err := NewProfileService(e.tx).SetTimezone(context.Background(), Actor{SchoolID: e.schoolA}, "Asia/Tokyo")
	if !errors.Is(err, ErrInvalidTimezone) {
		t.Errorf("err = %v, want ErrInvalidTimezone", err)
	}
}

// Zona waktu di sesi diperbarui setelah admin mengubahnya, sehingga halaman
// berikutnya langsung memakai jam sekolah yang baru.
func TestProfileUpdatesSessionTimezone(t *testing.T) {
	e, sm := newProfileEnv(t)
	var got string
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Handle("/*", e.router)
	r.Get("/_tz", func(w http.ResponseWriter, r *http.Request) {
		got = sm.GetString(r.Context(), "timezone")
	})
	req := httptest.NewRequest(http.MethodPost, "/pengaturan/sekolah/", strings.NewReader("timezone=Asia%2FMakassar"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(identityHeader, admin)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/_tz", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	r.ServeHTTP(httptest.NewRecorder(), req)
	if got != "Asia/Makassar" {
		t.Errorf("session timezone = %q, want Asia/Makassar", got)
	}
}
