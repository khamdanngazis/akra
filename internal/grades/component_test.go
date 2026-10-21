package grades

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
	"strconv"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/internal/school"
	"akra/web"
)

// fixture: sekolah A dengan tahun ajaran aktif dan tahun lama, mapel
// Matematika dan IPA. guru1 mengajar Matematika di 1A (tahun aktif); guru2
// hanya punya penugasan di tahun lama; guru3 tanpa penugasan. Sekolah B
// punya mapel sendiri. Semua di satu transaksi yang dibatalkan.
type fixture struct {
	tx                  pgx.Tx
	svc                 *ComponentService
	actor               school.Actor
	schoolA, schoolB    string
	year, oldYear       string
	mtk, ipa, subjectB  string
	guru1, guru2, guru3 string
}

func newFixture(t *testing.T) *fixture {
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
	f := &fixture{tx: tx, svc: NewComponentService(tx)}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', 'test-nilai-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('B', 'test-nilai-b') RETURNING id`)
	user := func(school, email string) string {
		return one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, $2, 'x', $2) RETURNING id`, school, email)
	}
	f.actor = school.Actor{SchoolID: f.schoolA, UserID: user(f.schoolA, "admin@a.uji"), IP: netip.MustParseAddr("192.0.2.9")}
	f.guru1, f.guru2, f.guru3 = user(f.schoolA, "guru1@a.uji"), user(f.schoolA, "guru2@a.uji"), user(f.schoolA, "guru3@a.uji")
	year := `INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, $2, $3, $4, $5) RETURNING id`
	f.year = one(year, f.schoolA, "2026/2027", "2026-07-13", "2027-06-25", true)
	f.oldYear = one(year, f.schoolA, "2025/2026", "2025-07-14", "2026-06-26", false)
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`
	class1A, old6A := one(class, f.schoolA, f.year, "1A"), one(class, f.schoolA, f.oldYear, "6A")
	subject := `INSERT INTO subjects (school_id, name) VALUES ($1, $2) RETURNING id`
	f.mtk, f.ipa = one(subject, f.schoolA, "Matematika"), one(subject, f.schoolA, "IPA")
	f.subjectB = one(subject, f.schoolB, "Matematika")
	assign := `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`
	for _, a := range [][4]string{{f.year, class1A, f.mtk, f.guru1}, {f.oldYear, old6A, f.ipa, f.guru2}} {
		if _, err := tx.Exec(ctx, assign, f.schoolA, a[0], a[1], a[2], a[3]); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func row(id, name, weight string, active bool) RowInput {
	return RowInput{ID: id, Name: name, Weight: weight, Active: active}
}

func (f *fixture) audits(action string) int {
	var n int
	_ = f.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, action).Scan(&n)
	return n
}

func (f *fixture) components(t *testing.T, subjectID string) map[string]Component {
	t.Helper()
	list, err := ListComponents(context.Background(), f.tx, f.schoolA, f.year, subjectID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Component{}
	for _, c := range list {
		out[c.Name] = c
	}
	return out
}

func validation(t *testing.T, err error) *ValidationError {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	return ve
}

func TestComponents_DatabaseConstraints(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	insert := func(name string, weight int) error {
		sp, err := f.tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sp.Rollback(ctx) }()
		_, err = insertComponent(ctx, sp, f.schoolA, Component{AcademicYearID: f.year, SubjectID: f.mtk, Name: name, Weight: weight, IsActive: true})
		if err == nil {
			return sp.Commit(ctx)
		}
		return err
	}
	for _, w := range []int{0, 101, -5} {
		if err := insert("Tugas", w); err == nil || !strings.Contains(err.Error(), "grade_components_weight_check") {
			t.Errorf("weight %d: error = %v", w, err)
		}
	}
	for _, name := range []string{"", " UTS", strings.Repeat("x", 41)} {
		if err := insert(name, 10); err == nil || !strings.Contains(err.Error(), "grade_components_name_check") {
			t.Errorf("name %q: error = %v", name, err)
		}
	}
	if err := insert("UTS", 30); err != nil {
		t.Fatal(err)
	}
	if err := insert("uts", 30); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("duplicate name (case-insensitive): error = %v", err)
	}
	// Nama sama di mapel lain atau tahun lain boleh.
	if _, err := insertComponent(ctx, f.tx, f.schoolA, Component{AcademicYearID: f.year, SubjectID: f.ipa, Name: "UTS", Weight: 30}); err != nil {
		t.Errorf("same name, other subject: %v", err)
	}
	if _, err := insertComponent(ctx, f.tx, f.schoolA, Component{AcademicYearID: f.oldYear, SubjectID: f.mtk, Name: "UTS", Weight: 30}); err != nil {
		t.Errorf("same name, other year: %v", err)
	}
	// Mapel sekolah lain ditolak foreign key komposit.
	sp, _ := f.tx.Begin(ctx)
	if _, err := insertComponent(ctx, sp, f.schoolA, Component{AcademicYearID: f.year, SubjectID: f.subjectB, Name: "X", Weight: 10}); err == nil {
		t.Error("component with another school's subject accepted")
	}
	_ = sp.Rollback(ctx)
}

func TestComponents_SaveValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		rows  []RowInput
		row   int
		field string
		msg   string
	}{
		{"weight 0", []RowInput{row("", "Tugas", "0", true)}, 0, "weight", "1–100"},
		{"weight 101", []RowInput{row("", "Tugas", "101", true)}, 0, "weight", "1–100"},
		{"weight text", []RowInput{row("", "Tugas", "tiga puluh", true)}, 0, "weight", "1–100"},
		{"weight empty", []RowInput{row("", "Tugas", "", true)}, 0, "weight", "1–100"},
		{"name empty", []RowInput{row("", "", "100", true)}, 0, "name", "Isi nama"},
		{"name too long", []RowInput{row("", strings.Repeat("a", 41), "100", true)}, 0, "name", "Maksimal 40"},
		{"duplicate name", []RowInput{row("", "UTS", "50", true), row("", " uts ", "50", true)}, 1, "name", "baris 1"},
	} {
		_, err := f.svc.Save(ctx, f.actor, f.mtk, tc.rows)
		ve := validation(t, err)
		if !strings.Contains(ve.Rows[tc.row][tc.field], tc.msg) {
			t.Errorf("%s: errors %v", tc.name, ve.Rows)
		}
	}
	// Total bobot aktif bukan 100: ditolak, tidak ada yang tersimpan.
	_, err := f.svc.Save(ctx, f.actor, f.mtk, []RowInput{row("", "Tugas", "30", true), row("", "UTS", "30", true), row("", "UAS", "30", true)})
	if ve := validation(t, err); !strings.Contains(ve.Total, "harus 100%, sekarang 90%") {
		t.Errorf("total 90: %q", ve.Total)
	}
	if got := f.components(t, f.mtk); len(got) != 0 || f.audits(ActionComponentCreate) != 0 {
		t.Errorf("rejected save stored %d components", len(got))
	}
	// Baris kosong diabaikan; mapel sekolah lain tidak ditemukan.
	if res, err := f.svc.Save(ctx, f.actor, f.mtk, []RowInput{row("", "", "", true)}); err != nil || res.Created != 0 {
		t.Errorf("blank rows: %v %+v", err, res)
	}
	if _, err := f.svc.Save(ctx, f.actor, f.subjectB, []RowInput{row("", "UAS", "100", true)}); !errors.Is(err, ErrSubjectNotFound) {
		t.Errorf("other school subject: %v", err)
	}
}

func TestComponents_ActivationRule(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	res, err := f.svc.Save(ctx, f.actor, f.mtk, []RowInput{row("", "Tugas", "30", true), row("", "UTS", "30", true), row("", "UAS", "40", true)})
	if err != nil || res.Created != 3 || res.Total != 100 {
		t.Fatalf("initial save: %v %+v", err, res)
	}
	c := f.components(t, f.mtk)
	tugas, uts, uas := c["Tugas"].ID, c["UTS"].ID, c["UAS"].ID
	if c["Tugas"].SortOrder != 1 || c["UAS"].SortOrder != 3 || f.audits(ActionComponentCreate) != 3 {
		t.Errorf("sort order or audits: %+v", c)
	}
	save := func(rows ...RowInput) error {
		_, err := f.svc.Save(ctx, f.actor, f.mtk, rows)
		return err
	}
	// Menonaktifkan UTS membuat total 70: ditolak.
	if ve := validation(t, save(row(tugas, "Tugas", "30", true), row(uts, "UTS", "30", false), row(uas, "UAS", "40", true))); !strings.Contains(ve.Total, "sekarang 70%") {
		t.Errorf("deactivate: %q", ve.Total)
	}
	if !f.components(t, f.mtk)["UTS"].IsActive {
		t.Fatal("rejected save changed data")
	}
	// Nonaktifkan UTS sambil menaikkan UAS ke 70: total 100, diterima.
	if err := save(row(tugas, "Tugas", "30", true), row(uts, "UTS", "30", false), row(uas, "UAS", "70", true)); err != nil {
		t.Fatal(err)
	}
	// Mengaktifkan lagi UTS (total 130) ditolak; dengan UAS kembali 40 diterima.
	if ve := validation(t, save(row(tugas, "Tugas", "30", true), row(uts, "UTS", "30", true), row(uas, "UAS", "70", true))); !strings.Contains(ve.Total, "sekarang 130%") {
		t.Errorf("reactivate: %q", ve.Total)
	}
	if err := save(row(tugas, "Tugas", "30", true), row(uts, "UTS", "30", true), row(uas, "UAS", "40", true)); err != nil {
		t.Fatal(err)
	}
	if n := f.audits(ActionComponentUpdate); n != 4 { // UTS nonaktif + UAS 70, lalu UTS aktif + UAS 40
		t.Errorf("update audits = %d, want 4", n)
	}
	// Tanpa perubahan: tidak ada audit.
	if err := save(row(tugas, "Tugas", "30", true), row(uts, "UTS", "30", true), row(uas, "UAS", "40", true)); err != nil || f.audits(ActionComponentUpdate) != 4 {
		t.Errorf("no-op save: %v, audits %d", err, f.audits(ActionComponentUpdate))
	}
	// Form lama tanpa baris yang sudah ada: total dihitung dari database (110).
	if ve := validation(t, save(row("", "Proyek", "10", true))); !strings.Contains(ve.Total, "sekarang 110%") {
		t.Errorf("stale form: %q", ve.Total)
	}
	// Nama yang sudah dipakai komponen yang tidak ikut terkirim.
	if ve := validation(t, save(row(tugas, "Tugas", "20", true), row("", "uas", "10", false))); !strings.Contains(ve.Rows[1]["name"], "sudah dipakai") {
		t.Errorf("duplicate with stored name: %v", ve.Rows)
	}
	// Baris dengan ID komponen mapel lain ditolak.
	var other string
	_ = f.tx.QueryRow(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent) VALUES ($1, $2, $3, 'X', 100) RETURNING id`,
		f.schoolA, f.year, f.ipa).Scan(&other)
	if err := save(row(other, "X", "100", true)); !errors.Is(err, ErrComponentNotFound) {
		t.Errorf("foreign component id: %v", err)
	}
	// Semua komponen nonaktif boleh (belum ada komponen aktif sama sekali).
	if err := save(row(tugas, "Tugas", "30", false), row(uts, "UTS", "30", false), row(uas, "UAS", "40", false)); err != nil {
		t.Errorf("all inactive: %v", err)
	}
	// Komponen tahun lama tidak ikut halaman tahun aktif.
	if _, err := insertComponent(ctx, f.tx, f.schoolA, Component{AcademicYearID: f.oldYear, SubjectID: f.mtk, Name: "Lama", Weight: 100, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.Page(ctx, f.schoolA, f.mtk)
	if err != nil || len(p.Components) != 3 || p.Total != 0 || p.Year.ID != f.year {
		t.Errorf("page: %v %d components, total %d", err, len(p.Components), p.Total)
	}
}

func TestCanManage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := func(user string, roles ...string) auth.Identity {
		return auth.Identity{SchoolID: f.schoolA, UserID: user, Roles: roles}
	}
	for _, tc := range []struct {
		name    string
		id      auth.Identity
		subject string
		want    bool
	}{
		{"admin", id(f.actor.UserID, auth.RoleAdminTU), f.ipa, true},
		{"assigned teacher", id(f.guru1, auth.RoleGuru), f.mtk, true},
		{"teacher, other subject", id(f.guru1, auth.RoleGuru), f.ipa, false},
		{"teacher, old year only", id(f.guru2, auth.RoleGuru), f.ipa, false},
		{"teacher without assignment", id(f.guru3, auth.RoleGuru), f.mtk, false},
		{"assigned user without guru role", id(f.guru1, auth.RoleBendahara), f.mtk, false},
		{"principal", id(f.guru3, auth.RoleKepalaSekolah), f.mtk, false},
		{"bad id", id(f.guru1, auth.RoleGuru), "x", false},
	} {
		if got, err := CanManage(ctx, f.tx, tc.id, tc.subject); err != nil || got != tc.want {
			t.Errorf("%s: %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
	subjects, err := f.svc.Subjects(ctx, id(f.guru1, auth.RoleGuru))
	if err != nil || len(subjects) != 1 || subjects[0].ID != f.mtk {
		t.Errorf("teacher subjects: %v %+v", err, subjects)
	}
	if subjects, _ := f.svc.Subjects(ctx, id(f.guru2, auth.RoleGuru)); len(subjects) != 0 {
		t.Errorf("old-year teacher subjects: %+v", subjects)
	}
	if subjects, _ := f.svc.Subjects(ctx, id(f.actor.UserID, auth.RoleAdminTU)); len(subjects) != 2 {
		t.Errorf("admin subjects: %+v", subjects)
	}
}

type env struct {
	*fixture
	router http.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	f := newFixture(t)
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
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID, roles, _ := strings.Cut(req.Header.Get("X-Test-User"), "|")
			id := auth.Identity{UserID: userID, SchoolID: f.schoolA, Roles: strings.Split(roles, ","), FullName: "Uji", SchoolName: "A"}
			next.ServeHTTP(w, req.WithContext(auth.ContextWithIdentity(req.Context(), id)))
		})
	})
	NewComponentHandler(f.svc, f.tx, sm, rd, logger).Routes(r)
	return &env{fixture: f, router: r}
}

func (e *env) do(method, path, user, roles string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Test-User", user+"|"+roles)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func formRows(rows ...[3]string) url.Values {
	v := url.Values{"rows": {strconv.Itoa(len(rows))}}
	for i, r := range rows {
		k := strconv.Itoa(i)
		v.Set("id_"+k, "")
		v.Set("name_"+k, r[0])
		v.Set("weight_"+k, r[1])
		if r[2] == "on" {
			v.Set("active_"+k, "on")
		}
	}
	return v
}

func TestComponentHandler(t *testing.T) {
	e := newEnv(t)
	admin := e.actor.UserID
	// Guru pengampu: halaman default ke mapelnya, menyimpan, dan melihat hasilnya.
	page := e.do(http.MethodGet, "/nilai/komponen", e.guru1, "guru", nil)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, `action="/nilai/komponen/`+e.mtk+`"`) || strings.Contains(body, ">IPA<") ||
		!strings.Contains(body, "Isi contoh") || !strings.Contains(body, `name="rows" value="3"`) {
		t.Fatalf("teacher page: status = %d", page.Code)
	}
	rec := e.do(http.MethodPost, "/nilai/komponen/"+e.mtk, e.guru1, "guru", formRows([3]string{"Tugas", "30", "on"}, [3]string{"UTS", "30", "on"}, [3]string{"UAS", "40", "on"}))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/nilai/komponen?mapel="+e.mtk {
		t.Fatalf("save: status = %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	body = e.do(http.MethodGet, "/nilai/komponen?mapel="+e.mtk, e.guru1, "guru", nil).Body.String()
	if !strings.Contains(body, `value="UTS"`) || !strings.Contains(body, `x-text="total">100</span>`) || strings.Contains(body, "Isi contoh") {
		t.Error("saved components not shown")
	}
	// Total bukan 100: 422 dengan pesan, isian tetap tampil.
	rec = e.do(http.MethodPost, "/nilai/komponen/"+e.ipa, admin, "admin_tu", formRows([3]string{"Praktik", "60", "on"}, [3]string{"UAS", "101", "on"}))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Bobot bilangan bulat 1–100.") || !strings.Contains(rec.Body.String(), `value="Praktik"`) {
		t.Errorf("invalid weight: status = %d", rec.Code)
	}
	rec = e.do(http.MethodPost, "/nilai/komponen/"+e.ipa, admin, "admin_tu", formRows([3]string{"Praktik", "60", "on"}))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "harus 100%, sekarang 60%") {
		t.Errorf("total 60: status = %d", rec.Code)
	}
	// Guru tanpa penugasan mapel: 403 untuk lihat dan simpan.
	for _, tc := range []struct{ user, subject string }{{e.guru1, e.ipa}, {e.guru2, e.ipa}, {e.guru3, e.mtk}} {
		if rec := e.do(http.MethodGet, "/nilai/komponen?mapel="+tc.subject, tc.user, "guru", nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as %s: status = %d, want 403", tc.subject, tc.user, rec.Code)
		}
		if rec := e.do(http.MethodPost, "/nilai/komponen/"+tc.subject, tc.user, "guru", formRows([3]string{"UAS", "100", "on"})); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s as %s: status = %d, want 403", tc.subject, tc.user, rec.Code)
		}
	}
	if got := e.components(t, e.ipa); len(got) != 0 {
		t.Error("forbidden or invalid requests stored components")
	}
	// Guru tanpa penugasan sama sekali melihat pesan kosong, bukan 403.
	if rec := e.do(http.MethodGet, "/nilai/komponen", e.guru3, "guru", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "belum ditugaskan") {
		t.Errorf("teacher without subjects: status = %d", rec.Code)
	}
	// Mapel sekolah lain: 404 untuk admin dan guru.
	for _, user := range [][2]string{{admin, "admin_tu"}, {e.guru1, "guru"}} {
		if rec := e.do(http.MethodPost, "/nilai/komponen/"+e.subjectB, user[0], user[1], formRows([3]string{"UAS", "100", "on"})); rec.Code != http.StatusNotFound {
			t.Errorf("POST other school as %s: status = %d", user[1], rec.Code)
		}
		if rec := e.do(http.MethodGet, "/nilai/komponen?mapel="+e.subjectB, user[0], user[1], nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET other school as %s: status = %d", user[1], rec.Code)
		}
	}
	// Peran lain 403.
	for _, role := range []string{"kepala_sekolah", "bendahara", "orang_tua"} {
		if rec := e.do(http.MethodGet, "/nilai/komponen", admin, role, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET as %s: status = %d", role, rec.Code)
		}
	}
}
