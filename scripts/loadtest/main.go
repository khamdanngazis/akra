// Command loadtest menjalankan uji beban ujian online (T152) dengan alur
// peserta sebenarnya. Hanya untuk database/lingkungan uji beban, tidak
// pernah produksi. Lihat doc/uji-mvp4.md.
//
// Isi data (sekolah ujibeban-1..N, 500 siswa per sekolah, ujian 40 soal
// dengan 10 gambar, sesi mulai -start-in dari sekarang, kartu berkode):
//
//	DATABASE_URL=... STORAGE_LOCAL_DIR=... go run ./scripts/loadtest seed -schools 10 -out loadtest/exam-data.json
//
// Jalankan skenario (S1–S3 satu sekolah, S4 10 sekolah, S5 dengan -fail
// dan restart/mematikan replika selama berjalan):
//
//	DATABASE_URL=... go run ./scripts/loadtest run -data loadtest/exam-data.json -url http://localhost:8090 -schools 1
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/platform/storage"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: loadtest seed|run [flags]")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	switch os.Args[1] {
	case "seed":
		fs := flag.NewFlagSet("seed", flag.ExitOnError)
		schools := fs.Int("schools", 1, "jumlah sekolah")
		per := fs.Int("per-school", 500, "siswa per sekolah")
		startIn := fs.Duration("start-in", 3*time.Minute, "sesi mulai sekian dari sekarang")
		dur := fs.Int("duration", 90, "durasi ujian (menit)")
		out := fs.String("out", "loadtest/exam-data.json", "file data untuk run")
		_ = fs.Parse(os.Args[2:])
		if strings.Contains(os.Getenv("APP_ENV"), "production") {
			log.Fatal("refusing to seed with APP_ENV=production")
		}
		store, err := storage.NewLocal(os.Getenv("STORAGE_LOCAL_DIR"))
		if err != nil {
			log.Fatal(err)
		}
		if err := seed(ctx, pool, store, *schools, *per, time.Now().Add(*startIn), *dur, *out); err != nil {
			log.Fatal(err)
		}
	case "run":
		os.Exit(run(ctx, pool, os.Args[2:]))
	default:
		log.Fatal("usage: loadtest seed|run [flags]")
	}
}

func run(ctx context.Context, pool *pgxpool.Pool, args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dataFile := fs.String("data", "loadtest/exam-data.json", "file data dari seed")
	cfg := runConfig{}
	fs.StringVar(&cfg.Base, "url", "http://localhost:8090", "URL server/load balancer")
	fs.IntVar(&cfg.Schools, "schools", 1, "jumlah sekolah yang dijalankan")
	from := fs.Int("from", 0, "lewati sekian sekolah pertama di data (memakai sekolah lain tanpa seed ulang)")
	fs.IntVar(&cfg.PerSchool, "per-school", 500, "peserta per sekolah")
	fs.DurationVar(&cfg.LoginWindow, "login-window", 2*time.Minute, "rentang login serentak (S1)")
	fs.DurationVar(&cfg.SaveInterval, "save-interval", 20*time.Second, "jeda simpan per peserta (S2)")
	fs.DurationVar(&cfg.Work, "work", 10*time.Minute, "lama mengerjakan setelah semua login (S2)")
	fs.DurationVar(&cfg.SubmitWindow, "submit-window", time.Minute, "rentang kumpul serentak (S3)")
	fs.Float64Var(&cfg.NoSubmitShare, "no-submit", 0.05, "porsi peserta yang tidak kumpul (kumpul otomatis)")
	fs.Float64Var(&cfg.Fail, "fail", 0, "porsi request klien yang gagal acak (S5)")
	fs.IntVar(&cfg.Proctors, "proctors", 15, "pengawas per sekolah")
	fs.DurationVar(&cfg.ProctorEvery, "proctor-every", 10*time.Second, "jeda polling pengawas")
	fs.BoolVar(&cfg.Images, "images", true, "muat gambar soal")
	pidsFlag := fs.String("pids", "", "pid proses server (dipisah koma) untuk mengukur CPU")
	waitAuto := fs.Duration("wait-autosubmit", 0, "tunggu job kumpul otomatis menutup attempt yang tidak dikumpulkan (S3)")
	_ = fs.Parse(args)
	b, err := os.ReadFile(*dataFile)
	if err != nil {
		log.Fatal(err)
	}
	var data seedData
	if err := json.Unmarshal(b, &data); err != nil {
		log.Fatal(err)
	}
	if *from+cfg.Schools > len(data.Schools) {
		log.Fatalf("data has %d schools", len(data.Schools))
	}
	data.Schools = data.Schools[*from:]
	var pids []int
	for _, p := range strings.Split(*pidsFlag, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			pids = append(pids, n)
		}
	}
	st := newStats()
	tok := &tokens{m: map[string]string{}}
	mon := &monitor{}
	mctx, stopMon := context.WithCancel(ctx)
	go mon.run(mctx, pool, pids, 5*time.Second)
	pctx, stopProctors := context.WithCancel(ctx)
	var pwg sync.WaitGroup
	var parts []participant
	for _, s := range data.Schools[:cfg.Schools] {
		for i, x := range s.Sessions {
			if i < cfg.Proctors {
				pwg.Add(1)
				go func(code, email, sid string) {
					defer pwg.Done()
					runProctor(pctx, cfg, st, tok, code, email, data.Password, sid)
				}(s.Code, s.Admin, x.ID)
			}
		}
		n := 0
		for round := 0; n < cfg.PerSchool; round++ {
			added := false
			for _, x := range s.Sessions {
				if round < len(x.Participants) && n < cfg.PerSchool {
					parts = append(parts, participant{school: s.Code, sid: x.ID, card: x.Participants[round], i: len(parts)})
					n++
					added = true
				}
			}
			if !added {
				break
			}
		}
	}
	// Tunggu token sesi pertama dari pengawas.
	for i := 0; i < 60; i++ {
		tok.mu.RLock()
		got := len(tok.m)
		tok.mu.RUnlock()
		if got > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	// S1 dimulai saat sesi dibuka (login serentak setelah token dibacakan).
	var sids []string
	for _, p := range parts {
		sids = append(sids, p.sid)
	}
	var opens time.Time
	if err := pool.QueryRow(ctx, `SELECT max(starts_at) FROM exam_sessions WHERE id = ANY($1::uuid[])`, sids).Scan(&opens); err == nil && time.Until(opens) > 0 {
		log.Printf("waiting %s for sessions to open", time.Until(opens).Round(time.Second))
		time.Sleep(time.Until(opens) + time.Second)
	}
	t0 := time.Now()
	workEnd := t0.Add(cfg.LoginWindow + cfg.Work)
	log.Printf("participants=%d schools=%d login_window=%s work=%s save_interval=%s fail=%.2f", len(parts), cfg.Schools, cfg.LoginWindow,
		cfg.Work, cfg.SaveInterval, cfg.Fail)
	outs := make([]outcome, len(parts))
	var injected atomic.Int64
	var wg sync.WaitGroup
	for i, p := range parts {
		loginAt := t0.Add(time.Duration(rand.Int64N(int64(cfg.LoginWindow))))
		submitAt := time.Time{}
		if rand.Float64() >= cfg.NoSubmitShare {
			submitAt = workEnd.Add(time.Duration(rand.Int64N(int64(cfg.SubmitWindow))))
		}
		wg.Add(1)
		go func(i int, p participant) {
			defer wg.Done()
			outs[i] = runParticipant(cfg, st, tok, p, loginAt, workEnd, submitAt, &injected)
		}(i, p)
	}
	wg.Wait()
	if *waitAuto > 0 {
		var ids []string
		for _, o := range outs {
			if o.attempt != "" {
				ids = append(ids, o.attempt)
			}
		}
		deadline := time.Now().Add(*waitAuto)
		for time.Now().Before(deadline) {
			var open int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM exam_attempts WHERE id = ANY($1::uuid[]) AND status = 'berjalan'`, ids).Scan(&open); err != nil {
				log.Fatal(err)
			}
			if open == 0 {
				log.Printf("all attempts submitted (auto-submit job done) after %s", time.Since(t0).Round(time.Second))
				break
			}
			log.Printf("waiting for auto-submit: %d attempts still open", open)
			time.Sleep(15 * time.Second)
		}
	}
	stopProctors()
	pwg.Wait()
	stopMon()
	elapsed := time.Since(t0)
	// Ringkasan.
	failed := map[string]int{}
	unflushed, submitted := 0, 0
	for _, o := range outs {
		if o.failed != "" {
			failed[o.failed]++
		}
		unflushed += o.unflushed
		if o.submitted {
			submitted++
		}
	}
	p95, total, errs, table := st.report()
	fmt.Println(table)
	checked, lost, open, err := verify(ctx, pool, outs)
	if err != nil {
		log.Fatal(err)
	}
	errRate := 0.0
	if total > 0 {
		errRate = float64(errs) / float64(total) * 100
	}
	fmt.Printf("elapsed=%s participants=%d submitted_by_client=%d still_open=%d failed_flows=%v injected_failures=%d\n",
		elapsed.Round(time.Second), len(parts), submitted, open, failed, injected.Load())
	fmt.Printf("answers_checked=%d lost=%d unflushed=%d server_errors=%d/%d (%.3f%%)\n", checked, lost, unflushed, errs, total, errRate)
	fmt.Println(mon.summary(runtime.NumCPU()))
	pass := lost == 0 && unflushed == 0 && p95["save"] < 300*time.Millisecond && p95["open_exam"] < 2*time.Second && errRate < 0.1 && len(failed) == 0
	fmt.Printf("targets: save_p95=%s (<300ms) open_p95=%s (<2s) errors=%.3f%% (<0.1%%) lost=%d → pass=%v\n",
		ms(p95["save"]), ms(p95["open_exam"]), errRate, lost, pass)
	if !pass {
		return 1
	}
	return 0
}
