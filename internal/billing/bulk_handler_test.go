package billing

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/render"
	"akra/web"
)

func newBulkRouter(t *testing.T, f *bulkFixture) func(method, path, roles string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: f.actor.UserID, SchoolID: f.schoolA, Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Uji", SchoolName: "Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	NewBulkHandler(f.svc, sm, rd, logger).Routes(r)
	return func(method, path, roles string, form url.Values) *httptest.ResponseRecorder {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req := httptest.NewRequest(method, path, body)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set("X-Roles", roles)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
}

func bulkValues(f BulkForm) url.Values {
	return url.Values{"fee_type_id": {f.FeeTypeID}, "period": {f.Period}, "class_id": {f.ClassID}, "due_date": {f.DueDate}, "amount": {f.Amount}}
}

func TestBulkHandler(t *testing.T) {
	f := newBulkFixture(t)
	do := newBulkRouter(t, f)
	form := bulkValues(sppForm(f, f.class1A))

	// Hanya bendahara.
	for _, roles := range []string{"admin_tu", "guru", "kepala_sekolah", "orang_tua"} {
		if rec := do(http.MethodGet, "/keuangan/tagihan-massal", roles, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s GET: status = %d, want 403", roles, rec.Code)
		}
		if rec := do(http.MethodPost, "/keuangan/tagihan-massal", roles, form); rec.Code != http.StatusForbidden {
			t.Errorf("%s POST run: status = %d, want 403", roles, rec.Code)
		}
	}

	page := do(http.MethodGet, "/keuangan/tagihan-massal", "bendahara", nil).Body.String()
	if !strings.Contains(page, f.spp.ID) || strings.Contains(page, f.inactive.ID) {
		t.Error("form must list active fee types only")
	}
	if !strings.Contains(page, f.class1A) || strings.Contains(page, f.classB) || strings.Contains(page, f.otherYearClass) {
		t.Error("form must list classes of the active year of this school only")
	}

	rec := do(http.MethodPost, "/keuangan/tagihan-massal/pratinjau", "bendahara", form)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Buat 19 tagihan") || !strings.Contains(rec.Body.String(), "Rp2.850.000") {
		t.Fatalf("preview: status = %d", rec.Code)
	}
	if f.invoices(t) != 0 {
		t.Error("preview created invoices")
	}
	rec = do(http.MethodPost, "/keuangan/tagihan-massal", "bendahara", form)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/keuangan/tagihan-massal/") {
		t.Fatalf("run: status = %d", rec.Code)
	}
	result := do(http.MethodGet, rec.Header().Get("Location"), "bendahara", nil).Body.String()
	if !strings.Contains(result, "Selesai") || !strings.Contains(result, `<span class="stat-value">19</span><span class="stat-label">tagihan dibuat`) {
		t.Error("result page does not show 19 created")
	}
	// Pratinjau ulang: semua dilewati, tombol nonaktif.
	if body := do(http.MethodPost, "/keuangan/tagihan-massal/pratinjau", "bendahara", form).Body.String(); !strings.Contains(body, "Tidak ada tagihan baru") {
		t.Error("second preview does not show that nothing is left")
	}

	// Jenis nonaktif dan kelas sekolah lain.
	inactive := bulkValues(BulkForm{FeeTypeID: f.inactive.ID, Period: "2026-10", ClassID: f.class1A})
	if rec := do(http.MethodPost, "/keuangan/tagihan-massal/pratinjau", "bendahara", inactive); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "nonaktif") {
		t.Errorf("inactive fee type: status = %d", rec.Code)
	}
	for _, classID := range []string{f.classB, f.otherYearClass} {
		bad := bulkValues(sppForm(f, classID))
		if rec := do(http.MethodPost, "/keuangan/tagihan-massal/pratinjau", "bendahara", bad); rec.Code != http.StatusNotFound {
			t.Errorf("class %s preview: status = %d, want 404", classID, rec.Code)
		}
		if rec := do(http.MethodPost, "/keuangan/tagihan-massal", "bendahara", bad); rec.Code != http.StatusNotFound {
			t.Errorf("class %s run: status = %d, want 404", classID, rec.Code)
		}
	}
	if rec := do(http.MethodGet, "/keuangan/tagihan-massal/00000000-0000-0000-0000-000000000000", "bendahara", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown run: status = %d, want 404", rec.Code)
	}
}
