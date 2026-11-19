// Package metrics mencatat latensi dan error endpoint (T145) tanpa pustaka
// luar: penghitung kumulatif dan histogram untuk /metrics (format teks
// Prometheus), serta jendela waktu pendek (ember 10 detik, 10 menit
// terakhir) untuk aturan alert.
//
// Data per proses. Dengan beberapa replika, pemantau menggabungkan hasil
// /metrics tiap replika; aturan alert dievaluasi di setiap replika dan
// kejadian dibuka sekali lewat database (paket ops).
package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Buckets adalah batas histogram latensi (detik).
var Buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

const (
	bucketWidth = 10 * time.Second
	bucketCount = 60 // 10 menit
)

// window adalah ember 10 detik: jumlah, error, dan histogram.
type window struct {
	start  int64 // unix detik / 10
	count  int64
	errors int64
	hist   [12]int64 // len(Buckets)+1
}

// series adalah statistik satu endpoint.
type series struct {
	count  int64
	errors int64
	sum    float64
	hist   [12]int64
	ring   [bucketCount]window
}

// Registry menyimpan semua statistik. Aman dipakai bersamaan.
type Registry struct {
	mu     sync.Mutex
	series map[string]*series
	gauges []gauge
	now    func() time.Time
}

type gauge struct {
	name, help string
	fn         func() []Sample
}

// Sample adalah satu nilai gauge dengan label opsional.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// New mengembalikan Registry kosong.
func New() *Registry {
	return &Registry{series: map[string]*series{}, now: time.Now}
}

// SetClock mengganti jam (untuk uji).
func (r *Registry) SetClock(now func() time.Time) { r.now = now }

func bucketIndex(sec float64) int {
	for i, b := range Buckets {
		if sec <= b {
			return i
		}
	}
	return len(Buckets)
}

// Observe mencatat satu request endpoint name: lamanya dan apakah gagal
// karena server (status ≥ 500).
func (r *Registry) Observe(name string, d time.Duration, failed bool) {
	sec := d.Seconds()
	bi := bucketIndex(sec)
	slot := r.now().Unix() / int64(bucketWidth/time.Second)
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.series[name]
	if s == nil {
		s = &series{}
		r.series[name] = s
	}
	s.count++
	s.sum += sec
	s.hist[bi]++
	w := &s.ring[slot%bucketCount]
	if w.start != slot {
		*w = window{start: slot}
	}
	w.count++
	w.hist[bi]++
	if failed {
		s.errors++
		w.errors++
	}
}

// Window adalah ringkasan satu endpoint dalam rentang waktu terakhir.
type Window struct {
	Count  int64
	Errors int64
	P95    time.Duration // batas atas ember histogram yang memuat p95
}

// ErrorRate adalah porsi request yang gagal.
func (w Window) ErrorRate() float64 {
	if w.Count == 0 {
		return 0
	}
	return float64(w.Errors) / float64(w.Count)
}

// Recent meringkas endpoint name dalam rentang d terakhir (dibulatkan ke
// ember 10 detik, paling lama 10 menit).
func (r *Registry) Recent(name string, d time.Duration) Window {
	n := int64(d / bucketWidth)
	if n < 1 {
		n = 1
	}
	if n > bucketCount {
		n = bucketCount
	}
	now := r.now().Unix() / int64(bucketWidth/time.Second)
	r.mu.Lock()
	defer r.mu.Unlock()
	var out Window
	s := r.series[name]
	if s == nil {
		return out
	}
	var hist [12]int64
	for i := int64(0); i < n; i++ {
		slot := now - i
		w := s.ring[((slot%bucketCount)+bucketCount)%bucketCount]
		if w.start != slot {
			continue
		}
		out.Count += w.count
		out.Errors += w.errors
		for j := range hist {
			hist[j] += w.hist[j]
		}
	}
	out.P95 = quantile(hist, out.Count, 0.95)
	return out
}

func quantile(hist [12]int64, total int64, q float64) time.Duration {
	if total == 0 {
		return 0
	}
	need := int64(math.Ceil(float64(total) * q))
	var acc int64
	for i, c := range hist {
		acc += c
		if acc >= need {
			if i < len(Buckets) {
				return time.Duration(Buckets[i] * float64(time.Second))
			}
			return time.Duration(Buckets[len(Buckets)-1]*2) * time.Second
		}
	}
	return 0
}

// Gauge mendaftarkan nilai yang dibaca saat /metrics diminta.
func (r *Registry) Gauge(name, help string, fn func() []Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, gauge{name: name, help: help, fn: fn})
}

// WriteText menulis semua statistik dalam format teks Prometheus.
func (r *Registry) WriteText(w io.Writer) {
	r.mu.Lock()
	names := make([]string, 0, len(r.series))
	for n := range r.series {
		names = append(names, n)
	}
	sort.Strings(names)
	type snap struct {
		count, errors int64
		sum           float64
		hist          [12]int64
	}
	snaps := make([]snap, len(names))
	for i, n := range names {
		s := r.series[n]
		snaps[i] = snap{s.count, s.errors, s.sum, s.hist}
	}
	gauges := append([]gauge(nil), r.gauges...)
	r.mu.Unlock()

	fmt.Fprintln(w, "# HELP akra_request_duration_seconds Latency of tracked endpoints.")
	fmt.Fprintln(w, "# TYPE akra_request_duration_seconds histogram")
	for i, n := range names {
		var acc int64
		for j, b := range Buckets {
			acc += snaps[i].hist[j]
			fmt.Fprintf(w, "akra_request_duration_seconds_bucket{endpoint=%q,le=%q} %d\n", n, trimFloat(b), acc)
		}
		acc += snaps[i].hist[len(Buckets)]
		fmt.Fprintf(w, "akra_request_duration_seconds_bucket{endpoint=%q,le=\"+Inf\"} %d\n", n, acc)
		fmt.Fprintf(w, "akra_request_duration_seconds_sum{endpoint=%q} %g\n", n, snaps[i].sum)
		fmt.Fprintf(w, "akra_request_duration_seconds_count{endpoint=%q} %d\n", n, snaps[i].count)
	}
	fmt.Fprintln(w, "# HELP akra_request_errors_total Server errors (status >= 500) of tracked endpoints.")
	fmt.Fprintln(w, "# TYPE akra_request_errors_total counter")
	for i, n := range names {
		fmt.Fprintf(w, "akra_request_errors_total{endpoint=%q} %d\n", n, snaps[i].errors)
	}
	for _, g := range gauges {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
		for _, s := range g.fn() {
			fmt.Fprintf(w, "%s%s %g\n", g.name, labels(s.Labels), s.Value)
		}
	}
}

func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", f), "0"), ".")
}

func labels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// Handler menyajikan /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		r.WriteText(w)
	})
}

// statusWriter merekam status balasan.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Track membungkus handler endpoint name: mencatat lama dan status ≥ 500.
// Panic dicatat sebagai error lalu diteruskan.
func (r *Registry) Track(name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		failed := true
		defer func() {
			if sw.status != 0 {
				failed = sw.status >= 500
			}
			r.Observe(name, time.Since(start), failed)
		}()
		next.ServeHTTP(sw, req)
		failed = false
	})
}

// TrackFunc adalah Track untuk http.HandlerFunc.
func (r *Registry) TrackFunc(name string, next http.HandlerFunc) http.HandlerFunc {
	return r.Track(name, next).ServeHTTP
}
