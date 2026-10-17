package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/config"
	"akra/internal/platform/database"
	"akra/internal/platform/health"
	"akra/internal/platform/httpserver"
)

const shutdownTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

// homePlaceholder menampilkan status login dan tombol logout.
//
// TODO(T08): lindungi dengan RequireLogin. TODO(T11): ganti dengan home.html.
func homePlaceholder(sessions *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := auth.SessionUserID(r.Context(), sessions)
		if userID == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>AKRA</title>
<p>Masuk sebagai pengguna %s.</p>
<form method="post" action="/logout"><button type="submit">Keluar</button></form>`, html.EscapeString(userID))
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := database.Connect(context.Background(), cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("connected to database", "max_conns", cfg.DBMaxConns)

	hasher := auth.NewHasher(auth.PasswordParams{
		MemoryKiB:   cfg.Argon2MemoryKiB,
		Iterations:  cfg.Argon2Iterations,
		Parallelism: cfg.Argon2Parallelism,
	})
	authSvc, err := auth.NewService(auth.NewRepository(pool), hasher)
	if err != nil {
		return err
	}

	sessionStore := pgxstore.NewWithCleanupInterval(pool, auth.SessionCleanupInterval)
	defer sessionStore.StopCleanup()
	sessions := auth.NewSessionManager(sessionStore, cfg.AppEnv != config.EnvDevelopment, logger)

	r := chi.NewRouter()
	r.Get("/healthz", health.Handler)
	r.Get("/healthz/db", health.DBHandler(pool))
	r.Group(func(r chi.Router) {
		r.Use(sessions.LoadAndSave)
		auth.NewHandler(authSvc, sessions, logger).Routes(r)
		r.Get("/", homePlaceholder(sessions))
	})

	srv := httpserver.New(cfg, r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server started", "addr", srv.Addr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped")
	return nil
}
