package exam

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/web"
)

// takeEnv adalah examEnv dengan router peserta dan jam yang bisa diatur.
type takeEnv struct {
	*examEnv
	router  http.Handler
	clock   time.Time
	jar     map[string]map[string]*http.Cookie // per "perangkat"
	extra   []string
	queries *countingDB    // query handler peserta
	store2  *countingStore // akses penyimpanan sesi peserta
}

// countingDB menghitung query yang dijalankan handler.
type countingDB struct {
	database.DB
	n int
}

func (c *countingDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.n++
	return c.DB.Exec(ctx, sql, args...)
}

func (c *countingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.n++
	return c.DB.Query(ctx, sql, args...)
}

func (c *countingDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.n++
	return c.DB.QueryRow(ctx, sql, args...)
}

func (c *countingDB) Begin(ctx context.Context) (pgx.Tx, error) {
	c.n++
	return c.DB.Begin(ctx)
}

// countingStore menghitung akses ke penyimpanan sesi.
type countingStore struct {
	scs.Store
	n int
}

func (c *countingStore) Find(token string) ([]byte, bool, error) {
	c.n++
	return c.Store.Find(token)
}

func (c *countingStore) Commit(token string, b []byte, expiry time.Time) error {
	c.n++
	return c.Store.Commit(token, b, expiry)
}

func (c *countingStore) Delete(token string) error {
	c.n++
	return c.Store.Delete(token)
}

func newTakeEnv(t *testing.T) *takeEnv {
	t.Helper()
	e := &takeEnv{examEnv: newExamEnv(t), jar: map[string]map[string]*http.Cookie{}}
	logger := slog.New(slog.NewTextHandler(debugOut(), nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	e.store2 = &countingStore{Store: memstore.NewWithCleanupInterval(0)}
	e.queries = &countingDB{DB: e.tx}
	sm := auth.NewParticipantSessionManager(e.store2, false, logger)
	h := NewParticipantHandler(e.queries, sm, rd, logger, false).WithStorage(e.store)
	h.now = func() time.Time { return e.clock }
	// Seperti cmd/server: route bertoken di luar sesi.
	r := chi.NewRouter()
	h.TokenRoutes(r)
	r.Group(func(r chi.Router) {
		r.Use(sm.LoadAndSave)
		h.Routes(r)
	})
	e.router = r
	return e
}

// do mengirim request dari perangkat dev (cookie terpisah per perangkat).
func (e *takeEnv) do(dev, method, path, ctype, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.RemoteAddr = "192.0.2.10:1234"
	jar := e.jar[dev]
	if jar == nil {
		jar = map[string]*http.Cookie{}
		e.jar[dev] = jar
	}
	for _, c := range jar {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(jar, c.Name)
		} else {
			jar[c.Name] = c
		}
	}
	return rec
}

// saveAs mengirim simpan jawaban dengan token attempt, tanpa cookie.
func (e *takeEnv) saveAs(path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(AttemptTokenHeader, token)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *takeEnv) form(dev, path string, v url.Values) *httptest.ResponseRecorder {
	return e.do(dev, http.MethodPost, path, "application/x-www-form-urlencoded", v.Encode())
}

// students mengembalikan siswa kelas ClassID: StudentIDs[0] ditambah dua
// siswa baru.
func (e *takeEnv) students(t *testing.T) []string {
	t.Helper()
	if len(e.extra) > 0 {
		return e.extra
	}
	ctx := context.Background()
	e.extra = []string{e.a.StudentIDs[0]}
	for i := 0; i < 2; i++ {
		var id string
		if err := e.tx.QueryRow(ctx, `WITH s AS (INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id),
			en AS (INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
			       SELECT $1, s.id, $4, $5, '2020-01-01' FROM s)
			SELECT id FROM s`, e.a.ID, "TK-"+itoa(i), "Peserta Tambahan "+itoa(i), e.a.ClassID, e.a.YearID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		e.extra = append(e.extra, id)
	}
	return e.extra
}

// allKindsExam membuat ulangan berisi satu soal setiap jenis dengan sesi
// mulai start, lalu mencetak kartu. Mengembalikan ujian, sesi, dan kartu.
func (e *takeEnv) allKindsExam(t *testing.T, start time.Time) (Exam, Session, []Card) {
	t.Helper()
	ctx := context.Background()
	e.students(t)
	var ids []string
	for _, k := range Kinds {
		q, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, sampleQuestion(k), now)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		ids = append(ids, q.ID)
	}
	ex := e.scheduledExam(t, start, ids...)
	ss, err := ListSessions(ctx, e.tx, e.a.ID, ex.ID)
	if err != nil || len(ss) != 1 {
		t.Fatalf("sessions = %d, %v", len(ss), err)
	}
	cards, err := IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now)
	if err != nil {
		t.Fatal(err)
	}
	return ex, ss[0], cards
}

func (e *takeEnv) login(t *testing.T, dev string, c Card) {
	t.Helper()
	rec := e.form(dev, "/u/"+e.a.Code, url.Values{"identifier": {c.Identifier}, "code": {c.Code}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login %s = %d\n%s", c.Identifier, rec.Code, rec.Body.String())
	}
}

var takeOrderRe = regexp.MustCompile(`data-q="([0-9a-f-]{36})"`)

func takeOrder(body string) []string {
	var out []string
	for _, m := range takeOrderRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func pageState(t *testing.T, body string) map[string]any {
	t.Helper()
	i := strings.Index(body, `id="take-state">`)
	if i < 0 {
		t.Fatal("take state missing")
	}
	rest := body[i+len(`id="take-state">`):]
	var st map[string]any
	if err := json.Unmarshal([]byte(rest[:strings.Index(rest, "</script>")]), &st); err != nil {
		t.Fatalf("take state: %v", err)
	}
	return st
}

func TestTakeFlow(t *testing.T) {
	e := newTakeEnv(t)
	start := now.Add(time.Hour)
	ex, sess, cards := e.allKindsExam(t, start)
	e.clock = start.Add(time.Minute)
	e.login(t, "hp1", cards[0])

	home := e.do("hp1", http.MethodGet, "/u/beranda", "", "").Body.String()
	if !strings.Contains(home, ex.Name) || !strings.Contains(home, `href="/u/ujian/`+sess.ID+`"`) || !strings.Contains(home, "Kerjakan") {
		t.Fatalf("home lacks exam:\n%s", home)
	}
	conf := e.do("hp1", http.MethodGet, "/u/ujian/"+sess.ID, "", "")
	if conf.Code != http.StatusOK || !strings.Contains(conf.Body.String(), "Mulai mengerjakan") ||
		!strings.Contains(conf.Body.String(), "Tes bunyi") || !strings.Contains(conf.Body.String(), "<dd>6</dd>") {
		t.Fatalf("confirm = %d\n%s", conf.Code, conf.Body.String())
	}
	rec := e.form("hp1", "/u/ujian/"+sess.ID+"/mulai", url.Values{})
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/u/a/") {
		t.Fatalf("start = %d %s\n%s", rec.Code, loc, rec.Body.String())
	}
	attempt := strings.TrimPrefix(loc, "/u/a/")

	page := e.do("hp1", http.MethodGet, loc, "", "")
	body := page.Body.String()
	if page.Code != http.StatusOK || page.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("take = %d %s", page.Code, page.Header().Get("Cache-Control"))
	}
	// Tidak ada kunci, pembahasan, rubrik, atau jawaban isian di halaman.
	for _, secret := range []string{"RAHASIA-PEMBAHASAN", "RAHASIA-12", "RAHASIA-RUBRIK", `"correct"`, `"truth"`, `"pairs"`, `"accepted"`, "rubric", "explanation", "answer_key"} {
		if strings.Contains(body, secret) {
			i := strings.Index(body, secret)
			t.Errorf("take page leaks %q: %s", secret, body[max(0, i-300):i+50])
		}
	}
	order := takeOrder(body)
	if len(order) != len(Kinds) {
		t.Fatalf("questions on page = %d", len(order))
	}
	for _, k := range []string{`inputmode="decimal"`, `data-kind="menjodohkan"`, `data-kind="esai"`, `data-left="l1"`, `value="B"`} {
		if !strings.Contains(body, k) {
			t.Errorf("take page lacks %s", k)
		}
	}

	// Simpan: jawaban PG + ragu, posisi soal 2.
	var mc string
	for _, id := range order {
		var kind string
		e.tx.QueryRow(context.Background(), `SELECT kind FROM exam_questions WHERE id = $1`, id).Scan(&kind)
		if kind == "pg" {
			mc = id
		}
	}
	token, _ := pageState(t, body)["token"].(string)
	if token == "" {
		t.Fatal("take state lacks attempt token")
	}
	save := func(_, body string) *httptest.ResponseRecorder {
		return e.saveAs(loc+"/simpan", token, body)
	}
	// Simpan bertoken: satu query, tanpa sesi peserta, tanpa cookie.
	q0, s0 := e.queries.n, e.store2.n
	rec = save("hp1", `{"changes":[{"q":"`+mc+`","v":"b","f":true,"s":10}],"pos":2}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) || !strings.Contains(rec.Body.String(), `"seq":10`) {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	if q, sn := e.queries.n-q0, e.store2.n-s0; q != 1 || sn != 0 {
		t.Errorf("save used %d queries and %d session store calls, want 1 and 0", q, sn)
	}
	if len(rec.Result().Cookies()) != 0 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("save response headers = %v", rec.Header())
	}
	// Tanpa token, token rusak, atau token attempt lain: 401.
	for name, tok := range map[string]string{"none": "", "garbage": "abc.def", "tampered": token[:10] + "A" + token[11:]} {
		if tok == token {
			continue
		}
		if rec := e.saveAs(loc+"/simpan", tok, `{"changes":[]}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("save with %s token = %d", name, rec.Code)
		}
	}
	// Request lama (seq lebih kecil) tidak menimpa.
	if rec := save("hp1", `{"changes":[{"q":"`+mc+`","v":"c","f":false,"s":5}]}`); rec.Code != http.StatusOK {
		t.Fatalf("stale save = %d", rec.Code)
	}
	// Soal yang bukan bagian attempt diabaikan, JSON rusak ditolak.
	save("hp1", `{"changes":[{"q":"`+e.qs[0].ID+`","v":"a","s":11}]}`)
	if rec := save("hp1", `{"changes":[{"q":"`+mc+`","v":nope,"s":12}]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json = %d", rec.Code)
	}

	// Muat ulang: urutan, jawaban, ragu, dan posisi tetap.
	body2 := e.do("hp1", http.MethodGet, loc, "", "").Body.String()
	if strings.Join(takeOrder(body2), ",") != strings.Join(order, ",") {
		t.Error("question order changed on reload")
	}
	st := pageState(t, body2)
	ans := st["answers"].(map[string]any)
	a, ok := ans[mc].(map[string]any)
	if !ok || a["v"] != "b" || a["f"] != true || len(ans) != 1 {
		t.Errorf("answers after reload = %v", ans)
	}
	if st["pos"].(float64) != 2 || st["sound"] != true {
		t.Errorf("state = %v", st)
	}

	// Perangkat lain: ditolak, juga simpan.
	if rec := e.form("hp1b", "/u/"+e.a.Code, url.Values{"identifier": {cards[0].Identifier}, "code": {cards[0].Code}}); rec.Code != http.StatusConflict {
		t.Errorf("login from second device = %d", rec.Code)
	}
	if rec := e.do("hp1b", http.MethodGet, loc, "", ""); rec.Code == http.StatusOK {
		t.Errorf("other device opened attempt: %d", rec.Code)
	}

	// Peserta lain tidak bisa membuka atau mengisi attempt ini.
	e.login(t, "hp2", cards[1])
	if rec := e.do("hp2", http.MethodGet, loc, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("other participant take = %d", rec.Code)
	}
	// Peserta lain memakai token attempt-nya sendiri untuk attempt ini: 401.
	rec2 := e.form("hp2", "/u/ujian/"+sess.ID+"/mulai", url.Values{})
	other := pageState(t, e.do("hp2", http.MethodGet, rec2.Header().Get("Location"), "", "").Body.String())["token"].(string)
	if rec := e.saveAs(loc+"/simpan", other, `{"changes":[{"q":"`+mc+`","v":"a","s":99}]}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("other participant save = %d", rec.Code)
	}

	// Kumpul: selesai, halaman soal dan gambar tidak bisa dibuka lagi.
	rec = e.form("hp1", loc+"/kumpul", url.Values{"alasan": {"peserta"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != loc+"/selesai" {
		t.Fatalf("submit = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	done := e.do("hp1", http.MethodGet, loc+"/selesai", "", "").Body.String()
	if !strings.Contains(done, "Jawaban sudah diterima") || !strings.Contains(done, "09.01 WIB") {
		t.Errorf("done page:\n%s", done)
	}
	if rec := e.do("hp1", http.MethodGet, loc, "", ""); rec.Code != http.StatusSeeOther {
		t.Errorf("take after submit = %d", rec.Code)
	}
	if rec := save("hp1", `{"changes":[{"q":"`+mc+`","v":"a","s":100}]}`); rec.Code != http.StatusConflict {
		t.Errorf("save after submit = %d", rec.Code)
	}
	if rec := e.do("hp1", http.MethodGet, loc+"/gambar/"+e.qs[0].ID, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("image after submit = %d", rec.Code)
	}
	// Kesempatan habis: konfirmasi langsung ke bukti kumpul.
	if rec := e.do("hp1", http.MethodGet, "/u/ujian/"+sess.ID, "", ""); rec.Header().Get("Location") != loc+"/selesai" {
		t.Errorf("confirm after submit = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var answered, reason string
	e.tx.QueryRow(context.Background(), `SELECT (SELECT value::text FROM exam_answers WHERE attempt_id = t.id), t.submit_reason
		FROM exam_attempts t WHERE t.id = $1`, attempt).Scan(&answered, &reason)
	if answered != `"b"` || reason != "peserta" {
		t.Errorf("stored answer %s, reason %s", answered, reason)
	}
}

func TestStartAttemptRules(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	start := now.Add(time.Hour)
	ex, sess, _ := e.allKindsExam(t, start)
	p := auth.Participant{Kind: auth.RoleSiswa, SchoolID: e.a.ID, StudentID: e.a.StudentIDs[0], Name: "Uji"}
	req := StartRequest{SessionID: sess.ID, DeviceID: dev1, Now: start.Add(-time.Minute)}
	if _, err := StartAttempt(ctx, e.tx, p, req); !errors.Is(err, ErrNotOpen) {
		t.Errorf("before start: %v", err)
	}
	req.Now = start.Add(2 * time.Hour)
	if _, err := StartAttempt(ctx, e.tx, p, req); !errors.Is(err, ErrSessionOver) {
		t.Errorf("after end: %v", err)
	}
	// Token wajib.
	e.tx.Exec(ctx, `UPDATE exam_sessions SET token_required = true, token = 'AB12' WHERE id = $1`, sess.ID)
	req.Now = start.Add(50 * time.Minute)
	if _, err := StartAttempt(ctx, e.tx, p, req); !errors.Is(err, ErrBadToken) {
		t.Errorf("no token: %v", err)
	}
	req.Token = "ab 12"
	a, err := StartAttempt(ctx, e.tx, p, req)
	if err != nil {
		t.Fatal(err)
	}
	// Tenggat dipotong akhir sesi (40 menit durasi, sisa sesi 10 menit).
	if !a.DeadlineAt.Equal(start.Add(time.Hour)) || a.No != 1 || len(a.QuestionOrder) != len(Kinds) {
		t.Errorf("attempt = %+v", a)
	}
	// Mulai lagi: attempt yang sama; dari perangkat lain ditolak.
	if again, err := StartAttempt(ctx, e.tx, p, req); err != nil || again.ID != a.ID {
		t.Errorf("restart = %v, %v", again.ID, err)
	}
	req.DeviceID = dev2
	if _, err := StartAttempt(ctx, e.tx, p, req); !errors.Is(err, ErrOtherDevice) {
		t.Errorf("other device: %v", err)
	}
	// Ujian terkunci saat attempt pertama.
	if got, _ := GetExam(ctx, e.tx, e.a.ID, ex.ID); !got.Locked() {
		t.Error("exam not locked on first start")
	}
	// Simpan setelah tenggat + tenggang ditolak; sebelum itu diterima.
	ch := []AnswerChange{{QuestionID: a.QuestionOrder[0], Value: json.RawMessage(`"x"`), Seq: 1}}
	if _, err := SaveAnswers(ctx, e.tx, e.a.ID, a.ID, dev1, ch, -1, a.DeadlineAt.Add(time.Minute)); err != nil {
		t.Errorf("save within grace: %v", err)
	}
	ch[0].Seq = 2
	if _, err := SaveAnswers(ctx, e.tx, e.a.ID, a.ID, dev1, ch, -1, a.DeadlineAt.Add(SaveGrace+time.Second)); !errors.Is(err, ErrAttemptClosed) {
		t.Errorf("save after grace: %v", err)
	}
	if _, err := SaveAnswers(ctx, e.tx, e.a.ID, a.ID, dev2, ch, -1, a.DeadlineAt); !errors.Is(err, ErrAttemptClosed) {
		t.Errorf("save from other device: %v", err)
	}
	// Kumpul lalu habis kesempatan.
	if _, err := SubmitAttempt(ctx, e.tx, p, a, SubmitByParticipant, req.Now); err != nil {
		t.Fatal(err)
	}
	req.DeviceID = dev1
	if _, err := StartAttempt(ctx, e.tx, p, req); !errors.Is(err, ErrNoAttemptsLeft) {
		t.Errorf("second attempt: %v", err)
	}
	var audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE entity_id = $1 AND action IN ('exam.attempt_start', 'exam.attempt_submit')`, a.ID).Scan(&audits)
	if audits != 2 {
		t.Errorf("audits = %d", audits)
	}
	// Peserta bukan anggota sesi.
	other := auth.Participant{Kind: auth.RoleSiswa, SchoolID: e.b.ID, StudentID: e.b.StudentIDs[0]}
	if _, err := StartAttempt(ctx, e.tx, other, req); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign participant: %v", err)
	}
}

// Urutan soal dan opsi diacak per attempt lalu tetap; tampilan memakai
// urutan tersimpan.
func TestDeliveryOrder(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	ex, sess, _ := e.allKindsExam(t, now.Add(time.Hour))
	at := now.Add(61 * time.Minute)
	var orders []string
	var opts []OptionOrder
	for i := 0; i < 3; i++ {
		p := auth.Participant{Kind: auth.RoleSiswa, SchoolID: e.a.ID, StudentID: e.extra[i]}
		a, err := StartAttempt(ctx, e.tx, p, StartRequest{SessionID: sess.ID, DeviceID: dev1, Now: at})
		if err != nil {
			t.Fatal(err)
		}
		ex, _ = GetExam(ctx, e.tx, e.a.ID, ex.ID)
		d, err := LoadDelivery(ctx, e.tx, ex, a, func(id string) string { return "/img/" + id })
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Questions) != len(Kinds) {
			t.Fatalf("delivery questions = %d", len(d.Questions))
		}
		var ids []string
		for j, q := range d.Questions {
			ids = append(ids, q.View.ID)
			if q.View.ID != a.QuestionOrder[j] || q.View.Number != j+1 {
				t.Errorf("question %d = %s, want %s", j, q.View.ID, a.QuestionOrder[j])
			}
			if q.View.Kind == KindMC {
				var got []string
				for _, c := range q.View.Choices {
					got = append(got, c.ID)
				}
				if strings.Join(got, "") != strings.Join(a.OptionOrder[q.View.ID].Choices, "") || q.View.Choices[0].Letter != "A" {
					t.Errorf("choices %v, stored %v", got, a.OptionOrder[q.View.ID].Choices)
				}
				opts = append(opts, a.OptionOrder[q.View.ID])
			}
		}
		orders = append(orders, strings.Join(ids, ","))
	}
	if orders[0] == orders[1] && orders[1] == orders[2] {
		t.Log("all three orders equal (possible but unlikely)")
	}
	if len(opts) != 3 {
		t.Errorf("mc option orders = %d", len(opts))
	}
}

func TestArabicChoices(t *testing.T) {
	ar := ParticipantQuestion{Choices: []PublicChoice{{HTML: `<p dir="rtl" lang="ar">كِتَابٌ</p>`}, {HTML: "<p>قَلَمٌ</p>"}}}
	if !arabicChoices(ar) {
		t.Error("arabic choices not detected")
	}
	mixed := ParticipantQuestion{Choices: []PublicChoice{{HTML: "<p>كِتَابٌ</p>"}, {HTML: "<p>buku</p>"}}}
	if arabicChoices(mixed) || arabicChoices(ParticipantQuestion{}) {
		t.Error("non-arabic choices marked rtl")
	}
}
