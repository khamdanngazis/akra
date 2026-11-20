package exam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/testutil"
)

// gradeEnv: ujian semua jenis soal (2 poin per soal, sampleQuestion) yang
// sedang berlangsung.
type gradeEnv struct {
	*proctorEnv
	ex    Exam
	items map[Kind]GradeItem
}

func newGradeEnv(t *testing.T) *gradeEnv {
	t.Helper()
	e := &gradeEnv{proctorEnv: newProctorEnv(t), items: map[Kind]GradeItem{}}
	return e
}

func (e *gradeEnv) load(t *testing.T, a Attempt) {
	t.Helper()
	ex, err := GetExam(context.Background(), e.tx, e.a.ID, a.ExamID)
	if err != nil {
		t.Fatal(err)
	}
	e.ex = ex
	items, err := LoadGradeItems(context.Background(), e.tx, ex, a.VariantID)
	if err != nil || len(items) != len(Kinds) {
		t.Fatalf("grade items = %d, %v", len(items), err)
	}
	for _, it := range items {
		e.items[it.Question.Kind] = it
	}
}

func (e *gradeEnv) answer(t *testing.T, a Attempt, dev string, values map[Kind]string) {
	t.Helper()
	var ch []AnswerChange
	seq := int64(0)
	for k, v := range values {
		seq++
		ch = append(ch, AnswerChange{QuestionID: e.items[k].Question.ID, Value: json.RawMessage(v), Seq: seq})
	}
	if _, err := SaveAnswers(context.Background(), e.tx, e.a.ID, a.ID, dev, SaveRequest{Pos: -1, Changes: ch}, e.real); err != nil {
		t.Fatal(err)
	}
}

func (e *gradeEnv) score(t *testing.T, attemptID string) (int, string) {
	t.Helper()
	var x *int
	var st *string
	if err := e.tx.QueryRow(context.Background(), `SELECT score_x100, score_status FROM exam_attempts WHERE id = $1`, attemptID).Scan(&x, &st); err != nil {
		t.Fatal(err)
	}
	if x == nil || st == nil {
		return -1, ""
	}
	return *x, *st
}

// correct: jawaban benar sampleQuestion kecuali isian (kuncinya bukan
// angka, jadi tidak pernah cocok) dan esai (dinilai guru).
var correct = map[Kind]string{
	KindMC: `"b"`, KindMulti: `["a","c"]`, KindTF: `{"s1":true,"s2":false}`, KindShort: `"12"`, KindMatch: `{"l1":"r1"}`, KindEssay: `"Jawaban esai"`,
}

func TestGradingOnSubmit(t *testing.T) {
	e := newGradeEnv(t)
	ctx := context.Background()
	students := e.students(t)
	a0 := e.start(t, students[0], dev1)
	e.load(t, a0)
	e.answer(t, a0, dev1, correct)
	if _, err := SubmitAttempt(ctx, e.tx, participantOf(e.takeEnv, students[0]), a0, SubmitByParticipant, e.real); err != nil {
		t.Fatal(err)
	}
	// 4 soal benar × 2 poin = 8 dari 12; esai menunggu koreksi.
	if x, st := e.score(t, a0.ID); x != 6667 || st != ScoreAwaitEssay {
		t.Fatalf("score = %d %s, want 6667 menunggu_esai", x, st)
	}
	var mcPts int
	e.tx.QueryRow(ctx, `SELECT points_x10000 FROM exam_answers WHERE attempt_id = $1 AND question_id = $2`, a0.ID, e.items[KindMC].Question.ID).Scan(&mcPts)
	if mcPts != 20000 {
		t.Errorf("mc points_x10000 = %v", mcPts)
	}
	// Nilai menunggu esai tidak keluar ke modul lain.
	fs, err := FinalScores(ctx, e.tx, e.a.ID, e.ex.ID)
	if err != nil || len(fs) != 0 {
		t.Fatalf("final scores with pending essay = %+v, %v", fs, err)
	}
	// Koreksi esai (T147) 1 poin → final 9/12.
	e.tx.Exec(ctx, `UPDATE exam_answers SET points_x10000 = 10000 WHERE attempt_id = $1 AND question_id = $2`, a0.ID, e.items[KindEssay].Question.ID)
	g, err := GradeAttempt(ctx, e.tx, e.a.ID, a0.ID, e.real)
	if err != nil || g.Status != ScoreFinal || g.ScoreX100 != 7500 {
		t.Fatalf("after essay = %+v, %v", g, err)
	}
	fs, _ = FinalScores(ctx, e.tx, e.a.ID, e.ex.ID)
	if len(fs) != 1 || fs[0].ScoreX100 != 7500 || fs[0].StudentID != students[0] {
		t.Errorf("final scores = %+v", fs)
	}
	// Idempoten: menilai ulang tidak menulis apa pun.
	var gradedAt time.Time
	e.tx.QueryRow(ctx, `SELECT graded_at FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&gradedAt)
	if _, err := GradeAttempt(ctx, e.tx, e.a.ID, a0.ID, e.real.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var again time.Time
	e.tx.QueryRow(ctx, `SELECT graded_at FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&again)
	if !again.Equal(gradedAt) {
		t.Errorf("idempotent grading rewrote attempt: %v → %v", gradedAt, again)
	}

	// Kumpul oleh pengawas tanpa jawaban: final 0.
	a1 := e.start(t, students[1], dev2)
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	if err := ForceSubmit(ctx, e.tx, sa, a1.ID, e.real); err != nil {
		t.Fatal(err)
	}
	if x, st := e.score(t, a1.ID); x != 0 || st != ScoreFinal {
		t.Errorf("force submitted empty = %d %s", x, st)
	}
	// Kumpul otomatis job: dinilai juga.
	a2 := e.start(t, students[2], devProctor3)
	e.answer(t, a2, devProctor3, map[Kind]string{KindMC: `"b"`})
	e.tx.Exec(ctx, `UPDATE exam_attempts SET deadline_at = $2 WHERE id = $1`, a2.ID, e.real.Add(-10*time.Minute))
	if _, err := AutoSubmitExpired(ctx, e.tx, e.real); err != nil {
		t.Fatal(err)
	}
	if x, st := e.score(t, a2.ID); x != 1667 || st != ScoreFinal {
		t.Errorf("auto submitted = %d %s, want 1667 final", x, st)
	}
	// Penilaian susulan untuk attempt tanpa nilai (mis. sebelum T146).
	e.tx.Exec(ctx, `UPDATE exam_attempts SET score_x100 = NULL, score_status = NULL, graded_at = NULL WHERE id = $1`, a2.ID)
	if n, err := GradePending(ctx, e.tx, 100, e.real); err != nil || n < 1 {
		t.Fatalf("grade pending = %d, %v", n, err)
	}
	if x, _ := e.score(t, a2.ID); x != 1667 {
		t.Errorf("backfilled score = %d", x)
	}
}

func TestRegradeKeyFix(t *testing.T) {
	e := newGradeEnv(t)
	ctx := context.Background()
	students := e.students(t)
	a0 := e.start(t, students[0], dev1)
	e.load(t, a0)
	// a0 memilih "a" pada PG (kunci "b"); a1 memilih "b".
	e.answer(t, a0, dev1, map[Kind]string{KindMC: `"a"`, KindTF: correct[KindTF]})
	SubmitAttempt(ctx, e.tx, participantOf(e.takeEnv, students[0]), a0, SubmitByParticipant, e.real)
	a1 := e.start(t, students[1], dev2)
	e.answer(t, a1, dev2, map[Kind]string{KindMC: `"b"`})
	SubmitAttempt(ctx, e.tx, participantOf(e.takeEnv, students[1]), a1, SubmitByParticipant, e.real)
	b0, _ := e.score(t, a0.ID)
	b1, _ := e.score(t, a1.ID)
	if b0 != 1667 || b1 != 1667 {
		t.Fatalf("before = %d %d", b0, b1)
	}
	mc := e.items[KindMC].Question.ID
	page := "/ujian/paket/" + e.ex.ID + "/soal/" + mc + "/kunci"
	if rec := e.staffDo(http.MethodGet, page, e.a.Admin, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Jawaban benar") {
		t.Fatalf("key fix form = %d", rec.Code)
	}
	// Guru lain tanpa hak: tidak boleh.
	if rec := e.staffDo(http.MethodPost, page, testutil.User{ID: e.teacher2, Role: "guru"}, url.Values{"correct": {"a"}, "aksi": {"pratinjau"}}); rec.Code == http.StatusOK {
		t.Errorf("other teacher preview = %d", rec.Code)
	}
	// Pratinjau: kunci "a" → a0 naik, a1 turun; belum ada yang berubah.
	rec := e.staffDo(http.MethodPost, page, e.a.Admin, url.Values{"correct": {"a"}, "aksi": {"pratinjau"}})
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "1 peserta naik") || !strings.Contains(body, "1 turun") || !strings.Contains(body, "Terapkan dan nilai ulang") {
		t.Fatalf("preview = %d\n%s", rec.Code, body)
	}
	if x, _ := e.score(t, a0.ID); x != b0 {
		t.Errorf("preview changed score: %d", x)
	}
	// Tanpa alasan: ditolak, pratinjau tetap tampil.
	rec = e.staffDo(http.MethodPost, page, e.a.Admin, url.Values{"correct": {"a"}, "aksi": {"terapkan"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Tulis alasan") || !strings.Contains(rec.Body.String(), "1 peserta naik") {
		t.Errorf("apply without reason = %d", rec.Code)
	}
	// Kunci tidak valid dan kunci sama ditolak.
	if rec := e.staffDo(http.MethodPost, page, e.a.Admin, url.Values{"correct": {"zz"}, "aksi": {"pratinjau"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid key = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodPost, page, e.a.Admin, url.Values{"correct": {"b"}, "aksi": {"pratinjau"}}); !strings.Contains(rec.Body.String(), "sama dengan") {
		t.Errorf("unchanged key accepted")
	}
	// Terapkan.
	rec = e.staffDo(http.MethodPost, page, e.a.Admin, url.Values{"correct": {"a"}, "aksi": {"terapkan"}, "alasan": {"kunci tertukar"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("apply = %d\n%s", rec.Code, rec.Body.String())
	}
	if x, _ := e.score(t, a0.ID); x != 3333 {
		t.Errorf("a0 after fix = %d, want 3333", x)
	}
	if x, _ := e.score(t, a1.ID); x != 0 {
		t.Errorf("a1 after fix = %d, want 0", x)
	}
	var auditN, regradeN int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'exam.regrade' AND entity_id = $1 AND after->>'reason' = 'kunci tertukar'`, e.ex.ID).Scan(&auditN)
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_regrades WHERE exam_id = $1 AND cause = 'kunci' AND raised = 1 AND lowered = 1`, e.ex.ID).Scan(&regradeN)
	if auditN != 1 || regradeN != 1 {
		t.Errorf("audit %d regrade log %d", auditN, regradeN)
	}
	// Tab Hasil dan Soal menampilkan nilai dan riwayat.
	hasil := e.staffDo(http.MethodGet, "/ujian/paket/"+e.ex.ID+"/hasil", e.a.Admin, nil).Body.String()
	if !strings.Contains(hasil, "33,33") || !strings.Contains(hasil, "Kunci dibetulkan") || !strings.Contains(hasil, "kunci tertukar") {
		t.Errorf("results tab missing score or regrade history")
	}
	if soal := e.staffDo(http.MethodGet, "/ujian/paket/"+e.ex.ID+"?tab=soal", e.a.Admin, nil).Body.String(); !strings.Contains(soal, "Betulkan kunci") || !strings.Contains(soal, "Kunci dibetulkan") {
		t.Errorf("soal tab missing key fix link or badge")
	}
	// Peserta yang mengumpulkan setelah pembetulan dinilai dengan kunci baru.
	a2 := e.start(t, students[2], devProctor3)
	e.answer(t, a2, devProctor3, map[Kind]string{KindMC: `"a"`})
	SubmitAttempt(ctx, e.tx, participantOf(e.takeEnv, students[2]), a2, SubmitByParticipant, e.real)
	if x, _ := e.score(t, a2.ID); x != 1667 {
		t.Errorf("submit after fix = %d", x)
	}
	// Membatalkan soal PG: total 10, nilai ulang otomatis tercatat.
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	if _, err := VoidQuestion(ctx, e.tx, sa, e.ex, mc, "soal ambigu", true, false, e.real); err != nil {
		t.Fatal(err)
	}
	if x, _ := e.score(t, a0.ID); x != 2000 {
		t.Errorf("a0 after void = %d, want 2000 (TF 2 of 10)", x)
	}
	var voidLog int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_regrades WHERE exam_id = $1 AND cause = 'batal'`, e.ex.ID).Scan(&voidLog)
	if voidLog != 1 {
		t.Errorf("void regrade log = %d", voidLog)
	}
}

func TestKeyFixOtherKinds(t *testing.T) {
	e := newGradeEnv(t)
	ctx := context.Background()
	students := e.students(t)
	a0 := e.start(t, students[0], dev1)
	e.load(t, a0)
	e.answer(t, a0, dev1, map[Kind]string{KindShort: `"12,0"`, KindTF: `{"s1":true,"s2":true}`})
	SubmitAttempt(ctx, e.tx, participantOf(e.takeEnv, students[0]), a0, SubmitByParticipant, e.real)
	base := "/ujian/paket/" + e.ex.ID + "/soal/"
	// Isian angka: kunci "12" menerima 12,0.
	rec := e.staffDo(http.MethodPost, base+e.items[KindShort].Question.ID+"/kunci", e.a.Admin,
		url.Values{"accepted": {"12\n"}, "tolerance": {"0,5"}, "aksi": {"terapkan"}, "alasan": {"kunci bukan angka"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("short key fix = %d\n%s", rec.Code, rec.Body.String())
	}
	// Benar/salah: s2 benar.
	tf := e.items[KindTF].Question
	form := url.Values{"aksi": {"terapkan"}, "alasan": {"pernyataan 2 benar"}}
	for _, s := range tf.Options.Statements {
		form.Set("truth_"+s.ID, "B")
	}
	if rec := e.staffDo(http.MethodPost, base+tf.ID+"/kunci", e.a.Admin, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("tf key fix = %d", rec.Code)
	}
	// 2 (isian) + 2 (B/S penuh) dari 12.
	if x, _ := e.score(t, a0.ID); x != 3333 {
		t.Errorf("score after fixes = %d, want 3333", x)
	}
	// Esai tidak punya kunci.
	if rec := e.staffDo(http.MethodGet, base+e.items[KindEssay].Question.ID+"/kunci", e.a.Admin, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("essay key fix = %d", rec.Code)
	}
}
