package main

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"akra/internal/platform/config"
)

// startPprof membuka profiler Go di PPROF_ADDR (mis. 127.0.0.1:6060) untuk
// uji beban dan diagnosis (T152). Mati bila kosong, dan selalu mati di
// production; alamatnya terpisah dari server aplikasi dan sebaiknya hanya
// localhost.
func startPprof(logger *slog.Logger, cfg config.Config) {
	addr := os.Getenv("PPROF_ADDR")
	if addr == "" {
		return
	}
	if cfg.AppEnv == config.EnvProduction {
		logger.Warn("PPROF_ADDR ignored in production")
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("pprof listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil {
			logger.Error("pprof", "error", err)
		}
	}()
}
