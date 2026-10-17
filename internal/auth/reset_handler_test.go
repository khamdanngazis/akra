package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
)

func newResetTestRouter(t *testing.T) (http.Handler, *fakeMailer) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, _, mailer, _ := newTestResetService()
	sm := NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	NewResetHandler(svc, sm, testRenderer(t), logger).Routes(r)
	return r, mailer
}

func postReset(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestForgotPassword_SameResponseForRegisteredAndUnknown(t *testing.T) {
	h, mailer := newResetTestRouter(t)

	registered := postReset(h, "/lupa-password", url.Values{"school_code": {"sd-harapan"}, "email": {"budi@sekolah.id"}})
	unknown := postReset(h, "/lupa-password", url.Values{"school_code": {"sd-harapan"}, "email": {"siapa@sekolah.id"}})

	if registered.Code != http.StatusOK || unknown.Code != http.StatusOK {
		t.Fatalf("status = %d / %d, want 200 / 200", registered.Code, unknown.Code)
	}
	if registered.Body.String() != unknown.Body.String() {
		t.Error("response bodies differ between a registered and an unknown email")
	}
	if !strings.Contains(registered.Body.String(), "tautan untuk mengatur ulang password sudah dikirim") {
		t.Error("success message missing")
	}
	if len(mailer.sent) != 1 {
		t.Errorf("sent %d emails, want 1", len(mailer.sent))
	}
}

func TestResetPasswordPage(t *testing.T) {
	h, mailer := newResetTestRouter(t)
	postReset(h, "/lupa-password", url.Values{"school_code": {"sd-harapan"}, "email": {"budi@sekolah.id"}})
	token := tokenFromLink(t, mailer.sent[0].Link)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reset-password?token="+token, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="password_confirm"`) {
		t.Fatalf("valid token: status = %d, form shown = %v", rec.Code, strings.Contains(rec.Body.String(), "password_confirm"))
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v, want Referrer-Policy: no-referrer and Cache-Control: no-store", rec.Header())
	}

	if rec := postReset(h, "/reset-password", url.Values{"token": {token}, "password": {"Baru-12345"}, "password_confirm": {"Lain-12345"}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), msgPasswordMismatch) {
		t.Errorf("mismatch: status = %d", rec.Code)
	}
	if rec := postReset(h, "/reset-password", url.Values{"token": {token}, "password": {"pendek"}, "password_confirm": {"pendek"}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), msgPasswordTooShort) {
		t.Errorf("short: status = %d", rec.Code)
	}

	rec = postReset(h, "/reset-password", url.Values{"token": {token}, "password": {"Baru-12345"}, "password_confirm": {"Baru-12345"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("reset: status = %d, Location = %q; want 303 to /login", rec.Code, rec.Header().Get("Location"))
	}

	rec = postReset(h, "/reset-password", url.Values{"token": {token}, "password": {"Baru-67890"}, "password_confirm": {"Baru-67890"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sudah dipakai") {
		t.Errorf("reused token: status = %d", rec.Code)
	}
}

func TestResetPasswordPage_InvalidToken(t *testing.T) {
	h, _ := newResetTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reset-password?token=palsu", nil))
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), `name="password"`) {
		t.Errorf("invalid token: status = %d, want 400 without the form", rec.Code)
	}
}
