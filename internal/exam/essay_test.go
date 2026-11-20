package exam

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"akra/internal/testutil"
)

const (
	essayOriginal = `"Fotosintesis adalah proses tumbuhan hijau membuat makanan sendiri dengan bantuan cahaya matahari, air, dan karbon dioksida sehingga menghasilkan glukosa dan oksigen."`
	essayCopy     = `"fotosintesis adalah proses tumbuhan hijau membuat makanan sendiri dengan bantuan cahaya matahari air dan karbon dioksida, sehingga menghasilkan glukosa serta oksigen"`
	essayOwn      = `"Tumbuhan memakai energi cahaya untuk mengubah air dan CO2 menjadi gula; oksigen dilepas ke udara sebagai hasil sampingan."`
)

// essayEnv: tiga peserta sudah mengumpulkan; jawaban esai peserta 1
// menyalin peserta 0.
type essayEnv struct {
	*gradeEnv
	att   []Attempt
	essay string
	page  string
}

func newEssayEnv(t *testing.T) *essayEnv {
	t.Helper()
	e := &essayEnv{gradeEnv: newGradeEnv(t)}
	ctx := context.Background()
	students := e.students(t)
	devs := []string{dev1, dev2, devProctor3}
	for i, text := range []string{essayOriginal, essayCopy, essayOwn} {
		a := e.start(t, students[i], devs[i])
		if i == 0 {
			e.load(t, a)
		}
		e.answer(t, a, devs[i], map[Kind]string{KindEssay: text, KindMC: `"b"`})
		if _, err := SubmitAttempt(ctx, e.tx, participantOf(e.takeEnv, students[i]), a, SubmitByParticipant, e.real); err != nil {
			t.Fatal(err)
		}
		e.att = append(e.att, a)
	}
	e.essay = e.items[KindEssay].Question.ID
	e.page = "/ujian/paket/" + e.ex.ID + "/esai"
	return e
}

func TestEssayGradingFlow(t *testing.T) {
	e := newEssayEnv(t)
	ctx := context.Background()
	for _, a := range e.att {
		if _, st := e.score(t, a.ID); st != ScoreAwaitEssay {
			t.Fatalf("before grading status = %s", st)
		}
	}
	body := e.staffDo(http.MethodGet, e.page, e.a.Admin, nil).Body.String()
	if !strings.Contains(body, "0/3 dikoreksi") || !strings.Contains(body, "Bagi koreksi") {
		t.Fatalf("queue page missing progress or assignment form")
	}
	// Urutan antrian (waktu kumpul, lalu nomor peserta).
	list, err := LoadEssayAnswers(ctx, e.tx, e.ex, e.essay, GraderScope{Full: true})
	if err != nil || len(list) != 3 {
		t.Fatalf("answers = %d, %v", len(list), err)
	}
	// Layar koreksi: anonim, jawaban pertama belum dikoreksi, kemiripan.
	rec := e.staffDo(http.MethodGet, e.page+"/"+e.essay, e.a.Admin, nil)
	body = rec.Body.String()
	name0 := list[0].Name
	if rec.Code != http.StatusOK || !strings.Contains(body, "Jawaban 1") || strings.Contains(body, name0) || !strings.Contains(body, "0/3") {
		t.Fatalf("grade screen = %d (anonymous, first answer, progress)", rec.Code)
	}
	copied := 0
	for _, a := range list {
		if len(a.Similar) == 1 && a.Similar[0].Percent >= 70 {
			copied++
		}
	}
	if copied != 2 {
		t.Errorf("copy pair not flagged: %+v", list)
	}
	if b := e.staffDo(http.MethodGet, e.page+"/"+e.essay+"?a="+e.att[1].ID, e.a.Admin, nil).Body.String(); !strings.Contains(b, "Mirip") {
		t.Errorf("similar note missing on copied answer")
	}
	if b := e.staffDo(http.MethodGet, e.page+"/"+e.essay+"?anonim=0", e.a.Admin, nil).Body.String(); !strings.Contains(b, name0) {
		t.Errorf("name not shown when anonymity turned off")
	}
	// Skor tidak valid.
	if rec := e.staffDo(http.MethodPost, e.page+"/"+e.essay+"/"+e.att[0].ID, e.a.Admin, url.Values{"skor": {"9"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("score above max = %d", rec.Code)
	}
	// Simpan & berikutnya.
	rec = e.staffDo(http.MethodPost, e.page+"/"+e.essay+"/"+e.att[0].ID, e.a.Admin, url.Values{"skor": {"1,5"}, "komentar": {"Kurang menyebut klorofil."}})
	nextWant := ""
	for i, a := range list {
		if a.AttemptID == e.att[0].ID {
			nextWant = list[(i+1)%3].AttemptID
		}
	}
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "a="+nextWant) {
		t.Fatalf("save = %d → %s", rec.Code, rec.Header().Get("Location"))
	}
	// MC 2 + esai 1,5 = 3,5 dari 12 → 29,17; final karena tidak ada esai lain.
	if x, st := e.score(t, e.att[0].ID); x != 2917 || st != ScoreFinal {
		t.Errorf("after essay grade = %d %s, want 2917 final", x, st)
	}
	fs, _ := FinalScores(ctx, e.tx, e.a.ID, e.ex.ID)
	if len(fs) != 1 {
		t.Errorf("final scores after one essay = %d, want 1", len(fs))
	}
	var comment string
	e.tx.QueryRow(ctx, `SELECT essay_comment FROM exam_answers WHERE attempt_id = $1 AND question_id = $2`, e.att[0].ID, e.essay).Scan(&comment)
	if comment != "Kurang menyebut klorofil." {
		t.Errorf("comment = %q", comment)
	}
	for _, a := range e.att[1:] {
		e.staffDo(http.MethodPost, e.page+"/"+e.essay+"/"+a.ID, e.a.Admin, url.Values{"skor": {"2"}})
	}
	if x, st := e.score(t, e.att[2].ID); x != 3333 || st != ScoreFinal {
		t.Errorf("third = %d %s", x, st)
	}
	if n, _ := PendingEssays(ctx, e.tx, e.a.ID, e.ex.ID); n != 0 {
		t.Errorf("pending = %d", n)
	}
	var audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'exam.essay_grade'`).Scan(&audits)
	if audits != 3 {
		t.Errorf("essay audits = %d", audits)
	}
	// Pemegang ujian diberi tahu nilai siap.
	work, err := MyEssayWork(ctx, e.tx, e.a.ID, e.ex.OwnerID, e.real)
	if err != nil || len(work) != 1 || !work[0].Ready {
		t.Errorf("owner essay work = %+v, %v", work, err)
	}
}

func TestEssayAssignmentScope(t *testing.T) {
	e := newEssayEnv(t)
	ctx := context.Background()
	grader := testutil.User{ID: e.teacher2, Role: "guru"}
	// Tanpa penugasan: tidak terlihat.
	if rec := e.staffDo(http.MethodGet, e.page, grader, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unassigned teacher = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodGet, e.page, e.a.Principal, nil); rec.Code != http.StatusForbidden {
		t.Errorf("principal = %d", rec.Code)
	}
	// Sesi lain (kosong) ditugaskan: soal terlihat, jawaban sesi ini tidak.
	var other string
	if err := e.tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, label, starts_at, ends_at, created_by)
		SELECT school_id, exam_id, 'Susulan', starts_at, ends_at, created_by FROM exam_sessions WHERE id = $1 RETURNING id`, e.sess.ID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if rec := e.staffDo(http.MethodPost, e.page+"/tugas", e.a.Admin, url.Values{"user_id": {e.teacher2}, "session_id": {other}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("assign = %d", rec.Code)
	}
	body := e.staffDo(http.MethodGet, e.page, grader, nil).Body.String()
	if !strings.Contains(body, "0/0 dikoreksi") || strings.Contains(body, "Bagi koreksi") || !strings.Contains(body, "ditugaskan") {
		t.Errorf("assigned (other session) queue wrong")
	}
	if b := e.staffDo(http.MethodGet, e.page+"/"+e.essay+"?a="+e.att[0].ID, grader, nil).Body.String(); strings.Contains(b, "Fotosintesis adalah") {
		t.Errorf("out-of-scope answer visible")
	}
	if rec := e.staffDo(http.MethodPost, e.page+"/"+e.essay+"/"+e.att[0].ID, grader, url.Values{"skor": {"1"}}); rec.Code != http.StatusNotFound {
		t.Errorf("grade out of scope = %d", rec.Code)
	}
	// Penugasan tidak membuka halaman ujian dan tidak bisa membagi koreksi.
	if rec := e.staffDo(http.MethodGet, "/ujian/paket/"+e.ex.ID, grader, nil); rec.Code == http.StatusOK {
		t.Errorf("grader opened exam detail = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodPost, e.page+"/tugas", grader, url.Values{"user_id": {e.teacher2}}); rec.Code != http.StatusForbidden {
		t.Errorf("grader assigning = %d", rec.Code)
	}
	// Ditugaskan soal esai di sesi ini: bisa mengoreksi, muncul di beranda.
	if err := AssignGrader(ctx, e.tx, StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}, e.ex,
		e.teacher2, e.essay, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	work, _ := MyEssayWork(ctx, e.tx, e.a.ID, e.teacher2, e.real)
	if len(work) != 1 || work[0].Pending != 3 {
		t.Errorf("grader home work = %+v", work)
	}
	if rec := e.staffDo(http.MethodPost, e.page+"/"+e.essay+"/"+e.att[0].ID, grader, url.Values{"skor": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("grade in scope = %d", rec.Code)
	}
	// Penugasan dengan guru bukan dari sekolah ini atau soal bukan esai ditolak.
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	if err := AssignGrader(ctx, e.tx, sa, e.ex, e.b.Teacher.ID, "", ""); err != ErrBadAssignment {
		t.Errorf("other school teacher = %v", err)
	}
	if err := AssignGrader(ctx, e.tx, sa, e.ex, e.teacher2, e.items[KindMC].Question.ID, ""); err != ErrBadAssignment {
		t.Errorf("non-essay question = %v", err)
	}
}

func TestParseEssayScore(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"2", 20000, true}, {"2,5", 25000, true}, {"0.25", 2500, true}, {"0", 0, true},
		{"4", 0, false}, {"-1", 0, false}, {"1,234", 0, false}, {"dua", 0, false}, {"", 0, false},
	} {
		got, err := ParseEssayScore(c.in, 3)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ParseEssayScore(%q) = %d, %v", c.in, got, err)
		}
	}
	if PointsLabel(25000) != "2,5" || PointsLabel(20000) != "2" || PointsLabel(6667) != "0,6667" {
		t.Errorf("points labels")
	}
}
