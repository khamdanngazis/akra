// Command savesim menguji keandalan simpan jawaban (T145): banyak klien
// simulasi menyimpan jawaban seperti halaman ujian (antrian per perangkat,
// kirim ulang sampai diterima server), lalu memeriksa bahwa setiap jawaban
// terakhir tersimpan di database. Dipakai sambil me-restart server atau
// mematikan salah satu replika.
//
// Hanya untuk database uji/dev: membuat siswa SIM-*, peserta, dan attempt
// sintetis di sesi -session, lalu menghapusnya (kecuali -keep). Server
// harus memakai kunci token development (APP_ENV=development tanpa
// EXAM_TOKEN_KEYS).
//
//	DATABASE_URL=... go run ./cmd/savesim -url http://localhost:8090 -session <id> -clients 200 -duration 60s
//
// Mode load balancer sederhana untuk menjalankan dua replika lokal
// (round-robin, backend yang /healthz-nya bukan 200 dilewati):
//
//	go run ./cmd/savesim -lb :8090 -backends http://localhost:8091,http://localhost:8092
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/exam"
)

func main() {
	lb := flag.String("lb", "", "jalankan load balancer di alamat ini (mis. :8090)")
	backends := flag.String("backends", "", "daftar backend dipisah koma (mode -lb)")
	base := flag.String("url", "http://localhost:8090", "URL server/load balancer")
	session := flag.String("session", "", "id sesi ujian tempat peserta sintetis dibuat")
	clients := flag.Int("clients", 200, "jumlah klien")
	duration := flag.Duration("duration", 60*time.Second, "lama simulasi")
	interval := flag.Duration("interval", time.Second, "jeda antar-perubahan jawaban per klien")
	keep := flag.Bool("keep", false, "jangan hapus data sintetis")
	flag.Parse()
	if *lb != "" {
		runLB(*lb, strings.Split(*backends, ","))
		return
	}
	if *session == "" {
		log.Fatal("-session is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	atts, err := setup(ctx, pool, *session, *clients)
	if err != nil {
		log.Fatal(err)
	}
	if !*keep {
		defer cleanup(ctx, pool, *session)
	}
	res := simulate(*base, atts, *duration, *interval)
	lost, checked, err := verify(ctx, pool, res)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("clients=%d saves_ok=%d http_5xx=%d net_errors=%d conflicts=%d unflushed=%d answers_checked=%d lost=%d\n",
		len(atts), res.ok.Load(), res.e5xx.Load(), res.netErr.Load(), res.conflict.Load(), res.unflushed, checked, lost)
	if lost > 0 || res.unflushed > 0 || res.conflict.Load() > 0 {
		os.Exit(1)
	}
}

type simAttempt struct {
	exam.Attempt
	token string
}

const simPrefix = "SIM-"

// setup membuat n siswa, peserta, dan attempt berjalan dengan 40 soal
// sintetis di sesi sessionID.
func setup(ctx context.Context, pool *pgxpool.Pool, sessionID string, n int) ([]simAttempt, error) {
	cleanup(ctx, pool, sessionID)
	rows, err := pool.Query(ctx, `
		WITH x AS (SELECT x.id, x.school_id, x.exam_id, (SELECT id FROM exam_variants v WHERE v.exam_id = x.exam_id LIMIT 1) AS variant
		           FROM exam_sessions x WHERE x.id = $1),
		st AS (INSERT INTO students (school_id, nis, full_name)
		       SELECT x.school_id, $3 || g, 'Peserta Simulasi ' || g FROM x, generate_series(1, $2) g RETURNING id, school_id),
		pp AS (INSERT INTO exam_session_participants (school_id, session_id, student_id, variant_id)
		       SELECT st.school_id, x.id, st.id, x.variant FROM st, x RETURNING id, school_id, variant_id),
		qo AS (SELECT array_agg(gen_random_uuid()) AS q FROM generate_series(1, 40))
		INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, variant_id, seed, question_order, device_id, started_at, deadline_at)
		SELECT pp.school_id, x.exam_id, x.id, pp.id, pp.variant_id, 1, qo.q, gen_random_uuid(), now(), now() + interval '3 hours'
		FROM pp, x, qo
		RETURNING id, school_id, device_id::text, deadline_at, question_order::text[]`, sessionID, n, simPrefix)
	if err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}
	defer rows.Close()
	signer := exam.DevTokenSigner()
	var out []simAttempt
	for rows.Next() {
		var a simAttempt
		if err := rows.Scan(&a.ID, &a.SchoolID, &a.DeviceID, &a.DeadlineAt, &a.QuestionOrder); err != nil {
			return nil, err
		}
		if a.token, err = signer.TokenFor(a.Attempt, time.Now()); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}
	if len(out) != n {
		return nil, fmt.Errorf("setup: created %d attempts, want %d (session exists and has a variant?)", len(out), n)
	}
	return out, nil
}

func cleanup(ctx context.Context, pool *pgxpool.Pool, sessionID string) {
	for _, q := range []string{
		`DELETE FROM exam_answers WHERE attempt_id IN (SELECT t.id FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id
			JOIN students s ON s.id = p.student_id WHERE p.session_id = $1 AND s.nis LIKE $2 || '%')`,
		`DELETE FROM exam_attempt_events WHERE attempt_id IN (SELECT t.id FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id
			JOIN students s ON s.id = p.student_id WHERE p.session_id = $1 AND s.nis LIKE $2 || '%')`,
		`DELETE FROM exam_attempts WHERE participant_id IN (SELECT p.id FROM exam_session_participants p JOIN students s ON s.id = p.student_id
			WHERE p.session_id = $1 AND s.nis LIKE $2 || '%')`,
		`DELETE FROM exam_session_participants WHERE session_id = $1 AND student_id IN (SELECT id FROM students WHERE nis LIKE $2 || '%')`,
		`DELETE FROM students WHERE nis LIKE $2 || '%' AND school_id = (SELECT school_id FROM exam_sessions WHERE id = $1)`,
	} {
		if _, err := pool.Exec(ctx, q, sessionID, simPrefix); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}
}

type change struct {
	Q string          `json:"q"`
	V json.RawMessage `json:"v"`
	F bool            `json:"f"`
	S int64           `json:"s"`
}

type result struct {
	ok, e5xx, netErr, conflict atomic.Int64
	unflushed                  int
	// last: jawaban terakhir per attempt per soal menurut klien.
	last map[string]map[string]string
}

// simulate menjalankan semua klien: tiap interval satu soal acak diubah
// (ditulis dulu ke antrian), antrian dikirim; gagal → antrian tetap dan
// dikirim ulang dengan jeda bertambah (seperti T139).
func simulate(base string, atts []simAttempt, d, interval time.Duration) *result {
	res := &result{last: map[string]map[string]string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 10 * time.Second}
	stop := time.Now().Add(d)
	var unflushed atomic.Int64
	for i, a := range atts {
		wg.Add(1)
		go func(i int, a simAttempt) {
			defer wg.Done()
			pending := map[string]change{}
			last := map[string]string{}
			var seq int64
			backoff := time.Duration(0)
			time.Sleep(time.Duration(rand.Int64N(int64(interval)))) // sebar awal
			deadline := stop.Add(90 * time.Second)                  // batas kirim sisa antrian
			for {
				now := time.Now()
				if now.Before(stop) {
					seq++
					q := a.QuestionOrder[rand.IntN(len(a.QuestionOrder))]
					v := fmt.Sprintf("%q", fmt.Sprintf("c%d-s%d", i, seq))
					pending[q] = change{Q: q, V: json.RawMessage(v), S: seq}
					last[q] = v
				} else if len(pending) == 0 || now.After(deadline) {
					break
				}
				if len(pending) > 0 {
					if err := send(client, base, a, pending, res); err != nil {
						backoff = min(max(backoff*2, 250*time.Millisecond), 2*time.Second)
						time.Sleep(backoff)
						continue
					}
					backoff = 0
				}
				if time.Now().Before(stop) {
					time.Sleep(interval)
				}
			}
			unflushed.Add(int64(len(pending)))
			mu.Lock()
			res.last[a.ID] = last
			mu.Unlock()
		}(i, a)
	}
	wg.Wait()
	res.unflushed = int(unflushed.Load())
	return res
}

func send(client *http.Client, base string, a simAttempt, pending map[string]change, res *result) error {
	batch := make([]change, 0, len(pending))
	for _, c := range pending {
		batch = append(batch, c)
	}
	body, _ := json.Marshal(map[string]any{"changes": batch, "events": []any{}, "pos": 0})
	req, _ := http.NewRequest(http.MethodPost, base+"/u/a/"+a.ID+"/simpan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(exam.AttemptTokenHeader, a.token)
	r, err := client.Do(req)
	if err != nil {
		res.netErr.Add(1)
		return err
	}
	defer r.Body.Close()
	var out struct {
		Seq int64 `json:"seq"`
	}
	_ = json.NewDecoder(r.Body).Decode(&out)
	switch {
	case r.StatusCode == http.StatusOK:
		res.ok.Add(1)
		for q, c := range pending {
			if c.S <= out.Seq {
				delete(pending, q)
			}
		}
		return nil
	case r.StatusCode == http.StatusConflict:
		res.conflict.Add(1)
		return errors.New("attempt closed")
	case r.StatusCode >= 500:
		res.e5xx.Add(1)
	}
	return fmt.Errorf("status %d", r.StatusCode)
}

// verify membandingkan jawaban terakhir tiap klien dengan database.
func verify(ctx context.Context, pool *pgxpool.Pool, res *result) (lost, checked int, err error) {
	for id, last := range res.last {
		rows, err := pool.Query(ctx, `SELECT question_id::text, value::text FROM exam_answers WHERE attempt_id = $1`, id)
		if err != nil {
			return 0, 0, err
		}
		got := map[string]string{}
		for rows.Next() {
			var q, v string
			if err := rows.Scan(&q, &v); err != nil {
				rows.Close()
				return 0, 0, err
			}
			got[q] = v
		}
		rows.Close()
		for q, v := range last {
			checked++
			if got[q] != v {
				lost++
				if lost <= 5 {
					log.Printf("lost answer: attempt %s question %s want %s got %q", id, q, v, got[q])
				}
			}
		}
	}
	return lost, checked, nil
}

// runLB menjalankan reverse proxy round-robin. Backend yang /healthz-nya
// bukan 200 (mati atau sedang berhenti) dilewati; request yang gagal
// tersambung dicoba ke backend lain sekali.
func runLB(addr string, backends []string) {
	type be struct {
		url     *url.URL
		proxy   *httputil.ReverseProxy
		healthy atomic.Bool
	}
	var bs []*be
	for _, b := range backends {
		u, err := url.Parse(strings.TrimSpace(b))
		if err != nil {
			log.Fatal(err)
		}
		x := &be{url: u, proxy: httputil.NewSingleHostReverseProxy(u)}
		x.healthy.Store(true)
		bs = append(bs, x)
	}
	go func() {
		c := &http.Client{Timeout: time.Second}
		for {
			for _, b := range bs {
				r, err := c.Get(b.url.String() + "/healthz")
				ok := err == nil && r.StatusCode == http.StatusOK
				if r != nil {
					r.Body.Close()
				}
				if b.healthy.Swap(ok) != ok {
					log.Printf("backend %s healthy=%v", b.url, ok)
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
	var next atomic.Uint64
	pick := func(skip *be) *be {
		for i := 0; i < len(bs); i++ {
			b := bs[int(next.Add(1))%len(bs)]
			if b != skip && b.healthy.Load() {
				return b
			}
		}
		return nil
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = readAll(r)
		}
		var tried *be
		for attempt := 0; attempt < 2; attempt++ {
			b := pick(tried)
			if b == nil {
				http.Error(w, "no healthy backend", http.StatusBadGateway)
				return
			}
			failed := false
			p := *b.proxy
			p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { failed = true }
			r2 := r.Clone(r.Context())
			r2.Body = ioNopCloser(body)
			rec := &bufferedWriter{header: http.Header{}}
			p.ServeHTTP(rec, r2)
			if failed {
				tried = b
				continue
			}
			for k, v := range rec.header {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body.Bytes())
			return
		}
		http.Error(w, "backends unavailable", http.StatusBadGateway)
	})
	log.Printf("load balancer on %s → %v", addr, backends)
	log.Fatal(http.ListenAndServe(addr, handler))
}
