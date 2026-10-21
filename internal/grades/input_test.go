package grades

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
	"akra/internal/school"
)

func TestParseScore(t *testing.T) {
	for in, want := range map[string]Score{"0": 0, "100": 10000, "85": 8500, "85,5": 8550, "85.5": 8550, "87,25": 8725, " 9,05 ": 905, "100,00": 10000, "007": 700} {
		got, empty, ok := ParseScore(in)
		if !ok || empty || got != want {
			t.Errorf("ParseScore(%q) = %d, %v, %v; want %d", in, got, empty, ok, want)
		}
	}
	if _, empty, ok := ParseScore("  "); !ok || !empty {
		t.Error("blank is not empty")
	}
	for _, in := range []string{"101", "-1", "100,01", "85,555", "abc", "8 5", "85,", ",5", "1e2", "1000", "85,5,1", "+5"} {
		if _, _, ok := ParseScore(in); ok {
			t.Errorf("ParseScore(%q) accepted", in)
		}
	}
	for s, want := range map[Score]string{8500: "85", 8550: "85,5", 8725: "87,25", 905: "9,05", 0: "0", 10000: "100"} {
		if s.String() != want {
			t.Errorf("Score(%d).String() = %q, want %q", s, s.String(), want)
		}
	}
}

// gradeFixture menambah 30 siswa di kelas 1A, komponen UTS (aktif) dan
// Lama (nonaktif) untuk Matematika, dan guru3 sebagai wali kelas 1A.
type gradeFixture struct {
	*fixture
	input       *InputService
	enrollments []string
	uts, old    string
}

func newGradeFixture(t *testing.T) *gradeFixture {
	t.Helper()
	f := &gradeFixture{fixture: newFixture(t)}
	f.input = NewInputService(f.tx)
	ctx := context.Background()
	if _, err := f.tx.Exec(ctx, `UPDATE classes SET homeroom_teacher_id = $1 WHERE id = $2`, f.guru3, f.class1A); err != nil {
		t.Fatal(err)
	}
	rows, err := f.tx.Query(ctx, `
		WITH s AS (
			INSERT INTO students (school_id, nis, full_name)
			SELECT $1, lpad(g::text, 3, '0'), 'Siswa ' || lpad(g::text, 2, '0') FROM generate_series(1, 30) g RETURNING id)
		INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		SELECT $1, s.id, $2, $3, '2026-07-13' FROM s RETURNING id`, f.schoolA, f.class1A, f.year)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		f.enrollments = append(f.enrollments, id)
	}
	rows.Close()
	comp := `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, is_active) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	if err := f.tx.QueryRow(ctx, comp, f.schoolA, f.year, f.mtk, "UTS", 100, true).Scan(&f.uts); err != nil {
		t.Fatal(err)
	}
	if err := f.tx.QueryRow(ctx, comp, f.schoolA, f.year, f.mtk, "Lama", 50, false).Scan(&f.old); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *gradeFixture) target(t *testing.T, componentID string) Target {
	t.Helper()
	tg, err := ResolveComponent(context.Background(), f.tx, f.schoolA, f.class1A, componentID)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

// stored mengembalikan nilai tersimpan per enrollment: "NULL" untuk nilai
// kosong; enrollment tanpa baris tidak ada di map.
func (f *gradeFixture) stored(t *testing.T, componentID string) map[string]string {
	t.Helper()
	rows, err := f.tx.Query(context.Background(), `SELECT enrollment_id, coalesce(score::text, 'NULL') FROM grades WHERE component_id = $1`, componentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var e, s string
		_ = rows.Scan(&e, &s)
		out[e] = s
	}
	return out
}

func TestGrades_DatabaseConstraints(t *testing.T) {
	f := newGradeFixture(t)
	ctx := context.Background()
	insert := func(score any, yearID string) error {
		sp, _ := f.tx.Begin(ctx)
		defer func() { _ = sp.Rollback(ctx) }()
		_, err := sp.Exec(ctx, `INSERT INTO grades (school_id, academic_year_id, enrollment_id, component_id, score, recorded_by) VALUES ($1, $2, $3, $4, $5, $6)`,
			f.schoolA, yearID, f.enrollments[0], f.uts, score, f.guru1)
		return err
	}
	for _, s := range []any{101, -1, "100.01"} {
		if err := insert(s, f.year); err == nil || !strings.Contains(err.Error(), "grades_score_check") {
			t.Errorf("score %v: error = %v", s, err)
		}
	}
	// Enrollment dan komponen harus satu tahun ajaran.
	if err := insert(80, f.oldYear); err == nil {
		t.Error("grade with mismatched academic year accepted")
	}
	for _, s := range []any{nil, 0, 100, "87.25"} {
		if err := insert(s, f.year); err != nil {
			t.Errorf("score %v rejected: %v", s, err)
		}
	}
}

func TestGrades_Save(t *testing.T) {
	f := newGradeFixture(t)
	ctx := context.Background()
	tg := f.target(t, f.uts)
	scores := map[string]string{}
	for i, e := range f.enrollments {
		scores[e] = fmt.Sprintf("%d", 60+i)
	}
	scores[f.enrollments[0]] = ""     // belum dinilai
	scores[f.enrollments[1]] = "0"    // nol, bukan kosong
	scores[f.enrollments[2]] = "85,5" // koma desimal

	start := time.Now()
	res, err := f.input.Save(ctx, f.teacher(), tg, scores)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("saved 30 grades in %s", elapsed)
	if elapsed > 2*time.Second {
		t.Errorf("saving 30 grades took %s", elapsed)
	}
	if res.Changed != 29 || res.Graded != 29 || res.Empty != 1 {
		t.Errorf("result: %+v", res)
	}
	got := f.stored(t, f.uts)
	if _, ok := got[f.enrollments[0]]; ok {
		t.Error("empty score created a row")
	}
	if got[f.enrollments[1]] != "0.00" || got[f.enrollments[2]] != "85.50" || len(got) != 29 {
		t.Errorf("stored: 0 -> %q, 85,5 -> %q, rows %d", got[f.enrollments[1]], got[f.enrollments[2]], len(got))
	}
	if n := f.audits(ActionGradeCreate); n != 29 {
		t.Errorf("create audits = %d", n)
	}

	// Ubah: 85,5 -> 90, 0 -> kosong (NULL, bukan dihapus), sisanya sama.
	scores[f.enrollments[2]], scores[f.enrollments[1]] = "90", ""
	res, err = f.input.Save(ctx, f.teacher(), tg, scores)
	if err != nil || res.Changed != 2 || res.Empty != 2 {
		t.Fatalf("update: %v %+v", err, res)
	}
	got = f.stored(t, f.uts)
	if got[f.enrollments[1]] != "NULL" || got[f.enrollments[2]] != "90.00" {
		t.Errorf("after update: %q %q", got[f.enrollments[1]], got[f.enrollments[2]])
	}
	var before, after string
	if err := f.tx.QueryRow(ctx, `SELECT before::text, after::text FROM audit_logs WHERE school_id = $1 AND action = $2 AND after->>'enrollment_id' = $3`,
		f.schoolA, ActionGradeUpdate, f.enrollments[2]).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, `"score": "85.50"`) || !strings.Contains(after, `"score": "90.00"`) || strings.Contains(after, "Siswa") {
		t.Errorf("audit before %s after %s", before, after)
	}
	if err := f.tx.QueryRow(ctx, `SELECT before::text, after::text FROM audit_logs WHERE school_id = $1 AND action = $2 AND after->>'enrollment_id' = $3`,
		f.schoolA, ActionGradeUpdate, f.enrollments[1]).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, `"score": "0.00"`) || !strings.Contains(after, `"score": null`) {
		t.Errorf("audit to empty: before %s after %s", before, after)
	}
	// Simpan lagi tanpa perubahan: tidak ada audit baru.
	if res, err := f.input.Save(ctx, f.teacher(), tg, scores); err != nil || res.Changed != 0 || f.audits(ActionGradeUpdate) != 2 {
		t.Errorf("no-op save: %v %+v", err, res)
	}

	// Isian tidak valid: ditolak semua, tidak ada yang berubah.
	for _, bad := range []string{"101", "-1", "abc", "85,555"} {
		in := map[string]string{f.enrollments[3]: "99", f.enrollments[4]: bad}
		_, err := f.input.Save(ctx, f.teacher(), tg, in)
		var se *ScoreError
		if !errors.As(err, &se) || se.Fields[f.enrollments[4]] == "" || len(se.Fields) != 1 {
			t.Errorf("score %q: err = %v", bad, err)
		}
	}
	if f.stored(t, f.uts)[f.enrollments[3]] != "63.00" {
		t.Error("rejected save changed a valid row")
	}
	// Siswa di luar kelas dan komponen nonaktif.
	if _, err := f.input.Save(ctx, f.teacher(), tg, map[string]string{"00000000-0000-0000-0000-000000000000": "80"}); !errors.Is(err, ErrUnknownEnrollment) {
		t.Errorf("unknown enrollment: %v", err)
	}
	if _, err := f.input.Save(ctx, f.teacher(), f.target(t, f.old), map[string]string{f.enrollments[0]: "80"}); !errors.Is(err, ErrComponentInactive) {
		t.Errorf("inactive component: %v", err)
	}
}

// teacher adalah guru1, guru pengampu Matematika di 1A.
func (f *gradeFixture) teacher() school.Actor {
	return school.Actor{SchoolID: f.schoolA, UserID: f.guru1}
}

func TestGrades_MovedStudents(t *testing.T) {
	f := newGradeFixture(t)
	ctx := context.Background()
	tg := f.target(t, f.uts)
	if _, err := f.input.Save(ctx, f.teacher(), tg, map[string]string{f.enrollments[0]: "70"}); err != nil {
		t.Fatal(err)
	}
	// Siswa 0 (sudah dinilai) dan siswa 1 (belum) pindah kelas.
	if _, err := f.tx.Exec(ctx, `UPDATE enrollments SET end_date = '2026-09-30' WHERE id = ANY($1)`, f.enrollments[:2]); err != nil {
		t.Fatal(err)
	}
	rows, err := ClassComponentRows(ctx, f.tx, f.schoolA, f.class1A, f.uts)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]StudentRow{}
	for _, r := range rows {
		seen[r.EnrollmentID] = r
	}
	if r, ok := seen[f.enrollments[0]]; !ok || r.Active || r.Grade == nil || r.Grade.Score.String() != "70" {
		t.Errorf("moved student with grade: %+v", r)
	}
	if _, ok := seen[f.enrollments[1]]; ok || len(rows) != 29 {
		t.Errorf("moved student without grade listed; rows %d", len(rows))
	}
	res, err := f.input.Save(ctx, f.teacher(), tg, map[string]string{f.enrollments[0]: "75"})
	if err != nil || res.Empty != 28 {
		t.Errorf("save with moved student: %v %+v", err, res)
	}
}

func TestCanEnterGrades(t *testing.T) {
	f := newGradeFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name          string
		user          string
		roles         []string
		class, subjct string
		want          bool
	}{
		{"assigned teacher", f.guru1, []string{auth.RoleGuru}, f.class1A, f.mtk, true},
		{"assigned teacher, other subject", f.guru1, []string{auth.RoleGuru}, f.class1A, f.ipa, false},
		{"homeroom teacher without assignment", f.guru3, []string{auth.RoleGuru}, f.class1A, f.mtk, false},
		{"other teacher", f.guru2, []string{auth.RoleGuru}, f.class1A, f.mtk, false},
		{"admin", f.actor.UserID, []string{auth.RoleAdminTU}, f.class1A, f.mtk, false},
		{"assignment without guru role", f.guru1, []string{auth.RoleBendahara}, f.class1A, f.mtk, false},
	} {
		id := auth.Identity{SchoolID: f.schoolA, UserID: tc.user, Roles: tc.roles}
		if got, err := CanEnterGrades(ctx, f.tx, id, tc.class, tc.subjct); err != nil || got != tc.want {
			t.Errorf("%s: %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
}

var (
	inputAction = regexp.MustCompile(`action="/nilai/input/([0-9a-f-]{36})/([0-9a-f-]{36})"`)
	scoreInput  = regexp.MustCompile(`name="g_([0-9a-f-]{36})"`)
)

func TestInputHandler(t *testing.T) {
	f := newGradeFixture(t)
	e := newEnvFor(t, f.fixture)
	ctx := context.Background()
	pagePath := "/nilai/input?kelas=" + f.class1A + "&mapel=" + f.mtk

	// Halaman awal guru: daftar kelas–mapel yang diampu.
	if body := e.do(http.MethodGet, "/nilai/input", f.guru1, "guru", nil).Body.String(); !strings.Contains(body, "kelas="+f.class1A+"&amp;mapel="+f.mtk) {
		t.Error("teaching list misses 1A Matematika")
	}
	// Tabel input: 30 siswa, komponen aktif pertama, tab tanpa komponen nonaktif.
	rec := e.do(http.MethodGet, pagePath, f.guru1, "guru", nil)
	body := rec.Body.String()
	m := inputAction.FindStringSubmatch(body)
	if rec.Code != http.StatusOK || m == nil || m[1] != f.class1A || m[2] != f.uts || len(scoreInput.FindAllString(body, -1)) != 30 || strings.Contains(body, ">Lama ·") {
		t.Fatalf("input page: status = %d", rec.Code)
	}
	if !strings.Contains(body, `x-text="empty">30</span> belum dinilai`) {
		t.Error("empty counter")
	}
	form := url.Values{}
	for i, en := range f.enrollments {
		form.Set("g_"+en, fmt.Sprintf("%d,5", 50+i))
	}
	form.Set("g_"+f.enrollments[5], "")
	rec = e.do(http.MethodPost, "/nilai/input/"+f.class1A+"/"+f.uts, f.guru1, "guru", form)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "komponen="+f.uts) {
		t.Fatalf("save: status = %d", rec.Code)
	}
	if body := e.do(http.MethodGet, pagePath+"&komponen="+f.uts, f.guru1, "guru", nil).Body.String(); !strings.Contains(body, `value="50,5"`) ||
		!strings.Contains(body, `x-text="empty">1</span>`) {
		t.Error("saved values not shown")
	}
	// Nilai tidak valid: 422, sel ditandai, isian tetap tampil.
	form.Set("g_"+f.enrollments[0], "101")
	rec = e.do(http.MethodPost, "/nilai/input/"+f.class1A+"/"+f.uts, f.guru1, "guru", form)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `value="101"`) || !strings.Contains(rec.Body.String(), "grade-row has-error") {
		t.Errorf("invalid score: status = %d", rec.Code)
	}
	if f.stored(t, f.uts)[f.enrollments[0]] != "50.50" {
		t.Error("invalid save changed data")
	}

	// Wali kelas tanpa penugasan, guru lain, dan admin: 403.
	for _, user := range [][2]string{{f.guru3, "guru"}, {f.guru2, "guru"}, {f.actor.UserID, "admin_tu"}, {f.guru1, "kepala_sekolah"}} {
		if rec := e.do(http.MethodGet, pagePath, user[0], user[1], nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET as %v: status = %d", user, rec.Code)
		}
		if rec := e.do(http.MethodPost, "/nilai/input/"+f.class1A+"/"+f.uts, user[0], user[1], url.Values{"g_" + f.enrollments[0]: {"10"}}); rec.Code != http.StatusForbidden {
			t.Errorf("POST as %v: status = %d", user, rec.Code)
		}
	}
	if f.stored(t, f.uts)[f.enrollments[0]] != "50.50" {
		t.Error("forbidden save changed data")
	}
	// Komponen atau kelas tahun lain, mapel sekolah lain, ID rusak: 404.
	var oldComp string
	if err := f.tx.QueryRow(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent) VALUES ($1, $2, $3, 'Lalu', 100) RETURNING id`,
		f.schoolA, f.oldYear, f.mtk).Scan(&oldComp); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/nilai/input/" + f.class1A + "/" + oldComp,
		"/nilai/input/" + f.oldClass + "/" + f.uts,
		"/nilai/input/bukan/" + f.uts,
		"/nilai/input/" + f.class1A + "/bukan",
	} {
		if rec := e.do(http.MethodPost, path, f.guru1, "guru", url.Values{"g_" + f.enrollments[0]: {"10"}}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d, want 404", path, rec.Code)
		}
	}
	for _, path := range []string{
		"/nilai/input?kelas=" + f.class1A + "&mapel=" + f.subjectB,
		"/nilai/input?kelas=" + f.class1A + "&mapel=" + f.mtk + "&komponen=" + oldComp,
	} {
		if rec := e.do(http.MethodGet, path, f.guru1, "guru", nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
	// Komponen nonaktif: hanya dilihat; simpan 400.
	if body := e.do(http.MethodGet, pagePath+"&komponen="+f.old, f.guru1, "guru", nil).Body.String(); !strings.Contains(body, "nonaktif") || !strings.Contains(body, " disabled") {
		t.Error("inactive component page")
	}
	if rec := e.do(http.MethodPost, "/nilai/input/"+f.class1A+"/"+f.old, f.guru1, "guru", url.Values{"g_" + f.enrollments[0]: {"10"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("save inactive component: status = %d", rec.Code)
	}
	// Mapel tanpa komponen aktif: arahkan ke halaman komponen.
	if _, err := f.tx.Exec(ctx, `UPDATE grade_components SET is_active = false WHERE id = $1`, f.uts); err != nil {
		t.Fatal(err)
	}
	if body := e.do(http.MethodGet, pagePath, f.guru1, "guru", nil).Body.String(); !strings.Contains(body, "/nilai/komponen?mapel="+f.mtk) {
		t.Error("setup link missing")
	}
}
