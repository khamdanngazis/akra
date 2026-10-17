package classes

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/web"
)

type env struct {
	t      *testing.T
	tx     pgx.Tx
	router http.Handler

	schoolA, schoolB       string
	yearA1, yearA2, yearB  string
	ani, lama, ortu, guruB string // guru aktif, guru nonaktif, orang tua (A), guru sekolah B
	classB                 string
}

const identityHeader = "X-Test-Identity"

const (
	admin     = "A|admin_tu"
	principal = "A|kepala_sekolah"
	teacher   = "A|guru"
	parent    = "A|orang_tua"
)

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
	school := `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`
	year := `INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, $2, $3, $4, $5) RETURNING id`
	user := `WITH u AS (INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, $2, 'x', $3, $4) RETURNING id)
		INSERT INTO user_roles (user_id, role_id) SELECT u.id, r.id FROM u, roles r WHERE r.code = $5 RETURNING user_id`
	e.schoolA = one(school, "test-clh-a")
	e.schoolB = one(school, "test-clh-b")
	e.yearA1 = one(year, e.schoolA, "2025/2026", "2025-07-01", "2026-06-30", true)
	e.yearA2 = one(year, e.schoolA, "2026/2027", "2026-07-01", "2027-06-30", false)
	e.yearB = one(year, e.schoolB, "2025/2026", "2025-07-01", "2026-06-30", true)
	e.ani = one(user, e.schoolA, "ani@a.id", "Bu Ani", true, "guru")
	e.lama = one(user, e.schoolA, "lama@a.id", "Pak Lama", false, "guru")
	e.ortu = one(user, e.schoolA, "ortu@a.id", "Ibu Ortu", true, "orang_tua")
	e.guruB = one(user, e.schoolB, "guru@b.id", "Pak Budi", true, "guru")
	e.classB = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1A', 1) RETURNING id`, e.schoolB, e.yearB)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	h := NewHandler(NewService(tx), tx, sm, rd, logger)

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, roles, _ := strings.Cut(r.Header.Get(identityHeader), "|")
			schoolID := e.schoolA
			if s == "B" {
				schoolID = e.schoolB
			}
			id := auth.Identity{UserID: "33333333-3333-3333-3333-333333333333", SchoolID: schoolID, Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "Sekolah Uji"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	h.Routes(r)
	e.router = r
	return e
}

func (e *env) do(method, path, who string, form url.Values) *httptest.ResponseRecorder {
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

func (e *env) audits(action string) int {
	var n int
	if err := e.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, e.schoolA, action).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) classID(name string) string {
	var id string
	if err := e.tx.QueryRow(context.Background(), `SELECT id FROM classes WHERE school_id = $1 AND name = $2`, e.schoolA, name).Scan(&id); err != nil {
		e.t.Fatalf("class %s: %v", name, err)
	}
	return id
}

func classForm(year, name, grade, teacherID string) url.Values {
	return url.Values{"academic_year_id": {year}, "name": {name}, "grade_level": {grade}, "homeroom_teacher_id": {teacherID}}
}

func TestClassesHandler_RoleAccess(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodPost, "/kelas", admin, classForm(e.yearA1, "1A", "1", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("seed: status = %d", rec.Code)
	}
	id := e.classID("1A")

	for _, who := range []string{teacher, parent} {
		for _, p := range []string{"/kelas", "/kelas/baru", "/kelas/" + id + "/ubah"} {
			if rec := e.do(http.MethodGet, p, who, nil); rec.Code != http.StatusForbidden {
				t.Errorf("%s GET %s: status = %d, want 403", who, p, rec.Code)
			}
		}
		if rec := e.do(http.MethodPost, "/kelas/"+id, who, classForm("", "Diretas", "1", "")); rec.Code != http.StatusForbidden {
			t.Errorf("%s POST update: status = %d, want 403", who, rec.Code)
		}
	}

	rec := e.do(http.MethodGet, "/kelas", principal, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1A") || strings.Contains(rec.Body.String(), "/ubah") {
		t.Errorf("principal list: status = %d, shows edit link = %v", rec.Code, strings.Contains(rec.Body.String(), "/ubah"))
	}
	if rec := e.do(http.MethodGet, "/kelas/"+id+"/ubah", principal, nil); rec.Code != http.StatusForbidden {
		t.Errorf("principal edit form: status = %d, want 403", rec.Code)
	}
}

func TestClassesHandler_CreateWithTeacher(t *testing.T) {
	e := newEnv(t)

	rec := e.do(http.MethodPost, "/kelas", admin, classForm(e.yearA1, " 1A ", "1", e.ani))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/kelas?tahun="+e.yearA1 {
		t.Fatalf("create: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if e.audits(ActionClassCreate) != 1 {
		t.Error("create: no audit row")
	}

	// Tanpa ?tahun, daftar memakai tahun aktif (yearA1).
	body := e.do(http.MethodGet, "/kelas", admin, nil).Body.String()
	for _, want := range []string{"<strong>1A</strong>", "Bu Ani", "2025/2026 (aktif)"} {
		if !strings.Contains(body, want) {
			t.Errorf("list does not contain %q", want)
		}
	}
	// Tahun lain belum punya kelas.
	if body := e.do(http.MethodGet, "/kelas?tahun="+e.yearA2, admin, nil).Body.String(); !strings.Contains(body, "Belum ada kelas di tahun ajaran 2026/2027") {
		t.Error("year 2026/2027 does not show the empty state")
	}
}

func TestClassesHandler_TeacherEligibility(t *testing.T) {
	e := newEnv(t)

	cases := map[string]string{"parent": e.ortu, "inactive teacher": e.lama, "teacher of another school": e.guruB, "not a uuid": "bukan-uuid"}
	for name, userID := range cases {
		rec := e.do(http.MethodPost, "/kelas", admin, classForm(e.yearA1, "2A", "2", userID))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Wali kelas harus guru aktif di sekolah ini.") {
			t.Errorf("%s: status = %d, want 400 with teacher error", name, rec.Code)
		}
	}
	if e.audits(ActionClassCreate) != 0 {
		t.Error("rejected teachers created classes")
	}

	// Pilihan wali kelas hanya guru aktif sekolah ini.
	form := e.do(http.MethodGet, "/kelas/baru", admin, nil).Body.String()
	if !strings.Contains(form, `value="`+e.ani+`"`) {
		t.Error("active teacher is not an option")
	}
	for name, id := range map[string]string{"inactive teacher": e.lama, "parent": e.ortu, "other school": e.guruB} {
		if strings.Contains(form, id) {
			t.Errorf("%s appears as a homeroom teacher option", name)
		}
	}
}

func TestClassesHandler_Update(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.do(http.MethodPost, "/kelas", admin, classForm(e.yearA1, "1A", "1", ""))
	id := e.classID("1A")

	if rec := e.do(http.MethodGet, "/kelas/"+id+"/ubah", admin, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "2025/2026") {
		t.Fatalf("edit form: status = %d", rec.Code)
	}

	// Ubah nama saja.
	if rec := e.do(http.MethodPost, "/kelas/"+id, admin, classForm("", "1 Melati", "1", "")); rec.Code != http.StatusSeeOther {
		t.Fatalf("rename: status = %d", rec.Code)
	}
	if e.audits(ActionClassUpdate) != 1 || e.audits(ActionClassAssignTeacher) != 0 {
		t.Errorf("rename: update=%d assign=%d, want 1 and 0", e.audits(ActionClassUpdate), e.audits(ActionClassAssignTeacher))
	}
	// Tetapkan wali kelas saja.
	e.do(http.MethodPost, "/kelas/"+id, admin, classForm("", "1 Melati", "1", e.ani))
	if e.audits(ActionClassUpdate) != 1 || e.audits(ActionClassAssignTeacher) != 1 {
		t.Errorf("assign: update=%d assign=%d, want 1 and 1", e.audits(ActionClassUpdate), e.audits(ActionClassAssignTeacher))
	}
	c, _ := GetByID(ctx, e.tx, e.schoolA, id)
	if c.Name != "1 Melati" || c.HomeroomTeacherID != e.ani {
		t.Errorf("after update: %+v", c)
	}
	// Tanpa perubahan: tidak ada audit baru.
	e.do(http.MethodPost, "/kelas/"+id, admin, classForm("", "1 Melati", "1", e.ani))
	if e.audits(ActionClassUpdate)+e.audits(ActionClassAssignTeacher) != 2 {
		t.Error("saving without changes added audit rows")
	}
	// Mengganti ke orang tua ditolak.
	if rec := e.do(http.MethodPost, "/kelas/"+id, admin, classForm("", "1 Melati", "1", e.ortu)); rec.Code != http.StatusBadRequest {
		t.Errorf("assign parent: status = %d, want 400", rec.Code)
	}
	if c, _ := GetByID(ctx, e.tx, e.schoolA, id); c.HomeroomTeacherID != e.ani {
		t.Error("homeroom teacher changed after a rejected update")
	}
}

func TestClassesHandler_InactiveCurrentTeacherIsKept(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.do(http.MethodPost, "/kelas", admin, classForm(e.yearA1, "4A", "4", e.ani))
	id := e.classID("4A")
	// Bu Ani dinonaktifkan setelah menjadi wali kelas.
	if _, err := e.tx.Exec(ctx, `UPDATE users SET is_active = false WHERE id = $1`, e.ani); err != nil {
		t.Fatal(err)
	}

	form := e.do(http.MethodGet, "/kelas/"+id+"/ubah", admin, nil).Body.String()
	if !strings.Contains(form, "Bu Ani (nonaktif)") {
		t.Error("edit form does not keep the inactive current teacher as an option")
	}
	if rec := e.do(http.MethodPost, "/kelas/"+id, admin, classForm("", "4 Mawar", "4", e.ani)); rec.Code != http.StatusSeeOther {
		t.Fatalf("rename with inactive current teacher: status = %d", rec.Code)
	}
	if c, _ := GetByID(ctx, e.tx, e.schoolA, id); c.Name != "4 Mawar" || c.HomeroomTeacherID != e.ani {
		t.Errorf("after rename: %+v", c)
	}
	// Daftar baru (form tambah) tetap tidak menawarkan guru nonaktif.
	if strings.Contains(e.do(http.MethodGet, "/kelas/baru", admin, nil).Body.String(), e.ani) {
		t.Error("inactive teacher is offered on the new class form")
	}
}

func TestClassesHandler_OtherSchool(t *testing.T) {
	e := newEnv(t)

	for _, p := range []string{"/kelas?tahun=" + e.yearB, "/kelas?tahun=bukan-uuid", "/kelas/" + e.classB + "/ubah", "/kelas/bukan-uuid/ubah"} {
		if rec := e.do(http.MethodGet, p, admin, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", p, rec.Code)
		}
	}
	if rec := e.do(http.MethodPost, "/kelas/"+e.classB, admin, classForm("", "Diretas", "1", "")); rec.Code != http.StatusNotFound {
		t.Errorf("update other school's class: status = %d, want 404", rec.Code)
	}
	rec := e.do(http.MethodPost, "/kelas", admin, classForm(e.yearB, "9Z", "9", ""))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Pilih tahun ajaran yang tersedia.") {
		t.Errorf("create in other school's year: status = %d", rec.Code)
	}
	var n int
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM classes WHERE name IN ('9Z', 'Diretas')`).Scan(&n)
	if n != 0 {
		t.Errorf("%d classes created across schools", n)
	}
}

func TestClassesHandler_Validation(t *testing.T) {
	e := newEnv(t)
	e.do(http.MethodPost, "/kelas", admin, classForm(e.yearA1, "1A", "1", ""))

	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"empty name", classForm(e.yearA1, " ", "1", ""), "Nama kelas wajib diisi."},
		{"long name", classForm(e.yearA1, strings.Repeat("x", 21), "1", ""), "Nama kelas maksimal 20 karakter."},
		{"grade 13", classForm(e.yearA1, "13A", "13", ""), "Pilih tingkat 1 sampai 12."},
		{"grade empty", classForm(e.yearA1, "2A", "", ""), "Pilih tingkat 1 sampai 12."},
		{"duplicate", classForm(e.yearA1, "1a", "1", ""), "Nama kelas sudah dipakai di tahun ajaran ini."},
	}
	for _, tc := range cases {
		rec := e.do(http.MethodPost, "/kelas", admin, tc.form)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: status = %d, message shown = %v", tc.name, rec.Code, strings.Contains(rec.Body.String(), tc.want))
		}
	}
	if e.audits(ActionClassCreate) != 1 {
		t.Errorf("invalid submissions created audit rows")
	}
}
