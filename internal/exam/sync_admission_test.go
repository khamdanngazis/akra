package exam

import (
	"context"
	"testing"

	"akra/internal/auth"
)

// admissionScore membaca nilai tes pendaftar (nil = belum ada baris atau
// NULL) dan sumbernya.
func admissionScore(t *testing.T, e *gradeEnv, applicantID, testID string) (*string, string) {
	t.Helper()
	var score *string
	var source string
	err := e.tx.QueryRow(context.Background(), `SELECT score::text, source FROM applicant_test_scores WHERE applicant_id = $1 AND test_id = $2`,
		applicantID, testID).Scan(&score, &source)
	if err != nil {
		return nil, ""
	}
	return score, source
}

func TestAdmissionScoreSync(t *testing.T) {
	e := newGradeEnv(t)
	ctx := context.Background()
	// Ujian dijadikan tes seleksi gelombang fixture; pendaftar fixture
	// diverifikasi dan ikut sesi. Nilai maksimal tes 50.
	students := e.students(t)
	first := e.start(t, students[0], dev1)
	e.load(t, first)
	testID := e.a.AdmissionTestID
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE exams SET purpose = 'seleksi' WHERE id = $1`, []any{e.ex.ID}},
		{`UPDATE applicants SET status = 'diverifikasi' WHERE id = $1`, []any{e.a.ApplicantID}},
		// Nilai ketikan fixture dihapus (linkExam menolak tes bernilai ketikan).
		{`DELETE FROM applicant_test_scores WHERE test_id = $1`, []any{testID}},
		{`UPDATE admission_tests SET exam_id = $1, max_score = 50 WHERE id = $2`, []any{e.ex.ID, testID}},
	} {
		if _, err := e.tx.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, applicant_id, participant_no, variant_id)
		SELECT school_id, session_id, $2, 'P-01', variant_id FROM exam_session_participants WHERE session_id = $1 LIMIT 1`, e.sess.ID, e.a.ApplicantID); err != nil {
		t.Fatal(err)
	}
	app := auth.Participant{Kind: auth.RolePendaftar, SchoolID: e.a.ID, ApplicantID: e.a.ApplicantID, Name: "Pendaftar"}
	a, err := StartAttempt(ctx, e.tx, app, StartRequest{SessionID: e.sess.ID, DeviceID: devProctor3, Now: e.real})
	if err != nil {
		t.Fatal(err)
	}
	// Belum dikumpulkan: belum ada nilai.
	if s, _ := admissionScore(t, e, e.a.ApplicantID, testID); s != nil {
		t.Fatalf("score before submit = %s", *s)
	}
	// Esai berisi: nilai belum final → baris ada tetapi NULL (belum lengkap).
	e.answer(t, a, devProctor3, map[Kind]string{KindMC: `"b"`, KindEssay: `"Jawaban esai pendaftar"`})
	if _, err := SubmitAttempt(ctx, e.tx, app, a, SubmitByParticipant, e.real); err != nil {
		t.Fatal(err)
	}
	if s, src := admissionScore(t, e, e.a.ApplicantID, testID); s != nil || src != "ujian_online" {
		t.Fatalf("awaiting essay: score %v source %q, want NULL ujian_online", s, src)
	}
	// Esai dikoreksi → final → nilai masuk, diskalakan ke nilai maksimal 50.
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	if _, err := GradeEssay(ctx, e.tx, sa, e.ex, GraderScope{Full: true}, e.items[KindEssay].Question.ID, a.ID, "2", "", e.real); err != nil {
		t.Fatal(err)
	}
	x, st := e.score(t, a.ID)
	if st != ScoreFinal {
		t.Fatalf("status = %s", st)
	}
	want := func(x100 int) string {
		v := x100 * 50                    // seperseratus nilai tes × 100
		return ScoreLabel((v + 50) / 100) // dua desimal, dibulatkan
	}
	s, _ := admissionScore(t, e, e.a.ApplicantID, testID)
	if s == nil || commaDecimal(*s) != want(x) {
		t.Fatalf("score = %v, want %s (exam score %d)", s, want(x), x)
	}
	// Nilai ulang (kunci PG dibetulkan ke opsi lain) memperbarui nilai tes.
	mc := e.items[KindMC].Question
	if _, err := ApplyKeyFix(ctx, e.tx, sa, e.ex, mc.ID, AnswerKey{Correct: []string{"a"}}, "kunci salah ketik", e.real); err != nil {
		t.Fatal(err)
	}
	x2, _ := e.score(t, a.ID)
	s2, _ := admissionScore(t, e, e.a.ApplicantID, testID)
	if x2 == x || s2 == nil || commaDecimal(*s2) != want(x2) {
		t.Errorf("after regrade: exam %d → %d, test score %v (want %s)", x, x2, s2, want(x2))
	}
	// Peserta siswa di ujian yang sama tidak menulis nilai pendaftar; tes
	// tanpa ujian ini tidak tersentuh.
	var rows int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM applicant_test_scores WHERE test_id = $1`, testID).Scan(&rows)
	if rows != 1 {
		t.Errorf("score rows = %d, want 1", rows)
	}
	// Sinkron ulang semua attempt idempoten.
	if n, err := SyncAdmissionExam(ctx, e.tx, e.a.ID, e.ex.ID); err != nil || n != 0 {
		t.Errorf("resync changed %d rows, %v", n, err)
	}
}

// commaDecimal mengubah "41.67" menjadi "41,67".
func commaDecimal(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c == '.' {
			out[i] = ','
		}
	}
	return string(out)
}
