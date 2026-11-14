package admission

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/pii"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/web"
)

func TestSchoolLogo(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewLogoService(e.tx, store)

	if _, ok, err := svc.Get(ctx, e.schoolA); err != nil || ok {
		t.Fatalf("logo before upload: %v %v", ok, err)
	}
	if err := svc.Save(ctx, e.schoolA, strings.NewReader("bukan gambar")); !errors.Is(err, ErrLogoType) {
		t.Errorf("text logo: %v", err)
	}
	// Header PNG palsu: lolos deteksi tipe, gagal dibaca sebagai gambar.
	if err := svc.Save(ctx, e.schoolA, bytes.NewReader(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...))); !errors.Is(err, ErrLogoType) {
		t.Errorf("broken png: %v", err)
	}
	if err := svc.Save(ctx, e.schoolA, bytes.NewReader(append(pngBytes(t, ""), make([]byte, MaxLogoSize)...))); !errors.Is(err, ErrLogoTooLarge) {
		t.Errorf("large logo: %v", err)
	}
	if err := svc.Save(ctx, e.schoolA, bytes.NewReader(pngBytes(t, "pertama"))); err != nil {
		t.Fatal(err)
	}
	first, _, _ := svc.Get(ctx, e.schoolA)
	if err := svc.Save(ctx, e.schoolA, bytes.NewReader(pngBytes(t, "kedua"))); err != nil {
		t.Fatal(err)
	}
	second, ok, err := svc.Get(ctx, e.schoolA)
	if err != nil || !ok || second.Key == first.Key || second.ContentType != "image/png" {
		t.Fatalf("replace logo: %+v %v %v", second, ok, err)
	}
	if _, err := store.Get(ctx, first.Key); err == nil {
		t.Error("old logo object not deleted")
	}

	// Halaman publik: logo tampil di kepala halaman dan bisa diunduh.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	h := NewPublicHandler(e.tx, e.svc, NewApplicantService(e.tx, pii.DevKeyring(), e.svc), NewDocumentService(e.tx, store, pii.DevKeyring()), sm, rd, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	h.Routes(r)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/s/test-adm-a/daftar/"); !strings.Contains(rec.Body.String(), "/s/test-adm-a/logo?v="+logoVersion(second.Key)) {
		t.Error("start page without logo url")
	}
	rec := get("/s/test-adm-a/logo")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !strings.HasSuffix(rec.Body.String(), "kedua") {
		t.Errorf("logo: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	// Sekolah lain tanpa logo: 404, bukan logo sekolah A.
	if rec := get("/s/test-adm-b/logo"); rec.Code != http.StatusNotFound {
		t.Errorf("other school logo: %d", rec.Code)
	}

	if err := svc.Remove(ctx, e.schoolA); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := svc.Get(ctx, e.schoolA); ok {
		t.Error("logo still set after remove")
	}
	if _, err := store.Get(ctx, second.Key); err == nil {
		t.Error("logo object not deleted after remove")
	}
	if rec := get("/s/test-adm-a/logo"); rec.Code != http.StatusNotFound {
		t.Errorf("logo after remove: %d", rec.Code)
	}
	if err := svc.Remove(ctx, e.schoolA); err != nil {
		t.Errorf("remove twice: %v", err)
	}
}
