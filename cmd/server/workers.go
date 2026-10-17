package main

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"akra/internal/billing"
	"akra/internal/platform/jobs"
)

// registerWorkers mendaftarkan semua worker job latar belakang.
func registerWorkers(workers *jobs.Workers, pool *pgxpool.Pool, logger *slog.Logger) {
	river.AddWorker(workers, &jobs.PingWorker{Logger: logger})
	river.AddWorker(workers, billing.NewBulkInvoiceWorker(pool, logger))
}
