package exam

import (
	"slices"
	"sync"
	"time"
)

// Indikator kesehatan simpan jawaban per sekolah untuk pusat kendali
// (T144): latensi server, error rate, dan waktu pulang-pergi menurut
// perangkat. Disimpan di memori proses selama HealthWindow; dengan beberapa
// replika setiap replika melihat bagiannya sendiri (metrik gabungan: T145).

// HealthWindow: rentang data indikator kesehatan.
const HealthWindow = 5 * time.Minute

// healthCap: sampel terakhir per sekolah yang disimpan.
const healthCap = 2048

type healthSample struct {
	at     time.Time
	server time.Duration
	failed bool
	rtt    int // ms; 0 = tidak dilaporkan
}

// HealthRecorder mengumpulkan sampel simpan per sekolah.
type HealthRecorder struct {
	mu      sync.Mutex
	schools map[string]*healthRing
}

type healthRing struct {
	buf  []healthSample
	next int
}

// SaveHealth adalah perekam simpan jawaban proses ini.
var SaveHealth = NewHealthRecorder()

// NewHealthRecorder mengembalikan perekam kosong.
func NewHealthRecorder() *HealthRecorder {
	return &HealthRecorder{schools: map[string]*healthRing{}}
}

// Record mencatat satu simpan: lama di server, gagal karena server (5xx),
// dan RTT simpan sebelumnya yang dilaporkan perangkat (ms).
func (h *HealthRecorder) Record(schoolID string, server time.Duration, failed bool, rtt int, now time.Time) {
	if schoolID == "" {
		return
	}
	if rtt < 0 || rtt > 120000 {
		rtt = 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.schools[schoolID]
	if r == nil {
		r = &healthRing{}
		h.schools[schoolID] = r
	}
	s := healthSample{at: now, server: server, failed: failed, rtt: rtt}
	if len(r.buf) < healthCap {
		r.buf = append(r.buf, s)
		return
	}
	r.buf[r.next] = s
	r.next = (r.next + 1) % healthCap
}

// Health adalah ringkasan kesehatan simpan satu sekolah.
type Health struct {
	Saves     int
	Errors    int
	ServerP50 time.Duration
	ServerP95 time.Duration
	RTTP50    time.Duration
	RTTP95    time.Duration
	RTTCount  int
}

// Ambang indikator.
const (
	HealthSlowServer = time.Second
	HealthSlowRTT    = 3 * time.Second
	HealthErrorRate  = 0.01
)

// ErrorRate adalah porsi simpan yang gagal di server.
func (h Health) ErrorRate() float64 {
	if h.Saves == 0 {
		return 0
	}
	return float64(h.Errors) / float64(h.Saves)
}

// ErrorPercent untuk tampilan (satu desimal).
func (h Health) ErrorPercent() float64 {
	return float64(int(h.ErrorRate()*1000+0.5)) / 10
}

// Verdict menyimpulkan letak masalah: "tenang" (belum ada data), "normal",
// "server" (server lambat/error), atau "jaringan" (server cepat tetapi
// perangkat lambat menjangkau server).
func (h Health) Verdict() string {
	switch {
	case h.Saves == 0:
		return "tenang"
	case h.ErrorRate() > HealthErrorRate || h.ServerP95 > HealthSlowServer:
		return "server"
	case h.RTTCount > 0 && h.RTTP95 > HealthSlowRTT:
		return "jaringan"
	}
	return "normal"
}

// Snapshot meringkas sampel sekolah dalam HealthWindow terakhir.
func (h *HealthRecorder) Snapshot(schoolID string, now time.Time) Health {
	h.mu.Lock()
	r := h.schools[schoolID]
	var samples []healthSample
	if r != nil {
		samples = append(samples, r.buf...)
	}
	h.mu.Unlock()
	var out Health
	var server, rtt []time.Duration
	from := now.Add(-HealthWindow)
	for _, s := range samples {
		if s.at.Before(from) || s.at.After(now.Add(time.Minute)) {
			continue
		}
		out.Saves++
		if s.failed {
			out.Errors++
		}
		server = append(server, s.server)
		if s.rtt > 0 {
			rtt = append(rtt, time.Duration(s.rtt)*time.Millisecond)
		}
	}
	out.ServerP50, out.ServerP95 = percentile(server, 50), percentile(server, 95)
	out.RTTP50, out.RTTP95 = percentile(rtt, 50), percentile(rtt, 95)
	out.RTTCount = len(rtt)
	return out
}

func percentile(xs []time.Duration, p int) time.Duration {
	if len(xs) == 0 {
		return 0
	}
	slices.Sort(xs)
	i := (len(xs)*p+99)/100 - 1
	return xs[max(0, min(i, len(xs)-1))]
}

// Nilai milidetik untuk tampilan.
func (h Health) ServerP95MS() int64 { return h.ServerP95.Milliseconds() }
func (h Health) ServerP50MS() int64 { return h.ServerP50.Milliseconds() }
func (h Health) RTTP95MS() int64    { return h.RTTP95.Milliseconds() }
func (h Health) RTTP50MS() int64    { return h.RTTP50.Milliseconds() }
