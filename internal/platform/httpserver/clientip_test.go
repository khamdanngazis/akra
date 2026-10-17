package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, header, value, want string
	}{
		{"disabled", "", "203.0.113.9", "10.0.0.1:5555"},
		{"x-real-ip", "X-Real-IP", "203.0.113.9", "203.0.113.9:0"},
		{"rightmost forwarded-for", "X-Forwarded-For", "1.2.3.4, 203.0.113.9", "203.0.113.9:0"},
		{"ipv6", "X-Real-IP", "2001:db8::7", "[2001:db8::7]:0"},
		{"garbage keeps remote addr", "X-Real-IP", "bukan-ip", "10.0.0.1:5555"},
		{"missing header keeps remote addr", "X-Real-IP", "", "10.0.0.1:5555"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := ClientIP(tc.header)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0.0.1:5555"
			if tc.value != "" {
				req.Header.Set("X-Real-IP", tc.value)
				req.Header.Set("X-Forwarded-For", tc.value)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != tc.want {
				t.Errorf("RemoteAddr = %q, want %q", got, tc.want)
			}
		})
	}
}
