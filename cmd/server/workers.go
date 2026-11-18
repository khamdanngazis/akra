package main

import (
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"akra/internal/admission"
	"akra/internal/billing"
	"akra/internal/exam"
	"akra/internal/exam/mathrender"
	"akra/internal/importer"
	"akra/internal/platform/clock"
	"akra/internal/platform/jobs"
	"akra/internal/platform/storage"
	"akra/internal/reports"
)

// registerWorkers mendaftarkan semua worker job latar belakang. static
// berisi katex.min.js untuk merender rumus soal impor.
func registerWorkers(workers *jobs.Workers, pool *pgxpool.Pool, store storage.Store, static fs.FS, logger *slog.Logger) error {
	math, err := mathrender.New(static)
	if err != nil {
		return err
	}
	river.AddWorker(workers, &exam.ImportWorker{DB: pool, Store: store, Math: math, Logger: logger})
	river.AddWorker(workers, &exam.AdvanceWorker{DB: pool, Logger: logger})
	river.AddWorker(workers, &exam.AutoSubmitWorker{DB: pool, Logger: logger})
	river.AddWorker(workers, &jobs.PingWorker{Logger: logger})
	river.AddWorker(workers, billing.NewBulkInvoiceWorker(pool, logger))
	river.AddWorker(workers, reports.NewExportWorker(pool, store, logger))
	river.AddWorker(workers, reports.NewCleanupWorker(pool, store, logger))
	river.AddWorker(workers, importer.NewImportStudentsWorker(pool, logger))
	river.AddWorker(workers, admission.NewRetentionWorker(pool, store, clock.Today, logger))
	return nil
}

// periodicJobs adalah job terjadwal.
func periodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{reports.CleanupPeriodicJob(), admission.RetentionPeriodicJob(), exam.AdvancePeriodicJob(),
		exam.AutoSubmitPeriodicJob()}
}
