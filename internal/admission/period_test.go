package admission

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/platform/render"
	"akra/internal/school"
	"akra/web"
)

type env struct {
	t                *testing.T
	tx               pgx.Tx
	svc              *Service
	actor            school.Actor
	schoolA, schoolB string
	yearA, yearB     string
	router           http.Handler
}

const userA = "77777777-7777-7777-7777-777777777777"

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	e := &env{t: t, tx: tx}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	e.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', 'test-adm-a') RETURNING id`)
	e.schoolB = one(`INSERT INTO schools (name, code) VALUES ('B', 'test-adm-b') RETURNING id`)
	one(`INSERT INTO users (id, school_id, email, password_hash, full_name) VALUES ($1, $2, 'tu@a.test', 'x', 'TU') RETURNING id`, userA, e.schoolA)
	e.yearA = one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2027/2028', '2027-07-12', '2028-06-20', false) RETURNING id`, e.schoolA)
	e.yearB = one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2027/2028', '2027-07-12', '2028-06-20') RETURNING id`, e.schoolB)
	e.svc = NewService(tx)
	e.svc.today = func() time.Time { return time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC) }
	e.actor = school.Actor{SchoolID: e.schoolA, UserID: userA, IP: netip.MustParseAddr("192.0.2.1")}

	e.router = e.routerWith(t, NewApplicantService(tx, pii.DevKeyring(), e.svc), NewDocumentService(tx, nil, pii.DevKeyring()))
	return e
}

// routerWith membuat router staf dengan identitas dari header X-Role.
func (e *env) routerWith(t *testing.T, apps *ApplicantService, docs *DocumentService) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	h := NewHandler(e.tx, e.svc, apps, docs, sm, rd, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: userA, SchoolID: e.schoolA, Roles: strings.Split(r.Header.Get("X-Role"), ","), FullName: "Uji", SchoolName: "Sekolah A"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	h.Routes(r)
	return r
}

func (e *env) do(method, path, role string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Role", role)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *env) form() PeriodForm {
	return PeriodForm{AcademicYearID: e.yearA, Name: " Gelombang  1 ", TargetGrade: "7", OpensOn: "2026-12-01", ClosesOn: "2027-01-31",
		FeeAmount: "Rp150.000", FeeRequired: true, RequiredDocs: []string{"kk", "akta", "kk"}}
}

func TestValidatePeriod(t *testing.T) {
	in, err := validatePeriod(PeriodForm{AcademicYearID: "11111111-1111-1111-1111-111111111111", Name: "G1", TargetGrade: "1", OpensOn: "2026-12-01", ClosesOn: "2026-12-01", FeeRequired: true})
	if err != nil || in.FeeRequired || len(in.RequiredDocs) != 0 {
		t.Errorf("free period must not require payment: %+v, %v", in, err)
	}
	cases := []struct {
		f     PeriodForm
		field string
	}{
		{PeriodForm{TargetGrade: "1", OpensOn: "2026-12-01", ClosesOn: "2026-12-31"}, "name"},
		{PeriodForm{Name: "x", TargetGrade: "1", OpensOn: "2026-12-01", ClosesOn: "2026-12-31"}, "academic_year_id"},
		{PeriodForm{Name: "x", TargetGrade: "13", OpensOn: "2026-12-01", ClosesOn: "2026-12-31"}, "target_grade"},
		{PeriodForm{Name: "x", TargetGrade: "1", OpensOn: "2026-12-31", ClosesOn: "2026-12-01"}, "closes_on"},
		{PeriodForm{Name: "x", TargetGrade: "1", OpensOn: "kemarin", ClosesOn: "2026-12-01"}, "opens_on"},
		{PeriodForm{Name: "x", TargetGrade: "1", OpensOn: "2026-12-01", ClosesOn: "2026-12-31", FeeAmount: "150rb"}, "fee_amount"},
		{PeriodForm{Name: "x", TargetGrade: "1", OpensOn: "2026-12-01", ClosesOn: "2026-12-31", RequiredDocs: []string{"ktp"}}, "required_docs"},
	}
	for _, tc := range cases {
		_, err := validatePeriod(tc.f)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Fields[tc.field] == "" {
			t.Errorf("%+v: error = %v, want message on %s", tc.f, err, tc.field)
		}
	}
}

func TestIsOpen(t *testing.T) {
	p := Period{Status: StatusOpen, OpensOn: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), ClosesOn: time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)}
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	for _, tc := range []struct {
		today time.Time
		want  bool
	}{{day(2026, 11, 30), false}, {day(2026, 12, 1), true}, {day(2027, 1, 31), true}, {day(2027, 2, 1), false}} {
		if got := p.IsOpen(tc.today); got != tc.want {
			t.Errorf("IsOpen(%s) = %v", tc.today.Format(dateLayout), got)
		}
	}
	p.Status = StatusDraft
	if p.IsOpen(day(2026, 12, 15)) {
		t.Error("draft period must not be open")
	}
}

func TestPeriodLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, err := e.svc.Create(ctx, e.actor, e.form())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Gelombang 1" || p.FeeAmount != 150000 || !p.FeeRequiredBeforeVerify || strings.Join(p.RequiredDocs, ",") != "kk,akta" || p.Status != StatusDraft {
		t.Errorf("created = %+v", p)
	}

	// Nama sama di tahun ajaran yang sama ditolak.
	_, err = e.svc.Create(ctx, e.actor, e.form())
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields["name"] == "" {
		t.Errorf("duplicate name: %v", err)
	}
	// Tahun ajaran sekolah lain ditolak.
	f := e.form()
	f.Name, f.AcademicYearID = "Lain", e.yearB
	if _, err := e.svc.Create(ctx, e.actor, f); !errors.As(err, &ve) || ve.Fields["academic_year_id"] == "" {
		t.Errorf("other school's year: %v", err)
	}

	// Tidak bisa dibuka tanpa jalur.
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusOpen); !errors.Is(err, ErrNoTracks) {
		t.Errorf("open without tracks: %v", err)
	}
	t1, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Reguler", Quota: "60"})
	if err != nil || t1.Quota != 60 {
		t.Fatalf("AddTrack = %+v, %v", t1, err)
	}
	if _, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Reguler"}); !errors.As(err, &ve) || ve.Fields["track_name"] == "" {
		t.Errorf("duplicate track: %v", err)
	}
	t2, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Prestasi", UsesTest: true})
	if err != nil || t2.Quota != 0 || !t2.UsesTest || t2.SortOrder <= t1.SortOrder {
		t.Fatalf("AddTrack 2 = %+v, %v", t2, err)
	}
	if _, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "X", Quota: "0"}); !errors.As(err, &ve) || ve.Fields["quota"] == "" {
		t.Errorf("zero quota: %v", err)
	}
	if u, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, t2.ID, TrackForm{Name: "Prestasi Akademik", Quota: "10", UsesTest: true}); err != nil || u.Quota != 10 || u.Name != "Prestasi Akademik" {
		t.Errorf("UpdateTrack = %+v, %v", u, err)
	}

	opened, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusOpen)
	if err != nil || opened.Status != StatusOpen {
		t.Fatalf("open: %+v, %v", opened, err)
	}
	open, err := e.svc.OpenPeriods(ctx, e.schoolA)
	if err != nil || len(open) != 1 || len(open[0].Tracks) != 2 {
		t.Errorf("OpenPeriods = %+v, %v", open, err)
	}
	if open, _ := e.svc.OpenPeriods(ctx, e.schoolB); len(open) != 0 {
		t.Error("other school sees open period")
	}

	// Periode terbuka: jalur terakhir tidak bisa dihapus; periode tidak bisa dihapus.
	if err := e.svc.DeleteTrack(ctx, e.actor, p.ID, t2.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteTrack(ctx, e.actor, p.ID, t1.ID); !errors.Is(err, ErrNoTracks) {
		t.Errorf("delete last track of open period: %v", err)
	}
	if err := e.svc.Delete(ctx, e.actor, p.ID); !errors.Is(err, ErrInUse) {
		t.Errorf("delete open period: %v", err)
	}

	// Sekolah lain tidak bisa membaca atau mengubah.
	actorB := e.actor
	actorB.SchoolID, actorB.UserID = e.schoolB, ""
	if _, err := e.svc.Get(ctx, e.schoolB, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get other school: %v", err)
	}
	if _, err := e.svc.SetStatus(ctx, actorB, p.ID, StatusClosed); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetStatus other school: %v", err)
	}
	if _, err := e.svc.AddTrack(ctx, actorB, p.ID, TrackForm{Name: "Y"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddTrack other school: %v", err)
	}

	// Draf bisa dihapus beserta jalurnya.
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusDraft); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(ctx, e.actor, p.ID); err != nil {
		t.Errorf("delete draft: %v", err)
	}
	if _, err := e.svc.Get(ctx, e.schoolA, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted period still exists: %v", err)
	}
	var audits int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND entity_type = 'admission_period'`, e.schoolA).Scan(&audits)
	if audits < 8 {
		t.Errorf("audit rows = %d", audits)
	}
}

func TestPeriodHandler(t *testing.T) {
	e := newEnv(t)
	form := url.Values{"name": {"Gelombang 1"}, "academic_year_id": {e.yearA}, "target_grade": {"1"}, "opens_on": {"2026-12-01"}, "closes_on": {"2027-01-31"}, "fee_amount": {"100.000"}, "required_docs": {"kk", "akta"}}
	rec := e.do(http.MethodPost, "/pendaftaran/periode", "admin_tu", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d\n%s", rec.Code, rec.Body.String())
	}
	// Wizard (T117): setelah langkah dasar, lanjut ke langkah jadwal.
	loc := rec.Header().Get("Location")
	if !strings.HasSuffix(loc, "/atur/jadwal") {
		t.Fatalf("create redirect = %q", loc)
	}
	path := strings.TrimSuffix(loc, "/atur/jadwal")

	for _, p := range []string{"/pendaftaran/periode", path, "/pendaftaran/periode/baru", path + "/atur/dasar", path + "/atur/jadwal", path + "/atur/jalur", path + "/atur/dokumen"} {
		if rec := e.do(http.MethodGet, p, "admin_tu", nil); rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d\n%s", p, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(http.MethodGet, path+"/ubah", "admin_tu", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path+"/atur/dasar" {
		t.Errorf("old edit link: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	body := e.do(http.MethodGet, "/pendaftaran/periode", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Gelombang 1") || !strings.Contains(body, "Rp100.000") || !strings.Contains(body, "Draf") {
		t.Error("list content")
	}

	// Membuka tanpa jalur: pesan jelas.
	rec = e.do(http.MethodPost, path+"/status", "admin_tu", url.Values{"status": {"buka"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Minimal satu jalur") || !strings.Contains(rec.Body.String(), "belum bisa dibuka") {
		t.Errorf("open without track: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path+"/jalur", "admin_tu", url.Values{"track_name": {""}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Nama jalur wajib") {
		t.Errorf("empty track: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path+"/jalur", "admin_tu", url.Values{"track_name": {"Reguler"}, "quota": {"30"}, "uses_test": {"on"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add track: %d\n%s", rec.Code, rec.Body.String())
	}
	// Jalur dengan tes: belum bisa dibuka sebelum bobot tes 100% (T117).
	if rec := e.do(http.MethodPost, path+"/status", "admin_tu", url.Values{"status": {"buka"}}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "bobot total 100%") {
		t.Errorf("open without tests: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path+"/tes", "admin_tu", url.Values{"test_name": {"Membaca"}, "weight": {"100"}, "max_score": {"100"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add test: %d\n%s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodPost, path+"/status", "admin_tu", url.Values{"status": {"buka"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("open: %d", rec.Code)
	}
	body = e.do(http.MethodGet, path, "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Sedang dibuka") || !strings.Contains(body, "Kuota 30") || !strings.Contains(body, "Tes seleksi</span>") || !strings.Contains(body, "Kartu Keluarga") {
		t.Error("detail content after opening")
	}

	// Kepala sekolah baca saja; guru ditolak.
	body = e.do(http.MethodGet, path, "kepala_sekolah", nil).Body.String()
	if strings.Contains(body, "Tambah jalur") || strings.Contains(body, "Tutup pendaftaran") {
		t.Error("principal sees edit controls")
	}
	if rec := e.do(http.MethodPost, path+"/status", "kepala_sekolah", url.Values{"status": {"tutup"}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal POST: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/pendaftaran/periode", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher GET: %d", rec.Code)
	}

	// Form error ditampilkan ulang.
	bad := url.Values{"name": {""}, "academic_year_id": {e.yearA}, "target_grade": {"1"}, "opens_on": {"2026-12-01"}, "closes_on": {"2026-11-01"}}
	rec = e.do(http.MethodPost, "/pendaftaran/periode", "admin_tu", bad)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Nama gelombang wajib") {
		t.Errorf("invalid form: %d", rec.Code)
	}
}

// TestPeriodWizard menjalankan wizard gelombang dari awal sampai dibuka,
// lalu menyalinnya (T117).
func TestPeriodWizard(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	today := e.svc.Today()
	rec := e.do(http.MethodPost, "/pendaftaran/periode", "admin_tu", url.Values{"kind": {"baru"}, "name": {"Gelombang A"}, "academic_year_id": {e.yearA}, "target_grade": {"7"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d\n%s", rec.Code, rec.Body.String())
	}
	path := strings.TrimSuffix(rec.Header().Get("Location"), "/atur/jadwal")
	id := strings.TrimPrefix(path, "/pendaftaran/periode/")
	p, err := e.svc.Get(ctx, e.schoolA, id)
	if err != nil || p.Status != StatusDraft || len(p.RequiredDocs) != 3 || !p.OpensOn.Equal(today) {
		t.Fatalf("draft after step 1 = %+v, %v", p, err)
	}

	// Langkah jadwal: error tanggal tampil di langkah yang sama.
	if rec := e.do(http.MethodPost, path+"/atur/jadwal", "admin_tu", url.Values{"opens_on": {"2027-02-01"}, "closes_on": {"2027-01-01"}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "tidak boleh sebelum") {
		t.Errorf("bad schedule: %d", rec.Code)
	}
	opens, closes := today.Format(dateLayout), today.AddDate(0, 2, 0).Format(dateLayout)
	rec = e.do(http.MethodPost, path+"/atur/jadwal", "admin_tu", url.Values{"opens_on": {opens}, "closes_on": {closes}, "fee_amount": {"50.000"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path+"/atur/jalur" {
		t.Fatalf("schedule: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// Langkah dasar tidak mengubah jadwal yang sudah diisi.
	if rec := e.do(http.MethodPost, path+"/atur/dasar", "admin_tu", url.Values{"name": {"Gelombang A1"}, "academic_year_id": {e.yearA}, "target_grade": {"7"}, "action": {"keluar"}}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Errorf("basic save and exit: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if p, _ = e.svc.Get(ctx, e.schoolA, id); p.Name != "Gelombang A1" || p.FeeAmount != 50000 || p.ClosesOn.Format(dateLayout) != closes {
		t.Errorf("after basic step = %+v", p)
	}

	// Langkah jalur: tanpa jalur, tombol Lanjut nonaktif; jalur cepat Reguler.
	if body := e.do(http.MethodGet, path+"/atur/jalur", "admin_tu", nil).Body.String(); !strings.Contains(body, "Buat jalur Reguler tanpa tes") || !strings.Contains(body, "minimal satu jalur dulu") {
		t.Error("tracks step without tracks")
	}
	if rec := e.do(http.MethodPost, path+"/jalur", "admin_tu", url.Values{"kembali": {"atur"}, "track_name": {""}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "3. Jalur pendaftaran") {
		t.Errorf("track error stays in wizard: %d", rec.Code)
	}
	rec = e.do(http.MethodPost, path+"/jalur", "admin_tu", url.Values{"kembali": {"atur"}, "track_name": {"Reguler"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path+"/atur/jalur" {
		t.Fatalf("quick track: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// Langkah dokumen: daftar periksa terpenuhi, simpan dan buka.
	body := e.do(http.MethodGet, path+"/atur/dokumen", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Simpan dan buka pendaftaran") || strings.Contains(body, "is-missing") || strings.Contains(body, "Surat keterangan pindah") {
		t.Error("docs step content")
	}
	rec = e.do(http.MethodPost, path+"/atur/dokumen", "admin_tu", url.Values{"required_docs": {"kk", "akta"}, "action": {"buka"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Fatalf("open from wizard: %d\n%s", rec.Code, rec.Body.String())
	}
	if p, _ = e.svc.Get(ctx, e.schoolA, id); p.Status != StatusOpen || len(p.RequiredDocs) != 2 {
		t.Errorf("after open = %s %v", p.Status, p.RequiredDocs)
	}

	// Salin: draf baru dengan jalur dan tes yang sama, jadwal setelah asal.
	if _, err := NewSelectionService(e.tx).AddTest(ctx, e.actor, id, TestForm{Name: "Wawancara", Weight: "100", MaxScore: "100"}); err != nil {
		t.Fatal(err)
	}
	rec = e.do(http.MethodPost, path+"/salin", "admin_tu", nil)
	if rec.Code != http.StatusSeeOther || !strings.HasSuffix(rec.Header().Get("Location"), "/atur/dasar") {
		t.Fatalf("copy: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	copyID := strings.TrimSuffix(strings.TrimPrefix(rec.Header().Get("Location"), "/pendaftaran/periode/"), "/atur/dasar")
	c, err := e.svc.Get(ctx, e.schoolA, copyID)
	if err != nil || c.Name != "Gelombang A1 (salinan)" || c.Status != StatusDraft || len(c.Tracks) != 1 || c.FeeAmount != 50000 ||
		!c.OpensOn.Equal(p.ClosesOn.AddDate(0, 0, 1)) || c.TargetGrade != 7 {
		t.Errorf("copy = %+v, %v", c, err)
	}
	if tests, _ := NewSelectionService(e.tx).Tests(ctx, e.schoolA, copyID); len(tests) != 1 || tests[0].Name != "Wawancara" {
		t.Errorf("copied tests = %+v", tests)
	}
	// Salin kedua kalinya: nama tidak bentrok.
	if c2, err := e.svc.Copy(ctx, e.actor, id); err != nil || c2.Name != "Gelombang A1 (salinan 2)" {
		t.Errorf("second copy = %q, %v", c2.Name, err)
	}

	// Kepala sekolah tidak bisa membuka wizard atau menyalin.
	if rec := e.do(http.MethodGet, path+"/atur/dasar", "kepala_sekolah", nil); rec.Code != http.StatusForbidden {
		t.Errorf("principal wizard: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, path+"/atur/lain", "admin_tu", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown step: %d", rec.Code)
	}
}

func TestCopyName(t *testing.T) {
	if got := copyName(strings.Repeat("a", 80), 1); len([]rune(got)) != 80 || !strings.HasSuffix(got, " (salinan)") {
		t.Errorf("long copy name = %q", got)
	}
}
