package httpserver

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitBody(t *testing.T) {
	var readErr error
	h := LimitBody(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789")))
	if readErr != nil {
		t.Errorf("body at limit: %v", readErr)
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789x")))
	var mbe *http.MaxBytesError
	if !errors.As(readErr, &mbe) {
		t.Errorf("body over limit: error = %v, want MaxBytesError", readErr)
	}
}
