// Package health provides health check endpoints.
package health

import (
	"context"
	"net/http"
	"time"
)

const dbPingTimeout = 2 * time.Second

// Pinger is anything whose connectivity can be checked, e.g. *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler responds 200 with body "ok".
func Handler(w http.ResponseWriter, _ *http.Request) {
	writeText(w, http.StatusOK, "ok")
}

// DBHandler responds 200 if db can be pinged, 503 otherwise.
func DBHandler(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), dbPingTimeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			writeText(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		writeText(w, http.StatusOK, "ok")
	}
}

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
