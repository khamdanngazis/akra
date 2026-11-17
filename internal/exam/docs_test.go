package exam

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scheduledExam membuat ulangan berisi soal untuk kelas guru dengan sesi
// mulai start (satu jam).
func (e *examEnv) scheduledExam(t *testing.T, start time.Time, questions ...string) Exam {
	t.Helper()
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	if _, err := AddQuestions(ctx, e.tx, e.owner, ex.ID, e.firstSection(t, ex), questions, now); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateClassSessions(ctx, e.tx, e.owner, ex.ID, []ClassSlot{{ClassID: e.a.ClassID, Start: start, End: start.Add(time.Hour)}}, now); err != nil {
		t.Fatal(err)
	}
	if err := ScheduleExam(ctx, e.tx, e.owner, ex.ID, now); err != nil {
		t.Fatal(err)
	}
	ex, _ = GetExam(ctx, e.tx, e.a.ID, ex.ID)
	return ex
}

func TestCardsCodesAndReissue(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	is := NewIssuer()
	// Sesi 5 hari lagi: kartu belum boleh dicetak (H-3).
	far := e.scheduledExam(t, now.Add(5*24*time.Hour), e.qs[0].ID)
	_, err := IssueCards(ctx, e.tx, e.owner, is, CardScope{ExamID: far.ID}, far.Name, "https://app.akra.test", false, now)
	var te *TooEarlyError
	if !errors.As(err, &te) || !te.Opens.Equal(now.Add(2*24*time.Hour)) {
		t.Fatalf("too early: %v", err)
	}
	start := now.Add(2 * 24 * time.Hour)
	ex := e.scheduledExam(t, start, e.qs[1].ID)
	cards, err := IssueCards(ctx, e.tx, e.owner, is, CardScope{ExamID: ex.ID}, ex.Name, "https://app.akra.test", false, now)
	if err != nil || len(cards) == 0 {
		t.Fatalf("cards = %d, %v", len(cards), err)
	}
	c := cards[0]
	if len(c.Code) != CodeLength || !strings.Contains(c.LoginURL, "/u/") || !strings.Contains(c.LoginURL, "nis="+c.Identifier) ||
		strings.Contains(c.LoginURL, c.Code) {
		t.Errorf("card = %+v", c)
	}
	var buf bytes.Buffer
	if err := WriteCardsPDF(&buf, Letterhead{School: "SMP Uji"}, cards, now, false); err != nil || !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
		t.Errorf("cards pdf: %v", err)
	}
	login := func(code string) error {
		_, err := Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: c.Identifier, Code: code, DeviceID: "11111111-1111-1111-1111-111111111111", Now: start.Add(time.Minute)})
		return err
	}
	if err := login(c.Code); err != nil {
		t.Fatalf("login with card code: %v", err)
	}
	// Cetak ulang perlu konfirmasi; kode lama lalu tidak berlaku.
	if _, err := IssueCards(ctx, e.tx, e.owner, is, CardScope{ExamID: ex.ID}, ex.Name, "", false, now); !errors.Is(err, ErrCardsIssued) {
		t.Fatalf("reissue without confirm: %v", err)
	}
	again, err := IssueCards(ctx, e.tx, e.owner, is, CardScope{ExamID: ex.ID}, ex.Name, "", true, now)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Code == c.Code {
		t.Fatal("reissue kept the code")
	}
	if err := login(c.Code); !errors.Is(err, ErrBadLogin) {
		t.Errorf("old code after reissue: %v", err)
	}
	// Kode baru: perangkat yang sama tetap boleh (kredensial baru).
	if err := login(again[0].Code); err != nil {
		t.Errorf("new code: %v", err)
	}
	var printed int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'exam.cards_print'`, e.a.ID).Scan(&printed)
	if printed != 2 {
		t.Errorf("card audits = %d", printed)
	}
	// Lewat HTTP: terlalu awal menjadi pesan, bukan PDF.
	rec := e.post("/ujian/paket/"+far.ID+"/kartu", e.a.Teacher.ID, "guru", nil)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("early cards http = %d", rec.Code)
	}
	// Handler memakai jam sungguhan: sesi dua jam lagi.
	soon := e.create(t, e.owner, PurposeQuiz)
	AddQuestions(ctx, e.tx, e.owner, soon.ID, e.firstSection(t, soon), []string{e.qs[2].ID}, time.Now())
	real := time.Now().Add(2 * time.Hour)
	if _, err := CreateClassSessions(ctx, e.tx, e.owner, soon.ID, []ClassSlot{{ClassID: e.a.ClassID, Start: real, End: real.Add(time.Hour)}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	rec = e.post("/ujian/paket/"+soon.ID+"/kartu", e.a.Teacher.ID, "guru", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" {
		t.Errorf("cards http = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// Satu kartu PTS/PAS: satu kode untuk beberapa sesi; login memilih sesi
// yang sedang berjalan.
func TestCardCoversSeveralSessions(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	is := NewIssuer()
	var sid string
	var nis string
	e.tx.QueryRow(ctx, `SELECT id, nis FROM students WHERE school_id = $1 AND id = $2`, e.a.ID, e.a.StudentIDs[0]).Scan(&sid, &nis)
	day1 := now.Add(24 * time.Hour)
	day2 := now.Add(48 * time.Hour)
	var sess []CardWindow
	for _, st := range []time.Time{day1, day2} {
		var id string
		if err := e.tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, class_id, starts_at, ends_at, created_by) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			e.a.ID, e.a.ExamID, e.a.ClassID, st, st.Add(90*time.Minute), e.a.Teacher.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		sess = append(sess, CardWindow{SessionID: id, Start: st, End: st.Add(90 * time.Minute)})
	}
	code, err := is.IssueCard(ctx, e.tx, e.a.ID, e.a.Admin.ID, Owner{StudentID: sid}, sess)
	if err != nil {
		t.Fatal(err)
	}
	for i, st := range []time.Time{day1, day2} {
		p, err := Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: nis, Code: code, DeviceID: "22222222-2222-2222-2222-222222222222", Now: st.Add(10 * time.Minute)})
		if err != nil {
			t.Fatalf("day %d: %v", i+1, err)
		}
		var got string
		e.tx.QueryRow(ctx, `SELECT exam_session_id FROM student_credentials WHERE id = $1`, p.CredentialID).Scan(&got)
		if got != sess[i].SessionID {
			t.Errorf("day %d logged into session %s, want %s", i+1, got, sess[i].SessionID)
		}
	}
	// Di antara sesi: kode belum berlaku untuk sesi berikutnya.
	_, err = Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: nis, Code: code, DeviceID: "22222222-2222-2222-2222-222222222222", Now: day1.Add(6 * time.Hour)})
	var nyv *NotYetValidError
	if !errors.As(err, &nyv) || !nyv.SessionStart.Equal(day2) {
		t.Errorf("between sessions: %v", err)
	}
}

// Naskah cadangan tidak memuat kunci atau pembahasan; stimulus dan teks
// Arab tercetak; kunci jawaban tercatat di audit.
func TestPaperAndKeyPrint(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	st, err := CreateStimulus(ctx, e.tx, e.owner, e.bank.ID, Stimulus{Title: "Bacaan Arab", BodySrc: `<p lang="ar" dir="rtl">ذَهَبَ أَحْمَدُ</p>`,
		BodyHTML: `<p lang="ar" dir="rtl">ذَهَبَ أَحْمَدُ</p>`}, now)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 2; i++ {
		q := sampleQuestion(KindMC)
		q.StimulusID = st.ID
		c, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, q, now)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	ex := e.scheduledExam(t, now.Add(time.Hour), append(ids, e.qs[0].ID)...)
	body := e.getAs("/ujian/paket/"+ex.ID+"/naskah", e.a.Teacher.ID, "guru").Body.String()
	if strings.Contains(body, "RAHASIA-PEMBAHASAN") || strings.Contains(body, "Kunci") {
		t.Error("paper leaks key or explanation")
	}
	if strings.Count(body, "Bacaan Arab") != 1 || !strings.Contains(body, `lang="ar"`) || !strings.Contains(body, "LEMBAR JAWABAN") {
		t.Error("paper missing stimulus once, Arabic, or answer sheet")
	}
	if rec := e.getAs("/ujian/paket/"+ex.ID+"/naskah", e.a.Principal.ID, "kepala_sekolah"); rec.Code != http.StatusForbidden {
		t.Errorf("principal paper = %d", rec.Code)
	}
	key := e.getAs("/ujian/paket/"+ex.ID+"/kunci", e.a.Teacher.ID, "guru").Body.String()
	if !strings.Contains(key, "KUNCI JAWABAN") || !strings.Contains(key, keyLines(e.qs[0])[0]) {
		t.Error("key page missing answers")
	}
	var n int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'exam.key_print' AND entity_id = $2`, e.a.ID, ex.ID).Scan(&n)
	if n != 1 {
		t.Errorf("key audits = %d", n)
	}
	for _, p := range []string{"/daftar-hadir.pdf", "/berita-acara.pdf"} {
		rec := e.getAs("/ujian/paket/"+ex.ID+p, e.a.Teacher.ID, "guru")
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "%PDF") {
			t.Errorf("%s = %d", p, rec.Code)
		}
	}
}

func TestInterleaveByClass(t *testing.T) {
	in := []string{"A1", "A2", "A3", "B1", "B2", "C1"}
	got := InterleaveByClass(in, func(s string) string { return s[:1] })
	if strings.Join(got, ",") != "A1,B1,C1,A2,B2,A3" {
		t.Errorf("interleave = %v", got)
	}
}
