package grades

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/academic"
	"akra/internal/classes"
)

// recapFixture menambah IPA di 1A (guru2) dengan komponen Tugas 30, UTS 30,
// UAS 40 di atas gradeFixture (Matematika: UTS 100, guru1; wali 1A: guru3).
type recapFixture struct {
	*gradeFixture
	ipaComps [3]string
}

func newRecapFixture(t *testing.T) *recapFixture {
	t.Helper()
	f := &recapFixture{gradeFixture: newGradeFixture(t)}
	ctx := context.Background()
	if _, err := f.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
		f.schoolA, f.year, f.class1A, f.ipa, f.guru2); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct {
		name   string
		weight int
	}{{"Tugas", 30}, {"UTS", 30}, {"UAS", 40}} {
		if err := f.tx.QueryRow(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, sort_order) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			f.schoolA, f.year, f.ipa, c.name, c.weight, i+1).Scan(&f.ipaComps[i]); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// grade menyimpan nilai lewat InputService (jalur yang sama dengan guru).
func (f *recapFixture) grade(t *testing.T, componentID string, scores map[string]string) {
	t.Helper()
	tg := f.target(t, componentID)
	if _, err := f.input.Save(context.Background(), f.teacher(), tg, scores); err != nil {
		t.Fatal(err)
	}
}

func (f *recapFixture) class(t *testing.T) classes.Class {
	t.Helper()
	c, err := activeClass(context.Background(), f.tx, f.schoolA, f.class1A)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *recapFixture) subject(t *testing.T, id string) academic.Subject {
	t.Helper()
	s, err := getSubject(context.Background(), f.tx, f.schoolA, id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSubjectSummary_MatchesFinalScore(t *testing.T) {
	f := newRecapFixture(t)
	ctx := context.Background()
	tugas, uts, uas := map[string]string{}, map[string]string{}, map[string]string{}
	for i, e := range f.enrollments {
		tugas[e], uts[e], uas[e] = fmt.Sprintf("%d", 60+i), fmt.Sprintf("%d,5", 55+i), fmt.Sprintf("%d", 70+i%30)
	}
	tugas[f.enrollments[0]], uts[f.enrollments[0]], uas[f.enrollments[0]] = "80", "90", "70" // contoh T71: 79
	uas[f.enrollments[1]] = ""                                                               // belum lengkap
	tugas[f.enrollments[2]], uts[f.enrollments[2]], uas[f.enrollments[2]] = "0", "0", "0"    // nol tetap lengkap
	f.grade(t, f.ipaComps[0], tugas)
	f.grade(t, f.ipaComps[1], uts)
	f.grade(t, f.ipaComps[2], uas)
	// Siswa yang sudah pindah tidak masuk rekap kelas ini.
	if _, err := f.tx.Exec(ctx, `UPDATE enrollments SET end_date = '2026-09-30' WHERE id = $1`, f.enrollments[29]); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	rep, err := SubjectSummary(ctx, f.tx, f.schoolA, f.class(t), f.subject(t, f.ipa))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("subject summary in %s", time.Since(start))
	if !rep.Configured || len(rep.Components) != 3 || rep.Components[2].Name != "UAS" || len(rep.Rows) != 29 || rep.TeacherName != "guru2@a.uji" {
		t.Fatalf("report: configured %v, %d components, %d rows, teacher %q", rep.Configured, len(rep.Components), len(rep.Rows), rep.TeacherName)
	}
	if rep.Complete != 28 || rep.Incomplete != 1 || rep.EmptyCells != 1 {
		t.Errorf("counts: complete %d, incomplete %d, empty %d", rep.Complete, rep.Incomplete, rep.EmptyCells)
	}
	scale := DefaultScale()
	var sum int64
	byEnrollment := map[string]StudentGrades{}
	for _, row := range rep.Rows {
		byEnrollment[row.EnrollmentID] = row
		// Setiap baris sama persis dengan FinalScore atas nilai yang tampil.
		in := make([]ComponentScore, len(rep.Components))
		for i, c := range rep.Components {
			in[i] = ComponentScore{Weight: c.Weight, Score: row.Scores[i]}
		}
		want, err := FinalScore(in)
		if err != nil || row.Result != want {
			t.Errorf("%s: result %+v, FinalScore %+v (%v)", row.FullName, row.Result, want, err)
		}
		if !want.Incomplete {
			sum += int64(want.Exact)
			if letter, _ := Letter(want.Score, scale); row.Letter != letter {
				t.Errorf("%s: letter %q, want %q", row.FullName, row.Letter, letter)
			}
		} else if row.Letter != "" {
			t.Errorf("%s: incomplete row has letter %q", row.FullName, row.Letter)
		}
	}
	if r := byEnrollment[f.enrollments[0]]; r.Result.Score != 79 || r.Letter != "C" {
		t.Errorf("T71 example: %+v %s", r.Result, r.Letter)
	}
	if r := byEnrollment[f.enrollments[1]]; !r.Result.Incomplete || r.Result.Missing != 1 || r.Result.Score != 0 || r.Scores[2] != nil {
		t.Errorf("incomplete row: %+v", r.Result)
	}
	if r := byEnrollment[f.enrollments[2]]; r.Result.Incomplete || r.Result.Score != 0 || r.Letter != "D" {
		t.Errorf("all-zero row: %+v %s", r.Result, r.Letter)
	}
	if rep.Average == nil || *rep.Average != Score(roundHalfUp(sum, 28)) {
		t.Errorf("average %v", rep.Average)
	}
}

func TestClassSummary(t *testing.T) {
	f := newRecapFixture(t)
	ctx := context.Background()
	// Enam mapel: Matematika, IPA, dan empat mapel baru (satu tanpa komponen).
	for i, name := range []string{"Bahasa Indonesia", "PJOK", "Seni", "Agama"} {
		var sub string
		if err := f.tx.QueryRow(ctx, `INSERT INTO subjects (school_id, name) VALUES ($1, $2) RETURNING id`, f.schoolA, name).Scan(&sub); err != nil {
			t.Fatal(err)
		}
		if _, err := f.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
			f.schoolA, f.year, f.class1A, sub, f.guru1); err != nil {
			t.Fatal(err)
		}
		if name == "Agama" {
			continue
		}
		for j, w := range []int{30, 30, 40} {
			var comp string
			if err := f.tx.QueryRow(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, sort_order) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
				f.schoolA, f.year, sub, fmt.Sprintf("K%d", j), w, j).Scan(&comp); err != nil {
				t.Fatal(err)
			}
			scores := map[string]string{}
			for k, e := range f.enrollments {
				scores[e] = fmt.Sprintf("%d", 50+(k+i+j)%50)
			}
			f.grade(t, comp, scores)
		}
	}
	for _, comp := range f.ipaComps {
		scores := map[string]string{}
		for k, e := range f.enrollments {
			scores[e] = fmt.Sprintf("%d", 70+k%30)
		}
		f.grade(t, comp, scores)
	}

	start := time.Now()
	rep, err := ClassSummary(ctx, f.tx, f.schoolA, f.class(t))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("class summary (30 students, %d subjects) in %s", len(rep.Subjects), elapsed)
	if elapsed > time.Second {
		t.Errorf("class summary took %s", elapsed)
	}
	if rep.Students != 30 || len(rep.Subjects) != 6 {
		t.Fatalf("students %d, subjects %d", rep.Students, len(rep.Subjects))
	}
	got := map[string]SubjectReport{}
	for _, s := range rep.Subjects {
		got[s.Subject.Name] = s
	}
	if s := got["Agama"]; s.Configured || s.Complete != 0 || s.Incomplete != 0 {
		t.Errorf("unconfigured subject: %+v", s)
	}
	if s := got["IPA"]; !s.Configured || s.Complete != 30 || s.EmptyCells != 0 {
		t.Errorf("IPA: complete %d, empty %d", s.Complete, s.EmptyCells)
	}
	// Matematika (UTS 100) belum diisi sama sekali: semua belum lengkap.
	if s := got["Matematika"]; s.Incomplete != 30 || s.EmptyCells != 30 || s.Average != nil {
		t.Errorf("Matematika: incomplete %d, empty %d", s.Incomplete, s.EmptyCells)
	}
	// Rekap kelas sama dengan rekap per mapel.
	single, err := SubjectSummary(ctx, f.tx, f.schoolA, f.class(t), f.subject(t, f.ipa))
	if err != nil {
		t.Fatal(err)
	}
	for i := range single.Rows {
		if single.Rows[i].Result != got["IPA"].Rows[i].Result {
			t.Fatalf("row %d differs between class and subject summary", i)
		}
	}
}

func TestReportHandler(t *testing.T) {
	f := newRecapFixture(t)
	e := newEnvFor(t, f.fixture)
	f.grade(t, f.ipaComps[0], map[string]string{f.enrollments[0]: "80"})
	f.grade(t, f.ipaComps[1], map[string]string{f.enrollments[0]: "90"})
	f.grade(t, f.ipaComps[2], map[string]string{f.enrollments[0]: "70"})
	classPath := "/nilai/rekap?kelas=" + f.class1A
	ipaPath := classPath + "&mapel=" + f.ipa
	mtkPath := classPath + "&mapel=" + f.mtk

	// Admin dan kepala sekolah: semua kelas dan mapel.
	for _, role := range []string{"admin_tu", "kepala_sekolah"} {
		rec := e.do(http.MethodGet, "/nilai/rekap", f.actor.UserID, role, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mapel="+f.ipa) {
			t.Errorf("%s default class summary: status = %d", role, rec.Code)
		}
		body := e.do(http.MethodGet, ipaPath, f.actor.UserID, role, nil).Body.String()
		if !strings.Contains(body, "<strong>79</strong>") || strings.Count(body, "Belum lengkap</span>") != 29 {
			t.Errorf("%s subject recap: final score or incomplete markers", role)
		}
	}
	// Guru pengampu: mapelnya saja.
	if rec := e.do(http.MethodGet, ipaPath, f.guru2, "guru", nil); rec.Code != http.StatusOK {
		t.Errorf("assigned teacher: status = %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, mtkPath, f.guru1, "guru", nil); rec.Code != http.StatusOK {
		t.Errorf("assigned teacher (mtk): status = %d", rec.Code)
	}
	// Guru mapel lain dan guru non-wali: 403.
	for _, tc := range []struct{ user, path string }{{f.guru1, ipaPath}, {f.guru2, mtkPath}, {f.guru1, classPath}, {f.guru2, classPath}} {
		if rec := e.do(http.MethodGet, tc.path, tc.user, "guru", nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s as %s: status = %d, want 403", tc.path, tc.user, rec.Code)
		}
	}
	// Wali kelas: ringkasan kelas dan semua mapel kelasnya (baca).
	for _, path := range []string{classPath, ipaPath, mtkPath} {
		if rec := e.do(http.MethodGet, path, f.guru3, "guru", nil); rec.Code != http.StatusOK {
			t.Errorf("homeroom %s: status = %d", path, rec.Code)
		}
	}
	// Halaman awal guru.
	if body := e.do(http.MethodGet, "/nilai/rekap", f.guru3, "guru", nil).Body.String(); !strings.Contains(body, "Kelas perwalian") || !strings.Contains(body, "kelas="+f.class1A) {
		t.Error("homeroom landing")
	}
	if body := e.do(http.MethodGet, "/nilai/rekap", f.guru2, "guru", nil).Body.String(); !strings.Contains(body, "mapel="+f.ipa) || strings.Contains(body, "Kelas perwalian") {
		t.Error("teacher landing")
	}
	// Sekolah lain, tahun lain, ID rusak: 404. Peran lain: 403.
	for _, path := range []string{"/nilai/rekap?kelas=" + f.oldClass, classPath + "&mapel=" + f.subjectB, "/nilai/rekap?kelas=bukan"} {
		if rec := e.do(http.MethodGet, path, f.actor.UserID, "admin_tu", nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
	for _, role := range []string{"bendahara", "orang_tua"} {
		if rec := e.do(http.MethodGet, classPath, f.actor.UserID, role, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET as %s: status = %d", role, rec.Code)
		}
	}
}
