package billing

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/render"
	"akra/web"
)

func TestArrearsHandler(t *testing.T) {
	f := newArrearsFixture(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: f.actor.UserID, SchoolID: f.schoolA, Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Uji", SchoolName: "Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	NewArrearsHandler(f.tx, rd, logger).Routes(r)
	get := func(path, roles string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Roles", roles)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	for _, roles := range []string{"guru", "orang_tua"} {
		if rec := get("/keuangan/tunggakan", roles); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", roles, rec.Code)
		}
	}
	// Handler memakai tanggal hari ini yang sebenarnya; semua jatuh tempo
	// fixture (paling akhir 15 Oktober 2026) sudah lewat sejak 16 Oktober 2026.
	for _, roles := range []string{"bendahara", "admin_tu", "kepala_sekolah"} {
		rec := get("/keuangan/tunggakan", roles)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `href="/keuangan/tagihan/`+f.inv1.ID+`"`) || !strings.Contains(body, `href="/keuangan/tagihan/`+f.inv3.ID+`"`) {
			t.Errorf("%s: status = %d or rows missing", roles, rec.Code)
		}
	}
	body := get("/keuangan/tunggakan?kelas="+f.classA2, "bendahara").Body.String()
	if !strings.Contains(body, f.inv3.ID) || strings.Contains(body, f.inv1.ID) || !strings.Contains(body, "Rp70.000") {
		t.Error("class filter")
	}
	if body := get("/keuangan/tunggakan?jenis="+f.spp.ID+"&sampai=2026-09-30", "bendahara").Body.String(); !strings.Contains(body, "Rp170.000") {
		t.Error("fee type and due date filter: total must be Rp170.000")
	}
	if body := get("/keuangan/tunggakan?sampai=2026-08-31", "bendahara").Body.String(); !strings.Contains(body, "Tidak ada tunggakan untuk filter ini.") {
		t.Error("empty filtered state")
	}
	if body := get("/keuangan/tunggakan?dari=kemarin", "bendahara").Body.String(); !strings.Contains(body, "Tanggal tidak valid.") {
		t.Error("invalid date message")
	}
}
