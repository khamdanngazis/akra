package exam

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func (e *examEnv) readiness(t *testing.T, ex Exam, st ExamSettings, at time.Time) Readiness {
	t.Helper()
	ctx := context.Background()
	ex, _ = GetExam(ctx, e.tx, e.a.ID, ex.ID)
	p, err := BuildPlan(ctx, e.tx, ex)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := ListSessions(ctx, e.tx, e.a.ID, ex.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := CheckReadiness(ctx, e.tx, ex, p, ss, st, at)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func level(r Readiness, key string) string {
	it, ok := r.Item(key)
	if !ok {
		return "-"
	}
	return it.Level
}

// Setiap butir cek kesiapan diuji dalam keadaan lolos dan gagal.
func TestReadinessItems(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	ex := e.create(t, e.owner, PurposeQuiz)
	sec := e.firstSection(t, ex)

	// Kosong: soal dan sesi merah.
	r := e.readiness(t, ex, ExamSettings{}, now)
	for k, want := range map[string]string{"questions": ReadyBlock, "sessions": ReadyBlock, "keys": ReadyOK, "math": ReadyOK, "images": ReadyOK} {
		if got := level(r, k); got != want {
			t.Errorf("empty %s = %s, want %s", k, got, want)
		}
	}
	if !r.Blocked {
		t.Error("empty exam not blocked")
	}

	// Rumus rusak dan gambar hilang: merah.
	bad := sampleQuestion(KindMC)
	bad.BodySrc = `<p>Gambar <img data-asset="11111111-2222-3333-4444-555555555555" alt="x"></p>`
	bad.BodyHTML = `<p><span class="math-error">\frac{</span></p>`
	bq, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, bad, now)
	if err != nil {
		t.Fatal(err)
	}
	AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{bq.ID}, now)
	r = e.readiness(t, ex, ExamSettings{}, now)
	if level(r, "math") != ReadyBlock || level(r, "images") != ReadyBlock || level(r, "questions") != ReadyOK {
		t.Errorf("bad question: math=%s images=%s", level(r, "math"), level(r, "images"))
	}
	secs, _ := LoadContent(ctx, e.tx, ex)
	RemoveItem(ctx, e.tx, e.owner, ex.ID, secs[0].Items[0].ID, now)

	// Aturan acak kurang: merah. Aturan dipasang lalu soal bank diarsipkan.
	AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{e.qs[3].ID}, now)
	if err := AddRule(ctx, e.tx, e.owner, ex.ID, RuleForm{SectionID: sec, BankID: e.bank.ID, Difficulty: "mudah", Count: "3"}, now); err != nil {
		t.Fatal(err)
	}
	ArchiveQuestion(ctx, e.tx, e.owner, e.qs[0].ID, now)
	if r = e.readiness(t, ex, ExamSettings{}, now); level(r, "rules") != ReadyBlock {
		t.Errorf("short rule = %s", level(r, "rules"))
	}
	secs, _ = LoadContent(ctx, e.tx, ex)
	for _, it := range secs[0].Items {
		if it.IsRule() {
			RemoveItem(ctx, e.tx, e.owner, ex.ID, it.ID, now)
		}
	}

	// Sesi di ruang tanpa pengawas: merah; dengan pengawas: hijau.
	start := now.Add(time.Hour)
	if _, err := CreateClassSessions(ctx, e.tx, e.owner, ex.ID, []ClassSlot{{ClassID: e.a.ClassID, Start: start, End: start.Add(time.Hour)}}, now); err != nil {
		t.Fatal(err)
	}
	r = e.readiness(t, ex, ExamSettings{}, now)
	if level(r, "proctors") != ReadyOK || level(r, "sessions") != ReadyOK || level(r, "window") != ReadyOK || level(r, "zone") != ReadyOK {
		t.Errorf("with session: proctors=%s sessions=%s window=%s", level(r, "proctors"), level(r, "sessions"), level(r, "window"))
	}
	e.tx.Exec(ctx, `DELETE FROM exam_session_proctors WHERE session_id IN (SELECT id FROM exam_sessions WHERE exam_id = $1)`, ex.ID)
	if r = e.readiness(t, ex, ExamSettings{}, now); level(r, "proctors") != ReadyBlock {
		t.Errorf("no proctor = %s", level(r, "proctors"))
	}
	// Item merah memblokir penjadwalan.
	if err := ScheduleExam(ctx, e.tx, e.owner, ex.ID, now); !errors.Is(err, ErrNotReady) {
		t.Errorf("schedule without proctor: %v", err)
	}
	e.tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by)
		SELECT school_id, id, $2, $2 FROM exam_sessions WHERE exam_id = $1`, ex.ID, e.a.Teacher.ID)

	// Kartu: dalam H-3 dan belum dicetak = peringatan; setelah cetak = hijau.
	r = e.readiness(t, ex, ExamSettings{}, now)
	if level(r, "cards") != ReadyWarn {
		t.Errorf("cards before print = %s", level(r, "cards"))
	}
	if _, err := IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now); err != nil {
		t.Fatal(err)
	}
	if r = e.readiness(t, ex, ExamSettings{}, now); level(r, "cards") != ReadyOK {
		t.Errorf("cards after print = %s", level(r, "cards"))
	}
	// Lebih dari H-3: hijau dengan petunjuk tanggal.
	if r = e.readiness(t, ex, ExamSettings{}, now.Add(-5*24*time.Hour)); level(r, "cards") != ReadyOK {
		t.Errorf("cards far = %s", level(r, "cards"))
	}

	// Durasi terlalu pendek untuk soal: peringatan; jendela sesi kurang: merah.
	e.tx.Exec(ctx, `UPDATE exams SET duration_minutes = 5 WHERE id = $1`, ex.ID)
	e.tx.Exec(ctx, `INSERT INTO exam_items (school_id, exam_id, section_id, position, kind, question_id) VALUES ($1, $2, $3, 9, 'soal', $4)`,
		e.a.ID, ex.ID, sec, e.qs[5].ID)
	e.tx.Exec(ctx, `UPDATE exams SET duration_minutes = 120 WHERE id = $1`, ex.ID)
	if r = e.readiness(t, ex, ExamSettings{}, now); level(r, "window") != ReadyBlock {
		t.Errorf("short window = %s", level(r, "window"))
	}
	e.tx.Exec(ctx, `UPDATE exams SET duration_minutes = 5 WHERE id = $1`, ex.ID)
	e.tx.Exec(ctx, `UPDATE exams SET duration_minutes = 6 WHERE id = $1`, ex.ID)

	// Siswa belum pernah latihan: peringatan; setelah kelas membuka latihan
	// sebelum ujian: hijau.
	if r = e.readiness(t, ex, ExamSettings{}, now); level(r, "practice") != ReadyWarn {
		t.Errorf("practice before = %s", level(r, "practice"))
	}
	e.tx.Exec(ctx, `UPDATE exams SET archived_at = now() WHERE school_id = $1 AND builtin`, e.a.ID)
	pr, err := EnsurePractice(ctx, e.tx, e.owner, mr(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPractice(ctx, e.tx, e.owner, pr, e.a.ClassID, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if r = e.readiness(t, ex, ExamSettings{}, now); level(r, "practice") != ReadyOK {
		t.Errorf("practice after = %s", level(r, "practice"))
	}

	// Soal draf: peringatan; PTS/PAS dengan telaah aktif: merah.
	draft := sampleQuestion(KindMC)
	draft.Status = StatusDraft
	dq, _ := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, draft, now)
	AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{dq.ID}, now)
	if r = e.readiness(t, ex, ExamSettings{}, now); level(r, "status") != ReadyWarn {
		t.Errorf("draft = %s", level(r, "status"))
	}
	e.tx.Exec(ctx, `UPDATE exams SET purpose = 'pas' WHERE id = $1`, ex.ID)
	if r = e.readiness(t, ex, ExamSettings{Review: true}, now); level(r, "status") != ReadyBlock {
		t.Errorf("pas with review = %s", level(r, "status"))
	}
}

// Paket latihan bawaan: dibuat sekali dengan semua jenis soal; dibuka
// untuk satu kelas dalam ≤ 3 klik (halaman → buka → cetak kartu).
func TestPracticePackage(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	// Mulai dari sekolah tanpa paket latihan (fixture punya satu).
	e.tx.Exec(ctx, `UPDATE exams SET archived_at = now() WHERE school_id = $1 AND builtin`, e.a.ID)
	// Klik 1: halaman latihan (paket dibuat otomatis).
	rec := e.getAs("/ujian/latihan", e.a.Teacher.ID, "guru")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), PracticeName) {
		t.Fatalf("practice page = %d", rec.Code)
	}
	pr, ok, err := GetPractice(ctx, e.tx, e.a.ID)
	if err != nil || !ok || !pr.Builtin || pr.Purpose != PurposePractice || pr.ResultsMode != ResultsExplanation {
		t.Fatalf("practice = %+v %v", pr, err)
	}
	again, err := EnsurePractice(ctx, e.tx, e.owner, mr(t), now)
	if err != nil || again.ID != pr.ID {
		t.Errorf("ensure twice = %s, %v", again.ID, err)
	}
	p, _ := BuildPlan(ctx, e.tx, pr)
	if c := p.Composition(); c.Count != 6 || len(c.ByKind) != 6 {
		t.Errorf("practice composition = %+v", c.ByKind)
	}
	// Guru lain tidak bisa mengubah paket bawaan.
	if err := AddSection(ctx, e.tx, e.owner, pr.ID, SectionForm{Title: "x"}, now); !errors.Is(err, ErrNotEditable) {
		t.Errorf("teacher edits practice: %v", err)
	}
	// Kelas yang tidak diajar: ditolak.
	var other string
	e.tx.QueryRow(ctx, `SELECT id FROM classes WHERE school_id = $1 AND name = '7Z'`, e.a.ID).Scan(&other)
	if _, err := OpenPractice(ctx, e.tx, e.owner, pr, other, now); !errors.Is(err, ErrNotYourClass) {
		t.Errorf("open other class: %v", err)
	}
	// Klik 2: buka untuk kelas.
	rec = e.post("/ujian/latihan", e.a.Teacher.ID, "guru", url.Values{"class_id": {e.a.ClassID}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("open = %d", rec.Code)
	}
	ss, _ := ListSessions(ctx, e.tx, e.a.ID, pr.ID)
	if len(ss) != 1 || ss[0].Participants == 0 || ss[0].Location != LocationRemote {
		t.Fatalf("practice sessions = %+v", ss)
	}
	// Klik 3: cetak kartu.
	rec = e.post("/ujian/latihan/"+ss[0].ID+"/kartu", e.a.Teacher.ID, "guru", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "%PDF") {
		t.Errorf("practice cards = %d", rec.Code)
	}
	// Kepala sekolah hanya melihat.
	if rec := e.post("/ujian/latihan", e.a.Principal.ID, "kepala_sekolah", url.Values{"class_id": {e.a.ClassID}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal open = %d", rec.Code)
	}
}

// Cek perangkat publik: penilaian browser dari server dan contoh rumus.
func TestDeviceCheckPage(t *testing.T) {
	e := newPEnv(t)
	e.h.WithMath(mr(t))
	req := httptest.NewRequest(http.MethodGet, "/u/cek", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; U; Android 8.1.0; Redmi 5A) AppleWebKit/537.36 Version/4.0 Chrome/57.0 UCBrowser/13.4.0.1306 Mobile Safari/537.36")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `data-server-level="fail"`) || !strings.Contains(body, "UC Browser") || !strings.Contains(body, "katex") {
		t.Errorf("device check = %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/u/cek/contoh.jpg", nil); rec.Code != http.StatusOK || rec.Body.Len() < 50_000 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("sample = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := e.do(http.MethodGet, "/u/cek/ping", nil); rec.Code != http.StatusOK {
		t.Errorf("ping = %d", rec.Code)
	}
}
