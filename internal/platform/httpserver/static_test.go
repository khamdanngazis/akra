package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestStatic(t *testing.T) {
	h := http.StripPrefix("/static/", Static(fstest.MapFS{
		"css/app.css": {Data: []byte("body{}")},
	}))

	cases := []struct {
		path       string
		wantStatus int
		wantCache  bool
	}{
		{"/static/css/app.css", http.StatusOK, true},
		{"/static/nope.css", http.StatusNotFound, false},
		{"/static/", http.StatusNotFound, false},
		{"/static/css/", http.StatusNotFound, false},
		{"/static/../embed.go", http.StatusNotFound, false},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.wantStatus {
			t.Errorf("GET %s status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
		}
		if got := rec.Header().Get("Cache-Control"); (got == StaticCacheControl) != tc.wantCache {
			t.Errorf("GET %s Cache-Control = %q", tc.path, got)
		}
	}
}
