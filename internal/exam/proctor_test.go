package exam

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
	"akra/internal/testutil"
)

// proctorEnv: ujian berisi soal semua jenis dengan sesi yang sedang
// berlangsung menurut jam nyata (handler staf memakai time.Now).
type proctorEnv struct {
	*takeEnv
	sess  Session
	cards []Card
	real  time.Time
}

func newProctorEnv(t *testing.T) *proctorEnv {
	t.Helper()
	e := &proctorEnv{takeEnv: newTakeEnv(t), real: time.Now()}
	ctx := context.Background()
	_, e.sess, e.cards = e.allKindsExam(t, now.Add(time.Hour))
	if _, err := e.tx.Exec(ctx, `UPDATE exam_sessions SET starts_at = $2, ends_at = $3 WHERE id = $1`, e.sess.ID,
		e.real.Add(-10*time.Minute), e.real.Add(50*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `UPDATE student_credentials SET valid_from = $2, valid_until = $3 WHERE exam_session_id = $1`, e.sess.ID,
		e.real.Add(-40*time.Minute), e.real.Add(50*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *proctorEnv) staffDo(method, path string, u testutil.User, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Test-User", u.ID+"|"+u.Role)
	rec := httptest.NewRecorder()
	e.examEnv.router.ServeHTTP(rec, req)
	return rec
}

func (e *proctorEnv) start(t *testing.T, student, dev string) Attempt {
	t.Helper()
	a, err := StartAttempt(context.Background(), e.tx, participantOf(e.takeEnv, student), StartRequest{SessionID: e.sess.ID, DeviceID: dev, Now: e.real})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (e *proctorEnv) participant(t *testing.T, student string) string {
	t.Helper()
	var id string
	if err := e.tx.QueryRow(context.Background(), `SELECT id FROM exam_session_participants WHERE session_id = $1 AND student_id = $2`,
		e.sess.ID, student).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProctorAccess(t *testing.T) {
	e := newProctorEnv(t)
	ctx := context.Background()
	page := "/ujian/sesi/" + e.sess.ID + "/awasi"
	proctor := testutil.User{ID: e.a.Teacher.ID, Role: "guru"}
	other := testutil.User{ID: e.teacher2, Role: "guru"}

	if rec := e.staffDo(http.MethodGet, page, other, nil); rec.Code != http.StatusNotFound {
		t.Errorf("non-proctor teacher = %d, want 404", rec.Code)
	}
	rec := e.staffDo(http.MethodGet, page, proctor, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Aksi sesi") || !strings.Contains(rec.Body.String(), "Perlu perhatian") {
		t.Fatalf("proctor page = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodGet, page, e.a.Principal, nil); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Aksi sesi") {
		t.Errorf("principal = %d (must be read only)", rec.Code)
	}
	if rec := e.staffDo(http.MethodPost, page+"/aksi", e.a.Principal, url.Values{"aksi": {"jeda"}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal action = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodGet, page, e.a.Admin, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Aksi sesi") {
		t.Errorf("admin = %d", rec.Code)
	}
	// Sekolah lain: 404.
	if rec := e.staffDo(http.MethodGet, "/ujian/sesi/"+e.b.ExamSessionID+"/awasi", e.a.Admin, nil); rec.Code != http.StatusNotFound {
		t.Errorf("other school session = %d", rec.Code)
	}
	// Pengawas di luar jendela (sesi besok): hanya baca.
	e.tx.Exec(ctx, `UPDATE exam_sessions SET starts_at = $2, ends_at = $3 WHERE id = $1`, e.sess.ID, e.real.Add(24*time.Hour), e.real.Add(25*time.Hour))
	rec = e.staffDo(http.MethodGet, page, proctor, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Aksi sesi") || !strings.Contains(rec.Body.String(), "hanya bisa melihat") {
		t.Errorf("proctor outside window = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodPost, page+"/aksi", proctor, url.Values{"aksi": {"jeda"}}); rec.Code != http.StatusForbidden {
		t.Errorf("proctor action outside window = %d", rec.Code)
	}
}

func TestProctorMonitorAndActions(t *testing.T) {
	e := newProctorEnv(t)
	ctx := context.Background()
	students := e.students(t)
	proctor := testutil.User{ID: e.a.Teacher.ID, Role: "guru"}
	page := "/ujian/sesi/" + e.sess.ID + "/awasi"
	a0 := e.start(t, students[0], dev1)
	a1 := e.start(t, students[1], dev2)
	// a0: menjawab 1 soal dan keluar halaman 3 kali; a1: offline.
	evs := []EventInput{}
	for i := 1; i <= 3; i++ {
		evs = append(evs, EventInput{Seq: int64(i), Kind: EventLeave, At: e.real.UnixMilli(), Duration: 4000})
	}
	if _, err := SaveAnswers(ctx, e.tx, e.a.ID, a0.ID, dev1, SaveRequest{Pos: -1, Events: evs,
		Changes: []AnswerChange{{QuestionID: a0.QuestionOrder[0], Value: json.RawMessage(`"x"`), Seq: 1}}}, e.real); err != nil {
		t.Fatal(err)
	}
	e.tx.Exec(ctx, `UPDATE exam_attempts SET last_seen_at = $2 WHERE id = $1`, a1.ID, e.real.Add(-5*time.Minute))

	m, err := LoadMonitor(ctx, e.tx, e.a.ID, e.sess.ID, e.real)
	if err != nil {
		t.Fatal(err)
	}
	if m.Counts[MonWorking] != 1 || m.Counts[MonOffline] != 1 || m.Counts[MonNotStarted] != 1 || m.Counts["perhatian"] != 2 {
		t.Errorf("counts = %v", m.Counts)
	}
	first := m.Rows[0]
	if first.AttemptID != a0.ID || first.Leaves != 3 || first.Answered != 1 || first.Attention[0] != "Keluar 3 kali" || first.LastEvent == 0 {
		t.Errorf("first row = %+v", first)
	}
	if m.Rows[1].AttemptID != a1.ID || m.Rows[1].State != MonOffline {
		t.Errorf("second row = %+v", m.Rows[1])
	}
	// Potongan data untuk pembaruan berkala.
	if rec := e.staffDo(http.MethodGet, page+"/data", proctor, nil); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "<html") ||
		!strings.Contains(rec.Body.String(), `data-event="`) {
		t.Errorf("data = %d", rec.Code)
	}
	var seen *time.Time
	e.tx.QueryRow(ctx, `SELECT proctor_seen_at FROM exam_sessions WHERE id = $1`, e.sess.ID).Scan(&seen)
	if seen == nil {
		t.Error("proctor_seen_at not recorded")
	}

	// Reset login: kode baru tampil dan bisa dipakai masuk dari perangkat mana pun.
	p0 := e.participant(t, students[0])
	rec := e.staffDo(http.MethodPost, page+"/peserta/"+p0, proctor, url.Values{"aksi": {"reset"}, "alasan": {"Lupa kode"}})
	code := regexp.MustCompile(`aria-label="Kode baru">([A-Z0-9]+)<`).FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || code == nil {
		t.Fatalf("reset = %d", rec.Code)
	}
	var nis string
	e.tx.QueryRow(ctx, `SELECT nis FROM students WHERE id = $1`, students[0]).Scan(&nis)
	if _, err := Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: nis, Code: code[1], DeviceID: devProctor3, Now: e.real}); err != nil {
		t.Errorf("login with new code: %v", err)
	}
	// Izin pindah perangkat (satu ketukan dari lembar aksi).
	rec = e.staffDo(http.MethodPost, page+"/peserta/"+p0, proctor, url.Values{"aksi": {"pindah"}})
	var allowed bool
	e.tx.QueryRow(ctx, `SELECT device_move_allowed FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&allowed)
	if rec.Code != http.StatusSeeOther || !allowed {
		t.Errorf("allow move = %d, allowed %v", rec.Code, allowed)
	}
	// Tambah waktu, jeda/lanjut, catatan, kumpul paksa.
	for _, f := range []url.Values{{"aksi": {"waktu"}, "menit": {"10"}}, {"aksi": {"jeda"}}, {"aksi": {"lanjut"}},
		{"aksi": {"catat"}, "catatan": {"Menangis, diberi waktu tenang"}}, {"aksi": {"kumpul"}, "alasan": {"Mencontek"}}} {
		if rec := e.staffDo(http.MethodPost, page+"/peserta/"+p0, proctor, f); rec.Code != http.StatusSeeOther {
			t.Errorf("action %v = %d", f, rec.Code)
		}
	}
	got, _ := GetAttempt(ctx, e.tx, participantOf(e.takeEnv, students[0]), a0.ID)
	if got.Status != AttemptSubmitted || got.SubmitReason != SubmitByProctor {
		t.Errorf("force submit: %s/%s", got.Status, got.SubmitReason)
	}
	// Aksi pada peserta yang sudah selesai: pesan, bukan error.
	if rec := e.staffDo(http.MethodPost, page+"/peserta/"+p0, proctor, url.Values{"aksi": {"jeda"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("pause submitted = %d", rec.Code)
	}
	// Peserta sesi lain tidak bisa disentuh lewat sesi ini.
	if rec := e.staffDo(http.MethodPost, page+"/peserta/"+e.b.ExamSessionID, proctor, url.Values{"aksi": {"reset"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("foreign participant = %d", rec.Code)
	}

	// Token: diwajibkan, berganti tiap 15 menit, token lama diterima 2 menit.
	if rec := e.staffDo(http.MethodPost, page+"/aksi", proctor, url.Values{"aksi": {"token_on"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("token on = %d", rec.Code)
	}
	t1, err := EnsureToken(ctx, e.tx, e.a.ID, e.sess.ID, e.real.Add(time.Minute))
	if err != nil || !t1.Required || len(t1.Token) != tokenLength {
		t.Fatalf("token = %+v, %v", t1, err)
	}
	t2, _ := EnsureToken(ctx, e.tx, e.a.ID, e.sess.ID, t1.NextAt.Add(time.Second))
	if t2.Token == t1.Token || !t2.NextAt.After(t1.NextAt) {
		t.Errorf("token not rotated: %+v → %+v", t1, t2)
	}
	rotated := t1.NextAt.Add(time.Second)
	p2 := participantOf(e.takeEnv, students[2])
	if _, err := StartAttempt(ctx, e.tx, p2, StartRequest{SessionID: e.sess.ID, DeviceID: devProctor3, Token: t1.Token, Now: rotated.Add(3 * time.Minute)}); !errors.Is(err, ErrBadToken) {
		t.Errorf("old token after grace: %v", err)
	}
	if _, err := StartAttempt(ctx, e.tx, p2, StartRequest{SessionID: e.sess.ID, DeviceID: devProctor3, Token: strings.ToLower(t1.Token), Now: rotated.Add(time.Minute)}); err != nil {
		t.Errorf("old token within grace: %v", err)
	}

	// Berita acara belum bisa ditandatangani sebelum sesi selesai.
	if err := SignMinutes(ctx, e.tx, e.owner, e.sess.ID, "", "x", e.real); !errors.Is(err, ErrSessionNotEnded) {
		t.Errorf("sign before end: %v", err)
	}
	// Tutup sesi: yang masih mengerjakan dikumpulkan; tidak bisa mulai lagi.
	if rec := e.staffDo(http.MethodPost, page+"/aksi", proctor, url.Values{"aksi": {"tutup"}, "alasan": {"Selesai lebih awal"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("close = %d", rec.Code)
	}
	var running int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_attempts WHERE session_id = $1 AND status = 'berjalan'`, e.sess.ID).Scan(&running)
	if running != 0 {
		t.Errorf("running after close = %d", running)
	}

	hash, _ := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}).HashPassword("Rahasia-Pengawas-1")
	e.tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, e.a.Teacher.ID, hash)
	if err := SignMinutes(ctx, e.tx, e.owner, e.sess.ID, "Lancar.", "salah", e.real); !errors.Is(err, ErrBadPassword) {
		t.Errorf("wrong password: %v", err)
	}
	rec = e.staffDo(http.MethodPost, "/ujian/sesi/"+e.sess.ID+"/berita-acara", proctor, url.Values{"catatan": {"Lancar."}, "sandi": {"Rahasia-Pengawas-1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign = %d", rec.Code)
	}
	min, err := LoadMinutes(ctx, e.tx, e.a.ID, e.sess.ID, time.UTC)
	if err != nil || min.SignedAt == nil || min.Present != 3 || min.Absent != 0 || min.Notes != "Lancar." {
		t.Fatalf("minutes = %+v, %v", min, err)
	}
	joined := strings.Join(min.Incidents, "\n")
	for _, want := range []string{"Menangis", "Kumpulkan paksa: Mencontek", "Reset login: Lupa kode", "Tutup sesi: Selesai lebih awal",
		"keluar halaman 3 kali", "dikumpulkan oleh pengawas"} {
		if !strings.Contains(joined, want) {
			t.Errorf("minutes lack %q:\n%s", want, joined)
		}
	}
	if rec := e.staffDo(http.MethodGet, "/ujian/sesi/"+e.sess.ID+"/berita-acara.pdf", proctor, nil); rec.Code != http.StatusOK ||
		rec.Header().Get("Content-Type") != "application/pdf" {
		t.Errorf("minutes pdf = %d", rec.Code)
	}
	var audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE user_id = $1 AND action IN ('exam.participant_reset_login', 'participant.reset_device',
		'exam.attempt_submit', 'exam.session_close', 'exam.minutes_sign', 'exam.session_token', 'exam.session_note')`, e.a.Teacher.ID).Scan(&audits)
	if audits < 7 {
		t.Errorf("audits = %d", audits)
	}
}

func TestMyProctorSessions(t *testing.T) {
	e := newProctorEnv(t)
	from, to := DayBounds(e.real, time.UTC)
	ss, err := MyProctorSessions(context.Background(), e.tx, e.a.ID, e.a.Teacher.ID, from.Add(-24*time.Hour), to.Add(24*time.Hour))
	if err != nil || len(ss) != 1 || ss[0].ID != e.sess.ID || ss[0].Participants != 3 {
		t.Fatalf("my sessions = %+v, %v", ss, err)
	}
	cards := MySessionCards(ss, time.UTC, e.real)
	if cards[0].Status != "Berlangsung" {
		t.Errorf("card = %+v", cards[0])
	}
	if rec := e.staffDo(http.MethodGet, "/ujian/sesi-saya", testutil.User{ID: e.a.Teacher.ID, Role: "guru"}, nil); rec.Code != http.StatusOK {
		t.Errorf("my sessions page = %d", rec.Code)
	}
}

// Kriteria T143: query layar pengawas untuk 500 peserta < 50 ms.
func TestProctorMonitorQuery500(t *testing.T) {
	e := newProctorEnv(t)
	ctx := context.Background()
	var variant, exam string
	e.tx.QueryRow(ctx, `SELECT variant_id, x.exam_id FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
		WHERE p.session_id = $1 LIMIT 1`, e.sess.ID).Scan(&variant, &exam)
	if _, err := e.tx.Exec(ctx, `
		WITH st AS (INSERT INTO students (school_id, nis, full_name) SELECT $1, 'BEBAN-' || g, 'Peserta Beban ' || g FROM generate_series(1, 500) g RETURNING id),
		pp AS (INSERT INTO exam_session_participants (school_id, session_id, student_id, variant_id) SELECT $1, $2, id, $3 FROM st RETURNING id),
		qo AS (SELECT array_agg(gen_random_uuid()) AS q FROM generate_series(1, 40))
		INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, variant_id, seed, question_order, device_id, started_at, deadline_at, last_seen_at)
		SELECT $1, $4, $2, pp.id, $3, 1, qo.q, gen_random_uuid(), $5, $5::timestamptz + interval '1 hour', $5 FROM pp, qo`,
		e.a.ID, e.sess.ID, variant, exam, e.real); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO exam_answers (school_id, attempt_id, question_id, value, client_seq)
		SELECT t.school_id, t.id, q, '"a"', 1 FROM exam_attempts t, unnest(t.question_order) q WHERE t.session_id = $1`, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at, duration_ms)
		SELECT t.school_id, t.id, 'keluar', $2, 3000 FROM exam_attempts t, generate_series(1, 3) WHERE t.session_id = $1`, e.sess.ID, e.real); err != nil {
		t.Fatal(err)
	}
	e.tx.Exec(ctx, `ANALYZE exam_attempts; ANALYZE exam_answers; ANALYZE exam_attempt_events; ANALYZE exam_session_participants`)
	best := time.Hour
	for i := 0; i < 5; i++ {
		t0 := time.Now()
		m, err := LoadMonitor(ctx, e.tx, e.a.ID, e.sess.ID, e.real)
		if err != nil || len(m.Rows) != 503 {
			t.Fatalf("monitor = %d rows, %v", len(m.Rows), err)
		}
		best = min(best, time.Since(t0))
	}
	t.Logf("monitor 503 participants: %s", best)
	if best > 50*time.Millisecond {
		t.Errorf("monitor query %s, want < 50ms", best)
	}
}

const devProctor3 = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
