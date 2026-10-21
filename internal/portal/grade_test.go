package portal

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/grades"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

// printDay adalah tanggal cetak rapor di uji.
var printDay = time.Date(2026, 12, 19, 0, 0, 0, 0, time.UTC)

// gradeData menyiapkan nilai: kelas 1A (Ani) punya IPA (Tugas 30, UTS 30,
// UAS 40) dan Matematika (UTS 100), semua nilai Ani lengkap (IPA 79, MTK 88).
// Kelas 2B (Budi) punya IPA tanpa nilai: belum lengkap.
func (f *fixture) gradeData(t *testing.T) (ipaComps [3]string) {
	t.Helper()
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	teacher := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru.nilai@uji.id', 'x', 'Bu Guru') RETURNING id`, f.schoolA)
	var c1A, c2B, year string
	if err := f.tx.QueryRow(ctx, `SELECT id, academic_year_id FROM classes WHERE school_id = $1 AND name = '1A'`, f.schoolA).Scan(&c1A, &year); err != nil {
		t.Fatal(err)
	}
	c2B = one(`SELECT id FROM classes WHERE school_id = $1 AND name = '2B'`, f.schoolA)
	ipa, mtk := one(`INSERT INTO subjects (school_id, name) VALUES ($1, 'IPA') RETURNING id`, f.schoolA), one(`INSERT INTO subjects (school_id, name) VALUES ($1, 'Matematika') RETURNING id`, f.schoolA)
	for _, a := range [][2]string{{c1A, ipa}, {c1A, mtk}, {c2B, ipa}} {
		if _, err := f.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
			f.schoolA, year, a[0], a[1], teacher); err != nil {
			t.Fatal(err)
		}
	}
	comp := `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, sort_order) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	ipaComps = [3]string{one(comp, f.schoolA, year, ipa, "Tugas", 30, 1), one(comp, f.schoolA, year, ipa, "UTS", 30, 2), one(comp, f.schoolA, year, ipa, "UAS", 40, 3)}
	mtkUTS := one(comp, f.schoolA, year, mtk, "UTS", 100, 1)
	var aniEnrollment string
	if err := f.tx.QueryRow(ctx, `SELECT id FROM enrollments WHERE student_id = $1 AND class_id = $2`, f.ani, c1A).Scan(&aniEnrollment); err != nil {
		t.Fatal(err)
	}
	input := grades.NewInputService(f.tx)
	actor := school.Actor{SchoolID: f.schoolA, UserID: teacher}
	for compID, score := range map[string]string{ipaComps[0]: "80", ipaComps[1]: "90", ipaComps[2]: "70", mtkUTS: "88"} {
		tg, err := grades.ResolveComponent(ctx, f.tx, f.schoolA, c1A, compID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := input.Save(ctx, actor, tg, map[string]string{aniEnrollment: score}); err != nil {
			t.Fatal(err)
		}
	}
	return ipaComps
}

func TestChildGrades_MatchesRecap(t *testing.T) {
	f := newFixture(t)
	f.gradeData(t)
	ctx := context.Background()
	svc := NewService(f.tx)
	g, err := svc.GetChildGrades(ctx, f.schoolA, f.userSari, f.ani)
	if err != nil {
		t.Fatal(err)
	}
	if !g.HasClass || g.Child.ClassName != "1A" || !g.Complete || len(g.Subjects) != 2 || g.ShowComponents || g.KKMLabel != "KKM" {
		t.Fatalf("grades: %+v", g)
	}
	// Sama dengan rekap guru (T72) untuk siswa yang sama.
	var enrollment, classID string
	if err := f.tx.QueryRow(ctx, `SELECT e.id, e.class_id FROM enrollments e WHERE e.student_id = $1 AND e.end_date IS NULL
		AND e.academic_year_id = (SELECT id FROM academic_years WHERE school_id = $2 AND is_active)`, f.ani, f.schoolA).Scan(&enrollment, &classID); err != nil {
		t.Fatal(err)
	}
	e, err := grades.GetCardEnrollment(ctx, f.tx, f.schoolA, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := grades.ClassSummary(ctx, f.tx, f.schoolA, e.Class)
	if err != nil {
		t.Fatal(err)
	}
	for i, sub := range summary.Subjects {
		for _, row := range sub.Rows {
			if row.EnrollmentID != enrollment {
				continue
			}
			got := g.Subjects[i]
			if got.Name != sub.Subject.Name || got.Score != row.Result.Score || got.Letter != row.Letter || got.KKM != sub.KKM || got.Incomplete {
				t.Errorf("%s: portal %+v, recap %+v %s", sub.Subject.Name, got, row.Result, row.Letter)
			}
		}
	}
	if g.Subjects[0].Name != "IPA" || g.Subjects[0].Score != 79 || g.Subjects[1].Score != 88 || g.Subjects[1].Letter != "B" {
		t.Errorf("scores: %+v", g.Subjects)
	}
	// Komponen hanya tampil jika sekolah mengaktifkannya.
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.ReportPortalComponents, "true"); err != nil {
		t.Fatal(err)
	}
	g, _ = svc.GetChildGrades(ctx, f.schoolA, f.userSari, f.ani)
	if !g.ShowComponents || len(g.Subjects[0].Components) != 3 || g.Subjects[0].Components[2].Name != "UAS" || g.Subjects[0].Components[2].Score.String() != "70" {
		t.Errorf("components: %+v", g.Subjects[0].Components)
	}
}

func TestChildGrades_IncompleteAndOwnership(t *testing.T) {
	f := newFixture(t)
	f.gradeData(t)
	ctx := context.Background()
	svc := NewService(f.tx)
	// Budi (kelas 2B): IPA belum dinilai, tidak ada angka akhir dan rapor.
	g, err := svc.GetChildGrades(ctx, f.schoolA, f.userSari, f.budi)
	if err != nil {
		t.Fatal(err)
	}
	if g.Complete || len(g.Subjects) != 1 || !g.Subjects[0].Incomplete || g.Subjects[0].Score != 0 || g.Subjects[0].Letter != "" || g.Subjects[0].Missing != 3 {
		t.Errorf("incomplete: %+v", g)
	}
	if _, err := svc.ChildReportCard(ctx, school.Actor{SchoolID: f.schoolA, UserID: f.userSari}, f.budi, 1, printDay); !errors.Is(err, ErrReportCardUnavailable) {
		t.Errorf("incomplete report card: %v", err)
	}
	// Citra (hanya tahun lalu): belum ditempatkan.
	if g, err := svc.GetChildGrades(ctx, f.schoolA, f.userRina, f.citra); err != nil || g.HasClass || g.Subjects != nil {
		t.Errorf("no class: %v %+v", err, g)
	}
	// Anak orang lain, sekolah lain, ID acak: ErrNotFound.
	for _, tc := range [][3]string{{f.schoolA, f.userRina, f.ani}, {f.schoolA, f.none, f.ani}, {f.schoolA, f.userSari, f.citra}, {f.schoolB, f.userSari, f.dewi},
		{f.schoolA, f.userSari, "00000000-0000-0000-0000-000000000000"}, {f.schoolA, f.userSari, "bukan"}} {
		if _, err := svc.GetChildGrades(ctx, tc[0], tc[1], tc[2]); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetChildGrades(%v): %v", tc, err)
		}
		if _, err := svc.ChildReportCard(ctx, school.Actor{SchoolID: tc[0], UserID: tc[1]}, tc[2], 1, printDay); !errors.Is(err, ErrNotFound) {
			t.Errorf("ChildReportCard(%v): %v", tc, err)
		}
	}
}

func TestChildGrades_Handler(t *testing.T) {
	f := newFixture(t)
	f.gradeData(t)
	ctx := context.Background()
	page := func(student string) (int, string) {
		rec := f.getPath("/portal/siswa/"+student+"/nilai", f.userSari, f.schoolA, "orang_tua")
		return rec.Code, rec.Body.String()
	}
	code, body := page(f.ani)
	if code != http.StatusOK || !strings.Contains(body, `<span class="grade-final">79 <span class="grade-letter">C</span>`) ||
		!strings.Contains(body, "/portal/siswa/"+f.ani+"/rapor.pdf") || strings.Contains(body, "component-list") {
		t.Errorf("complete child page: %d", code)
	}
	// Belum lengkap: tidak ada angka akhir dan tidak ada tautan rapor.
	code, body = page(f.budi)
	if code != http.StatusOK || !strings.Contains(body, "Belum lengkap") || strings.Contains(body, "grade-final") || strings.Contains(body, "rapor.pdf") {
		t.Errorf("incomplete child page: %d", code)
	}
	// Unduh rapor: PDF untuk anak yang lengkap, dengan audit; 400 jika belum lengkap.
	rec := f.getPath("/portal/siswa/"+f.ani+"/rapor.pdf?semester=1", f.userSari, f.schoolA, "orang_tua")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("report card: status = %d", rec.Code)
	}
	var n int
	_ = f.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2 AND user_id = $3`, f.schoolA, ActionReportCardDownloadParent, f.userSari).Scan(&n)
	if n != 1 {
		t.Errorf("download audits = %d", n)
	}
	if rec := f.getPath("/portal/siswa/"+f.budi+"/rapor.pdf", f.userSari, f.schoolA, "orang_tua"); rec.Code != http.StatusBadRequest {
		t.Errorf("incomplete report card: status = %d", rec.Code)
	}
	if rec := f.getPath("/portal/siswa/"+f.ani+"/rapor.pdf?semester=9", f.userSari, f.schoolA, "orang_tua"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad semester: status = %d", rec.Code)
	}
	// Anak orang lain 404, staf 403.
	for _, path := range []string{"/portal/siswa/" + f.ani + "/nilai", "/portal/siswa/" + f.ani + "/rapor.pdf"} {
		if rec := f.getPath(path, f.userRina, f.schoolA, "orang_tua"); rec.Code != http.StatusNotFound {
			t.Errorf("%s as other parent: %d", path, rec.Code)
		}
		if rec := f.getPath(path, f.userSari, f.schoolA, "guru"); rec.Code != http.StatusForbidden {
			t.Errorf("%s as staff: %d", path, rec.Code)
		}
	}
	// Komponen tampil jika diaktifkan; nilai kosong ditulis "belum dinilai".
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.ReportPortalComponents, "true"); err != nil {
		t.Fatal(err)
	}
	if _, body := page(f.budi); !strings.Contains(body, "component-list") || !strings.Contains(body, "belum dinilai") {
		t.Error("components not shown when enabled")
	}
}
