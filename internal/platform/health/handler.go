// Package health menyediakan endpoint health check.
package health

import "net/http"

// Handler membalas 200 dengan body "ok".
func Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
