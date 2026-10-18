// Package jobs menjalankan antrean pekerjaan latar belakang dengan River di
// Postgres yang sama dengan aplikasi (tanpa Redis). Client (yang memasukkan
// job) dan worker (yang menjalankan job) berada dalam proses yang sama.
//
// Catatan koneksi: River memakai koneksi dari pool aplikasi, ditambah satu
// koneksi untuk LISTEN/NOTIFY. DB_MAX_CONNS harus cukup untuk request HTTP
// sekaligus RIVER_WORKERS.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Client adalah client River yang dipakai aplikasi.
type Client = river.Client[pgx.Tx]

// Workers adalah daftar worker yang didaftarkan sebelum client dibuat.
type Workers = river.Workers

// NewWorkers mengembalikan daftar worker kosong.
func NewWorkers() *Workers { return river.NewWorkers() }

// Config mengatur client River.
type Config struct {
	// Workers adalah jumlah job yang dijalankan bersamaan (RIVER_WORKERS).
	Workers int
	// JobTimeout membatasi lama satu job berjalan.
	JobTimeout time.Duration
	// PeriodicJobs dijalankan terjadwal (mis. pembersihan file laporan).
	PeriodicJobs []*river.PeriodicJob
}

// DefaultJobTimeout membatasi lama satu job (mis. tagihan massal ratusan
// siswa). Job yang melewati batas dibatalkan lalu dicoba ulang.
const DefaultJobTimeout = 10 * time.Minute

// NewClient membuat client River di atas pool aplikasi. workers boleh nil
// untuk client yang hanya memasukkan job (mis. di uji).
func NewClient(pool *pgxpool.Pool, workers *Workers, cfg Config, logger *slog.Logger) (*Client, error) {
	if cfg.JobTimeout == 0 {
		cfg.JobTimeout = DefaultJobTimeout
	}
	rc := &river.Config{
		Logger:     logger,
		JobTimeout: cfg.JobTimeout,
	}
	if workers != nil {
		if cfg.Workers < 1 {
			return nil, fmt.Errorf("jobs: workers must be at least 1, got %d", cfg.Workers)
		}
		rc.Workers = workers
		rc.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: cfg.Workers}}
		rc.PeriodicJobs = cfg.PeriodicJobs
	}
	client, err := river.NewClient(riverpgxv5.New(pool), rc)
	if err != nil {
		return nil, fmt.Errorf("jobs: create river client: %w", err)
	}
	return client, nil
}

// Shutdown menghentikan client dengan rapi: menunggu job yang sedang
// berjalan selesai sampai grace habis, lalu membatalkan sisanya. Job yang
// dibatalkan menerima context yang dibatalkan, gagal, dan dicoba ulang oleh
// River (tidak hilang tanpa tanda).
func Shutdown(client *Client, grace time.Duration, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := client.Stop(ctx)
	if err == nil {
		return
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		logger.Error("stop river client", "error", err)
	}
	logger.Warn("river jobs still running after grace period; cancelling them for retry")
	cancelCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := client.StopAndCancel(cancelCtx); err != nil {
		logger.Error("stop and cancel river client", "error", err)
	}
}
