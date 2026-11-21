package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Alur peserta sebenarnya (T152): halaman login → login (CSRF, cookie) →
// beranda → konfirmasi → mulai dengan token sesi dari pengawas → halaman
// ujian + gambar → simpan berkala (antrian per klien, seq naik, kirim ulang
// sampai diterima) → kumpul. Setiap klien mencatat jawaban terakhirnya
// untuk dibandingkan dengan database.

type runConfig struct {
	Base          string
	Schools       int
	PerSchool     int // peserta per sekolah (≤ seed)
	LoginWindow   time.Duration
	SaveInterval  time.Duration
	Work          time.Duration // lama mengerjakan setelah semua login
	SubmitWindow  time.Duration
	NoSubmitShare float64 // porsi yang tidak kumpul (kumpul otomatis job)
	Fail          float64 // porsi request klien yang gagal acak (jaringan)
	Proctors      int     // pengawas per sekolah
	ProctorEvery  time.Duration
	Images        bool
}

// faultyTransport menggagalkan request acak: separuh sebelum terkirim,
// separuh setelah server menjawab (jawaban hilang di jalan), seperti
// jaringan sekolah yang putus-sambung.
type faultyTransport struct {
	base http.RoundTripper
	rate float64
	n    *atomic.Int64
}

var errInjected = errors.New("injected network failure")

func (t faultyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.rate > 0 && rand.Float64() < t.rate {
		t.n.Add(1)
		if rand.IntN(2) == 0 {
			return nil, errInjected
		}
		res, err := t.base.RoundTrip(r)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		return nil, errInjected
	}
	return t.base.RoundTrip(r)
}

var sharedTransport = &http.Transport{MaxIdleConns: 20000, MaxIdleConnsPerHost: 20000, IdleConnTimeout: 90 * time.Second}

func newClient(rate float64, injected *atomic.Int64) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: faultyTransport{base: sharedTransport, rate: rate, n: injected},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

var (
	csrfRe  = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	stateRe = regexp.MustCompile(`(?s)id="take-state"[^>]*>(.*?)</script>`)
	imgRe   = regexp.MustCompile(`/u/a/[0-9a-f-]{36}/gambar/[0-9a-f-]{36}`)
)

// noToken menandai sesi yang tidak mewajibkan token (pengaturan sekolah).
const noToken = "-"

// tokens menyimpan token sesi terbaru dari pengawas.
type tokens struct {
	mu sync.RWMutex
	m  map[string]string
}

func (t *tokens) get(sid string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.m[sid]
}

func (t *tokens) set(sid, v string) {
	t.mu.Lock()
	t.m[sid] = v
	t.mu.Unlock()
}

// outcome adalah hasil satu peserta.
type outcome struct {
	attempt   string
	last      map[string]string // soal → nilai JSON terakhir menurut klien
	unflushed int
	submitted bool
	failed    string // tahap yang gagal permanen
}

// do mengirim request dengan pengulangan bila gagal jaringan atau 5xx
// (seperti pengguna memuat ulang), mencatat latensi request yang berhasil.
func do(c *http.Client, st *stats, name string, build func() (*http.Request, error), tries int) (*http.Response, []byte, error) {
	var last error
	for i := 0; i < tries; i++ {
		req, err := build()
		if err != nil {
			return nil, nil, err
		}
		t0 := time.Now()
		res, err := c.Do(req)
		if err != nil {
			if !errors.Is(err, errInjected) {
				st.fail(name)
			}
			last = err
			time.Sleep(time.Duration(200*(i+1)) * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode >= 500 {
			st.fail(name)
			last = fmt.Errorf("%s: status %d", name, res.StatusCode)
			time.Sleep(time.Duration(200*(i+1)) * time.Millisecond)
			continue
		}
		st.ok(name, time.Since(t0))
		return res, body, nil
	}
	return nil, nil, last
}

func get(base, path string) func() (*http.Request, error) {
	return func() (*http.Request, error) { return http.NewRequest(http.MethodGet, base+path, nil) }
}

func postForm(base, path string, v url.Values) func() (*http.Request, error) {
	return func() (*http.Request, error) {
		r, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(v.Encode()))
		if err == nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", base)
		}
		return r, err
	}
}

type participant struct {
	school string
	sid    string
	card   seedCard
	i      int
}

// runParticipant menjalankan satu peserta. loginAt: waktu login; workEnd:
// akhir masa mengerjakan; submitAt: waktu kumpul (zero = tidak kumpul).
func runParticipant(cfg runConfig, st *stats, tok *tokens, p participant, loginAt, workEnd, submitAt time.Time, injected *atomic.Int64) outcome {
	out := outcome{last: map[string]string{}}
	c := newClient(cfg.Fail, injected)
	base := cfg.Base
	time.Sleep(time.Until(loginAt))
	// Login.
	_, body, err := do(c, st, "login_page", get(base, "/u/"+p.school+"?nis="+p.card.NIS), 5)
	if err != nil {
		out.failed = "login_page"
		return out
	}
	m := csrfRe.FindSubmatch(body)
	if m == nil {
		out.failed = "login_csrf"
		return out
	}
	res, _, err := do(c, st, "login", postForm(base, "/u/"+p.school, url.Values{"csrf_token": {string(m[1])}, "identifier": {p.card.NIS}, "code": {p.card.Code}}), 5)
	if err != nil || res.StatusCode != http.StatusSeeOther {
		// Jawaban login hilang di jalan: sesi mungkin sudah terbentuk.
		if _, b, err2 := do(c, st, "home", get(base, "/u/beranda"), 3); err2 != nil || !bytes.Contains(b, []byte("/u/ujian/")) {
			out.failed = "login"
			return out
		}
	}
	if _, _, err := do(c, st, "home", get(base, "/u/beranda"), 5); err != nil {
		out.failed = "home"
		return out
	}
	// Konfirmasi lalu mulai dengan token dari pengawas.
	var attempt string
	var t0 time.Time
	for try := 0; try < 20 && attempt == ""; try++ {
		t0 = time.Now()
		res, body, err := do(c, st, "confirm", get(base, "/u/ujian/"+p.sid), 5)
		if err != nil {
			continue
		}
		if res.StatusCode == http.StatusSeeOther && strings.HasPrefix(res.Header.Get("Location"), "/u/a/") {
			attempt = strings.TrimPrefix(res.Header.Get("Location"), "/u/a/")
			break
		}
		m := csrfRe.FindSubmatch(body)
		token := tok.get(p.sid)
		if m == nil || token == "" {
			debugf("participant %s: confirm %d csrf=%v token=%q", p.card.NIS, res.StatusCode, m != nil, token)
			time.Sleep(time.Second)
			continue
		}
		var sb []byte
		if token == noToken {
			token = ""
		}
		res, sb, err = do(c, st, "start", postForm(base, "/u/ujian/"+p.sid+"/mulai", url.Values{"csrf_token": {string(m[1])}, "token": {token}}), 1)
		if err == nil && res.StatusCode == http.StatusSeeOther && strings.HasPrefix(res.Header.Get("Location"), "/u/a/") {
			attempt = strings.TrimPrefix(res.Header.Get("Location"), "/u/a/")
		} else {
			if err == nil {
				debugf("participant %s: start %d %s", p.card.NIS, res.StatusCode, alertText(sb))
			}
			time.Sleep(time.Second)
		}
	}
	if attempt == "" {
		out.failed = "start"
		return out
	}
	attempt = strings.SplitN(attempt, "/", 2)[0]
	out.attempt = attempt
	// Buka ujian: halaman + gambar (6 sekaligus, seperti browser).
	_, page, err := do(c, st, "take", get(base, "/u/a/"+attempt), 5)
	if err != nil {
		out.failed = "take"
		return out
	}
	if cfg.Images {
		imgs := imgRe.FindAll(page, -1)
		seen := map[string]bool{}
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, u := range imgs {
			if seen[string(u)] {
				continue
			}
			seen[string(u)] = true
			wg.Add(1)
			sem <- struct{}{}
			go func(path string) {
				defer wg.Done()
				defer func() { <-sem }()
				res, _, err := do(c, st, "image_redirect", get(base, path), 3)
				if err == nil && res.StatusCode == http.StatusFound {
					loc := res.Header.Get("Location")
					if strings.HasPrefix(loc, "/") {
						loc = base + loc
					}
					do(c, st, "image", func() (*http.Request, error) { return http.NewRequest(http.MethodGet, loc, nil) }, 3)
				}
			}(string(u))
		}
		wg.Wait()
	}
	st.ok("open_exam", time.Since(t0))
	var state struct {
		IDs   []string `json:"ids"`
		Token string   `json:"token"`
	}
	if m := stateRe.FindSubmatch(page); m == nil || json.Unmarshal(m[1], &state) != nil || state.Token == "" {
		out.failed = "take_state"
		return out
	}
	csrf := ""
	if m := csrfRe.FindSubmatch(page); m != nil {
		csrf = string(m[1])
	}
	// Mengerjakan: tiap interval 1–3 jawaban berubah, antrian dikirim; gagal →
	// tetap di antrian dan dikirim ulang dengan jeda bertambah (T139).
	pending := map[string]change{}
	var seq int64
	backoff := time.Duration(0)
	stop := workEnd
	if !submitAt.IsZero() && submitAt.Before(stop) {
		stop = submitAt
	}
	time.Sleep(time.Duration(rand.Int64N(int64(cfg.SaveInterval))))
	flushDeadline := stop.Add(2 * time.Minute)
	for {
		now := time.Now()
		if now.Before(stop) {
			for k := 0; k < 1+rand.IntN(3); k++ {
				seq++
				q := state.IDs[rand.IntN(len(state.IDs))]
				v := fmt.Sprintf("%q", []string{"a", "b", "c", "d"}[rand.IntN(4)]+fmt.Sprintf("-%d-%d", p.i, seq))
				pending[q] = change{Q: q, V: json.RawMessage(v), S: seq}
				out.last[q] = v
			}
		} else if len(pending) == 0 || now.After(flushDeadline) {
			break
		}
		if len(pending) > 0 {
			if err := save(c, st, base, attempt, state.Token, pending); err != nil {
				if errors.Is(err, errClosed) {
					break
				}
				backoff = min(max(backoff*2, 500*time.Millisecond), 5*time.Second)
				time.Sleep(backoff)
				continue
			}
			backoff = 0
		}
		if time.Now().Before(stop) {
			// ±25% jitter seperti jam perangkat yang berbeda.
			d := cfg.SaveInterval * time.Duration(75+rand.IntN(51)) / 100
			time.Sleep(min(d, time.Until(stop)+time.Millisecond))
		}
	}
	out.unflushed = len(pending)
	if submitAt.IsZero() || len(pending) > 0 {
		return out
	}
	time.Sleep(time.Until(submitAt))
	res, _, err = do(c, st, "submit", postForm(base, "/u/a/"+attempt+"/kumpul", url.Values{"csrf_token": {csrf}}), 5)
	out.submitted = err == nil && res.StatusCode == http.StatusSeeOther
	if !out.submitted {
		// Jawaban kumpul hilang di jalan: periksa apakah sudah selesai.
		if res, _, err := do(c, st, "take", get(base, "/u/a/"+attempt), 3); err == nil && res.StatusCode == http.StatusSeeOther {
			out.submitted = true
		}
	}
	return out
}

type change struct {
	Q string          `json:"q"`
	V json.RawMessage `json:"v"`
	F bool            `json:"f"`
	S int64           `json:"s"`
}

var errClosed = errors.New("attempt closed")

func save(c *http.Client, st *stats, base, attempt, token string, pending map[string]change) error {
	batch := make([]change, 0, len(pending))
	for _, ch := range pending {
		batch = append(batch, ch)
	}
	body, _ := json.Marshal(map[string]any{"changes": batch, "events": []any{}, "pos": 0})
	t0 := time.Now()
	req, _ := http.NewRequest(http.MethodPost, base+"/u/a/"+attempt+"/simpan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Attempt-Token", token)
	res, err := c.Do(req)
	if err != nil {
		if !errors.Is(err, errInjected) {
			st.fail("save")
		}
		return err
	}
	defer res.Body.Close()
	var out struct {
		Seq int64 `json:"seq"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	switch {
	case res.StatusCode == http.StatusOK:
		st.ok("save", time.Since(t0))
		for q, ch := range pending {
			if ch.S <= out.Seq {
				delete(pending, q)
			}
		}
		return nil
	case res.StatusCode == http.StatusConflict:
		st.fail("save_closed")
		return errClosed
	case res.StatusCode >= 500:
		st.fail("save")
	default:
		st.fail("save_" + fmt.Sprint(res.StatusCode))
	}
	return fmt.Errorf("save status %d", res.StatusCode)
}

// runProctor masuk sebagai TU lalu memantau satu sesi: token sesi (untuk
// dibacakan) dan data layar pengawas tiap ProctorEvery.
func runProctor(ctx context.Context, cfg runConfig, st *stats, tok *tokens, school, email, password, sid string) {
	c := newClient(0, new(atomic.Int64))
	base := cfg.Base
	_, body, err := do(c, st, "staff_login_page", get(base, "/login"), 5)
	if err != nil {
		log.Printf("proctor login page: %v", err)
		return
	}
	m := csrfRe.FindSubmatch(body)
	if m == nil {
		log.Printf("proctor: no csrf")
		return
	}
	res, _, err := do(c, st, "staff_login", postForm(base, "/login", url.Values{"csrf_token": {string(m[1])}, "school_code": {school},
		"email": {email}, "password": {password}}), 3)
	if err != nil || res.StatusCode != http.StatusSeeOther {
		log.Printf("proctor login failed: %v", err)
		return
	}
	for {
		if _, b, err := do(c, st, "proctor_token", get(base, "/ujian/sesi/"+sid+"/token.json"), 3); err == nil {
			var t struct {
				Required bool   `json:"required"`
				Token    string `json:"token"`
			}
			if json.Unmarshal(b, &t) == nil {
				switch {
				case t.Token != "":
					tok.set(sid, t.Token)
				case !t.Required:
					tok.set(sid, noToken)
				}
			}
		}
		do(c, st, "proctor_poll", get(base, "/ujian/sesi/"+sid+"/awasi/data"), 1)
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.ProctorEvery):
		}
	}
}

var debug = os.Getenv("LOADTEST_DEBUG") != ""

func debugf(format string, args ...any) {
	if debug {
		log.Printf(format, args...)
	}
}

var alertRe = regexp.MustCompile(`(?s)class="alert[^"]*"[^>]*>(.*?)</`)

// alertText mengambil pesan galat dari halaman HTML untuk diagnosis.
func alertText(b []byte) string {
	if m := alertRe.FindSubmatch(b); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}
