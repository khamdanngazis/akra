package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/exam"
	"akra/internal/ops"
	"akra/internal/platform/config"
	"akra/internal/platform/mail"
	"akra/internal/platform/metrics"
)

// registerGauges mendaftarkan gauge /metrics (T145): pool koneksi
// database, attempt berjalan per sekolah, dan tunggakan kumpul otomatis.
func registerGauges(reg *metrics.Registry, pool *pgxpool.Pool, logger *slog.Logger) {
	reg.Gauge("akra_db_connections", "Database pool connections by state.", func() []metrics.Sample {
		s := pool.Stat()
		return []metrics.Sample{
			{Labels: map[string]string{"state": "acquired"}, Value: float64(s.AcquiredConns())},
			{Labels: map[string]string{"state": "idle"}, Value: float64(s.IdleConns())},
			{Labels: map[string]string{"state": "max"}, Value: float64(s.MaxConns())},
		}
	})
	reg.Gauge("akra_db_acquire_wait_seconds_total", "Total time spent waiting for a database connection.", func() []metrics.Sample {
		return []metrics.Sample{{Value: pool.Stat().AcquireDuration().Seconds()}}
	})
	reg.Gauge("akra_exam_running_attempts", "Running exam attempts per school.", func() []metrics.Sample {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		m, err := exam.RunningAttemptsBySchool(ctx, pool, time.Now())
		if err != nil {
			logger.Error("metrics running attempts", "error", err)
			return nil
		}
		out := make([]metrics.Sample, 0, len(m))
		for code, n := range m {
			out = append(out, metrics.Sample{Labels: map[string]string{"school": code}, Value: float64(n)})
		}
		return out
	})
	reg.Gauge("akra_exam_autosubmit_backlog", "Attempts past deadline not yet auto-submitted (count, oldest delay seconds).", func() []metrics.Sample {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		n, oldest, err := exam.AutoSubmitBacklog(ctx, pool, time.Now())
		if err != nil {
			logger.Error("metrics autosubmit backlog", "error", err)
			return nil
		}
		return []metrics.Sample{
			{Labels: map[string]string{"value": "count"}, Value: float64(n)},
			{Labels: map[string]string{"value": "oldest_seconds"}, Value: oldest.Seconds()},
		}
	})
}

// alertNotifier menyusun kanal alert dari konfigurasi: Telegram dan/atau
// email. Nil bila tidak ada (alert hanya ke log).
func alertNotifier(cfg config.Config, mailer mail.Mailer) ops.Notifier {
	var m ops.Multi
	if cfg.AlertTelegramToken != "" {
		m = append(m, ops.Telegram{Token: cfg.AlertTelegramToken, ChatID: cfg.AlertTelegramChatID})
	}
	if cfg.AlertEmail != "" {
		m = append(m, ops.Email{Mailer: mailer, To: cfg.AlertEmail})
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
