package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
	})
	for _, hsts := range []bool{false, true} {
		rec := httptest.NewRecorder()
		SecurityHeaders(hsts)(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		h := rec.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" ||
			h.Get("Content-Security-Policy") != "frame-ancestors 'none'" {
			t.Fatalf("missing headers: %v", h)
		}
		if got := h.Get("Referrer-Policy"); got != "no-referrer" {
			t.Fatalf("handler override lost: %q", got)
		}
		if (h.Get("Strict-Transport-Security") != "") != hsts {
			t.Fatalf("hsts=%v: got %q", hsts, h.Get("Strict-Transport-Security"))
		}
	}
}
