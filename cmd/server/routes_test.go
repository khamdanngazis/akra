package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"

	"akra/internal/auth"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	// Repository nil cukup karena uji ini tidak memanggil login.
	svc, err := auth.NewService(nil, auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}))
	if err != nil {
		t.Fatal(err)
	}
	return newRouter(routerDeps{sessions: sessions, authHandler: auth.NewHandler(svc, sessions, logger)})
}

func TestRouter_PublicRoutesDoNotRedirect(t *testing.T) {
	h := newTestRouter(t)
	for _, path := range []string{"/healthz", "/login"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestRouter_HomeRequiresLogin(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?next=%2F" {
		t.Errorf("status = %d, Location = %q; want 302 to /login?next=%%2F", rec.Code, rec.Header().Get("Location"))
	}
}
