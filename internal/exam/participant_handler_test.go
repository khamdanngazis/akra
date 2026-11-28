package exam

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/render"
	"akra/web"
)

type pEnv struct {
	*loginEnv
	logs    *bytes.Buffer
	router  http.Handler
	h       *ParticipantHandler
	cookies map[string]*http.Cookie
}

func newPEnv(t *testing.T) *pEnv {
	t.Helper()
	e := &pEnv{loginEnv: newLoginEnv(t), logs: &bytes.Buffer{}, cookies: map[string]*http.Cookie{}}
	logger := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewParticipantSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	e.h = NewParticipantHandler(e.tx, sm, rd, logger, false)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	e.h.Routes(r)
	e.router = r
	return e
}

func (e *pEnv) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.RemoteAddr = "192.0.2.10:1234"
	for _, c := range e.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(e.cookies, c.Name)
		} else {
			e.cookies[c.Name] = c
		}
	}
	return rec
}

func TestParticipantLoginFlow(t *testing.T) {
	e := newPEnv(t)
	now := time.Now()
	start := now.Add(10 * time.Minute)
	_, code := e.issue(Owner{StudentID: e.a.StudentIDs[0]}, start, start.Add(time.Hour))
	base := "/u/" + e.a.Code

	// QR di kartu: NIS terisi, fokus langsung ke kolom kode (2 langkah:
	// pindai, ketik kode).
	rec := e.do(http.MethodGet, base+"?nis="+url.QueryEscape(e.nis), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login form = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="`+e.nis+`"`) || !strings.Contains(body, `maxlength="12" required autofocus`) {
		t.Errorf("QR prefill missing or focus not on code field")
	}
	dev, ok := e.cookies[DeviceCookieName]
	if !ok || dev.Path != "/u" || !dev.HttpOnly {
		t.Fatalf("device cookie = %+v", dev)
	}

	// Kode salah: pesan umum.
	rec = e.do(http.MethodPost, base, url.Values{"identifier": {e.nis}, "code": {"ZZZZZZZZ"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "kode salah") {
		t.Errorf("wrong code = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "ZZZZZZZZ") {
		t.Error("form echoes the typed code")
	}

	rec = e.do(http.MethodPost, base, url.Values{"identifier": {e.nis}, "code": {code}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/u/beranda" {
		t.Fatalf("login = %d %s\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/u/beranda", nil)
	// Kode tanpa sesi ujian: beranda terbuka, belum ada ujian (daftar ujian
	// dari sesi diuji di TestTakeFlow).
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Belum ada ujian untuk Anda") {
		t.Errorf("home = %d\n%s", rec.Code, rec.Body.String())
	}

	// Keluar: beranda dialihkan ke login sekolah.
	rec = e.do(http.MethodPost, "/u/keluar", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != base {
		t.Errorf("logout = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := e.do(http.MethodGet, "/u/beranda", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("home after logout = %d", rec.Code)
	}

	// Pemindai log: kode tidak pernah tercatat, dalam bentuk apa pun.
	logs := e.logs.String()
	for _, s := range []string{code, strings.ToLower(code), "ZZZZZZZZ"} {
		if strings.Contains(logs, s) {
			t.Errorf("log contains participant code %q", s)
		}
	}
	if !strings.Contains(logs, "participant login") {
		t.Error("login was not logged at all")
	}
}

func TestParticipantLoginMessages(t *testing.T) {
	e := newPEnv(t)
	now := time.Now()
	base := "/u/" + e.a.Code
	e.do(http.MethodGet, base, nil)

	// Sesi besok 08.00 WIB.
	loc := time.FixedZone("WIB", 7*3600)
	y, m, d := now.In(loc).AddDate(0, 0, 1).Date()
	start := time.Date(y, m, d, 8, 0, 0, 0, loc)
	_, code := e.issue(Owner{StudentID: e.a.StudentIDs[0]}, start, start.Add(time.Hour))
	rec := e.do(http.MethodPost, base, url.Values{"identifier": {e.nis}, "code": {code}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "sesi mulai pukul 08.00 WIB") {
		t.Errorf("not yet valid = %d\n%s", rec.Code, rec.Body.String())
	}

	// Kode sudah dipakai di perangkat lain.
	_, code2 := e.issue(Owner{ApplicantID: e.a.ApplicantID}, now.Add(-5*time.Minute), now.Add(time.Hour))
	if rec := e.do(http.MethodPost, base, url.Values{"identifier": {e.regNumber}, "code": {code2}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("first device = %d", rec.Code)
	}
	delete(e.cookies, DeviceCookieName)
	delete(e.cookies, auth.ParticipantCookieName)
	e.do(http.MethodGet, base, nil) // perangkat baru
	rec = e.do(http.MethodPost, base, url.Values{"identifier": {e.regNumber}, "code": {code2}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "dipakai di perangkat lain, minta pengawas") {
		t.Errorf("other device = %d", rec.Code)
	}
}

func TestParticipantLoginGuards(t *testing.T) {
	e := newPEnv(t)
	ctx := context.Background()
	base := "/u/" + e.a.Code

	if rec := e.do(http.MethodGet, "/u/tidak-ada", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown school = %d", rec.Code)
	}
	// Batas per nomor peserta: percobaan ke-11 dalam 15 menit ditolak 429.
	for i := 0; i < 10; i++ {
		e.do(http.MethodPost, base, url.Values{"identifier": {"0000"}, "code": {"ZZZZZZZZ"}})
	}
	if rec := e.do(http.MethodPost, base, url.Values{"identifier": {"0000"}, "code": {"ZZZZZZZZ"}}); rec.Code != http.StatusTooManyRequests {
		t.Errorf("rate limit = %d, want 429", rec.Code)
	}
	// Modul ujian nonaktif: halaman login 404.
	if err := SetModule(ctx, e.tx, e.a.ID, Module{Enabled: false, Release: R3}, "uji", nil); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodGet, base, nil); rec.Code != http.StatusNotFound {
		t.Errorf("disabled module = %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, base, url.Values{"identifier": {e.nis}, "code": {"X"}}); rec.Code != http.StatusNotFound {
		t.Errorf("disabled module POST = %d", rec.Code)
	}
	// Beranda tanpa sesi: ke halaman petunjuk peserta (kode sekolah tidak
	// diketahui), bukan login staf.
	e.cookies = map[string]*http.Cookie{}
	if rec := e.do(http.MethodGet, "/u/beranda", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/u/" {
		t.Errorf("home without session = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}
