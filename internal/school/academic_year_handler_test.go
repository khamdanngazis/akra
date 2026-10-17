package school

import (
	"context"
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
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/csrf"
	"akra/internal/platform/render"
	"akra/web"
)

// handlerEnv adalah router tahun ajaran di atas satu transaksi yang
// di-rollback setelah uji, dengan dua sekolah.
type handlerEnv struct {
	t       *testing.T
	tx      pgx.Tx
	router  http.Handler
	schoolA string
	schoolB string
	yearB   AcademicYear
}

// identityHeader dipakai uji untuk memilih pengguna: "<school>|<role,role>".
const identityHeader = "X-Test-Identity"

func newHandlerEnv(t *testing.T, withCSRF bool) *handlerEnv {
	t.Helper()
	pool := testDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	env := &handlerEnv{t: t, tx: tx}
	env.schoolA = createSchool(t, tx, "test-ayh-a")
	env.schoolB = createSchool(t, tx, "test-ayh-b")
	if env.yearB, err = CreateAcademicYear(ctx, tx, env.schoolB, yearInput("2026/2027")); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	h := NewAcademicYearHandler(NewAcademicYearService(tx), sm, rd, logger)

	r := chi.NewRouter()
	if withCSRF {
		r.Use(csrf.Middleware(false, logger))
	}
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			school, roles, _ := strings.Cut(r.Header.Get(identityHeader), "|")
			id := auth.Identity{UserID: "22222222-2222-2222-2222-222222222222", SchoolID: env.schoolID(school), Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "Sekolah Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	h.Routes(r)
	env.router = r
	return env
}

func (e *handlerEnv) schoolID(name string) string {
	if name == "B" {
		return e.schoolB
	}
	return e.schoolA
}

func (e *handlerEnv) do(method, path, who string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set(identityHeader, who)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *handlerEnv) auditCount(action string) int {
	var n int
	if err := e.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, e.schoolA, action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

const (
	admin     = "A|admin_tu"
	principal = "A|kepala_sekolah"
	teacher   = "A|guru"
	parent    = "A|orang_tua"
)

func yearForm(name, start, end string) url.Values {
	return url.Values{"name": {name}, "start_date": {start}, "end_date": {end}}
}

func TestAcademicYearHandler_RoleAccess(t *testing.T) {
	env := newHandlerEnv(t, false)

	for _, who := range []string{teacher, parent, "A|guru,orang_tua"} {
		for _, req := range []struct{ method, path string }{
			{http.MethodGet, "/pengaturan/tahun-ajaran"},
			{http.MethodGet, "/pengaturan/tahun-ajaran/baru"},
			{http.MethodPost, "/pengaturan/tahun-ajaran"},
		} {
			var form url.Values
			if req.method == http.MethodPost {
				form = yearForm("2030/2031", "2030-07-01", "2031-06-30")
			}
			if rec := env.do(req.method, req.path, who, form); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s %s: status = %d, want 403", who, req.method, req.path, rec.Code)
			}
		}
	}

	// Kepala sekolah hanya membaca.
	rec := env.do(http.MethodGet, "/pengaturan/tahun-ajaran", principal, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Tambah tahun ajaran") {
		t.Errorf("principal list: status = %d, has add button = %v", rec.Code, strings.Contains(rec.Body.String(), "Tambah tahun ajaran"))
	}
	if rec := env.do(http.MethodPost, "/pengaturan/tahun-ajaran", principal, yearForm("2030/2031", "2030-07-01", "2031-06-30")); rec.Code != http.StatusForbidden {
		t.Errorf("principal create: status = %d, want 403", rec.Code)
	}

	if rec := env.do(http.MethodGet, "/pengaturan/tahun-ajaran", admin, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Tambah tahun ajaran") {
		t.Errorf("admin list: status = %d", rec.Code)
	}
	if n := env.auditCount(ActionAcademicYearCreate); n != 0 {
		t.Errorf("forbidden requests created %d audit rows", n)
	}
}

func TestAcademicYearHandler_CreateUpdateActivate(t *testing.T) {
	env := newHandlerEnv(t, false)
	ctx := context.Background()

	rec := env.do(http.MethodPost, "/pengaturan/tahun-ajaran", admin, yearForm(" 2026/2027 ", "2026-07-13", "2027-06-25"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/pengaturan/tahun-ajaran" {
		t.Fatalf("create: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	years, _ := ListAcademicYears(ctx, env.tx, env.schoolA)
	if len(years) != 1 || years[0].Name != "2026/2027" || years[0].IsActive {
		t.Fatalf("after create: %+v", years)
	}
	y := years[0]
	if env.auditCount(ActionAcademicYearCreate) != 1 {
		t.Error("create: no audit row")
	}

	list := env.do(http.MethodGet, "/pengaturan/tahun-ajaran", admin, nil).Body.String()
	for _, want := range []string{"2026/2027", "13 Juli 2026", "25 Juni 2027", "Nonaktif", "/" + y.ID + "/aktifkan"} {
		if !strings.Contains(list, want) {
			t.Errorf("list does not contain %q", want)
		}
	}

	if rec := env.do(http.MethodGet, "/pengaturan/tahun-ajaran/"+y.ID+"/ubah", admin, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="2026-07-13"`) {
		t.Errorf("edit form: status = %d", rec.Code)
	}
	rec = env.do(http.MethodPost, "/pengaturan/tahun-ajaran/"+y.ID, admin, yearForm("2026/2027 Genap", "2026-07-13", "2027-06-30"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update: status = %d", rec.Code)
	}
	if got, _ := GetAcademicYear(ctx, env.tx, env.schoolA, y.ID); got.Name != "2026/2027 Genap" || !got.EndDate.Equal(date("2027-06-30")) {
		t.Errorf("after update: %+v", got)
	}
	var before, after string
	if err := env.tx.QueryRow(ctx, `SELECT before->>'name', after->>'name' FROM audit_logs WHERE school_id = $1 AND action = $2`, env.schoolA, ActionAcademicYearUpdate).Scan(&before, &after); err != nil {
		t.Fatalf("update audit: %v", err)
	}
	if before != "2026/2027" || after != "2026/2027 Genap" {
		t.Errorf("update audit before/after = %q / %q", before, after)
	}

	rec = env.do(http.MethodPost, "/pengaturan/tahun-ajaran/"+y.ID+"/aktifkan", admin, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("activate: status = %d", rec.Code)
	}
	if active, _ := GetActiveAcademicYear(ctx, env.tx, env.schoolA); active.ID != y.ID {
		t.Error("year is not active after activate")
	}
	if env.auditCount(ActionAcademicYearActivate) != 1 {
		t.Error("activate: no audit row")
	}
	// Mengaktifkan lagi tidak menambah audit.
	env.do(http.MethodPost, "/pengaturan/tahun-ajaran/"+y.ID+"/aktifkan", admin, url.Values{})
	if n := env.auditCount(ActionAcademicYearActivate); n != 1 {
		t.Errorf("activating an active year: %d audit rows, want 1", n)
	}
	if body := env.do(http.MethodGet, "/pengaturan/tahun-ajaran", admin, nil).Body.String(); !strings.Contains(body, "badge-success") {
		t.Error("list does not mark the active year")
	}
}

func TestAcademicYearHandler_OtherSchoolIs404(t *testing.T) {
	env := newHandlerEnv(t, false)
	ctx := context.Background()
	other := env.yearB.ID

	cases := []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodGet, "/pengaturan/tahun-ajaran/" + other + "/ubah", nil},
		{http.MethodPost, "/pengaturan/tahun-ajaran/" + other, yearForm("diretas", "2030-07-01", "2031-06-30")},
		{http.MethodPost, "/pengaturan/tahun-ajaran/" + other + "/aktifkan", url.Values{}},
		{http.MethodGet, "/pengaturan/tahun-ajaran/bukan-uuid/ubah", nil},
		{http.MethodPost, "/pengaturan/tahun-ajaran/bukan-uuid/aktifkan", url.Values{}},
	}
	for _, tc := range cases {
		rec := env.do(tc.method, tc.path, admin, tc.form)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", tc.method, tc.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), env.yearB.Name) && strings.Contains(tc.path, other) {
			t.Errorf("%s %s: response leaks the other school's data", tc.method, tc.path)
		}
	}
	got, _ := GetAcademicYear(ctx, env.tx, env.schoolB, other)
	if got.Name != env.yearB.Name || got.IsActive {
		t.Errorf("other school's year changed: %+v", got)
	}
	// Daftar sekolah A tidak memuat tahun ajaran sekolah B.
	if body := env.do(http.MethodGet, "/pengaturan/tahun-ajaran", admin, nil).Body.String(); strings.Contains(body, other) {
		t.Error("list of school A contains school B's year")
	}
}

func TestAcademicYearHandler_Validation(t *testing.T) {
	env := newHandlerEnv(t, false)
	if rec := env.do(http.MethodPost, "/pengaturan/tahun-ajaran", admin, yearForm("2026/2027", "2026-07-01", "2027-06-30")); rec.Code != http.StatusSeeOther {
		t.Fatalf("seed create: status = %d", rec.Code)
	}

	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"empty name", yearForm("  ", "2027-07-01", "2028-06-30"), "Nama tahun ajaran wajib diisi."},
		{"long name", yearForm(strings.Repeat("x", 21), "2027-07-01", "2028-06-30"), "Nama maksimal 20 karakter."},
		{"bad start", yearForm("2027/2028", "01/07/2027", "2028-06-30"), "Tanggal mulai wajib diisi"},
		{"missing end", yearForm("2027/2028", "2027-07-01", ""), "Tanggal selesai wajib diisi"},
		{"end before start", yearForm("2027/2028", "2028-06-30", "2027-07-01"), "Tanggal selesai harus setelah tanggal mulai."},
		{"duplicate name", yearForm("2026/2027", "2027-07-01", "2028-06-30"), "Nama tahun ajaran sudah dipakai."},
	}
	for _, tc := range cases {
		rec := env.do(http.MethodPost, "/pengaturan/tahun-ajaran", admin, tc.form)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: status = %d, message %q shown = %v", tc.name, rec.Code, tc.want, strings.Contains(rec.Body.String(), tc.want))
		}
	}
	// Isian tetap ditampilkan setelah error.
	rec := env.do(http.MethodPost, "/pengaturan/tahun-ajaran", admin, yearForm("2027/2028", "2028-06-30", "2027-07-01"))
	if !strings.Contains(rec.Body.String(), `value="2027/2028"`) {
		t.Error("form does not keep the submitted name")
	}
	if n := env.auditCount(ActionAcademicYearCreate); n != 1 {
		t.Errorf("invalid submissions created audit rows: %d, want 1", n)
	}
}

func TestAcademicYearHandler_CSRF(t *testing.T) {
	env := newHandlerEnv(t, true)
	req := httptest.NewRequest(http.MethodPost, "/pengaturan/tahun-ajaran", strings.NewReader(yearForm("2026/2027", "2026-07-01", "2027-06-30").Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set(identityHeader, admin)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF token: status = %d, want 403", rec.Code)
	}
	if years, _ := ListAcademicYears(context.Background(), env.tx, env.schoolA); len(years) != 0 {
		t.Error("year created without a CSRF token")
	}
}
