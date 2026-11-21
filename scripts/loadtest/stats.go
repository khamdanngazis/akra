package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// stats mengumpulkan latensi request yang berhasil dan jumlah gagal per
// endpoint (gagal jaringan buatan tidak dihitung sebagai error server).
type stats struct {
	mu   sync.Mutex
	lat  map[string][]time.Duration
	errs map[string]int
}

func newStats() *stats { return &stats{lat: map[string][]time.Duration{}, errs: map[string]int{}} }

func (s *stats) ok(name string, d time.Duration) {
	s.mu.Lock()
	s.lat[name] = append(s.lat[name], d)
	s.mu.Unlock()
}

func (s *stats) fail(name string) {
	s.mu.Lock()
	s.errs[name]++
	s.mu.Unlock()
}

func pct(xs []time.Duration, p float64) time.Duration {
	if len(xs) == 0 {
		return 0
	}
	i := int(float64(len(xs)-1) * p)
	return xs[i]
}

// report menulis tabel latensi dan error, dan mengembalikan p95 per
// endpoint serta jumlah request/error total.
func (s *stats) report() (map[string]time.Duration, int, int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := map[string]bool{}
	for n := range s.lat {
		names[n] = true
	}
	for n := range s.errs {
		names[n] = true
	}
	var keys []string
	for n := range names {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "%-18s %8s %6s %8s %8s %8s %8s\n", "endpoint", "ok", "err", "p50", "p95", "p99", "max")
	p95 := map[string]time.Duration{}
	total, errs := 0, 0
	for _, n := range keys {
		xs := s.lat[n]
		sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
		p95[n] = pct(xs, 0.95)
		var mx time.Duration
		if len(xs) > 0 {
			mx = xs[len(xs)-1]
		}
		if n != "open_exam" {
			total += len(xs) + s.errs[n]
			errs += s.errs[n]
		}
		fmt.Fprintf(&b, "%-18s %8d %6d %8s %8s %8s %8s\n", n, len(xs), s.errs[n], ms(pct(xs, 0.5)), ms(p95[n]), ms(pct(xs, 0.99)), ms(mx))
	}
	return p95, total, errs, b.String()
}

func ms(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) + "ms" }

// monitor mencatat puncak koneksi database dan CPU proses server (pids,
// dibaca dari /proc) selama uji.
type monitor struct {
	mu        sync.Mutex
	peakConns int
	cpu       []float64 // % satu inti, jumlah semua pid, per sampel
}

func (m *monitor) run(ctx context.Context, pool *pgxpool.Pool, pids []int, every time.Duration) {
	ticks := map[int]uint64{}
	last := time.Now()
	for _, p := range pids {
		ticks[p], _ = procTicks(p)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND backend_type = 'client backend'`).Scan(&n); err == nil {
			m.mu.Lock()
			m.peakConns = max(m.peakConns, n)
			m.mu.Unlock()
		}
		if len(pids) > 0 {
			now := time.Now()
			var used uint64
			for _, p := range pids {
				t, err := procTicks(p)
				if err == nil {
					used += t - ticks[p]
					ticks[p] = t
				}
			}
			// /proc memakai 100 tick per detik.
			pctOne := float64(used) / 100 / now.Sub(last).Seconds() * 100
			last = now
			m.mu.Lock()
			m.cpu = append(m.cpu, pctOne)
			m.mu.Unlock()
		}
	}
}

// procTicks membaca utime+stime proses pid (tick).
func procTicks(pid int) (uint64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	f := strings.Fields(s[i+2:])
	if len(f) < 13 {
		return 0, fmt.Errorf("short stat")
	}
	u, _ := strconv.ParseUint(f[11], 10, 64)
	st, _ := strconv.ParseUint(f[12], 10, 64)
	return u + st, nil
}

func (m *monitor) summary(cores int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cpu) == 0 {
		return fmt.Sprintf("db connections peak=%d", m.peakConns)
	}
	var sum, peak float64
	for _, c := range m.cpu {
		sum += c
		peak = max(peak, c)
	}
	avg := sum / float64(len(m.cpu))
	return fmt.Sprintf("db connections peak=%d; server CPU avg=%.0f%% peak=%.0f%% of one core (%.0f%%/%.0f%% of %d cores)",
		m.peakConns, avg, peak, avg/float64(cores), peak/float64(cores), cores)
}

// verify membandingkan jawaban terakhir tiap klien dengan database dan
// menghitung attempt yang belum dikumpulkan.
func verify(ctx context.Context, pool *pgxpool.Pool, outs []outcome) (checked, lost, notSubmitted int, err error) {
	for _, o := range outs {
		if o.attempt == "" {
			continue
		}
		rows, err := pool.Query(ctx, `SELECT question_id::text, value::text FROM exam_answers WHERE attempt_id = $1`, o.attempt)
		if err != nil {
			return 0, 0, 0, err
		}
		got := map[string]string{}
		for rows.Next() {
			var q, v string
			if err := rows.Scan(&q, &v); err != nil {
				rows.Close()
				return 0, 0, 0, err
			}
			got[q] = v
		}
		rows.Close()
		for q, v := range o.last {
			checked++
			if got[q] != v {
				lost++
			}
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM exam_attempts WHERE id = $1`, o.attempt).Scan(&status); err != nil {
			return 0, 0, 0, err
		}
		if status != "dikumpulkan" {
			notSubmitted++
		}
	}
	return checked, lost, notSubmitted, nil
}
