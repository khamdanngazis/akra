// Package httpserver builds an *http.Server with safe timeouts.
package httpserver

import (
	"net"
	"net/http"
	"time"

	"akra/internal/platform/config"
)

// New returns an *http.Server that listens on cfg.Port and serves
// requests with handler h.
func New(cfg config.Config, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              net.JoinHostPort("", cfg.Port),
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
