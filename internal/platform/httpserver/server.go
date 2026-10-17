// Package httpserver menyiapkan *http.Server dengan timeout yang aman.
package httpserver

import (
	"net"
	"net/http"
	"time"

	"akra/internal/platform/config"
)

// New mengembalikan *http.Server yang mendengarkan di cfg.Port dan
// melayani request dengan handler h.
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
