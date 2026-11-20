package exam

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/academic"
	"akra/internal/classes"
	"akra/internal/grades"
	"akra/internal/testutil"
)

func TestNewGradeValue(t *testing.T) {
	p := func(v int) *int { return &v }
	cases := []struct {
		name, policy, rule string
		value              int
		current            *int
		kktp, want         int
	}{
		{"timpa menurunkan", GradeOverwrite, "", 6000, p(8000), 75, 6000},
		{"timpa kosong", GradeOverwrite, "", 6000, nil, 75, 6000},
		{"tertinggi tidak menurunkan", GradeHighest, "", 6000, p(8000), 75, 8000},
		{"tertinggi naik", GradeHighest, "", 9000, p(8000), 75, 9000},
		{"tertinggi kosong", GradeHighest, "", 6000, nil, 75, 6000},
		{"remedial dibatasi KKTP", GradeRemedial, grades.RemedialCapAtThreshold, 9000, p(6000), 75, 7500},
		{"remedial di bawah KKTP", GradeRemedial, grades.RemedialCapAtThreshold, 7000, p(6000), 75, 7000},
		{"remedial tidak menurunkan", GradeRemedial, grades.RemedialCapAtThreshold, 5000, p(6000), 75, 6000},
		{"remedial aturan tertinggi", GradeRemedial, grades.RemedialHighest, 9000, p(6000), 75, 9000},
		{"remedial nilai awal di atas KKTP", GradeRemedial, grades.RemedialCapAtThreshold, 9000, p(8000), 75, 8000},
		{"pengayaan tidak menurunkan", GradeEnrichment, "", 7000, p(8000), 75, 8000},
		{"pengayaan naik", GradeEnrichment, "", 9500, p(8000), 75, 9500},
	}
	for _, c := range cases {
		if got := NewGradeValue(c.policy, c.value, c.current, c.kktp, c.rule); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// gradeSyncEnv: tiga siswa bernilai final (esai dikoreksi) dan komponen
// "Ulangan Harian" mapel ujian di tahun ajaran ujian.
type gradeSyncEnv struct {
	*essayEnv
	comp    string
	page    string
	teacher testutil.User
}

func newGradeSyncEnv(t *testing.T) *gradeSyncEnv {
	t.Helper()
	g := &gradeSyncEnv{essayEnv: gradedEssayEnv(t)}
	ctx := context.Background()
	if err := g.tx.QueryRow(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent)
		VALUES ($1, $2, $3, 'Ulangan Harian', 30) RETURNING id`, g.a.ID, g.ex.AcademicYearID, g.ex.SubjectID).Scan(&g.comp); err != nil {
		t.Fatal(err)
	}
	g.page = "/ujian/paket/" + g.ex.ID + "/rapor"
	g.teacher = testutil.User{ID: g.ex.OwnerID, Role: "guru"}
	return g
}

// componentGrade membaca nilai komponen siswa (×100), -1 bila kosong.
func (g *gradeSyncEnv) componentGrade(t *testing.T, studentID string) int {
	t.Helper()
	var v *int
	err := g.tx.QueryRow(context.Background(), `SELECT (gr.score * 100)::int FROM grades gr JOIN enrollments en ON en.id = gr.enrollment_id
		WHERE gr.component_id = $1 AND en.student_id = $2`, g.comp, studentID).Scan(&v)
	if err != nil || v == nil {
		return -1
	}
	return *v
}

func TestSendGradesFlow(t *testing.T) {
	g := newGradeSyncEnv(t)
	ctx := context.Background()
	students := g.students(t)
	// Belum ada komponen tujuan: halaman meminta memilih komponen.
	if body := g.staffDo(http.MethodGet, g.page, g.teacher, nil).Body.String(); !strings.Contains(body, "Pilih dulu komponen") || !strings.Contains(body, "Ulangan Harian") {
		t.Fatalf("component picker missing")
	}
	if rec := g.staffDo(http.MethodPost, g.page, g.teacher, url.Values{"aksi": {"komponen"}, "grade_component_id": {g.comp}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("set component = %d", rec.Code)
	}
	// Siswa ketiga sudah punya nilai 95 di komponen; siswa keempat tidak hadir.
	var enroll2 string
	g.tx.QueryRow(ctx, `SELECT id FROM enrollments WHERE student_id = $1 AND end_date IS NULL`, students[2]).Scan(&enroll2)
	if _, err := g.tx.Exec(ctx, `INSERT INTO grades (school_id, academic_year_id, enrollment_id, component_id, score, recorded_by)
		VALUES ($1, $2, $3, $4, 95, $5)`, g.a.ID, g.ex.AcademicYearID, enroll2, g.comp, g.ex.OwnerID); err != nil {
		t.Fatal(err)
	}
	var absent string
	if err := g.tx.QueryRow(ctx, `WITH s AS (INSERT INTO students (school_id, nis, full_name) VALUES ($1, 'ABSEN-1', 'Siswa Absen') RETURNING id),
		en AS (INSERT INTO enrollments (school_id, academic_year_id, class_id, student_id, start_date)
		       SELECT $1, $2, $3, s.id, '2026-07-13' FROM s RETURNING student_id)
		INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no) SELECT $1, $4, student_id, 'ABSEN-1' FROM en
		RETURNING student_id`, g.a.ID, g.ex.AcademicYearID, g.a.ClassID, g.sess.ID).Scan(&absent); err != nil {
		t.Fatal(err)
	}
	ex, _ := GetExam(ctx, g.tx, g.a.ID, g.ex.ID)
	sa := StaffActor{Actor: Actor{UserID: g.ex.OwnerID, Roles: []string{"guru"}}, SchoolID: g.a.ID}
	plan, err := PlanGradeSync(ctx, g.tx, sa, ex, GradeHighest)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]GradeSyncRow{}
	for _, r := range plan.Rows {
		status[r.StudentID] = r
	}
	if len(plan.Rows) != 4 || status[absent].Status != rowAbsent || status[students[0]].Status != rowNew || status[students[2]].Status != rowSame ||
		!plan.CanSendAny || plan.Changes != 2 {
		t.Fatalf("plan = %+v", plan.Rows)
	}
	// Kirim "ambil tertinggi": nilai 95 tidak turun, siswa absen tetap kosong.
	body := g.staffDo(http.MethodGet, g.page+"?aturan=tertinggi", g.teacher, nil).Body.String()
	if !strings.Contains(body, "Kirim 2 nilai ke rapor") || !strings.Contains(body, "Tidak hadir") {
		t.Fatalf("preview missing send button or absent row")
	}
	if rec := g.staffDo(http.MethodPost, g.page, g.teacher, url.Values{"aksi": {"kirim"}, "aturan": {"tertinggi"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("send = %d", rec.Code)
	}
	for _, sid := range students[:2] {
		if got := g.componentGrade(t, sid); got != *status[sid].ExamScore {
			t.Errorf("student grade = %d, want %d", got, *status[sid].ExamScore)
		}
	}
	if got := g.componentGrade(t, students[2]); got != 9500 {
		t.Errorf("highest lowered existing grade: %d", got)
	}
	if got := g.componentGrade(t, absent); got != -1 {
		t.Errorf("absent student changed: %d", got)
	}
	// Tampil di input nilai (T70): baris kelas komponen membaca nilai yang sama.
	rows, err := grades.ClassComponentRows(ctx, g.tx, g.a.ID, g.a.ClassID, g.comp)
	if err != nil {
		t.Fatal(err)
	}
	shown := 0
	for _, r := range rows {
		if r.Grade != nil && r.Grade.Score != nil {
			shown++
		}
	}
	if shown != 3 {
		t.Errorf("input page shows %d grades, want 3", shown)
	}
	// Rekap (T72) dan rapor PDF (T74) membaca nilai yang sama. Bobot
	// komponen dibuat 100 agar nilai akhir mapel terhitung.
	if _, err := g.tx.Exec(ctx, `UPDATE grade_components SET is_active = (id = $1), weight_percent = CASE WHEN id = $1 THEN 100 ELSE weight_percent END
		WHERE school_id = $2 AND subject_id = $3`, g.comp, g.a.ID, g.ex.SubjectID); err != nil {
		t.Fatal(err)
	}
	class, err := classes.GetByID(ctx, g.tx, g.a.ID, g.a.ClassID)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := academic.GetSubjectByID(ctx, g.tx, g.a.ID, g.ex.SubjectID)
	if err != nil {
		t.Fatal(err)
	}
	recap, err := grades.SubjectSummary(ctx, g.tx, g.a.ID, class, sub)
	if err != nil {
		t.Fatal(err)
	}
	var enroll0 string
	for _, r := range recap.Rows {
		if r.StudentID == students[0] {
			enroll0 = r.EnrollmentID
			if len(r.Scores) != 1 || r.Scores[0] == nil || int(*r.Scores[0]) != *status[students[0]].ExamScore {
				t.Errorf("recap score = %v, want %d", r.Scores, *status[students[0]].ExamScore)
			}
		}
	}
	ce, err := grades.GetCardEnrollment(ctx, g.tx, g.a.ID, enroll0)
	if err != nil {
		t.Fatal(err)
	}
	card, err := grades.BuildReportCard(ctx, g.tx, g.a.ID, ce, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	onCard := false
	for _, cs := range card.Subjects {
		onCard = onCard || (cs.Name == sub.Name && cs.Score == (*status[students[0]].ExamScore+50)/100)
	}
	if !onCard {
		t.Errorf("report card subjects = %+v", card.Subjects)
	}
	var audits, syncs int
	g.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'grade.create' AND school_id = $1`, g.a.ID).Scan(&audits)
	g.tx.QueryRow(ctx, `SELECT count(*) FROM exam_grade_syncs WHERE exam_id = $1`, g.ex.ID).Scan(&syncs)
	if audits != 2 || syncs != 1 {
		t.Errorf("grade audits = %d, syncs = %d", audits, syncs)
	}
	if n, _ := GradesStale(ctx, g.tx, g.a.ID, g.ex.ID); n != 0 {
		t.Errorf("stale right after send = %d", n)
	}
	// Nilai berubah setelah dikirim (koreksi esai ulang) → tanda kirim ulang.
	if _, err := GradeEssay(ctx, g.tx, StaffActor{Actor: Actor{UserID: g.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: g.a.ID}, g.ex,
		GraderScope{Full: true}, g.essay, g.att[0].ID, "0", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := GradesStale(ctx, g.tx, g.a.ID, g.ex.ID); n != 1 {
		t.Fatalf("stale after essay regrade = %d, want 1", n)
	}
	if body := g.staffDo(http.MethodGet, "/ujian/paket/"+g.ex.ID, g.teacher, nil).Body.String(); !strings.Contains(body, "Nilai berubah setelah dikirim ke rapor") {
		t.Errorf("exam detail missing resend notice")
	}
	if body := g.staffDo(http.MethodGet, "/ujian/paket/"+g.ex.ID+"/hasil", g.teacher, nil).Body.String(); !strings.Contains(body, "kirim ulang") {
		t.Errorf("results page missing resend notice")
	}
	// Kirim ulang "timpa": nilai turun mengikuti nilai ujian baru.
	g.staffDo(http.MethodPost, g.page, g.teacher, url.Values{"aksi": {"kirim"}, "aturan": {"timpa"}})
	x, _ := g.score(t, g.att[0].ID)
	if got := g.componentGrade(t, students[0]); got != x {
		t.Errorf("after resend = %d, want %d", got, x)
	}
	if n, _ := GradesStale(ctx, g.tx, g.a.ID, g.ex.ID); n != 0 {
		t.Errorf("stale after resend = %d", n)
	}
}

func TestSendGradesOnlyOwnClasses(t *testing.T) {
	g := newGradeSyncEnv(t)
	ctx := context.Background()
	if _, err := g.tx.Exec(ctx, `UPDATE exams SET grade_component_id = $2 WHERE id = $1`, g.ex.ID, g.comp); err != nil {
		t.Fatal(err)
	}
	// Admin/TU: tidak mengampu kelas, sama dengan input nilai T70.
	rec := g.staffDo(http.MethodPost, g.page, g.a.Admin, url.Values{"aksi": {"kirim"}})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "tidak mengampu") {
		t.Errorf("admin send = %d", rec.Code)
	}
	// Pemegang ujian yang penugasannya dicabut: tidak bisa mengirim.
	if _, err := g.tx.Exec(ctx, `DELETE FROM teaching_assignments WHERE teacher_id = $1`, g.ex.OwnerID); err != nil {
		t.Fatal(err)
	}
	if rec := g.staffDo(http.MethodPost, g.page, g.teacher, url.Values{"aksi": {"kirim"}}); rec.Code != http.StatusForbidden {
		t.Errorf("teacher without assignment = %d", rec.Code)
	}
	var n int
	g.tx.QueryRow(ctx, `SELECT count(*) FROM grades WHERE component_id = $1`, g.comp).Scan(&n)
	if n != 0 {
		t.Errorf("grades written without assignment: %d", n)
	}
	// Komponen mapel lain ditolak.
	var other string
	g.tx.QueryRow(ctx, `WITH s AS (INSERT INTO subjects (school_id, name, code) VALUES ($1, 'Mapel Lain', 'ML') RETURNING id)
		INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent) SELECT $1, $2, s.id, 'UH', 20 FROM s RETURNING id`,
		g.a.ID, g.ex.AcademicYearID).Scan(&other)
	ex, _ := GetExam(ctx, g.tx, g.a.ID, g.ex.ID)
	if err := SetGradeComponent(ctx, g.tx, StaffActor{Actor: Actor{UserID: g.ex.OwnerID}, SchoolID: g.a.ID}, ex, other); err != ErrBadGradeComponent {
		t.Errorf("other subject component = %v", err)
	}
}

func TestSendGradesAverageAndRemedial(t *testing.T) {
	g := newGradeSyncEnv(t)
	ctx := context.Background()
	students := g.students(t)
	if _, err := g.tx.Exec(ctx, `UPDATE exams SET grade_component_id = $2 WHERE id = $1`, g.ex.ID, g.comp); err != nil {
		t.Fatal(err)
	}
	// copyExam membuat ujian salinan (ulangan kedua atau remedial) dengan
	// satu sesi; nilai final siswa diisi langsung.
	copyExam := func(extra string, scores map[string]int) string {
		t.Helper()
		var id string
		if err := g.tx.QueryRow(ctx, `INSERT INTO exams SELECT (jsonb_populate_record(NULL::exams, to_jsonb(e) || jsonb_build_object('id', gen_random_uuid(),
			'name', e.name || ' 2', 'grade_component_id', NULL) || $2::jsonb)).* FROM exams e WHERE e.id = $1 RETURNING id`, g.ex.ID, extra).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var sess, variant string
		g.tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, class_id, starts_at, ends_at, created_by)
			SELECT school_id, $2, class_id, starts_at, ends_at, created_by FROM exam_sessions WHERE id = $1 RETURNING id`, g.sess.ID, id).Scan(&sess)
		g.tx.QueryRow(ctx, `INSERT INTO exam_variants (school_id, exam_id, label, seed) VALUES ($1, $2, 'A', 1) RETURNING id`, g.a.ID, id).Scan(&variant)
		for sid, v := range scores {
			var part string
			g.tx.QueryRow(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no) VALUES ($1, $2, $3, 'X') RETURNING id`,
				g.a.ID, sess, sid).Scan(&part)
			if _, err := g.tx.Exec(ctx, `INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, variant_id, seed, question_order, device_id,
					started_at, deadline_at, submitted_at, submit_reason, status, score_x100, score_status, graded_at)
				VALUES ($1, $2, $3, $4, $5, 1, '{}', gen_random_uuid(), now(), now(), now(), 'peserta', 'dikumpulkan', $6, 'final', now())`,
				g.a.ID, id, sess, part, variant, v); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	second := copyExam(`{}`, map[string]int{students[0]: 9000})
	g.tx.Exec(ctx, `UPDATE exams SET grade_component_id = $2 WHERE id = $1`, second, g.comp)
	sa := StaffActor{Actor: Actor{UserID: g.ex.OwnerID, Roles: []string{"guru"}}, SchoolID: g.a.ID}
	ex, _ := GetExam(ctx, g.tx, g.a.ID, g.ex.ID)
	plan, err := PlanGradeSync(ctx, g.tx, sa, ex, GradeOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.AverageOf) != 2 {
		t.Fatalf("average of = %v", plan.AverageOf)
	}
	for _, r := range plan.Rows {
		switch r.StudentID {
		case students[0]:
			// Rata-rata ujian ini dan ujian kedua (90), dibulatkan.
			want := (2*(*r.ExamScore+9000) + 2) / 4
			if r.Value == nil || *r.Value != want || r.Sources != 2 {
				t.Errorf("average = %v (sources %d), want %d", r.Value, r.Sources, want)
			}
		case students[1]:
			// Tidak ikut ujian kedua: hanya nilai ujian ini.
			if r.Value == nil || *r.Value != *r.ExamScore || r.Sources != 1 {
				t.Errorf("single source value = %v", r.Value)
			}
		}
	}
	if _, err := SendGrades(ctx, g.tx, sa, ex, GradeOverwrite, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Remedial: komponen induk, nilai baru dibatasi KKM, tidak pernah turun.
	g.tx.Exec(ctx, `UPDATE grades SET score = 50 WHERE component_id = $1 AND enrollment_id = (SELECT id FROM enrollments WHERE student_id = $2 AND end_date IS NULL)`,
		g.comp, students[1])
	rem := copyExam(`{"followup": "remedial", "parent_exam_id": "`+g.ex.ID+`"}`, map[string]int{students[1]: 9800, students[0]: 1000})
	remExam, _ := GetExam(ctx, g.tx, g.a.ID, rem)
	rp, err := PlanGradeSync(ctx, g.tx, sa, remExam, GradeOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if rp.Policy != GradeRemedial || rp.Component.ID != g.comp || len(rp.AverageOf) != 0 {
		t.Fatalf("remedial plan policy %s component %s", rp.Policy, rp.Component.ID)
	}
	if _, err := SendGrades(ctx, g.tx, sa, remExam, GradeOverwrite, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := g.componentGrade(t, students[1]); got != rp.KKTP*100 {
		t.Errorf("remedial grade = %d, want KKTP %d", got, rp.KKTP*100)
	}
	before := 0
	for _, r := range plan.Rows {
		if r.StudentID == students[0] && r.Value != nil {
			before = *r.Value
		}
	}
	if got := g.componentGrade(t, students[0]); got != before {
		t.Errorf("remedial lowered grade: %d → %d", before, got)
	}
}

func TestRulesGradeComponent(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	var comp, other string
	e.tx.QueryRow(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent)
		VALUES ($1, $2, $3, 'UH Wizard', 20) RETURNING id`, e.a.ID, ex.AcademicYearID, e.a.SubjectID).Scan(&comp)
	e.tx.QueryRow(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, is_active)
		VALUES ($1, $2, $3, 'UH Nonaktif', 20, false) RETURNING id`, e.a.ID, ex.AcademicYearID, e.a.SubjectID).Scan(&other)
	f := RulesFormOf(ex)
	f.GradeComponentID = other
	if err := UpdateRules(ctx, e.tx, e.owner, ex.ID, f, now); err == nil {
		t.Fatalf("inactive component accepted")
	}
	f.GradeComponentID = comp
	if err := UpdateRules(ctx, e.tx, e.owner, ex.ID, f, now); err != nil {
		t.Fatal(err)
	}
	got, _ := GetExam(ctx, e.tx, e.a.ID, ex.ID)
	if got.GradeComponentID != comp {
		t.Errorf("grade component = %q", got.GradeComponentID)
	}
	// Tes seleksi tidak punya komponen rapor.
	if (Exam{Purpose: PurposeAdmission}).FeedsGrades() || (Exam{Purpose: PurposeQuiz}).FeedsGrades() {
		t.Errorf("FeedsGrades must require a graded purpose and a subject")
	}
}

func TestAchievementSuggestion(t *testing.T) {
	refs := []ObjectiveRef{{ID: "a", Code: "TP 3.1", Description: "operasi pecahan"}, {ID: "b", Code: "TP 3.2", Description: "desimal"},
		{ID: "c", Code: "TP 3.4", Description: "perbandingan"}}
	u := PointsUnit
	a := AnalyzeObjectives([]StudentOutcome{{Key: "s", StudentID: "s", ScoreX100: 7000, Items: []ItemOutcome{
		{QuestionID: "1", ObjectiveID: "a", Earned: 2 * u, Points: 2},
		{QuestionID: "2", ObjectiveID: "b", Earned: u + u/2, Points: 2},
		{QuestionID: "3", ObjectiveID: "c", Earned: 0, Points: 2},
	}}}, refs, 75)
	got := a.AchievementSuggestion(a.Students[0])
	want := "Menguasai TP 3.1 (operasi pecahan) dan TP 3.2 (desimal); perlu ditingkatkan TP 3.4 (perbandingan)."
	if got != want {
		t.Errorf("suggestion = %q\nwant %q", got, want)
	}
	weakOnly := AnalyzeObjectives([]StudentOutcome{{Key: "w", StudentID: "w", Items: []ItemOutcome{{QuestionID: "3", ObjectiveID: "c", Points: 2}}}}, refs, 75)
	if s := weakOnly.AchievementSuggestion(weakOnly.Students[0]); s != "Perlu ditingkatkan TP 3.4 (perbandingan)." {
		t.Errorf("weak only = %q", s)
	}
}
