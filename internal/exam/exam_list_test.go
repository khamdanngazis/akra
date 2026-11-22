package exam

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"akra/internal/auth"
)

// Daftar ujian (T159): saringan, paginasi, ujian bawaan tidak tampil,
// cakupan per peran, dan tahun ajaran.
func TestListExamsFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.a
	// 30 ulangan milik guru (selain fixture), 2 di antaranya berstatus
	// selesai, satu PTS milik admin, satu ujian tahun ajaran lain.
	for i := 1; i <= 30; i++ {
		status := "draf"
		if i <= 2 {
			status = "selesai"
		}
		if _, err := e.tx.Exec(ctx, `INSERT INTO exams (school_id, academic_year_id, name, purpose, subject_id, grade_level, duration_minutes, owner_id, created_by, status)
			VALUES ($1, $2, $3, 'ulangan', $4, 1, 60, $5, $5, $6)`, a.ID, a.YearID, fmt.Sprintf("Ulangan Pecahan %02d", i), a.SubjectID, a.Teacher.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO exams (school_id, academic_year_id, name, purpose, subject_id, grade_level, duration_minutes, owner_id, created_by)
		VALUES ($1, $2, 'PTS Ganjil', 'pts', $3, 1, 90, $4, $4)`, a.ID, a.YearID, a.SubjectID, a.Admin.ID); err != nil {
		t.Fatal(err)
	}
	var oldYear string
	if err := e.tx.QueryRow(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2020/2021', '2020-07-01', '2021-06-30') RETURNING id`, a.ID).Scan(&oldYear); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO exams (school_id, academic_year_id, name, purpose, subject_id, grade_level, duration_minutes, owner_id, created_by)
		VALUES ($1, $2, 'Ulangan Lama', 'ulangan', $3, 1, 60, $4, $4)`, a.ID, oldYear, a.SubjectID, a.Teacher.ID); err != nil {
		t.Fatal(err)
	}
	ident := func(id, role string) auth.Identity {
		return auth.Identity{UserID: id, SchoolID: a.ID, Roles: []string{role}}
	}
	teacher, err := LoadActor(ctx, e.tx, ident(a.Teacher.ID, "guru"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := LoadActor(ctx, e.tx, ident(a.Admin.ID, "admin_tu"))

	names := func(pg ExamPage) string {
		var s []string
		for _, r := range pg.Rows {
			s = append(s, r.Name)
		}
		return strings.Join(s, "|")
	}
	// Guru: 31 ulangan miliknya (30 + fixture) di tahun aktif, 25 per halaman;
	// tanpa PTS admin dan tanpa ujian bawaan.
	p1, err := ListExams(ctx, e.tx, a.ID, teacher, ExamFilter{YearID: a.YearID})
	if err != nil {
		t.Fatal(err)
	}
	if p1.Total != 31 || len(p1.Rows) != ExamPageSize || p1.Pages != 2 || strings.Contains(names(p1), "PTS Ganjil") || strings.Contains(names(p1), PracticeName) {
		t.Fatalf("teacher page 1: total %d rows %d pages %d", p1.Total, len(p1.Rows), p1.Pages)
	}
	if p2, _ := ListExams(ctx, e.tx, a.ID, teacher, ExamFilter{YearID: a.YearID, Page: 2}); len(p2.Rows) != 6 || p2.Page != 2 {
		t.Errorf("teacher page 2: %d rows", len(p2.Rows))
	}
	for _, c := range []struct {
		f    ExamFilter
		want int
	}{
		{ExamFilter{YearID: a.YearID, Text: "pecahan 0"}, 9},
		{ExamFilter{YearID: a.YearID, Text: "100%"}, 0},
		{ExamFilter{YearID: a.YearID, Status: "selesai"}, 2},
		{ExamFilter{YearID: a.YearID, Purpose: PurposeMidterm}, 0},
		{ExamFilter{YearID: a.YearID, SubjectID: a.SubjectID}, 31},
		{ExamFilter{}, 32}, // semua tahun: termasuk Ulangan Lama
		{ExamFilter{YearID: oldYear}, 1},
	} {
		pg, err := ListExams(ctx, e.tx, a.ID, teacher, c.f)
		if err != nil || pg.Total != c.want {
			t.Errorf("filter %+v: total %d, want %d (%v)", c.f, pg.Total, c.want, err)
		}
	}
	// Admin melihat ujian semua guru, termasuk PTS.
	if pg, _ := ListExams(ctx, e.tx, a.ID, admin, ExamFilter{YearID: a.YearID, Purpose: PurposeMidterm}); pg.Total != 1 || names(pg) != "PTS Ganjil" {
		t.Errorf("admin PTS = %s", names(pg))
	}
	opts, err := LoadExamFilterOptions(ctx, e.tx, a.ID, teacher)
	if err != nil || len(opts.Subjects) != 1 || opts.ActiveYear != a.YearID || len(opts.Years) < 2 {
		t.Errorf("options = %+v, %v", opts, err)
	}
}

func TestExamListPage(t *testing.T) {
	e := newEnv(t)
	body := e.get("/ujian/paket/", e.a.Teacher).Body.String()
	for _, want := range []string{`href="/ujian/paket/baru">Buat ujian`, `name="q"`, `name="status"`, `href="/ujian/latihan">Coba halaman ujian`, "Lanjutkan"} {
		if !strings.Contains(body, want) {
			t.Errorf("list page missing %q", want)
		}
	}
	if strings.Contains(body, "option-card") {
		t.Error("purpose cards still on the list page")
	}
	if body := e.get("/ujian/paket/?q=tidak-ada-ujian-ini", e.a.Teacher).Body.String(); !strings.Contains(body, "Tidak ada ujian yang cocok") {
		t.Error("filtered empty state missing")
	}
	if body := e.get("/ujian/paket/", e.a.Principal).Body.String(); strings.Contains(body, `href="/ujian/paket/baru"`) {
		t.Error("principal sees create button")
	}
}
