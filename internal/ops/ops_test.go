package ops

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/exam"
	"akra/internal/platform/database"
	"akra/internal/platform/metrics"
	"akra/internal/platform/render"
	"akra/internal/testutil"
	"akra/web"
)

type env struct {
	tx   pgx.Tx
	a, b *testutil.School
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	e := &env{tx: tx, now: time.Now().Truncate(time.Second)}
	e.a, e.b = testutil.SeedTwoSchools(t, tx, "$argon2id$v=19$m=1024,t=1,p=1$c2FsdA$aGFzaA")
	// Sesi fixture di masa lalu: tidak menahan deploy.
	if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET starts_at = $1::timestamptz - interval '3 days', ends_at = $1::timestamptz - interval '2 days'
		WHERE id = ANY ($2::uuid[])`, e.now, []string{e.a.ExamSessionID, e.b.ExamSessionID}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) setSession(t *testing.T, id string, start, end time.Time, location string) {
	t.Helper()
	if _, err := e.tx.Exec(context.Background(), `UPDATE exam_sessions SET starts_at = $2, ends_at = $3, location = $4 WHERE id = $1`,
		id, start, end, location); err != nil {
		t.Fatal(err)
	}
}

// runningAttempt membuat satu attempt berjalan di sesi fixture sekolah s.
func (e *env) runningAttempt(t *testing.T, s *testutil.School, deadline time.Time) string {
	t.Helper()
	var id string
	ctx := context.Background()
	if _, err := e.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, variant_id)
		SELECT $1, $2, $3, (SELECT id FROM exam_variants WHERE exam_id = $4 LIMIT 1) ON CONFLICT DO NOTHING`,
		s.ID, s.ExamSessionID, s.StudentIDs[0], s.ExamID); err != nil {
		t.Fatal(err)
	}
	err := e.tx.QueryRow(ctx, `
		INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, variant_id, seed, question_order, device_id, started_at, deadline_at)
		SELECT $1, $4, $2, p.id, coalesce(p.variant_id, (SELECT id FROM exam_variants WHERE exam_id = $4 LIMIT 1)), 1, '{}', gen_random_uuid(),
			$5::timestamptz - interval '30 minutes', $6
		FROM exam_session_participants p WHERE p.session_id = $2 AND p.student_id = $3 RETURNING id`,
		s.ID, s.ExamSessionID, s.StudentIDs[0], s.ExamID, e.now, deadline).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestActiveExamsAndGuard(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	base, err := exam.LoadActiveExams(ctx, e.tx, e.now, exam.DeployLead)
	if err != nil {
		t.Fatal(err)
	}
	if base.Blocking() {
		t.Skipf("test database has other active exams: %+v", base)
	}
	if res, err := CheckDeploy(ctx, e.tx, e.now, ""); err != nil || !res.Allowed {
		t.Fatalf("no exams: %+v, %v", res, err)
	}
	// Ulangan jarak jauh berhari-hari tanpa peserta mengerjakan: tidak menahan.
	e.setSession(t, e.a.ExamSessionID, e.now.Add(-24*time.Hour), e.now.Add(48*time.Hour), "jarak_jauh")
	if a, _ := exam.LoadActiveExams(ctx, e.tx, e.now, exam.DeployLead); a.Blocking() {
		t.Errorf("idle remote window blocks deploy: %+v", a)
	}
	// Ada yang mengerjakan: menahan.
	e.runningAttempt(t, e.a, e.now.Add(20*time.Minute))
	a, _ := exam.LoadActiveExams(ctx, e.tx, e.now, exam.DeployLead)
	if a.Active != 1 || a.RunningAttempts != 1 || !a.Blocking() {
		t.Errorf("running remote attempt = %+v", a)
	}
	// Sesi di ruang mulai 20 menit lagi: menahan sebagai "segera".
	e.setSession(t, e.b.ExamSessionID, e.now.Add(20*time.Minute), e.now.Add(110*time.Minute), "di_ruang")
	a, _ = exam.LoadActiveExams(ctx, e.tx, e.now, exam.DeployLead)
	if a.Upcoming != 1 || a.Active != 1 {
		t.Errorf("upcoming = %+v", a)
	}
	res, err := CheckDeploy(ctx, e.tx, e.now, "")
	if err != nil || res.Allowed || !strings.Contains(res.Reason, "1 sesi mulai dalam 30 menit") {
		t.Fatalf("guard with active exams = %+v, %v", res, err)
	}
	// Deploy darurat: berlaku hanya ≤ 6 jam ke depan dan belum lewat.
	for until, want := range map[string]bool{
		e.now.Add(time.Hour).Format(time.RFC3339):     true,
		e.now.Add(-time.Minute).Format(time.RFC3339):  false,
		e.now.Add(7 * time.Hour).Format(time.RFC3339): false,
		"besok": false,
	} {
		res, err := CheckDeploy(ctx, e.tx, e.now, until)
		if err != nil || res.Allowed != want || res.Emergency != want {
			t.Errorf("emergency %q = %+v, %v; want allowed %v", until, res, err, want)
		}
	}
}

// fakeNotifier mencatat pesan alert.
type fakeNotifier struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeNotifier) Notify(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, text)
	return nil
}

func (f *fakeNotifier) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.msgs...)
}

func TestMonitorSaveErrorsAlertOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clock := e.now
	notes := &fakeNotifier{}
	// Dua replika: metrik masing-masing, kejadian bersama di database.
	var reps []*Monitor
	for i := 0; i < 2; i++ {
		reg := metrics.New()
		reg.SetClock(func() time.Time { return clock })
		reps = append(reps, &Monitor{DB: e.tx, Metrics: reg, Notifier: notes, Env: "staging", BaseURL: "https://x", Logger: logger})
	}
	// Simulasi error simpan 5% di kedua replika.
	for _, m := range reps {
		for i := 0; i < 95; i++ {
			m.Metrics.Observe(EndpointSave, 30*time.Millisecond, false)
		}
		for i := 0; i < 5; i++ {
			m.Metrics.Observe(EndpointSave, 30*time.Millisecond, true)
		}
	}
	for _, m := range reps {
		if err := m.Tick(ctx, clock); err != nil {
			t.Fatal(err)
		}
	}
	msgs := notes.all()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "Simpan jawaban gagal > 1%") || !strings.Contains(msgs[0], "5 dari 100") {
		t.Fatalf("alerts = %q, want exactly one save error alert", msgs)
	}
	st, _ := LoadStatus(ctx, e.tx, clock)
	if st.State != StatusDisrupted {
		t.Errorf("status = %s, want gangguan", st.State)
	}
	// Kondisi baik tetapi belum 5 menit: belum pulih.
	clock = clock.Add(3 * time.Minute)
	for _, m := range reps {
		m.Tick(ctx, clock)
	}
	if len(notes.all()) != 1 {
		t.Errorf("resolved too early: %q", notes.all())
	}
	clock = clock.Add(3 * time.Minute)
	for _, m := range reps {
		m.Tick(ctx, clock)
	}
	msgs = notes.all()
	if len(msgs) != 2 || !strings.HasPrefix(msgs[1], "✅ Pulih: Simpan jawaban gagal") {
		t.Fatalf("recovery = %q", msgs)
	}
	if st, _ := LoadStatus(ctx, e.tx, clock); st.State != StatusNormal {
		t.Errorf("status after recovery = %s", st.State)
	}
	// Sampel sedikit (< 30) tidak dinilai.
	reps[0].Metrics.Observe(EndpointSave, time.Millisecond, true)
	if checks, _ := reps[0].Evaluate(ctx, clock); checks[0].Bad {
		t.Error("single failure triggered alert")
	}
}

func TestMonitorSlowAutoSubmitAndPool(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	reg := metrics.New()
	reg.SetClock(func() time.Time { return e.now })
	for i := 0; i < 40; i++ {
		reg.Observe(EndpointSave, 1500*time.Millisecond, false)
	}
	pool := PoolStat{Acquired: 9, Max: 10}
	m := &Monitor{DB: e.tx, Metrics: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Pool: func() PoolStat { return pool }}
	// Attempt yang tenggatnya + tenggang lewat 4 menit lalu.
	e.runningAttempt(t, e.a, e.now.Add(-exam.SaveGrace-4*time.Minute))
	bad := func() map[string]bool {
		checks, err := m.Evaluate(ctx, e.now)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, c := range checks {
			out[c.Rule] = c.Bad
		}
		return out
	}
	got := bad()
	if !got[RuleSaveSlow] || !got[RuleAutoSubmit] || got[RuleDBPool] {
		t.Errorf("first evaluation = %v (pool needs two in a row)", got)
	}
	if got = bad(); !got[RuleDBPool] {
		t.Errorf("second evaluation pool = %v", got)
	}
	pool = PoolStat{Acquired: 2, Max: 10}
	if got = bad(); got[RuleDBPool] {
		t.Errorf("pool recovered = %v", got)
	}
}

func TestTelegramNotifier(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	n := Telegram{Token: "123:abc", ChatID: "-100", Endpoint: srv.URL}
	if err := n.Notify(context.Background(), "🔴 uji"); err != nil {
		t.Fatal(err)
	}
	if path != "/bot123:abc/sendMessage" || got["chat_id"] != "-100" || got["text"] != "🔴 uji" {
		t.Errorf("telegram request = %s %v", path, got)
	}
	bad := Telegram{Token: "123:abc", ChatID: "-100", Endpoint: "http://127.0.0.1:1"}
	if err := bad.Notify(context.Background(), "x"); err == nil || strings.Contains(err.Error(), "123:abc") {
		t.Errorf("unreachable telegram error = %v (must not leak token)", err)
	}
}

func TestHandlers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	notes := &fakeNotifier{}
	const token = "token-uji-tim-akra-panjang"
	h := &Handler{DB: e.tx, Metrics: metrics.New(), Render: rd, Logger: logger, Notifier: notes, Token: token, Env: "staging"}
	r := chi.NewRouter()
	h.Routes(r)
	do := func(method, path, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		switch {
		case strings.HasPrefix(auth, "basic:"):
			req.SetBasicAuth("ops", strings.TrimPrefix(auth, "basic:"))
		case auth != "":
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("GET", "/status", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Semua layanan berjalan normal") {
		t.Fatalf("status normal = %d", rec.Code)
	}
	if _, _, err := OpenIncident(ctx, e.tx, RuleSaveErrors, "detail-internal-rahasia", e.now); err != nil {
		t.Fatal(err)
	}
	if _, err := AddMaintenance(ctx, e.tx, e.now.Add(2*time.Hour), e.now.Add(3*time.Hour), "Pembaruan server"); err != nil {
		t.Fatal(err)
	}
	body := do("GET", "/status", "").Body.String()
	if !strings.Contains(body, "Sedang ada gangguan") || !strings.Contains(body, "Pembaruan server") || strings.Contains(body, "detail-internal-rahasia") {
		t.Errorf("status page with incident missing state or leaks detail")
	}
	for _, p := range []string{"/metrics", "/internal/", "/internal/active-exams"} {
		if rec := do("GET", p, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token = %d", p, rec.Code)
		}
		if rec := do("GET", p, "salah"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s wrong token = %d", p, rec.Code)
		}
		if rec := do("GET", p, "basic:"+token); rec.Code != http.StatusOK {
			t.Errorf("%s basic auth = %d", p, rec.Code)
		}
	}
	rec := do("GET", "/internal/active-exams", token)
	var ae map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ae); err != nil || ae["deploy_ok"] == nil {
		t.Errorf("active-exams = %s", rec.Body.String())
	}
	if body := do("GET", "/internal/", token).Body.String(); !strings.Contains(body, "Jendela deploy") || !strings.Contains(body, "Simpan jawaban gagal") {
		t.Errorf("dashboard missing sections")
	}
	// Uji alert: hanya Bearer, bukan Basic auth browser.
	if rec := do("POST", "/internal/alert-test", "basic:"+token); rec.Code != http.StatusUnauthorized {
		t.Errorf("alert test with basic auth = %d", rec.Code)
	}
	if rec := do("POST", "/internal/alert-test", token); rec.Code != 200 || len(notes.all()) != 1 {
		t.Errorf("alert test = %d, %d messages", rec.Code, len(notes.all()))
	}
	// Tanpa token tim: route internal tidak ada.
	h.Token = ""
	r2 := chi.NewRouter()
	h.Routes(r2)
	rec = httptest.NewRecorder()
	r2.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("metrics without OPS_TOKEN = %d", rec.Code)
	}
}
