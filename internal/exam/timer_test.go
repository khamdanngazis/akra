package exam

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
)

func TestComputeDeadline(t *testing.T) {
	s := time.Date(2027, 3, 10, 1, 0, 0, 0, time.UTC)
	m := func(n float64) time.Time { return s.Add(time.Duration(n * float64(time.Minute))) }
	base := DeadlineInput{SessionStart: s, SessionEnd: m(60), LatePolicy: LateFull, DurationMinutes: 40}
	cases := []struct {
		name string
		mod  func(*DeadlineInput)
		want time.Time
	}{
		{"on time", func(in *DeadlineInput) { in.StartedAt = s }, m(40)},
		{"late, full duration", func(in *DeadlineInput) { in.StartedAt = m(10) }, m(50)},
		{"late, full duration capped by session end", func(in *DeadlineInput) { in.StartedAt = m(30) }, m(60)},
		{"late, cut", func(in *DeadlineInput) { in.StartedAt = m(10); in.LatePolicy = LateCut }, m(40)},
		{"late, cut, very late", func(in *DeadlineInput) { in.StartedAt = m(45); in.LatePolicy = LateCut }, m(40)},
		{"participant extra", func(in *DeadlineInput) { in.StartedAt = s; in.ParticipantExtra = 15 }, m(55)},
		{"participant extra capped", func(in *DeadlineInput) { in.StartedAt = m(10); in.ParticipantExtra = 15 }, m(60)},
		{"session extra moves the cap", func(in *DeadlineInput) { in.StartedAt = m(30); in.SessionExtra = 10 }, m(70)},
		{"pause on time", func(in *DeadlineInput) { in.StartedAt = s; in.Paused = 10 * time.Minute }, m(50)},
		{"pause moves the cap", func(in *DeadlineInput) { in.StartedAt = m(30); in.Paused = 10 * time.Minute }, m(70)},
		{"pause with seconds", func(in *DeadlineInput) { in.StartedAt = s; in.Paused = 90 * time.Second }, m(41.5)},
		{"everything", func(in *DeadlineInput) {
			in.StartedAt, in.LatePolicy, in.ParticipantExtra, in.SessionExtra, in.Paused = m(5), LateCut, 10, 5, 3*time.Minute
		}, m(58)},
		{"long window: cap not reached", func(in *DeadlineInput) { in.StartedAt = m(10); in.SessionEnd = m(24 * 60) }, m(50)},
	}
	for _, c := range cases {
		in := base
		c.mod(&in)
		if got := ComputeDeadline(in); !got.Equal(c.want) {
			t.Errorf("%s: deadline = %s, want %s", c.name, got.Sub(s), c.want.Sub(s))
		}
	}
}

func participantOf(e *takeEnv, studentID string) auth.Participant {
	return auth.Participant{Kind: auth.RoleSiswa, SchoolID: e.a.ID, StudentID: studentID, Name: "Uji"}
}

func TestPauseAndExtraTime(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	s := now.Add(time.Hour)
	m := func(n int) time.Time { return s.Add(time.Duration(n) * time.Minute) }
	_, sess, _ := e.allKindsExam(t, s)
	students := e.students(t)
	p0, p1 := participantOf(e, students[0]), participantOf(e, students[1])

	a, err := StartAttempt(ctx, e.tx, p0, StartRequest{SessionID: sess.ID, DeviceID: dev1, Now: m(1)})
	if err != nil {
		t.Fatal(err)
	}
	if !a.DeadlineAt.Equal(m(41)) {
		t.Fatalf("deadline = %s", a.DeadlineAt.Sub(s))
	}

	// Jeda sesi 10 menit: tenggat bertambah tepat 10 menit.
	n, err := PauseSession(ctx, e.tx, e.owner, sess.ID, m(11))
	if err != nil || n != 1 {
		t.Fatalf("PauseSession = %d, %v", n, err)
	}
	if _, err := StartAttempt(ctx, e.tx, p1, StartRequest{SessionID: sess.ID, DeviceID: dev2, Now: m(12)}); !errors.Is(err, ErrSessionPaused) {
		t.Errorf("start during pause: %v", err)
	}
	info, _ := GetSessionInfo(ctx, e.tx, p0, sess.ID)
	if !info.Paused || info.Last.PausedAt == nil {
		t.Errorf("paused info = %+v", info)
	}
	// Antrian dari perangkat tetap diterima selama jeda; balasan membawa jeda.
	ch := []AnswerChange{{QuestionID: a.QuestionOrder[0], Value: json.RawMessage(`"x"`), Seq: 1}}
	res, err := SaveAnswers(ctx, e.tx, e.a.ID, a.ID, dev1, SaveRequest{Changes: ch, Pos: -1}, m(15))
	if err != nil || res.PausedAt == nil || !res.PausedAt.Equal(m(11)) {
		t.Errorf("save during pause = %+v, %v", res, err)
	}
	if n, err := ResumeSession(ctx, e.tx, e.owner, sess.ID, m(21)); err != nil || n != 1 {
		t.Fatalf("ResumeSession = %d, %v", n, err)
	}
	a, _ = GetAttempt(ctx, e.tx, p0, a.ID)
	if !a.DeadlineAt.Equal(m(51)) || a.PausedAt != nil || a.PausedMS != (10*time.Minute).Milliseconds() {
		t.Errorf("after 10 minute pause: deadline %s, paused %v, ms %d", a.DeadlineAt.Sub(s), a.PausedAt, a.PausedMS)
	}
	// Jeda dengan detik dihitung tepat.
	if err := PauseAttempt(ctx, e.tx, e.owner, a.ID, m(30)); err != nil {
		t.Fatal(err)
	}
	if err := PauseAttempt(ctx, e.tx, e.owner, a.ID, m(31)); err != nil {
		t.Errorf("pause twice: %v", err)
	}
	// Selama jeda attempt tidak pernah kedaluwarsa: tidak dikumpulkan job
	// walau tenggat lama sudah lewat jauh, dan simpan tetap diterima.
	late := m(100)
	if _, err := AutoSubmitExpired(ctx, e.tx, late); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetAttempt(ctx, e.tx, p0, a.ID); !got.Running() || got.Expired(late) {
		t.Errorf("paused attempt closed: %+v", got)
	}
	ch[0].Seq = 2
	if _, err := SaveAnswers(ctx, e.tx, e.a.ID, a.ID, dev1, SaveRequest{Changes: ch, Pos: -1}, late); err != nil {
		t.Errorf("save while paused past old deadline: %v", err)
	}
	if err := ResumeAttempt(ctx, e.tx, e.owner, a.ID, late.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	a, _ = GetAttempt(ctx, e.tx, p0, a.ID)
	// 51 + jeda 70,5 menit.
	if want := m(51).Add(70*time.Minute + 30*time.Second); !a.DeadlineAt.Equal(want) {
		t.Errorf("after attempt pause: deadline %s, want %s", a.DeadlineAt.Sub(s), want.Sub(s))
	}

	// Tambah waktu sesi dan peserta.
	at := late.Add(time.Minute)
	before := a.DeadlineAt
	if err := AddSessionTime(ctx, e.tx, e.owner, sess.ID, 5, at); err != nil {
		t.Fatal(err)
	}
	a, _ = GetAttempt(ctx, e.tx, p0, a.ID)
	if !a.DeadlineAt.Equal(before.Add(5 * time.Minute)) {
		t.Errorf("session +5: deadline %s", a.DeadlineAt.Sub(before))
	}
	// Waktu tambahan peserta tetap dibatasi akhir sesi: mulai 1 + 40 + 10
	// = 51 (di bawah akhir sesi 60), jadi bertambah 10 menit.
	part := info.ParticipantID
	if err := AddParticipantTime(ctx, e.tx, e.owner, part, 10, at); err != nil {
		t.Fatal(err)
	}
	a, _ = GetAttempt(ctx, e.tx, p0, a.ID)
	if !a.DeadlineAt.Equal(before.Add(15 * time.Minute)) {
		t.Errorf("participant +10: deadline %s", a.DeadlineAt.Sub(before))
	}
	// Tambahan berikutnya melewati akhir sesi: dibatasi (1+40+20 > 60 →
	// 60), jadi hanya bertambah 9 menit lagi.
	if err := AddParticipantTime(ctx, e.tx, e.owner, part, 10, at); err != nil {
		t.Fatal(err)
	}
	a, _ = GetAttempt(ctx, e.tx, p0, a.ID)
	if !a.DeadlineAt.Equal(before.Add(24 * time.Minute)) {
		t.Errorf("participant +10 capped: deadline %s", a.DeadlineAt.Sub(before))
	}
	info, _ = GetSessionInfo(ctx, e.tx, p0, sess.ID)
	if info.Duration != 40+20+5 || info.Paused {
		t.Errorf("info duration %d paused %v", info.Duration, info.Paused)
	}
	for _, bad := range []int{0, -1, 61} {
		if err := AddSessionTime(ctx, e.tx, e.owner, sess.ID, bad, at); !errors.Is(err, ErrBadExtra) {
			t.Errorf("AddSessionTime(%d) = %v", bad, err)
		}
	}
	for i := 0; i < 3; i++ {
		AddSessionTime(ctx, e.tx, e.owner, sess.ID, 60, at)
	}
	if err := AddSessionTime(ctx, e.tx, e.owner, sess.ID, 60, at); !errors.Is(err, ErrBadExtra) {
		t.Errorf("session extra over limit: %v", err)
	}
	var audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action IN ('exam.session_pause', 'exam.session_resume', 'exam.attempt_pause',
		'exam.attempt_resume', 'exam.session_add_time', 'exam.participant_add_time')`).Scan(&audits)
	// Jeda/lanjut sesi 2, jeda/lanjut peserta 2 (jeda kedua tidak berubah),
	// tambah waktu sesi 4, tambah waktu peserta 2.
	if audits != 2+2+4+2 {
		t.Errorf("audits = %d", audits)
	}

	// Attempt yang sudah dikumpulkan tidak bisa dijeda.
	if _, err := SubmitAttempt(ctx, e.tx, p0, a, SubmitByParticipant, at); err != nil {
		t.Fatal(err)
	}
	if err := PauseAttempt(ctx, e.tx, e.owner, a.ID, at); !errors.Is(err, ErrAttemptClosed) {
		t.Errorf("pause submitted: %v", err)
	}
	// Sekolah lain tidak bisa menyentuh sesi ini.
	foreign := StaffActor{Actor: e.owner.Actor, SchoolID: e.b.ID}
	if _, err := PauseSession(ctx, e.tx, foreign, sess.ID, at); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign pause: %v", err)
	}
}

func TestPausedSaveResponse(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	s := now.Add(time.Hour)
	_, sess, _ := e.allKindsExam(t, s)
	a, err := StartAttempt(ctx, e.tx, participantOf(e, e.students(t)[0]), StartRequest{SessionID: sess.ID, DeviceID: dev1, Now: s})
	if err != nil {
		t.Fatal(err)
	}
	e.clock = s.Add(time.Minute)
	tok, _ := DevTokenSigner().TokenFor(a, e.clock)
	rec := e.saveAs("/u/a/"+a.ID+"/simpan", tok, `{"changes":[]}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "paused") {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	if err := PauseAttempt(ctx, e.tx, e.owner, a.ID, e.clock); err != nil {
		t.Fatal(err)
	}
	// Jauh setelah tenggat lama: tetap diterima dan membawa waktu jeda.
	e.clock = s.Add(3 * time.Hour)
	rec = e.saveAs("/u/a/"+a.ID+"/simpan", tok, `{"changes":[]}`)
	var body struct {
		OK     bool  `json:"ok"`
		Paused int64 `json:"paused"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusOK || !body.OK || body.Paused != s.Add(time.Minute).UnixMilli() {
		t.Errorf("paused save = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAutoSubmitExpired(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	s := now.Add(time.Hour)
	_, sess, _ := e.allKindsExam(t, s)
	students := e.students(t)
	var as []Attempt
	for i, st := range students[:2] {
		a, err := StartAttempt(ctx, e.tx, participantOf(e, st), StartRequest{SessionID: sess.ID, DeviceID: []string{dev1, dev2}[i], Now: s})
		if err != nil {
			t.Fatal(err)
		}
		as = append(as, a)
	}
	ch := []AnswerChange{{QuestionID: as[0].QuestionOrder[0], Value: json.RawMessage(`"jawaban"`), Seq: 1}}
	if _, err := SaveAnswers(ctx, e.tx, e.a.ID, as[0].ID, dev1, SaveRequest{Changes: ch, Pos: -1}, s.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	status := func(id string) (st, reason string) {
		e.tx.QueryRow(ctx, `SELECT status, coalesce(submit_reason, '') FROM exam_attempts WHERE id = $1`, id).Scan(&st, &reason)
		return
	}
	// Masih dalam tenggang: belum dikumpulkan.
	if _, err := AutoSubmitExpired(ctx, e.tx, as[0].DeadlineAt.Add(SaveGrace-time.Second)); err != nil {
		t.Fatal(err)
	}
	if st, _ := status(as[0].ID); st != AttemptRunning {
		t.Fatalf("submitted within grace: %s", st)
	}
	// Ditinggal (HP mati): job mengumpulkan dengan jawaban tersimpan.
	at := as[0].DeadlineAt.Add(SaveGrace)
	w := &AutoSubmitWorker{DB: e.tx, Logger: slog.New(slog.NewTextHandler(debugOut(), nil)), Now: func() time.Time { return at }}
	if err := w.Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, a := range as {
		if st, reason := status(a.ID); st != AttemptSubmitted || reason != SubmitBySystem {
			t.Errorf("attempt %s = %s/%s", a.ID, st, reason)
		}
	}
	var answered int
	e.tx.QueryRow(ctx, `SELECT (after->>'answered')::int FROM audit_logs WHERE entity_id = $1 AND action = 'exam.attempt_submit'`,
		as[0].ID).Scan(&answered)
	if answered != 1 {
		t.Errorf("audit answered = %d", answered)
	}
	var kept string
	e.tx.QueryRow(ctx, `SELECT value #>> '{}' FROM exam_answers WHERE attempt_id = $1`, as[0].ID).Scan(&kept)
	if kept != "jawaban" {
		t.Errorf("saved answer = %q", kept)
	}
	// Putaran berikutnya tidak mengubah apa pun; halaman selesai menyebut
	// waktu habis.
	before := 0
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'exam.attempt_submit'`).Scan(&before)
	AutoSubmitExpired(ctx, e.tx, at.Add(time.Minute))
	after := 0
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'exam.attempt_submit'`).Scan(&after)
	if after != before {
		t.Errorf("second run audits %d → %d", before, after)
	}
}

func TestLatePolicy(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	s := now.Add(time.Hour)
	ex, sess, _ := e.allKindsExam(t, s)
	students := e.students(t)
	if err := SetLatePolicy(ctx, e.tx, e.owner, ex.ID, sess.ID, "lain", now); !errors.Is(err, ErrBadLatePolicy) {
		t.Errorf("bad policy: %v", err)
	}
	if err := SetLatePolicy(ctx, e.tx, e.owner, ex.ID, sess.ID, LateCut, now); err != nil {
		t.Fatal(err)
	}
	got, _ := GetSession(ctx, e.tx, e.a.ID, sess.ID)
	if got.LatePolicy != LateCut || got.LatePolicyLabel() != "Dipotong sesuai keterlambatan" {
		t.Errorf("session = %+v", got)
	}
	// Terlambat 10 menit: waktu tetap berakhir 40 menit setelah sesi mulai.
	a, err := StartAttempt(ctx, e.tx, participantOf(e, students[0]), StartRequest{SessionID: sess.ID, DeviceID: dev1, Now: s.Add(10 * time.Minute)})
	if err != nil || !a.DeadlineAt.Equal(s.Add(40*time.Minute)) {
		t.Errorf("late start = %v, %v", a.DeadlineAt.Sub(s), err)
	}
	// Terlambat melewati durasi: tidak bisa mulai.
	if _, err := StartAttempt(ctx, e.tx, participantOf(e, students[1]), StartRequest{SessionID: sess.ID, DeviceID: dev2,
		Now: s.Add(45 * time.Minute)}); !errors.Is(err, ErrSessionOver) {
		t.Errorf("start after cut duration: %v", err)
	}
	// Setelah sesi mulai aturan tidak bisa diubah; peserta lain tidak bisa.
	if err := SetLatePolicy(ctx, e.tx, e.owner, ex.ID, sess.ID, LateFull, s); !errors.Is(err, ErrSessionStarted) {
		t.Errorf("after start: %v", err)
	}
	other := e.staff(e.b.Teacher.ID, "guru")
	if err := SetLatePolicy(ctx, e.tx, other, ex.ID, sess.ID, LateFull, now); err == nil {
		t.Error("foreign teacher changed late policy")
	}
}
