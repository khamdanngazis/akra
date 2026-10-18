package main

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"akra/internal/billing"
	"akra/internal/importer"
	"akra/internal/platform/jobs"
	"akra/internal/platform/storage"
	"akra/internal/reports"
)

// registerWorkers mendaftarkan semua worker job latar belakang.
func registerWorkers(workers *jobs.Workers, pool *pgxpool.Pool, store storage.Store, logger *slog.Logger) {
	river.AddWorker(workers, &jobs.PingWorker{Logger: logger})
	river.AddWorker(workers, billing.NewBulkInvoiceWorker(pool, logger))
	river.AddWorker(workers, reports.NewExportWorker(pool, store, logger))
	river.AddWorker(workers, reports.NewCleanupWorker(pool, store, logger))
	river.AddWorker(workers, importer.NewImportStudentsWorker(pool, logger))
}

// periodicJobs adalah job terjadwal.
func periodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{reports.CleanupPeriodicJob()}
}
