package exam

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
)

func studentParticipant(e *essayEnv, studentID string) auth.Participant {
	return auth.Participant{Kind: auth.RoleSiswa, SchoolID: e.a.ID, StudentID: studentID, Name: "Siswa"}
}

func TestStudentResultsRelease(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	students := e.students(t)
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	exam := func() Exam {
		x, err := GetExam(ctx, e.tx, e.a.ID, e.ex.ID)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	if _, err := e.tx.Exec(ctx, `UPDATE exams SET results_mode = 'nilai_dan_pembahasan', results_after_all = true WHERE id = $1`, e.ex.ID); err != nil {
		t.Fatal(err)
	}
	p0 := studentParticipant(e, students[0])
	later := e.real.Add(2 * time.Hour)
	// Sesi masih berjalan: tidak bisa dibuka, hasil tertutup.
	if err := ReleaseResults(ctx, e.tx, sa, exam(), true, e.real); !errors.Is(err, ErrSessionsNotEnded) {
		t.Fatalf("release during session = %v", err)
	}
	// Sesi berakhir tetapi guru belum membuka: tertutup.
	if res, _ := StudentResults(ctx, e.tx, e.a.ID, students[0], later); len(res) != 0 {
		t.Fatalf("results before release = %+v", res)
	}
	if err := ReleaseResults(ctx, e.tx, sa, exam(), true, later); err != nil {
		t.Fatal(err)
	}
	res, err := StudentResults(ctx, e.tx, e.a.ID, students[0], later)
	if err != nil || len(res) != 1 || res[0].ScoreX100 == nil || !res[0].Review || res[0].AttemptID != e.att[0].ID {
		t.Fatalf("results after release = %+v, %v", res, err)
	}
	// Kode kartu siswa berlaku 30 hari untuk melihat hasil.
	var until *time.Time
	e.tx.QueryRow(ctx, `SELECT max(results_until) FROM student_credentials WHERE exam_session_id = $1 AND student_id IS NOT NULL`, e.sess.ID).Scan(&until)
	if until == nil || until.Sub(later) < 29*24*time.Hour {
		t.Errorf("results_until = %v", until)
	}
	// Pembahasan: soal, jawaban, kunci, dan ketuntasan TP.
	rv, err := LoadStudentReview(ctx, e.tx, p0, e.att[0].ID, later)
	if err != nil || len(rv.Answers) != len(Kinds) || rv.Result.Score() == "" {
		t.Fatalf("review = %d answers, %v", len(rv.Answers), err)
	}
	keyed := 0
	for _, a := range rv.Answers {
		if len(a.Key) > 0 {
			keyed++
		}
	}
	if keyed != len(Kinds) {
		t.Errorf("review keys shown for %d questions", keyed)
	}
	// Attempt siswa lain tidak bisa dibuka.
	if _, err := LoadStudentReview(ctx, e.tx, p0, e.att[1].ID, later); !errors.Is(err, ErrNotFound) {
		t.Errorf("other student's review = %v", err)
	}
	// Susulan terjadwal kemudian: hasil tertutup lagi sampai susulan selesai.
	if _, err := e.tx.Exec(ctx, `INSERT INTO exam_sessions (school_id, exam_id, kind, label, starts_at, ends_at, created_by)
		VALUES ($1, $2, 'susulan', 'Susulan', $3, $4, $5)`, e.a.ID, e.ex.ID, later.Add(24*time.Hour), later.Add(26*time.Hour), e.a.Admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStudentReview(ctx, e.tx, p0, e.att[0].ID, later); !errors.Is(err, ErrNotFound) {
		t.Errorf("review open while makeup pending = %v", err)
	}
	if res, _ := StudentResults(ctx, e.tx, e.a.ID, students[0], later); len(res) != 0 {
		t.Errorf("results open while makeup pending")
	}
	afterMakeup := later.Add(27 * time.Hour)
	if _, err := LoadStudentReview(ctx, e.tx, p0, e.att[0].ID, afterMakeup); err != nil {
		t.Errorf("review after makeup = %v", err)
	}
	// Aturan "nilai saja": nilai terbuka, pembahasan tidak.
	e.tx.Exec(ctx, `UPDATE exams SET results_mode = 'nilai' WHERE id = $1`, e.ex.ID)
	if res, _ := StudentResults(ctx, e.tx, e.a.ID, students[0], afterMakeup); len(res) != 1 || res[0].Review {
		t.Errorf("score-only results = %+v", res)
	}
	if _, err := LoadStudentReview(ctx, e.tx, p0, e.att[0].ID, afterMakeup); !errors.Is(err, ErrNotFound) {
		t.Errorf("review with score-only rule = %v", err)
	}
	// Menutup kembali hasil.
	if err := ReleaseResults(ctx, e.tx, sa, exam(), false, afterMakeup); err != nil {
		t.Fatal(err)
	}
	if res, _ := StudentResults(ctx, e.tx, e.a.ID, students[0], afterMakeup); len(res) != 0 {
		t.Errorf("results after hide = %+v", res)
	}
}

func TestStudentResultsReadOnlyLogin(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	later := e.real.Add(2 * time.Hour)
	ex, _ := GetExam(ctx, e.tx, e.a.ID, e.ex.ID)
	if err := ReleaseResults(ctx, e.tx, sa, ex, true, later); err != nil {
		t.Fatal(err)
	}
	// Kode kartu yang sesinya sudah berakhir masuk hanya-baca.
	var card Card
	for _, c := range e.cards {
		if c.Code != "" {
			card = c
		}
	}
	p, err := Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: card.Identifier, Code: card.Code, DeviceID: dev2, Now: later.Add(10 * 24 * time.Hour)})
	if err != nil || !p.ReadOnly {
		t.Fatalf("results login = %+v, %v", p, err)
	}
	if _, err := Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: card.Identifier, Code: card.Code, DeviceID: dev2,
		Now: later.Add(ResultsWindow + time.Hour)}); err == nil {
		t.Errorf("login after results window must fail")
	}
}

func TestApplicantNeverSeesResults(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	if _, err := e.tx.Exec(ctx, `UPDATE exams SET purpose = 'seleksi', results_mode = 'nilai_dan_pembahasan', results_after_all = false WHERE id = $1`, e.ex.ID); err != nil {
		t.Fatal(err)
	}
	ex, _ := GetExam(ctx, e.tx, e.a.ID, e.ex.ID)
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	if err := ReleaseResults(ctx, e.tx, sa, ex, true, e.real.Add(2*time.Hour)); !errors.Is(err, ErrAdmissionNoResults) {
		t.Errorf("release admission results = %v", err)
	}
	if open, _ := ResultsOpen(ctx, e.tx, ex, e.real.Add(2*time.Hour)); open {
		t.Errorf("admission results open")
	}
	// Peserta pendaftar tidak pernah mendapat pembahasan, attempt apa pun.
	app := auth.Participant{Kind: auth.RolePendaftar, SchoolID: e.a.ID, ApplicantID: e.a.ApplicantID}
	for _, a := range e.att {
		if _, err := LoadStudentReview(ctx, e.tx, app, a.ID, e.real.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
			t.Errorf("applicant review = %v", err)
		}
	}
	// Siswa pun tidak, untuk tes seleksi.
	if res, _ := StudentResults(ctx, e.tx, e.a.ID, e.students(t)[0], e.real.Add(2*time.Hour)); len(res) != 0 {
		t.Errorf("admission exam listed in results")
	}
}

func TestChildExamsSummary(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	students := e.students(t)
	later := e.real.Add(2 * time.Hour)
	// Siswa keempat terdaftar tetapi tidak hadir; susulan dijadwalkan.
	var absent string
	if err := e.tx.QueryRow(ctx, `WITH s AS (INSERT INTO students (school_id, nis, full_name) VALUES ($1, 'ABS-2', 'Siswa Absen') RETURNING id)
		INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no) SELECT $1, $2, id, 'ABS-2' FROM s RETURNING student_id`,
		e.a.ID, e.sess.ID).Scan(&absent); err != nil {
		t.Fatal(err)
	}
	c, err := LoadChildExams(ctx, e.tx, e.a.ID, absent, later)
	if err != nil || len(c.Absent) != 1 || c.Absent[0].Makeup != nil {
		t.Fatalf("absent = %+v, %v", c.Absent, err)
	}
	var mk string
	e.tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, kind, label, starts_at, ends_at, created_by)
		VALUES ($1, $2, 'susulan', 'Susulan', $3, $4, $5) RETURNING id`, e.a.ID, e.ex.ID, later.Add(24*time.Hour), later.Add(26*time.Hour), e.a.Admin.ID).Scan(&mk)
	e.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no) VALUES ($1, $2, $3, 'ABS-2')`, e.a.ID, mk, absent)
	c, _ = LoadChildExams(ctx, e.tx, e.a.ID, absent, later)
	if len(c.Absent) != 1 || c.Absent[0].Makeup == nil || len(c.Upcoming) != 1 || !c.Upcoming[0].Makeup {
		t.Errorf("makeup: absent %+v upcoming %+v", c.Absent, c.Upcoming)
	}
	// Siswa yang hadir: belum ada nilai sampai susulan selesai dan dibuka.
	c, _ = LoadChildExams(ctx, e.tx, e.a.ID, students[0], later)
	if len(c.Results) != 0 || len(c.Absent) != 0 {
		t.Errorf("present student before release: %+v", c)
	}
}

func TestStudentReviewHTTP(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	later := e.real.Add(2 * time.Hour)
	e.tx.Exec(ctx, `UPDATE exams SET results_mode = 'nilai_dan_pembahasan', results_after_all = true WHERE id = $1`, e.ex.ID)
	ex, _ := GetExam(ctx, e.tx, e.a.ID, e.ex.ID)
	students := e.students(t)
	var card Card
	for _, c := range e.cards {
		var sid string
		e.tx.QueryRow(ctx, `SELECT id FROM students WHERE school_id = $1 AND nis = $2`, e.a.ID, c.Identifier).Scan(&sid)
		if sid == students[0] {
			card = c
		}
	}
	if card.Code == "" {
		t.Fatal("no card for first student")
	}
	e.clock = later
	// Sebelum dibuka: login hasil ditolak (kode sudah lewat jendela).
	if rec := e.form(dev2, "/u/"+e.a.Code, url.Values{"identifier": {card.Identifier}, "code": {card.Code}}); rec.Code == http.StatusSeeOther {
		t.Fatalf("read-only login before release succeeded")
	}
	if err := ReleaseResults(ctx, e.tx, sa, ex, true, later); err != nil {
		t.Fatal(err)
	}
	e.login(t, dev2, card)
	if body := e.do(dev2, http.MethodGet, "/u/beranda", "", "").Body.String(); !strings.Contains(body, "Hasil ujian") || !strings.Contains(body, "/u/hasil/"+e.att[0].ID) {
		t.Fatalf("home results missing")
	}
	rec := e.do(dev2, http.MethodGet, "/u/hasil/"+e.att[0].ID, "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Pembahasan") || !strings.Contains(rec.Body.String(), "Kunci") {
		t.Fatalf("own review = %d", rec.Code)
	}
	// Attempt siswa lain atau ID asal: 404.
	for _, id := range []string{e.att[1].ID, "00000000-0000-0000-0000-000000000000", "bukan-uuid"} {
		if rec := e.do(dev2, http.MethodGet, "/u/hasil/"+id, "", ""); rec.Code != http.StatusNotFound {
			t.Errorf("review %s = %d", id, rec.Code)
		}
	}
	// Ditutup kembali: 404.
	ReleaseResults(ctx, e.tx, sa, ex, false, later)
	if rec := e.do(dev2, http.MethodGet, "/u/hasil/"+e.att[0].ID, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("review after hide = %d", rec.Code)
	}
}

func TestApplicantReviewHTTP(t *testing.T) {
	e := newGradeEnv(t)
	ctx := context.Background()
	students := e.students(t)
	first := e.start(t, students[0], dev1)
	e.load(t, first)
	e.tx.Exec(ctx, `UPDATE exams SET purpose = 'seleksi', results_mode = 'nilai_dan_pembahasan', results_after_all = false WHERE id = $1`, e.ex.ID)
	e.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, applicant_id, participant_no, variant_id)
		SELECT school_id, session_id, $2, 'P-01', variant_id FROM exam_session_participants WHERE session_id = $1 LIMIT 1`, e.sess.ID, e.a.ApplicantID)
	app := auth.Participant{Kind: auth.RolePendaftar, SchoolID: e.a.ID, ApplicantID: e.a.ApplicantID}
	a, err := StartAttempt(ctx, e.tx, app, StartRequest{SessionID: e.sess.ID, DeviceID: devProctor3, Now: e.real})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitAttempt(ctx, e.tx, app, a, SubmitByParticipant, e.real); err != nil {
		t.Fatal(err)
	}
	_, code, err := NewIssuer().Issue(ctx, e.tx, e.a.ID, e.a.Admin.ID, IssueRequest{Owner: Owner{ApplicantID: e.a.ApplicantID}, SessionID: e.sess.ID,
		Start: e.real.Add(-10 * time.Minute), End: e.real.Add(50 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	var reg string
	e.tx.QueryRow(ctx, `SELECT registration_number FROM applicants WHERE id = $1`, e.a.ApplicantID).Scan(&reg)
	e.clock = e.real
	if rec := e.form(devProctor3, "/u/"+e.a.Code, url.Values{"identifier": {reg}, "code": {code}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("applicant login = %d", rec.Code)
	}
	for _, id := range []string{a.ID, first.ID} {
		if rec := e.do(devProctor3, http.MethodGet, "/u/hasil/"+id, "", ""); rec.Code != http.StatusNotFound {
			t.Errorf("applicant review %s = %d", id, rec.Code)
		}
	}
	if body := e.do(devProctor3, http.MethodGet, "/u/beranda", "", "").Body.String(); strings.Contains(body, "Hasil ujian") {
		t.Errorf("applicant home shows results")
	}
	if body := e.do(devProctor3, http.MethodGet, "/u/a/"+a.ID+"/selesai", "", "").Body.String(); strings.Contains(body, "Nilai:") || strings.Contains(body, "pembahasan") {
		t.Errorf("applicant done page shows score or review")
	}
}
