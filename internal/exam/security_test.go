package exam

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/testutil"
	"akra/web"
)

// Uji keamanan peserta (T153): kebocoran soal/kunci di semua route
// peserta, akses lintas peserta, token attempt palsu/kedaluwarsa, gambar
// soal di luar attempt berjalan, dan banjir simpan.

// leakSecrets adalah penanda kunci, pembahasan, dan rubrik yang tidak boleh
// muncul di respons peserta sebelum hasil dibuka. sampleQuestion memberi
// penanda RAHASIA-* pada pembahasan, jawaban isian, dan rubrik; nama field
// kunci menangkap kebocoran struktur kunci semua jenis soal.
var leakSecrets = []string{"RAHASIA-PEMBAHASAN", "RAHASIA-12", "RAHASIA-RUBRIK", `"correct"`, `"truth"`, `"pairs"`, `"accepted"`,
	`"tolerance"`, "rubric", "explanation", "answer_key", "Kunci:"}

func scanLeak(t *testing.T, where, body string) {
	t.Helper()
	for _, s := range leakSecrets {
		if i := strings.Index(body, s); i >= 0 {
			t.Errorf("%s leaks %q: …%s…", where, s, body[max(0, i-120):min(len(body), i+60)])
		}
	}
}

// secEnv: ujian berisi semua jenis soal + satu soal bergambar; dua siswa
// sudah login di perangkat berbeda dan memulai attempt.
type secEnv struct {
	*takeEnv
	sess, asset string
	cards       []Card
	attA, attB  string
}

func newSecEnv(t *testing.T) *secEnv {
	t.Helper()
	e := &secEnv{takeEnv: newTakeEnv(t)}
	ctx := context.Background()
	e.students(t)
	a, err := SaveAsset(ctx, e.tx, e.store, e.owner, e.bank.ID, photo(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	e.asset = a.ID
	var ids []string
	for _, k := range Kinds {
		q := sampleQuestion(k)
		if k == KindMC {
			q.BodySrc = `<p>Lihat <img data-asset="` + a.ID + `" alt="gambar"></p>`
			q.BodyHTML = q.BodySrc
		}
		created, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, q, now)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	start := now.Add(time.Hour)
	ex := e.scheduledExam(t, start, ids...)
	// Pembahasan untuk peserta, dibuka setelah sesi terakhir (bila guru
	// membuka): sebelum itu tidak boleh ada yang bocor.
	if _, err := e.tx.Exec(ctx, `UPDATE exams SET results_mode = 'nilai_dan_pembahasan', results_after_all = true WHERE id = $1`, ex.ID); err != nil {
		t.Fatal(err)
	}
	if e.cards, err = IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now); err != nil || len(e.cards) < 2 {
		t.Fatalf("cards = %d, %v", len(e.cards), err)
	}
	e.sess = e.sessionOf(t, ex)
	e.clock = start.Add(time.Minute)
	e.login(t, "hpA", e.cards[0])
	e.login(t, "hpB", e.cards[1])
	e.attA = strings.TrimPrefix(e.form("hpA", "/u/ujian/"+e.sess+"/mulai", url.Values{}).Header().Get("Location"), "/u/a/")
	e.attB = strings.TrimPrefix(e.form("hpB", "/u/ujian/"+e.sess+"/mulai", url.Values{}).Header().Get("Location"), "/u/a/")
	if len(e.attA) != 36 || len(e.attB) != 36 {
		t.Fatalf("attempts %q %q", e.attA, e.attB)
	}
	return e
}

// participantGETs memetakan setiap route GET peserta di registry ke path
// yang dipindai. Route peserta baru tanpa entri di sini membuat uji gagal.
func (e *secEnv) participantGETs() map[string][]string {
	att := e.attA
	return map[string][]string{
		"/u/":                           {"/u/"},
		"/u/beranda":                    {"/u/beranda"},
		"/u/cek":                        {"/u/cek"},
		"/u/cek/ping":                   {"/u/cek/ping"},
		"/u/cek/contoh.jpg":             {"/u/cek/contoh.jpg"},
		"/u/panduan/peserta.pdf":        {"/u/panduan/peserta.pdf"},
		"/u/panduan/orang-tua.pdf":      {"/u/panduan/orang-tua.pdf"},
		"/u/sw.js":                      {"/u/sw.js"},
		"/u/{code}":                     {"/u/" + e.a.Code},
		"/u/ujian/{session}":            {"/u/ujian/" + e.sess},
		"/u/a/{attempt}":                {"/u/a/" + att},
		"/u/a/{attempt}/selesai":        {"/u/a/" + att + "/selesai"},
		"/u/a/{attempt}/gambar/{asset}": {"/u/a/" + att + "/gambar/" + e.asset},
		"/u/hasil/{attempt}":            {"/u/hasil/" + att},
	}
}

func TestParticipantLeakScanner(t *testing.T) {
	e := newSecEnv(t)
	scans := e.participantGETs()
	// Semua route GET peserta di registry harus dipindai.
	for _, r := range testutil.Routes {
		if r.Method == http.MethodGet && (strings.HasPrefix(r.Pattern, "/u/") || r.Pattern == "/u") {
			if _, ok := scans[r.Pattern]; !ok {
				t.Errorf("participant route %s is not covered by the leak scanner", r.Pattern)
			}
		}
	}
	codes := map[string]int{}
	scanAll := func(phase string) {
		for pattern, paths := range scans {
			for _, p := range paths {
				rec := e.do("hpA", http.MethodGet, p, "", "")
				codes[phase+" "+pattern] = rec.Code
				scanLeak(t, phase+" GET "+pattern, rec.Body.String())
				// Pengalihan gambar menunjuk berkas gambar, bukan data soal.
				scanLeak(t, phase+" GET "+pattern+" Location", rec.Header().Get("Location"))
			}
		}
	}
	// Saat mengerjakan: halaman ujian (yang disalin service worker), JSON
	// state, dan respons simpan.
	scanAll("during attempt")
	// Pemindai harus benar-benar membaca halaman yang dipakai peserta (dan
	// disalin service worker), bukan halaman galat.
	for pattern, want := range map[string]int{"/u/beranda": 200, "/u/ujian/{session}": 303, "/u/a/{attempt}": 200,
		"/u/a/{attempt}/gambar/{asset}": 302, "/u/cek": 200, "/u/{code}": 200} {
		if got := codes["during attempt "+pattern]; got != want {
			t.Errorf("during attempt %s = %d, want %d (scan would be empty)", pattern, got, want)
		}
	}
	// Service worker dan skrip halaman ujian: berkas statis (router uji
	// tidak menyajikan /static), dipindai langsung.
	for _, f := range []string{"js/exam-sw.js", "js/exam-take.js"} {
		b, err := fs.ReadFile(web.FS, "static/"+f)
		if err != nil {
			t.Fatal(err)
		}
		scanLeak(t, f, string(b))
	}
	if take := e.do("hpA", http.MethodGet, "/u/a/"+e.attA, "", "").Body.String(); len(takeOrder(take)) != len(Kinds) {
		t.Errorf("take page shows %d questions, want all %d kinds", len(takeOrder(take)), len(Kinds))
	}
	page := e.do("hpA", http.MethodGet, "/u/a/"+e.attA, "", "").Body.String()
	token, _ := pageState(t, page)["token"].(string)
	rec := e.saveAs("/u/a/"+e.attA+"/simpan", token, `{"changes":[],"events":[],"pos":0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d", rec.Code)
	}
	scanLeak(t, "save response", rec.Body.String())
	if rec := e.do("hpA", http.MethodGet, "/u/hasil/"+e.attA, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("review during attempt = %d", rec.Code)
	}
	// Setelah dikumpulkan, sebelum hasil dibuka: tetap tertutup.
	if rec := e.form("hpA", "/u/a/"+e.attA+"/kumpul", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("submit = %d", rec.Code)
	}
	scanAll("after submit")
	if rec := e.do("hpA", http.MethodGet, "/u/hasil/"+e.attA, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("review before release = %d", rec.Code)
	}
	// Gambar soal tidak bisa dibuka setelah attempt selesai (sebelum
	// pembahasan dibuka).
	if rec := e.do("hpA", http.MethodGet, "/u/a/"+e.attA+"/gambar/"+e.asset, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("image after submit = %d", rec.Code)
	}
}

func TestParticipantCrossAccess(t *testing.T) {
	e := newSecEnv(t)
	// Peserta A membuka attempt, gambar, hasil, dan kumpul milik B: 404.
	for _, p := range []string{"/u/a/" + e.attB, "/u/a/" + e.attB + "/selesai", "/u/a/" + e.attB + "/gambar/" + e.asset, "/u/hasil/" + e.attB} {
		if rec := e.do("hpA", http.MethodGet, p, "", ""); rec.Code != http.StatusNotFound {
			t.Errorf("A GET %s = %d", p, rec.Code)
		}
	}
	if rec := e.form("hpA", "/u/a/"+e.attB+"/kumpul", url.Values{}); rec.Code == http.StatusSeeOther || rec.Code == http.StatusOK {
		t.Errorf("A submits B = %d", rec.Code)
	}
	// Belum mulai = tidak ada attempt: gambar lewat attempt asal → 404.
	if rec := e.do("hpA", http.MethodGet, "/u/a/00000000-0000-0000-0000-000000000000/gambar/"+e.asset, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("image without attempt = %d", rec.Code)
	}
	// Token attempt: milik attempt lain, dipalsukan, kedaluwarsa → 401.
	pageA := e.do("hpA", http.MethodGet, "/u/a/"+e.attA, "", "").Body.String()
	tokA, _ := pageState(t, pageA)["token"].(string)
	body := `{"changes":[],"events":[],"pos":0}`
	if rec := e.saveAs("/u/a/"+e.attB+"/simpan", tokA, body); rec.Code != http.StatusUnauthorized {
		t.Errorf("A token on B = %d", rec.Code)
	}
	forged := tokA[:len(tokA)-2] + map[bool]string{true: "AA", false: "BB"}[!strings.HasSuffix(tokA, "AA")]
	if rec := e.saveAs("/u/a/"+e.attA+"/simpan", forged, body); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged token = %d", rec.Code)
	}
	var device string
	if err := e.tx.QueryRow(context.Background(), `SELECT device_id::text FROM exam_attempts WHERE id = $1`, e.attA).Scan(&device); err != nil {
		t.Fatal(err)
	}
	expired, err := DevTokenSigner().Sign(AttemptClaims{SchoolID: e.a.ID, AttemptID: e.attA, DeviceID: device, Expires: e.clock.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.saveAs("/u/a/"+e.attA+"/simpan", expired, body); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired token = %d", rec.Code)
	}
	if rec := e.saveAs("/u/a/"+e.attA+"/simpan", "", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d", rec.Code)
	}
	// Token sah tetap diterima.
	if rec := e.saveAs("/u/a/"+e.attA+"/simpan", tokA, body); rec.Code != http.StatusOK {
		t.Errorf("valid token = %d", rec.Code)
	}
}

func TestSaveFloodLimitedPerAttempt(t *testing.T) {
	e := newSecEnv(t)
	tok := func(dev, att string) string {
		v, _ := pageState(t, e.do(dev, http.MethodGet, "/u/a/"+att, "", "").Body.String())["token"].(string)
		return v
	}
	tokA, tokB := tok("hpA", e.attA), tok("hpB", e.attB)
	body := `{"changes":[],"events":[],"pos":0}`
	limited := 0
	for i := 0; i < 60; i++ {
		rec := e.saveAs("/u/a/"+e.attA+"/simpan", tokA, body)
		if rec.Code == http.StatusTooManyRequests {
			limited++
			if rec.Header().Get("Retry-After") == "" {
				t.Errorf("429 without Retry-After")
			}
		}
	}
	if limited < 30 {
		t.Errorf("flood of 60 saves limited only %d times", limited)
	}
	// Attempt lain (satu IP lab) tidak ikut terbatas.
	if rec := e.saveAs("/u/a/"+e.attB+"/simpan", tokB, body); rec.Code != http.StatusOK {
		t.Errorf("other attempt after flood = %d", rec.Code)
	}
}

func TestSaveLimiterRefill(t *testing.T) {
	l := newSaveLimiter()
	t0 := time.Unix(1_800_000_000, 0)
	for i := 0; i < saveBurst; i++ {
		if !l.allow("a", t0) {
			t.Fatalf("burst request %d refused", i)
		}
	}
	if l.allow("a", t0) {
		t.Fatalf("over burst allowed")
	}
	if !l.allow("a", t0.Add(saveRefill)) {
		t.Errorf("refill after one interval refused")
	}
	// Klien normal (1 simpan / 20 dtk) tidak pernah tertahan.
	for i := 0; i < 100; i++ {
		if !l.allow("b", t0.Add(time.Duration(i)*20*time.Second)) {
			t.Fatalf("normal client refused at %d", i)
		}
	}
	// Entri lama dibersihkan.
	l.allow("c", t0.Add(time.Hour))
	if _, ok := l.buckets["a"]; ok {
		t.Errorf("idle bucket not swept")
	}
}

func TestCodeBruteForceLocksOverHTTP(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	e.students(t)
	q, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, sampleQuestion(KindMC), now)
	if err != nil {
		t.Fatal(err)
	}
	start := now.Add(time.Hour)
	ex := e.scheduledExam(t, start, q.ID)
	cards, err := IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now)
	if err != nil || len(cards) == 0 {
		t.Fatalf("cards: %v", err)
	}
	c := cards[0]
	e.clock = start.Add(time.Minute)
	var last int
	for i := 0; i < MaxFailures; i++ {
		last = e.form("brute", "/u/"+e.a.Code, url.Values{"identifier": {c.Identifier}, "code": {"ZZZZZZZZ"}}).Code
		if last == http.StatusSeeOther {
			t.Fatalf("wrong code accepted")
		}
	}
	// Terkunci: kode benar pun ditolak, dengan pesan untuk memanggil pengawas.
	rec := e.form("brute", "/u/"+e.a.Code, url.Values{"identifier": {c.Identifier}, "code": {c.Code}})
	if rec.Code == http.StatusSeeOther || !strings.Contains(strings.ToLower(rec.Body.String()), "terkunci") {
		t.Errorf("correct code after %d failures = %d (last wrong = %d)", MaxFailures, rec.Code, last)
	}
}

// Log aplikasi tidak memuat kode akses, isi jawaban, komentar guru, atau
// nama peserta (T153).
func TestLogsHaveNoParticipantSecrets(t *testing.T) {
	logCapture = &lockedBuffer{}
	t.Cleanup(func() { logCapture = nil })
	e := newSecEnv(t)
	ctx := context.Background()
	// Kode salah (tercatat sebagai login gagal), lalu simpan esai berpenanda.
	e.form("tebak", "/u/"+e.a.Code, url.Values{"identifier": {e.cards[2].Identifier}, "code": {"ZZZZZZZZ"}})
	var essay string
	if err := e.tx.QueryRow(ctx, `SELECT q.id FROM exam_questions q JOIN exam_attempts t ON q.id = ANY(t.question_order)
		WHERE t.id = $1 AND q.kind = 'esai'`, e.attA).Scan(&essay); err != nil {
		t.Fatal(err)
	}
	page := e.do("hpA", http.MethodGet, "/u/a/"+e.attA, "", "").Body.String()
	token, _ := pageState(t, page)["token"].(string)
	body := `{"changes":[{"q":"` + essay + `","v":"JAWABAN-PRIBADI-91 isi esai siswa","f":false,"s":1}],"events":[],"pos":0}`
	if rec := e.saveAs("/u/a/"+e.attA+"/simpan", token, body); rec.Code != http.StatusOK {
		t.Fatalf("save = %d", rec.Code)
	}
	if rec := e.form("hpA", "/u/a/"+e.attA+"/kumpul", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("submit = %d", rec.Code)
	}
	var examID string
	e.tx.QueryRow(ctx, `SELECT exam_id FROM exam_attempts WHERE id = $1`, e.attA).Scan(&examID)
	req := httptest.NewRequest(http.MethodPost, "/ujian/paket/"+examID+"/esai/"+essay+"/"+e.attA,
		strings.NewReader(url.Values{"skor": {"1"}, "komentar": {"KOMENTAR-PRIBADI-77"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Test-User", e.a.Admin.ID+"|admin_tu")
	rec := httptest.NewRecorder()
	e.examEnv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("essay grade = %d", rec.Code)
	}
	logs := logCapture.String()
	if !strings.Contains(logs, "attempt started") || !strings.Contains(logs, "attempt submitted") {
		t.Fatalf("log capture is empty or incomplete:\n%s", logs)
	}
	secrets := []string{"JAWABAN-PRIBADI", "KOMENTAR-PRIBADI", "ZZZZZZZZ"}
	for _, c := range e.cards {
		secrets = append(secrets, c.Code, c.Name)
	}
	for _, s := range secrets {
		if s != "" && strings.Contains(logs, s) {
			i := strings.Index(logs, s)
			t.Errorf("logs contain %q: …%s…", s, logs[max(0, i-150):min(len(logs), i+50)])
		}
	}
}
