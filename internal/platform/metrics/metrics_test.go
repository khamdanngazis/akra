package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecentWindow(t *testing.T) {
	r := New()
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	r.SetClock(func() time.Time { return now })
	for i := 0; i < 95; i++ {
		r.Observe("simpan", 20*time.Millisecond, false)
	}
	for i := 0; i < 5; i++ {
		r.Observe("simpan", 2*time.Second, true)
	}
	w := r.Recent("simpan", 2*time.Minute)
	if w.Count != 100 || w.Errors != 5 || w.ErrorRate() != 0.05 {
		t.Fatalf("window = %+v", w)
	}
	if w.P95 != 25*time.Millisecond {
		t.Errorf("p95 = %v, want 25ms", w.P95)
	}
	r.Observe("simpan", 3*time.Second, false)
	if w := r.Recent("simpan", time.Minute); w.P95 != 2500*time.Millisecond {
		t.Errorf("p95 with slow tail = %v", w.P95)
	}
	// Tiga menit kemudian: jendela 2 menit kosong, jendela 10 menit tidak.
	now = now.Add(3 * time.Minute)
	if w := r.Recent("simpan", 2*time.Minute); w.Count != 0 {
		t.Errorf("old samples in 2m window: %+v", w)
	}
	if w := r.Recent("simpan", 10*time.Minute); w.Count != 101 {
		t.Errorf("10m window = %+v", w)
	}
	// Setelah 10 menit ember dipakai ulang.
	now = now.Add(10 * time.Minute)
	r.Observe("simpan", time.Millisecond, false)
	if w := r.Recent("simpan", 10*time.Minute); w.Count != 1 {
		t.Errorf("ring reuse = %+v", w)
	}
}

func TestTrackAndText(t *testing.T) {
	r := New()
	ok := r.Track("kumpul", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusSeeOther) }))
	bad := r.Track("kumpul", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "x", http.StatusInternalServerError) }))
	implicit := r.Track("kumpul", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, h := range []http.Handler{ok, bad, implicit} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	}
	func() {
		defer func() { _ = recover() }()
		r.Track("kumpul", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })).
			ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	}()
	if w := r.Recent("kumpul", time.Minute); w.Count != 4 || w.Errors != 2 {
		t.Fatalf("tracked = %+v, want 4 requests 2 errors", w)
	}
	r.Gauge("akra_db_connections", "Pool connections.", func() []Sample {
		return []Sample{{Labels: map[string]string{"state": "acquired"}, Value: 3}}
	})
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`akra_request_duration_seconds_count{endpoint="kumpul"} 4`,
		`akra_request_errors_total{endpoint="kumpul"} 2`,
		`akra_request_duration_seconds_bucket{endpoint="kumpul",le="+Inf"} 4`,
		`akra_db_connections{state="acquired"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q:\n%s", want, body)
		}
	}
}
