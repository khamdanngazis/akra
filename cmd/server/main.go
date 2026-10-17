package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexedwards/scs/pgxstore"

	"akra/internal/auth"
	"akra/internal/platform/config"
	"akra/internal/platform/csrf"
	"akra/internal/platform/database"
	"akra/internal/platform/httpserver"
	"akra/internal/platform/mail"
	"akra/internal/platform/render"
	"akra/web"
)

const shutdownTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	templates, static, err := web.Load(cfg.WebDir)
	if err != nil {
		return err
	}
	if cfg.WebDir != "" {
		logger.Info("serving templates and static files from disk", "web_dir", cfg.WebDir)
	}
	rd, err := render.New(templates, logger)
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
	secure := cfg.AppEnv != config.EnvDevelopment
	sessions := auth.NewSessionManager(sessionStore, secure, logger)

	mailer, err := mail.New(mail.Config{Driver: cfg.MailDriver, From: cfg.MailFrom, ResendAPIKey: cfg.ResendAPIKey}, logger)
	if err != nil {
		return err
	}
	logger.Info("mail driver configured", "driver", cfg.MailDriver, "from", cfg.MailFrom)
	if cfg.ClientIPHeader != "" {
		logger.Info("client IP read from proxy header", "header", cfg.ClientIPHeader)
	}

	r := newRouter(routerDeps{
		pool:        pool,
		csrf:        csrf.Middleware(secure, logger),
		sessions:    sessions,
		render:      rd,
		static:      static,
		authHandler: auth.NewHandler(authSvc, sessions, rd, logger),
		resetHandler: auth.NewResetHandler(
			auth.NewResetService(auth.NewRepository(pool), hasher, mailer,
				auth.NewSessionRevoker(sessions), cfg.BaseURL, logger),
			sessions, rd, logger),
	})

	srv := httpserver.New(cfg, httpserver.ClientIP(cfg.ClientIPHeader)(r))

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
