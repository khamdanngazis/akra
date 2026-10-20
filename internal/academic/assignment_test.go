package academic

import (
	"context"
	"errors"
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
	"akra/internal/platform/render"
	"akra/web"
)

// aFixture menambah data penugasan di atas fixture mapel: tahun ajaran aktif,
// kelas, guru, dan mapel di sekolah A dan B.
type aFixture struct {
	*fixture
	svc                    *AssignmentService
	yearA, oldYearA        string
	class1A, class1B, old  string
	classB                 string
	guru1, guru2, inactive string
	notTeacher, guruB      string
	mtk, ipa, offSubject   string
	subjectB               string
}

func newAFixture(t *testing.T) *aFixture {
	t.Helper()
	f := &aFixture{fixture: newFixture(t)}
	f.svc = NewAssignmentService(f.tx)
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	year := `INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, $2, $3, $4, $5) RETURNING id`
	f.yearA = one(year, f.schoolA, "2026/2027", "2026-07-13", "2027-06-25", true)
	f.oldYearA = one(year, f.schoolA, "2025/2026", "2025-07-14", "2026-06-26", false)
	yearB := one(year, f.schoolB, "2026/2027", "2026-07-13", "2027-06-25", true)
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`
	f.class1A, f.class1B = one(class, f.schoolA, f.yearA, "1A"), one(class, f.schoolA, f.yearA, "1B")
	f.old = one(class, f.schoolA, f.oldYearA, "6A")
	f.classB = one(class, f.schoolB, yearB, "1A")
	user := func(school, email string, active bool, role string) string {
		id := one(`INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, $2, 'x', $2, $3) RETURNING id`, school, email, active)
		if role != "" {
			if _, err := f.tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, id, role); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	f.guru1 = user(f.schoolA, "guru1@a.uji", true, "guru")
	f.guru2 = user(f.schoolA, "guru2@a.uji", true, "guru")
	f.inactive = user(f.schoolA, "lama@a.uji", false, "guru")
	f.notTeacher = user(f.schoolA, "bendahara@a.uji", true, "bendahara")
	f.guruB = user(f.schoolB, "guru@b.uji", true, "guru")
	subject := `INSERT INTO subjects (school_id, name, code, is_active) VALUES ($1, $2, $3, $4) RETURNING id`
	f.mtk = one(subject, f.schoolA, "Matematika", "MTK", true)
	f.ipa = one(subject, f.schoolA, "IPA", nil, true)
	f.offSubject = one(subject, f.schoolA, "Bahasa Jawa", nil, false)
	f.subjectB = one(subject, f.schoolB, "Matematika", "MTK", true)
	return f
}

func (f *aFixture) teacherOf(t *testing.T, classID, subjectID string) string {
	t.Helper()
	var id string
	err := f.tx.QueryRow(context.Background(), `SELECT teacher_id FROM teaching_assignments WHERE class_id = $1 AND subject_id = $2`, classID, subjectID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Dua guru untuk mapel yang sama di kelas yang sama ditolak database, juga
// penugasan dengan kelas, mapel, atau guru sekolah lain.
func TestAssignments_DatabaseConstraints(t *testing.T) {
	f := newAFixture(t)
	insert := func(school, year, class, subject, teacher string) error {
		return f.sp(t, func(q pgx.Tx) error {
			_, err := q.Exec(context.Background(), `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id)
				VALUES ($1, $2, $3, $4, $5)`, school, year, class, subject, teacher)
			return err
		})
	}
	if _, err := f.tx.Exec(context.Background(), `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id)
		VALUES ($1, $2, $3, $4, $5)`, f.schoolA, f.yearA, f.class1A, f.mtk, f.guru1); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"second teacher, same class and subject": insert(f.schoolA, f.yearA, f.class1A, f.mtk, f.guru2),
		"class of school B":                      insert(f.schoolA, f.yearA, f.classB, f.ipa, f.guru1),
		"subject of school B":                    insert(f.schoolA, f.yearA, f.class1B, f.subjectB, f.guru1),
		"teacher of school B":                    insert(f.schoolA, f.yearA, f.class1B, f.ipa, f.guruB),
		"class not in the given year":            insert(f.schoolA, f.oldYearA, f.class1B, f.ipa, f.guru1),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAssignments_SaveAndAudit(t *testing.T) {
	f := newAFixture(t)
	ctx := context.Background()
	res, err := f.svc.Save(ctx, f.actor, f.class1A, map[string]string{f.mtk: f.guru1, f.ipa: f.guru2})
	if err != nil || res.Assigned != 2 || res.Unassigned != 0 {
		t.Fatalf("save = %+v, %v", res, err)
	}
	if f.teacherOf(t, f.class1A, f.mtk) != f.guru1 || f.teacherOf(t, f.class1A, f.ipa) != f.guru2 {
		t.Error("assignments not stored")
	}
	// Tanpa perubahan: tidak ada audit baru.
	if res, err := f.svc.Save(ctx, f.actor, f.class1A, map[string]string{f.mtk: f.guru1, f.ipa: f.guru2}); err != nil || res.Assigned+res.Unassigned != 0 {
		t.Errorf("unchanged save = %+v, %v", res, err)
	}
	// Ganti guru dan lepas satu mapel.
	if res, err := f.svc.Save(ctx, f.actor, f.class1A, map[string]string{f.mtk: f.guru2, f.ipa: ""}); err != nil || res.Assigned != 1 || res.Unassigned != 1 {
		t.Errorf("change = %+v, %v", res, err)
	}
	if f.teacherOf(t, f.class1A, f.mtk) != f.guru2 || f.teacherOf(t, f.class1A, f.ipa) != "" {
		t.Error("change not stored")
	}
	if n := f.audits(f.schoolA, ActionTeachingAssign); n != 3 {
		t.Errorf("assign audits = %d, want 3", n)
	}
	if n := f.audits(f.schoolA, ActionTeachingUnassign); n != 1 {
		t.Errorf("unassign audits = %d, want 1", n)
	}
	var before, after string
	if err := f.tx.QueryRow(ctx, `SELECT before::text, after::text FROM audit_logs WHERE school_id = $1 AND action = $2 AND before IS NOT NULL`,
		f.schoolA, ActionTeachingAssign).Scan(&before, &after); err != nil || !strings.Contains(before, f.guru1) || !strings.Contains(after, f.guru2) {
		t.Errorf("reassign audit: %s -> %s, %v", before, after, err)
	}
	// Mapel yang tidak dikirim tidak berubah.
	if _, err := f.svc.Save(ctx, f.actor, f.class1A, map[string]string{f.ipa: f.guru1}); err != nil {
		t.Fatal(err)
	}
	if f.teacherOf(t, f.class1A, f.mtk) != f.guru2 {
		t.Error("subject missing from the form was changed")
	}
}

func TestAssignments_Validation(t *testing.T) {
	f := newAFixture(t)
	ctx := context.Background()
	save := func(class string, teachers map[string]string) error {
		return f.sp(t, func(q pgx.Tx) error {
			_, err := NewAssignmentService(q).Save(ctx, f.actor, class, teachers)
			return err
		})
	}
	for name, teacher := range map[string]string{
		"inactive teacher":    f.inactive,
		"user without guru":   f.notTeacher,
		"teacher of school B": f.guruB,
		"not a UUID":          "bukan-uuid",
	} {
		if fields := fieldsOf(t, save(f.class1A, map[string]string{f.mtk: teacher})); fields[f.mtk] == "" {
			t.Errorf("%s: fields = %v", name, fields)
		}
	}
	// Semua atau tidak sama sekali: satu baris salah membatalkan baris lain.
	fieldsOf(t, save(f.class1A, map[string]string{f.mtk: f.guru1, f.ipa: f.inactive}))
	if f.teacherOf(t, f.class1A, f.mtk) != "" {
		t.Error("valid row saved although another row failed")
	}
	// Mapel nonaktif tidak bisa diberi guru baru.
	if fields := fieldsOf(t, save(f.class1A, map[string]string{f.offSubject: f.guru1})); fields[f.offSubject] == "" {
		t.Errorf("inactive subject: %v", fields)
	}
	// Kelas tahun lalu, kelas dan mapel sekolah lain.
	for name, tc := range map[string]struct {
		class, subject string
		want           error
	}{
		"class of an old year": {f.old, f.mtk, ErrClassNotFound},
		"class of school B":    {f.classB, f.mtk, ErrClassNotFound},
		"subject of school B":  {f.class1A, f.subjectB, ErrSubjectNotFound},
	} {
		if err := save(tc.class, map[string]string{tc.subject: f.guru1}); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", name, err, tc.want)
		}
	}
	// Admin sekolah B tidak bisa menyentuh kelas sekolah A.
	if _, err := f.svc.Save(ctx, f.actorB, f.class1A, map[string]string{f.subjectB: f.guruB}); !errors.Is(err, ErrClassNotFound) {
		t.Errorf("school B on class A: %v", err)
	}
}

func TestAssignments_PlanAndMine(t *testing.T) {
	f := newAFixture(t)
	ctx := context.Background()
	if _, err := f.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES
		($1, $2, $3, $4, $5), ($1, $2, $6, $7, $5)`, f.schoolA, f.yearA, f.class1A, f.offSubject, f.guru1, f.class1B, f.mtk); err != nil {
		t.Fatal(err)
	}
	plan, err := f.svc.Plan(ctx, f.schoolA, f.class1A)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range plan.Rows {
		names[r.Subject.Name] = true
	}
	// Mapel aktif ditambah mapel nonaktif yang masih punya penugasan.
	if len(names) != 3 || !names["Bahasa Jawa"] || !names["IPA"] || !names["Matematika"] {
		t.Errorf("plan rows = %v", names)
	}
	for _, tch := range plan.Teachers {
		if tch.ID == f.inactive || tch.ID == f.notTeacher || tch.ID == f.guruB {
			t.Errorf("plan offers %s", tch.Email)
		}
	}
	plan1B, _ := f.svc.Plan(ctx, f.schoolA, f.class1B)
	for _, r := range plan1B.Rows {
		if r.Subject.ID == f.offSubject {
			t.Error("inactive subject without assignment offered")
		}
	}
	mine, err := f.svc.Mine(ctx, f.schoolA, f.guru1)
	if err != nil || len(mine) != 2 || mine[0].ClassName != "1A" || mine[1].ClassName != "1B" {
		t.Errorf("mine = %+v, %v", mine, err)
	}
	if other, _ := f.svc.Mine(ctx, f.schoolA, f.guru2); len(other) != 0 {
		t.Errorf("guru2 sees %+v", other)
	}
}

// ---- Handler ----

type aEnv struct {
	*aFixture
	router http.Handler
}

func newAEnv(t *testing.T) *aEnv {
	t.Helper()
	f := newAFixture(t)
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
	NewAssignmentHandler(f.svc, f.tx, sm, rd, logger).Routes(r)
	return &aEnv{aFixture: f, router: r}
}

func (e *aEnv) do(method, path, user, roles string, form url.Values) *httptest.ResponseRecorder {
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

func TestAssignmentHandler(t *testing.T) {
	e := newAEnv(t)
	admin := e.actor.UserID
	// Admin melihat form kelas 1A dan menyimpan.
	page := e.do(http.MethodGet, "/pengaturan/penugasan?kelas="+e.class1A, admin, "admin_tu", nil)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "Matematika") || !strings.Contains(body, `name="t_`+e.mtk+`"`) || strings.Contains(body, "Bahasa Jawa") {
		t.Fatalf("admin page: status = %d", page.Code)
	}
	rec := e.do(http.MethodPost, "/pengaturan/penugasan/"+e.class1A, admin, "admin_tu", url.Values{"t_" + e.mtk: {e.guru1}, "t_" + e.ipa: {""}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/pengaturan/penugasan?kelas="+e.class1A || e.teacherOf(t, e.class1A, e.mtk) != e.guru1 {
		t.Fatalf("save: status = %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	// Guru tidak aktif ditolak dengan pesan di baris.
	rec = e.do(http.MethodPost, "/pengaturan/penugasan/"+e.class1A, admin, "admin_tu", url.Values{"t_" + e.ipa: {e.inactive}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Pilih guru aktif dari daftar.") {
		t.Errorf("invalid teacher: status = %d", rec.Code)
	}
	// Kelas dan mapel sekolah lain: 404, tidak ada perubahan.
	for _, tc := range []struct{ path, field string }{
		{"/pengaturan/penugasan/" + e.classB, "t_" + e.mtk},
		{"/pengaturan/penugasan/" + e.class1A, "t_" + e.subjectB},
		{"/pengaturan/penugasan/bukan-uuid", "t_" + e.mtk},
	} {
		if rec := e.do(http.MethodPost, tc.path, admin, "admin_tu", url.Values{tc.field: {e.guru2}}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d, want 404", tc.path, rec.Code)
		}
	}
	if rec := e.do(http.MethodGet, "/pengaturan/penugasan?kelas="+e.classB, admin, "admin_tu", nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET other school class: status = %d", rec.Code)
	}
	// Guru hanya melihat penugasannya sendiri, tanpa form.
	mine := e.do(http.MethodGet, "/pengaturan/penugasan", e.guru1, "guru", nil).Body.String()
	if !strings.Contains(mine, "Penugasan Saya") || !strings.Contains(mine, "Matematika") || strings.Contains(mine, `action="/pengaturan/penugasan/`) {
		t.Error("teacher page")
	}
	if other := e.do(http.MethodGet, "/pengaturan/penugasan", e.guru2, "guru", nil).Body.String(); strings.Contains(other, "Matematika") {
		t.Error("teacher sees someone else's assignment")
	}
	// Hanya admin/TU yang mengubah; guru dan peran lain 403.
	for _, role := range []string{"guru", "bendahara", "kepala_sekolah", "orang_tua"} {
		if rec := e.do(http.MethodPost, "/pengaturan/penugasan/"+e.class1A, e.guru1, role, url.Values{"t_" + e.mtk: {e.guru2}}); rec.Code != http.StatusForbidden {
			t.Errorf("POST as %s: status = %d", role, rec.Code)
		}
	}
	for _, role := range []string{"bendahara", "kepala_sekolah", "orang_tua"} {
		if rec := e.do(http.MethodGet, "/pengaturan/penugasan", e.guru1, role, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET as %s: status = %d", role, rec.Code)
		}
	}
	if e.teacherOf(t, e.class1A, e.mtk) != e.guru1 {
		t.Error("forbidden requests changed data")
	}
}
