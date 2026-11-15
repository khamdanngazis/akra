package dapodik

import (
	"context"
	"fmt"
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
	"akra/internal/platform/storage"
	"akra/internal/school"
	"akra/internal/students"
	"akra/web"
)

const testUser = "88888888-8888-8888-8888-888888888888"

type env struct {
	t                 *testing.T
	tx                pgx.Tx
	router            http.Handler
	schoolA, schoolB  string
	budi, ani, otherB string
	classA            string
	keys              *pii.Keyring
	actor             school.Actor
}

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
	e := &env{t: t, tx: tx, keys: pii.DevKeyring()}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	e.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', 'test-dpd-a') RETURNING id`)
	e.schoolB = one(`INSERT INTO schools (name, code) VALUES ('B', 'test-dpd-b') RETURNING id`)
	one(`INSERT INTO users (id, school_id, email, password_hash, full_name) VALUES ($1, $2, 'tu@a.test', 'x', 'TU') RETURNING id`, testUser, e.schoolA)
	today := time.Now()
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, 'Aktif', $2, $3, true) RETURNING id`,
		e.schoolA, today.AddDate(0, -2, 0), today.AddDate(0, 9, 0))
	e.classA = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1A', 1) RETURNING id`, e.schoolA, year)
	e.budi = one(`INSERT INTO students (school_id, nis, full_name, gender, birth_date) VALUES ($1, '001', 'Budi', 'L', '2019-05-17') RETURNING id`, e.schoolA)
	e.ani = one(`INSERT INTO students (school_id, nis, full_name, gender) VALUES ($1, '002', 'Citra', 'P') RETURNING id`, e.schoolA)
	e.otherB = one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, '001', 'Siswa B') RETURNING id`, e.schoolB)
	for _, s := range []string{e.budi, e.ani} {
		one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			e.schoolA, s, e.classA, year, today.AddDate(0, -1, 0))
	}
	e.actor = school.Actor{SchoolID: e.schoolA, UserID: testUser, IP: netip.MustParseAddr("192.0.2.1")}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(tx, e.keys, store, sm, rd, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: testUser, SchoolID: e.schoolA, Roles: strings.Split(r.Header.Get("X-Role"), ","), FullName: "Uji", SchoolName: "Sekolah A"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	h.Routes(r)
	e.router = r
	return e
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

func TestCopySheetHandler(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := students.NewProfileService(e.tx, e.keys).Save(ctx, e.actor, e.budi, map[string]string{
		"nik": "3201010101010001", "religion": "01", "birth_place": "Bandung", "street": "Jl. Melati 5",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := students.NewGuardianDetailService(e.tx, e.keys).SaveParent(ctx, e.actor, e.budi, "ibu", "Sari", students.GuardianDetailsForm{NIK: "3201010101800002", Education: "09"}); err != nil {
		t.Fatal(err)
	}

	body := e.do(http.MethodGet, "/dapodik", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Budi") || !strings.Contains(body, "Citra") {
		t.Fatal("index lists students")
	}

	rec := e.do(http.MethodGet, "/dapodik/siswa/"+e.budi, "admin_tu", nil)
	body = rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("sheet: %d", rec.Code)
	}
	for _, want := range []string{`data-copy="3201010101010001"`, `data-copy="Islam"`, `data-copy="17/05/2019"`, `data-copy="Sari"`, `data-copy="D4/S1"`, "diisi oleh pusat", "Berikutnya: Citra"} {
		if !strings.Contains(body, want) {
			t.Errorf("sheet missing %q", want)
		}
	}
	// Urutan: Nama sebelum NIK sebelum Data Ayah sebelum Data Ibu.
	iName, iNIK, iAyah, iIbu := strings.Index(body, `<span class="sheet-label">Nama</span>`), strings.Index(body, `<span class="sheet-label">NIK</span>`), strings.Index(body, "Data Ayah Kandung"), strings.Index(body, "Data Ibu Kandung")
	if !(iName < iNIK && iNIK < iAyah && iAyah < iIbu) {
		t.Errorf("section order wrong: %d %d %d %d", iName, iNIK, iAyah, iIbu)
	}

	// T114: accordion per bagian, sembunyikan kosong, bar tanda di bawah.
	for _, want := range []string{`class="sheet-accordion"`, "Sembunyikan yang kosong", `class="sheet-bar"`, `data-sec="0"`, "disalin"} {
		if !strings.Contains(body, want) {
			t.Errorf("sheet missing %q", want)
		}
	}
	// Tandai lalu ke siswa berikutnya; filter "sudah".
	rec = e.do(http.MethodPost, "/dapodik/siswa/"+e.budi+"/tandai", "admin_tu", url.Values{"entered": {"1"}, "next": {e.ani}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/dapodik/siswa/"+e.ani {
		t.Fatalf("mark: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	body = e.do(http.MethodGet, "/dapodik?status=sudah", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Budi") || strings.Contains(body, "Citra") {
		t.Error("entered filter")
	}
	// Siswa tanpa data rinci juga bisa ditandai.
	if rec := e.do(http.MethodPost, "/dapodik/siswa/"+e.ani+"/tandai", "admin_tu", url.Values{"entered": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("mark without profile: %d", rec.Code)
	}
	// Tanda tidak menghapus data rinci saat profil disimpan ulang.
	if _, err := students.NewProfileService(e.tx, e.keys).Save(ctx, e.actor, e.budi, map[string]string{"street": "Jl. Mawar"}); err != nil {
		t.Fatal(err)
	}
	var entered bool
	_ = e.tx.QueryRow(ctx, `SELECT dapodik_entered_at IS NOT NULL FROM student_profiles WHERE student_id = $1`, e.budi).Scan(&entered)
	if !entered {
		t.Error("profile save cleared Dapodik mark")
	}
	if rec := e.do(http.MethodPost, "/dapodik/siswa/"+e.budi+"/tandai", "admin_tu", url.Values{"entered": {"0"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("unmark: %d", rec.Code)
	}

	// Akses.
	if rec := e.do(http.MethodGet, "/dapodik/siswa/"+e.budi, "kepala_sekolah", nil); rec.Code != http.StatusForbidden {
		t.Errorf("principal sheet: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/dapodik", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher index: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/dapodik/siswa/"+e.otherB, "admin_tu", nil); rec.Code != http.StatusNotFound {
		t.Errorf("other school sheet: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/dapodik/siswa/"+e.otherB+"/tandai", "admin_tu", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("other school mark: %d", rec.Code)
	}
}

func TestDapodikIndexPages(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// 55 siswa tambahan di kelas 1A (total 57 di sekolah A).
	for i := 0; i < 55; i++ {
		var id string
		if err := e.tx.QueryRow(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`,
			e.schoolA, fmt.Sprintf("1%03d", i), fmt.Sprintf("Siswa %02d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := e.tx.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
			SELECT $1, $2, c.id, c.academic_year_id, y.start_date FROM classes c JOIN academic_years y ON y.id = c.academic_year_id WHERE c.id = $3`,
			e.schoolA, id, e.classA); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO student_profiles (student_id, school_id, dapodik_entered_at, dapodik_entered_by) VALUES ($1, $2, now(), $3)`,
		e.budi, e.schoolA, testUser); err != nil {
		t.Fatal(err)
	}

	body := e.do(http.MethodGet, "/dapodik", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "<strong>57</strong>") || !strings.Contains(body, "57 siswa · halaman 1 dari 2") || strings.Contains(body, "Siswa B") {
		t.Errorf("index summary or pages wrong")
	}
	if n := strings.Count(body, `>F-PD</a>`); n != indexPageSize {
		t.Errorf("rows on page 1 = %d", n)
	}
	page2 := e.do(http.MethodGet, "/dapodik?hal=2", "admin_tu", nil).Body.String()
	if n := strings.Count(page2, `>F-PD</a>`); n != 7 || !strings.Contains(page2, "halaman 2 dari 2") {
		t.Errorf("rows on page 2 = %d", n)
	}
	// Halaman di luar jangkauan jatuh ke halaman terakhir.
	if body := e.do(http.MethodGet, "/dapodik?hal=99", "admin_tu", nil).Body.String(); !strings.Contains(body, "halaman 2 dari 2") {
		t.Error("out-of-range page")
	}
	// Filter dan pencarian.
	if body := e.do(http.MethodGet, "/dapodik?status=sudah", "admin_tu", nil).Body.String(); strings.Count(body, `>F-PD</a>`) != 1 || !strings.Contains(body, "Budi") {
		t.Error("entered filter")
	}
	if body := e.do(http.MethodGet, "/dapodik?status=kurang", "admin_tu", nil).Body.String(); !strings.Contains(body, "57 siswa") {
		t.Error("incomplete filter")
	}
	if body := e.do(http.MethodGet, "/dapodik?q=siswa+0", "admin_tu", nil).Body.String(); !strings.Contains(body, "10 siswa") {
		t.Error("name search")
	}
	if body := e.do(http.MethodGet, "/dapodik?q=1054", "admin_tu", nil).Body.String(); !strings.Contains(body, "1 siswa") || !strings.Contains(body, "Siswa 54") {
		t.Error("NIS search")
	}
	if body := e.do(http.MethodGet, "/dapodik?q=%25", "admin_tu", nil).Body.String(); !strings.Contains(body, "Tidak ada siswa") {
		t.Error("LIKE wildcard not escaped")
	}
	// Status tidak dikenal diabaikan; tautan halaman membawa filter.
	if body := e.do(http.MethodGet, "/dapodik?status=x&kelas="+e.classA, "admin_tu", nil).Body.String(); !strings.Contains(body, "hal=2&amp;kelas="+e.classA) {
		t.Error("pager link without class filter")
	}
}
