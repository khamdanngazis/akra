package students

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/mail"
	"akra/internal/platform/render"
	"akra/web"
)

type hEnv struct {
	t      *testing.T
	tx     pgx.Tx
	router http.Handler
	svc    *Service

	schoolA, schoolB       string
	yearActive, yearOld    string
	class1A, class1B, cl9Z string
	studentB               string
}

const idHeader = "X-Test-Identity"

const (
	admin     = "A|admin_tu"
	principal = "A|kepala_sekolah"
	teacher   = "A|guru"
	parent    = "A|orang_tua"
)

func newHEnv(t *testing.T) *hEnv {
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

	e := &hEnv{t: t, tx: tx}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	year := `INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, $2, $3, $4, $5) RETURNING id`
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, $4) RETURNING id`
	e.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', 'test-sth-a') RETURNING id`)
	e.schoolB = one(`INSERT INTO schools (name, code) VALUES ('B', 'test-sth-b') RETURNING id`)
	// Tahun aktif mencakup "hari ini" agar enrollment awal (mulai hari ini) valid.
	today := time.Now()
	e.yearActive = one(year, e.schoolA, "Aktif", today.AddDate(0, -2, 0), today.AddDate(0, 9, 0), true)
	e.yearOld = one(year, e.schoolA, "Lama", today.AddDate(-1, -2, 0), today.AddDate(0, -3, 0), false)
	e.class1A = one(class, e.schoolA, e.yearActive, "1A", 1)
	e.class1B = one(class, e.schoolA, e.yearActive, "1B", 1)
	e.cl9Z = one(class, e.schoolA, e.yearOld, "9Z", 9)
	e.studentB = one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, '001', 'Siswa Sekolah B') RETURNING id`, e.schoolB)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	e.svc = NewService(tx)
	activation := auth.NewActivationService(tx, auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}),
		&mail.LogMailer{Logger: logger}, "http://localhost:8080", logger)
	h := NewHandler(e.svc, NewGuardianService(tx), NewGuardianAccountService(tx, activation, logger), sm, rd, logger)

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, roles, _ := strings.Cut(r.Header.Get(idHeader), "|")
			schoolID := e.schoolA
			if s == "B" {
				schoolID = e.schoolB
			}
			id := auth.Identity{UserID: "55555555-5555-5555-5555-555555555555", SchoolID: schoolID, Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "Sekolah Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	h.Routes(r)
	e.router = r
	return e
}

func (e *hEnv) do(method, path, who string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set(idHeader, who)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *hEnv) audits(action string) int {
	var n int
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, e.schoolA, action).Scan(&n)
	return n
}

func studentForm(nis, name, class string) url.Values {
	return url.Values{"nis": {nis}, "full_name": {name}, "class_name": {class}, "gender": {"L"}, "birth_date": {"2018-05-17"}}
}

// createStudent membuat siswa lewat handler dan mengembalikan ID-nya.
func (e *hEnv) createStudent(nis, name, class string) string {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/siswa", admin, studentForm(nis, name, class))
	if rec.Code != http.StatusSeeOther {
		e.t.Fatalf("create %s: status = %d\n%s", nis, rec.Code, rec.Body.String())
	}
	return strings.TrimPrefix(rec.Header().Get("Location"), "/siswa/")
}

func TestStudentsHandler_RoleAccess(t *testing.T) {
	e := newHEnv(t)
	id := e.createStudent("001", "Budi", "1A")

	for _, who := range []string{teacher, parent} {
		for _, p := range []string{"/siswa", "/siswa/" + id, "/siswa/baru", "/siswa/" + id + "/ubah"} {
			if rec := e.do(http.MethodGet, p, who, nil); rec.Code != http.StatusForbidden {
				t.Errorf("%s GET %s: status = %d, want 403", who, p, rec.Code)
			}
		}
	}
	rec := e.do(http.MethodGet, "/siswa", principal, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Budi") || strings.Contains(rec.Body.String(), "Tambah siswa") {
		t.Errorf("principal list: status = %d", rec.Code)
	}
	rec = e.do(http.MethodGet, "/siswa/"+id, principal, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "pindah-kelas") || strings.Contains(rec.Body.String(), "Ubah data") {
		t.Errorf("principal detail: status = %d, shows edit controls", rec.Code)
	}
	for _, p := range []string{"/siswa", "/siswa/" + id, "/siswa/" + id + "/pindah-kelas"} {
		if rec := e.do(http.MethodPost, p, principal, studentForm("002", "X", "1A")); rec.Code != http.StatusForbidden {
			t.Errorf("principal POST %s: status = %d, want 403", p, rec.Code)
		}
	}
}

func TestStudentsHandler_CreateRequiresClass(t *testing.T) {
	e := newHEnv(t)
	cases := map[string]string{
		"":   "Kelas wajib diisi.",
		"7C": "tidak ada di tahun ajaran Aktif",
		"9Z": "tidak ada di tahun ajaran Aktif", // kelas tahun ajaran lain
	}
	for class, want := range cases {
		rec := e.do(http.MethodPost, "/siswa", admin, studentForm("010", "Tanpa Kelas", class))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("class %q: status = %d, message shown = %v", class, rec.Code, strings.Contains(rec.Body.String(), want))
		}
	}
	var n int
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM students WHERE school_id = $1`, e.schoolA).Scan(&n)
	if n != 0 || e.audits(ActionStudentCreate) != 0 {
		t.Errorf("students created without a class: %d", n)
	}
}

func TestStudentsHandler_CreateAndMove(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()

	id := e.createStudent(" 007 ", "  Siti   Aminah ", "1a")
	st, err := GetByID(ctx, e.tx, e.schoolA, id)
	if err != nil || st.NIS != "007" || st.FullName != "Siti Aminah" || st.Gender != "L" {
		t.Fatalf("created = %+v, %v", st, err)
	}
	active, err := ActiveEnrollment(ctx, e.tx, e.schoolA, id, e.yearActive)
	if err != nil || active.ClassID != e.class1A {
		t.Fatalf("initial enrollment = %+v, %v", active, err)
	}
	if e.audits(ActionStudentCreate) != 1 || e.audits(ActionStudentPlace) != 1 {
		t.Errorf("audits: create=%d place=%d", e.audits(ActionStudentCreate), e.audits(ActionStudentPlace))
	}

	// Pindah kelas: enrollment baru, bukan edit kolom.
	moveDate := time.Now().AddDate(0, 0, 1).Format(dateLayout)
	rec := e.do(http.MethodPost, "/siswa/"+id+"/pindah-kelas", admin, url.Values{"class_name": {"1B"}, "date": {moveDate}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("move: status = %d\n%s", rec.Code, rec.Body.String())
	}
	history, _ := ListEnrollments(ctx, e.tx, e.schoolA, id)
	if len(history) != 2 || history[0].ClassID != e.class1B || !history[0].Active() || history[1].ClassID != e.class1A || history[1].Active() {
		t.Fatalf("history after move = %+v", history)
	}
	if history[1].ID != active.ID {
		t.Error("the original enrollment row was replaced instead of closed")
	}
	if e.audits(ActionStudentPlace) != 2 {
		t.Errorf("place audits = %d, want 2", e.audits(ActionStudentPlace))
	}

	body := e.do(http.MethodGet, "/siswa/"+id, admin, nil).Body.String()
	for _, want := range []string{"Riwayat kelas", "<strong>1B</strong>", "<strong>1A</strong>", "Sekarang"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail does not contain %q", want)
		}
	}

	// Pindah ke kelas yang tidak ada.
	if rec := e.do(http.MethodPost, "/siswa/"+id+"/pindah-kelas", admin, url.Values{"class_name": {"9Z"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("move to class of another year: status = %d, want 400", rec.Code)
	}
	// Duplikat NIS.
	if rec := e.do(http.MethodPost, "/siswa", admin, studentForm("007", "Lain", "1A")); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "NIS sudah dipakai") {
		t.Errorf("duplicate NIS: status = %d", rec.Code)
	}
}

func TestStudentsHandler_Update(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()
	id := e.createStudent("100", "Budi", "1A")

	form := url.Values{"nis": {"100"}, "full_name": {"Budi Santoso"}, "birth_date": {""}, "gender": {"L"}, "status": {"pindah"}, "class_name": {"1B"}}
	if rec := e.do(http.MethodPost, "/siswa/"+id, admin, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("update: status = %d\n%s", rec.Code, rec.Body.String())
	}
	st, _ := GetByID(ctx, e.tx, e.schoolA, id)
	if st.FullName != "Budi Santoso" || st.Status != StatusTransferred || st.BirthDate != nil {
		t.Errorf("after update = %+v", st)
	}
	// Form ubah tidak memindahkan kelas.
	if active, _ := ActiveEnrollment(ctx, e.tx, e.schoolA, id, e.yearActive); active.ClassID != e.class1A {
		t.Error("update form changed the class")
	}
	var before, after string
	_ = e.tx.QueryRow(ctx, `SELECT before->>'full_name', after->>'full_name' FROM audit_logs WHERE school_id = $1 AND action = 'student.update'`, e.schoolA).Scan(&before, &after)
	if before != "Budi" || after != "Budi Santoso" {
		t.Errorf("update audit = %q -> %q", before, after)
	}
	// Simpan tanpa perubahan: tidak ada audit baru.
	e.do(http.MethodPost, "/siswa/"+id, admin, form)
	if e.audits(ActionStudentUpdate) != 1 {
		t.Errorf("update audits = %d, want 1", e.audits(ActionStudentUpdate))
	}
	if rec := e.do(http.MethodPost, "/siswa/"+id, admin, url.Values{"nis": {""}, "full_name": {""}, "status": {"naik"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid update: status = %d", rec.Code)
	}
}

func TestStudentsHandler_OtherSchoolIs404(t *testing.T) {
	e := newHEnv(t)
	other := e.studentB
	for _, p := range []string{"/siswa/" + other, "/siswa/" + other + "/ubah", "/siswa/bukan-uuid"} {
		if rec := e.do(http.MethodGet, p, admin, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", p, rec.Code)
		}
	}
	for _, p := range []string{"/siswa/" + other, "/siswa/" + other + "/pindah-kelas"} {
		rec := e.do(http.MethodPost, p, admin, url.Values{"nis": {"x"}, "full_name": {"Diretas"}, "class_name": {"1A"}})
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d, want 404", p, rec.Code)
		}
	}
	if body := e.do(http.MethodGet, "/siswa?q=Sekolah", admin, nil).Body.String(); strings.Contains(body, "Siswa Sekolah B") {
		t.Error("list shows a student of another school")
	}
	st, _ := GetByID(context.Background(), e.tx, e.schoolB, other)
	if st.FullName != "Siswa Sekolah B" {
		t.Error("other school's student was modified")
	}
}

// seedStudents membuat n siswa langsung lewat SQL (cepat), setengahnya di
// kelas 1A dan setengahnya di 1B pada tahun aktif.
func (e *hEnv) seedStudents(n int) {
	e.t.Helper()
	_, err := e.tx.Exec(context.Background(), `
		WITH s AS (
			INSERT INTO students (school_id, nis, full_name, status)
			SELECT $1, lpad(g::text, 5, '0'),
			       (ARRAY['Ahmad','Budi','Citra','Dewi','Eka','Fajar','Gita','Hadi','Intan','Joko'])[1 + g % 10] || ' ' || g,
			       CASE WHEN g % 20 = 0 THEN 'lulus' ELSE 'aktif' END
			FROM generate_series(1, $2) g
			RETURNING id, nis
		)
		INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		SELECT $1, s.id, CASE WHEN s.nis::int % 2 = 0 THEN $3::uuid ELSE $4::uuid END, $5, current_date
		FROM s`, e.schoolA, n, e.class1A, e.class1B, e.yearActive)
	if err != nil {
		e.t.Fatal(err)
	}
}

var rowRe = regexp.MustCompile(`<td class="nowrap">`)

func TestStudentsHandler_PaginationAndFilters(t *testing.T) {
	e := newHEnv(t)
	e.seedStudents(60)

	body := e.do(http.MethodGet, "/siswa", admin, nil).Body.String()
	if n := len(rowRe.FindAllString(body, -1)); n != 25 {
		t.Errorf("page 1 rows = %d, want 25", n)
	}
	if !strings.Contains(body, "Menampilkan 1–25 dari 60 siswa.") || !strings.Contains(body, "Halaman 1 dari 3") {
		t.Error("page 1 summary missing")
	}
	body = e.do(http.MethodGet, "/siswa?page=3", admin, nil).Body.String()
	if n := len(rowRe.FindAllString(body, -1)); n != 10 || !strings.Contains(body, "Menampilkan 51–60 dari 60 siswa.") {
		t.Errorf("page 3 rows = %d", n)
	}

	// Filter kelas + status, dan tautan halaman berikutnya membawa filter.
	body = e.do(http.MethodGet, "/siswa?kelas="+e.class1A+"&status=aktif", admin, nil).Body.String()
	if !strings.Contains(body, "dari 27 siswa") { // 30 siswa genap di 1A, 3 di antaranya lulus
		t.Errorf("class+status filter summary wrong:\n%s", regexp.MustCompile(`Menampilkan[^<]*`).FindString(body))
	}
	if !strings.Contains(body, "kelas="+e.class1A) || !strings.Contains(body, "status=aktif") || !strings.Contains(body, "page=2") {
		t.Error("next page link does not keep the filters")
	}
	body = e.do(http.MethodGet, "/siswa?q=dewi", admin, nil).Body.String()
	if !strings.Contains(body, "dari 6 siswa") {
		t.Errorf("name search summary wrong: %s", regexp.MustCompile(`Menampilkan[^<]*`).FindString(body))
	}
	if body := e.do(http.MethodGet, "/siswa?q=00012", admin, nil).Body.String(); !strings.Contains(body, "dari 1 siswa") {
		t.Error("NIS prefix search failed")
	}
	if body := e.do(http.MethodGet, "/siswa?page=99", admin, nil).Body.String(); !strings.Contains(body, "Tidak ada siswa") && !strings.Contains(body, "Belum ada data siswa") {
		t.Error("out-of-range page does not show the empty state")
	}
}

func TestStudentsList_500StudentsUnderOneSecond(t *testing.T) {
	e := newHEnv(t)
	e.seedStudents(500)
	if _, err := e.tx.Exec(context.Background(), `ANALYZE students; ANALYZE enrollments`); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"", "a", "budi 1", "004", "tidak-ada"} {
		start := time.Now()
		rec := e.do(http.MethodGet, "/siswa?q="+url.QueryEscape(q)+"&kelas="+e.class1B, admin, nil)
		elapsed := time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("q=%q: status = %d", q, rec.Code)
		}
		t.Logf("q=%-10q %v", q, elapsed)
		if elapsed > time.Second {
			t.Errorf("q=%q took %v, want < 1s", q, elapsed)
		}
	}

	pg, err := e.svc.List(context.Background(), e.schoolA, ListFilter{Query: "a", AcademicYearID: e.yearActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Rows) != PageSize || pg.Total < 100 {
		t.Errorf("rows = %d, total = %d", len(pg.Rows), pg.Total)
	}
}

func TestStudentsHandler_CreateAfterActiveYearEnded(t *testing.T) {
	e := newHEnv(t)
	// Tahun aktif sudah berakhir kemarin.
	if _, err := e.tx.Exec(context.Background(), `UPDATE academic_years SET start_date = current_date - 300, end_date = current_date - 1 WHERE id = $1`, e.yearActive); err != nil {
		t.Fatal(err)
	}
	rec := e.do(http.MethodPost, "/siswa", admin, studentForm("300", "Terlambat", "1A"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Tahun ajaran aktif sudah berakhir") {
		t.Errorf("status = %d, message shown = %v", rec.Code, strings.Contains(rec.Body.String(), "sudah berakhir"))
	}
	var n int
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM students WHERE school_id = $1`, e.schoolA).Scan(&n)
	if n != 0 {
		t.Errorf("%d students created", n)
	}
}
