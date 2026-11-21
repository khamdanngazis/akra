package exam

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
)

func TestExamRetentionJob(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	var yearEnd time.Time
	if err := e.tx.QueryRow(ctx, `SELECT end_date FROM academic_years WHERE id = $1`, e.ex.AcademicYearID).Scan(&yearEnd); err != nil {
		t.Fatal(err)
	}
	// Tahun ajaran berikutnya (berakhir setahun setelahnya) menentukan
	// batas: data disimpan sampai akhir tahun ajaran berikutnya.
	nextEnd := yearEnd.AddDate(1, 0, 0)
	if _, err := e.tx.Exec(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, 'Berikutnya', $2, $3)`,
		e.a.ID, yearEnd.AddDate(0, 0, 20), nextEnd); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		var n int
		if err := e.tx.QueryRow(ctx, q, e.ex.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	answers := `SELECT count(*) FROM exam_answers x JOIN exam_attempts t ON t.id = x.attempt_id WHERE t.exam_id = $1`
	events := `SELECT count(*) FROM exam_attempt_events v JOIN exam_attempts t ON t.id = v.attempt_id WHERE t.exam_id = $1`
	creds := `SELECT count(*) FROM student_credentials c JOIN exam_sessions x ON x.id = c.exam_session_id WHERE x.exam_id = $1`
	linked := `SELECT count(*) FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id WHERE x.exam_id = $1 AND p.student_id IS NOT NULL`
	scored := `SELECT count(*) FROM exam_attempts WHERE exam_id = $1 AND score_x100 IS NOT NULL`
	if _, err := e.tx.Exec(ctx, `INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at, client_seq)
		SELECT school_id, id, 'keluar', now(), 1 FROM exam_attempts WHERE exam_id = $1`, e.ex.ID); err != nil {
		t.Fatal(err)
	}
	before := map[string]int{"answers": count(answers), "events": count(events), "creds": count(creds), "linked": count(linked), "scored": count(scored)}
	for k, v := range before {
		if v == 0 {
			t.Fatalf("setup has no %s", k)
		}
	}
	w := &RetentionWorker{DB: e.tx, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// Masih di tahun ajaran berikutnya: belum disentuh.
	w.Now = func() time.Time { return nextEnd.AddDate(0, 0, -1) }
	if n, err := w.Run(ctx); err != nil || n != 0 {
		t.Fatalf("before cutoff processed %d, %v", n, err)
	}
	if count(answers) != before["answers"] {
		t.Fatalf("answers removed before cutoff")
	}
	// Lewat akhir tahun ajaran berikutnya: dianonimkan.
	w.Now = func() time.Time { return nextEnd.AddDate(0, 0, 2) }
	n, err := w.Run(ctx)
	if err != nil || n == 0 {
		t.Fatalf("after cutoff processed %d, %v", n, err)
	}
	for name, q := range map[string]string{"answers": answers, "events": events, "credentials": creds, "linked participants": linked} {
		if got := count(q); got != 0 {
			t.Errorf("%s after retention = %d", name, got)
		}
	}
	if got := count(scored); got != before["scored"] {
		t.Errorf("scores kept = %d, want %d", got, before["scored"])
	}
	var audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'exam.data_retention'`).Scan(&audits)
	if audits != n {
		t.Errorf("retention audits = %d, sessions %d", audits, n)
	}
	// Diulang: tidak ada yang diproses lagi.
	if again, err := w.Run(ctx); err != nil || again != 0 {
		t.Errorf("second run processed %d, %v", again, err)
	}
	// Halaman hasil tetap bisa dibuka (tanpa identitas).
	if rec := e.staffDo(http.MethodGet, "/ujian/paket/"+e.ex.ID+"/hasil", e.a.Admin, nil); rec.Code != http.StatusOK {
		t.Errorf("results after retention = %d", rec.Code)
	}
}

func TestEraseApplicantExamData(t *testing.T) {
	e := newGradeEnv(t)
	ctx := context.Background()
	students := e.students(t)
	first := e.start(t, students[0], dev1)
	e.load(t, first)
	e.tx.Exec(ctx, `UPDATE exams SET purpose = 'seleksi' WHERE id = $1`, e.ex.ID)
	e.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, applicant_id, participant_no, variant_id)
		SELECT school_id, session_id, $2, 'P-01', variant_id FROM exam_session_participants WHERE session_id = $1 LIMIT 1`, e.sess.ID, e.a.ApplicantID)
	app := auth.Participant{Kind: auth.RolePendaftar, SchoolID: e.a.ID, ApplicantID: e.a.ApplicantID}
	a, err := StartAttempt(ctx, e.tx, app, StartRequest{SessionID: e.sess.ID, DeviceID: devProctor3, Now: e.real})
	if err != nil {
		t.Fatal(err)
	}
	e.answer(t, a, devProctor3, map[Kind]string{KindMC: `"b"`, KindEssay: `"jawaban pribadi pendaftar"`})
	if _, err := SubmitAttempt(ctx, e.tx, app, a, SubmitByParticipant, e.real); err != nil {
		t.Fatal(err)
	}
	tx, err := e.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := EraseApplicantExamData(ctx, tx, e.a.ID, e.a.ApplicantID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var answers, linked, attempts int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_answers WHERE attempt_id = $1`, a.ID).Scan(&answers)
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_session_participants WHERE applicant_id = $1`, e.a.ApplicantID).Scan(&linked)
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_attempts WHERE id = $1 AND score_x100 IS NOT NULL`, a.ID).Scan(&attempts)
	if answers != 0 || linked != 0 || attempts != 1 {
		t.Errorf("after erase: answers %d linked %d scored attempts %d", answers, linked, attempts)
	}
	// Siswa di ujian yang sama tidak tersentuh.
	var studentLinked int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_session_participants WHERE student_id = $1`, students[0]).Scan(&studentLinked)
	if studentLinked == 0 {
		t.Errorf("student participant unlinked by applicant erase")
	}
}

// Ekspor data siswa (UU PDP) memuat jawaban dan komentar guru, tanpa kunci
// atau pembahasan.
func TestStudentExamExport(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	students := e.students(t)
	if _, err := e.tx.Exec(ctx, `UPDATE exam_answers SET essay_comment = 'Komentar untuk siswa' WHERE attempt_id = $1 AND question_id = $2`,
		e.att[0].ID, e.essay); err != nil {
		t.Fatal(err)
	}
	out, err := StudentExamExport(ctx, e.tx, e.a.ID, students[0])
	if err != nil || len(out) != 1 {
		t.Fatalf("export = %d, %v", len(out), err)
	}
	x := out[0]
	if x.Exam == "" || x.Score == "" || len(x.Answers) != len(Kinds) {
		t.Fatalf("export attempt = %+v", x)
	}
	found := false
	for _, a := range x.Answers {
		if a.Comment == "Komentar untuk siswa" && strings.Contains(strings.Join(a.Answer, " "), "Fotosintesis") {
			found = true
		}
	}
	if !found {
		t.Errorf("essay answer and comment missing from export")
	}
	b, _ := json.Marshal(out)
	for _, s := range []string{"RAHASIA-PEMBAHASAN", "RAHASIA-12", "RAHASIA-RUBRIK", `"correct"`, "Kunci:", "rubric", "explanation"} {
		if i := strings.Index(string(b), s); i >= 0 {
			t.Errorf("export leaks %q: %s", s, string(b)[max(0, i-200):min(len(b), i+80)])
		}
	}
	// Siswa lain: hanya miliknya.
	other, _ := StudentExamExport(ctx, e.tx, e.a.ID, students[1])
	if len(other) != 1 || other[0].Score == x.Score && len(other[0].Answers) == 0 {
		t.Errorf("other student export = %+v", other)
	}
}
