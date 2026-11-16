// saveproto adalah prototipe protokol simpan jawaban (spike T123).
//
// Server dan generator beban dalam satu proses:
//
//	go run ./saveproto -db postgres://.../akra_spike -clients 5000 -interval 10s -dur 60s -mode token
//
// mode token: verifikasi token attempt HMAC tanpa membaca DB, lalu satu
// pernyataan INSERT ... SELECT yang sekaligus mengecek attempt masih
// berjalan dan perangkatnya cocok.
// mode session: pembanding gaya scs — SELECT sesi + UPDATE kedaluwarsa,
// lalu upsert yang sama.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var key = []byte("spike-only-key-0123456789abcdef!")

// Token: base64(attempt_id(16) | device_id(16) | exp(8)) "." base64(hmac)
func makeToken(attempt, device [16]byte, exp time.Time) string {
	var p [40]byte
	copy(p[:16], attempt[:])
	copy(p[16:32], device[:])
	binary.BigEndian.PutUint64(p[32:], uint64(exp.Unix()))
	m := hmac.New(sha256.New, key)
	m.Write(p[:])
	return base64.RawURLEncoding.EncodeToString(p[:]) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func verifyToken(tok string) (attempt, device [16]byte, err error) {
	a, b, ok := strings.Cut(tok, ".")
	if !ok {
		return attempt, device, errors.New("malformed")
	}
	p, err1 := base64.RawURLEncoding.DecodeString(a)
	sig, err2 := base64.RawURLEncoding.DecodeString(b)
	if err1 != nil || err2 != nil || len(p) != 40 {
		return attempt, device, errors.New("malformed")
	}
	m := hmac.New(sha256.New, key)
	m.Write(p)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return attempt, device, errors.New("bad signature")
	}
	if time.Now().Unix() > int64(binary.BigEndian.Uint64(p[32:])) {
		return attempt, device, errors.New("expired")
	}
	copy(attempt[:], p[:16])
	copy(device[:], p[16:32])
	return attempt, device, nil
}

type answer struct {
	Q   int32  `json:"q"`
	V   string `json:"v"`
	Seq int64  `json:"s"`
}

const schema = `
DROP TABLE IF EXISTS sp_answers, sp_attempts, sp_sessions;
CREATE TABLE sp_attempts (id uuid PRIMARY KEY, device_id uuid NOT NULL, status text NOT NULL, deadline timestamptz NOT NULL);
CREATE TABLE sp_answers (attempt_id uuid NOT NULL REFERENCES sp_attempts(id), question_no int NOT NULL, value text NOT NULL,
  client_seq bigint NOT NULL, saved_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (attempt_id, question_no));
CREATE TABLE sp_sessions (token text PRIMARY KEY, data bytea NOT NULL, expiry timestamptz NOT NULL);
`

// Satu pernyataan: cek attempt + upsert batch. Jawaban dengan seq lebih
// lama tidak menimpa (urutan kirim ulang offline aman).
const upsert = `
INSERT INTO sp_answers (attempt_id, question_no, value, client_seq)
SELECT a.id, u.q, u.v, u.s
FROM sp_attempts a, unnest($3::int[], $4::text[], $5::bigint[]) AS u(q, v, s)
WHERE a.id = $1 AND a.device_id = $2 AND a.status = 'berjalan' AND a.deadline > now()
ON CONFLICT (attempt_id, question_no) DO UPDATE
SET value = excluded.value, client_seq = excluded.client_seq, saved_at = now()
WHERE sp_answers.client_seq < excluded.client_seq`

func handler(pool *pgxpool.Pool, mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		attempt, device, err := verifyToken(r.Header.Get("X-Attempt-Token"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if mode == "session" {
			var data []byte
			tok := r.Header.Get("X-Session")
			if err := pool.QueryRow(ctx, `SELECT data FROM sp_sessions WHERE token=$1 AND expiry > now()`, tok).Scan(&data); err != nil {
				http.Error(w, "session", http.StatusUnauthorized)
				return
			}
			if _, err := pool.Exec(ctx, `UPDATE sp_sessions SET expiry = now() + interval '2 hours' WHERE token=$1`, tok); err != nil {
				http.Error(w, "session", http.StatusInternalServerError)
				return
			}
		}
		var body struct {
			Answers []answer `json:"a"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil || len(body.Answers) == 0 || len(body.Answers) > 200 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		qs := make([]int32, len(body.Answers))
		vs := make([]string, len(body.Answers))
		ss := make([]int64, len(body.Answers))
		for i, a := range body.Answers {
			qs[i], vs[i], ss[i] = a.Q, a.V, a.Seq
		}
		if _, err := pool.Exec(ctx, upsert, attempt, device, qs, vs, ss); err != nil {
			log.Printf("upsert: %v", err)
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"s":%d}`, ss[len(ss)-1])
	}
}

func uuid() (u [16]byte) {
	rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return
}

func main() {
	dsn := flag.String("db", "", "DSN database spike (tabel sp_* dibuat ulang)")
	clients := flag.Int("clients", 5000, "jumlah peserta simulasi")
	interval := flag.Duration("interval", 10*time.Second, "jeda simpan per peserta")
	dur := flag.Duration("dur", 60*time.Second, "lama uji")
	mode := flag.String("mode", "token", "token atau session")
	maxConns := flag.Int("conns", 10, "batas koneksi pool DB (produksi: 10)")
	flag.Parse()

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(*dsn)
	if err != nil {
		log.Fatal(err)
	}
	cfg.MaxConns = int32(*maxConns)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		log.Fatal(err)
	}
	type cl struct {
		attempt, device [16]byte
		tok, sess       string
	}
	cls := make([]cl, *clients)
	for i := range cls {
		c := &cls[i]
		c.attempt, c.device = uuid(), uuid()
		c.tok = makeToken(c.attempt, c.device, time.Now().Add(3*time.Hour))
		s := uuid()
		c.sess = base64.RawURLEncoding.EncodeToString(s[:])
		if _, err := pool.Exec(ctx, `INSERT INTO sp_attempts VALUES ($1,$2,'berjalan',now()+interval '2 hours')`, c.attempt, c.device); err != nil {
			log.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO sp_sessions VALUES ($1, $2, now()+interval '2 hours')`, c.sess, bytes.Repeat([]byte("x"), 300)); err != nil {
			log.Fatal(err)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: handler(pool, *mode)}
	go srv.Serve(ln)
	url := "http://" + ln.Addr().String() + "/save"

	tr := &http.Transport{MaxIdleConns: *clients, MaxIdleConnsPerHost: *clients, IdleConnTimeout: time.Minute}
	hc := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	var mu sync.Mutex
	var lat []time.Duration
	var ok, fail atomic.Int64
	deadline := time.Now().Add(*dur)
	var wg sync.WaitGroup
	for i := range cls {
		wg.Add(1)
		go func(i int, c cl) {
			defer wg.Done()
			// sebar awal agar tidak serentak
			time.Sleep(time.Duration(int64(*interval) * int64(i) / int64(len(cls))))
			seq := int64(0)
			for time.Now().Before(deadline) {
				seq++
				// rata-rata 2 jawaban per simpan (jawaban berubah + ragu-ragu)
				body, _ := json.Marshal(map[string]any{"a": []answer{
					{Q: int32(seq%40 + 1), V: "B", Seq: seq},
					{Q: int32((seq+7)%40 + 1), V: "esai singkat jawaban peserta", Seq: seq},
				}})
				req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
				req.Header.Set("X-Attempt-Token", c.tok)
				req.Header.Set("X-Session", c.sess)
				t0 := time.Now()
				resp, err := hc.Do(req)
				d := time.Since(t0)
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				if err != nil || resp.StatusCode != 200 {
					fail.Add(1)
				} else {
					ok.Add(1)
					mu.Lock()
					lat = append(lat, d)
					mu.Unlock()
				}
				time.Sleep(*interval)
			}
		}(i, cls[i])
	}
	wg.Wait()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	pct := func(p int) time.Duration {
		if len(lat) == 0 {
			return 0
		}
		return lat[len(lat)*p/100]
	}
	var rows int64
	pool.QueryRow(ctx, `SELECT count(*) FROM sp_answers`).Scan(&rows)
	fmt.Printf("mode=%s clients=%d interval=%v dur=%v conns=%d\n", *mode, *clients, *interval, *dur, *maxConns)
	fmt.Printf("ok=%d fail=%d throughput=%.0f req/s p50=%v p95=%v p99=%v max=%v answers_rows=%d\n",
		ok.Load(), fail.Load(), float64(ok.Load())/dur.Seconds(), pct(50), pct(95), pct(99), lat[len(lat)-1], rows)
	pool.Exec(ctx, `DROP TABLE sp_answers, sp_attempts, sp_sessions`)
}
