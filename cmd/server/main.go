package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alexedwards/scs/pgxstore"

	"akra/internal/auth"
	"akra/internal/exam"
	"akra/internal/ops"
	"akra/internal/platform/config"
	"akra/internal/platform/database"
	"akra/internal/platform/httpserver"
	"akra/internal/platform/jobs"
	"akra/internal/platform/mail"
	"akra/internal/platform/metrics"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/internal/wilayah"
	"akra/web"
)

// jobShutdownGrace adalah waktu tunggu job yang sedang berjalan saat server
// berhenti. Setelah itu job dibatalkan dan dicoba ulang oleh River.
const jobShutdownGrace = 20 * time.Second

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
	rd, err := render.New(templates, static, logger)
	if err != nil {
		return err
	}
	rd.SetMapsKey(cfg.GoogleMapsAPIKey)
	if cfg.GoogleMapsAPIKey != "" {
		logger.Info("google maps enabled for home location picker")
	}

	pool, err := database.Connect(context.Background(), cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("connected to database", "max_conns", cfg.DBMaxConns)

	// Data wilayah (T110) diimpor di latar belakang bila versinya berubah;
	// selama belum ada, isian alamat jatuh ke isian manual.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		loaded, err := wilayah.EnsureLoaded(ctx, pool)
		switch {
		case err != nil:
			logger.Error("load wilayah data", "error", err)
		case loaded:
			logger.Info("wilayah data imported", "version", wilayah.DataVersion())
		}
	}()

	hasher := auth.NewHasher(auth.PasswordParams{
		MemoryKiB:   cfg.Argon2MemoryKiB,
		Iterations:  cfg.Argon2Iterations,
		Parallelism: cfg.Argon2Parallelism,
	})
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

	keys, err := cfg.Keyring()
	if err != nil {
		return err
	}
	if cfg.PIIEncKeys == "" {
		logger.Warn("using development PII keys; never use this outside development")
	}
	logger.Info("PII encryption configured", "active_key_version", keys.ActiveVersion())

	examTokens := exam.DevTokenSigner()
	if cfg.ExamTokenKeys != "" {
		if examTokens, err = exam.ParseTokenSigner(cfg.ExamTokenKeys); err != nil {
			return err
		}
	} else {
		logger.Warn("using development exam token key; never use this outside development")
	}

	store, err := storage.New(cfg)
	if err != nil {
		return err
	}
	logger.Info("storage driver configured", "driver", cfg.StorageDriver)

	// Antrean job (River) di Postgres yang sama. Worker didaftarkan sebelum
	// client dibuat; client berhenti setelah server HTTP berhenti dan sebelum
	// pool ditutup (urutan defer).
	workers := jobs.NewWorkers()
	if err := registerWorkers(workers, pool, store, static, logger); err != nil {
		return err
	}
	jobClient, err := jobs.NewClient(pool, workers, jobs.Config{Workers: cfg.RiverWorkers, PeriodicJobs: periodicJobs()}, logger)
	if err != nil {
		return err
	}
	if err := jobClient.Start(context.Background()); err != nil {
		return fmt.Errorf("start river client: %w", err)
	}
	defer jobs.Shutdown(jobClient, jobShutdownGrace, logger)
	logger.Info("river job workers started", "workers", cfg.RiverWorkers)

	// Keandalan (T145): metrics, alert, dan halaman status.
	reg := metrics.New()
	registerGauges(reg, pool, logger)
	notifier := alertNotifier(cfg, mailer)
	if notifier == nil {
		logger.Warn("no alert channel configured; alerts are only logged")
	}
	if cfg.ExamFaultSavePercent > 0 {
		logger.Warn("simulated save failures enabled", "percent", cfg.ExamFaultSavePercent)
	}
	draining := new(atomic.Bool)

	r, err := newApp(appDeps{
		db: pool, pinger: pool, sessions: sessions, render: rd, static: static,
		mailer: mailer, hasher: hasher, secure: secure, baseURL: cfg.BaseURL, logger: logger,
		jobs: jobClient, dev: cfg.AppEnv == config.EnvDevelopment, store: store, pii: keys,
		examTokens: examTokens,
		metrics:    reg, saveFault: cfg.ExamFaultSavePercent, draining: draining,
		ops: opsDeps{token: cfg.OpsToken, env: cfg.AppEnv, notifier: notifier},
	})
	if err != nil {
		return err
	}

	srv := httpserver.New(cfg, httpserver.ClientIP(cfg.ClientIPHeader)(r))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	monitorCtx, stopMonitor := context.WithCancel(context.Background())
	defer stopMonitor()
	monitor := &ops.Monitor{DB: pool, Metrics: reg, Notifier: notifier, Env: cfg.AppEnv, BaseURL: cfg.BaseURL, Logger: logger,
		Pool: func() ops.PoolStat {
			s := pool.Stat()
			return ops.PoolStat{Acquired: s.AcquiredConns(), Max: s.MaxConns()}
		}}
	go monitor.Run(monitorCtx)

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

	// Berhenti bertahap (T145): /healthz 503 agar load balancer berhenti
	// mengirim request, tunggu ShutdownDrain, lalu Shutdown menunggu
	// request yang sedang berjalan (simpan jawaban) selesai. Klien yang
	// terputus mengulang dari antrian perangkat (T139).
	draining.Store(true)
	stopMonitor()
	logger.Info("shutting down server", "drain", cfg.ShutdownDrain.String(), "timeout", cfg.ShutdownTimeout.String())
	if cfg.ShutdownDrain > 0 {
		time.Sleep(cfg.ShutdownDrain)
	}
	srv.SetKeepAlivesEnabled(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped")
	return nil
}
