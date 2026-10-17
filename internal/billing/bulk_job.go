package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"akra/internal/platform/database"
)

// bulkMaxAttempts membatasi percobaan job tagihan massal.
const bulkMaxAttempts = 5

// BulkInvoiceArgs adalah argumen job tagihan massal.
type BulkInvoiceArgs struct {
	SchoolID string `json:"school_id"`
	RunID    string `json:"run_id"`
}

// Kind mengembalikan nama jenis job.
func (BulkInvoiceArgs) Kind() string { return "billing_bulk_invoice" }

// BulkInvoiceWorker membuat tagihan massal di latar belakang, per batch
// bulkBatchSize siswa per transaksi. Job idempoten: jika gagal di tengah dan
// diulang River, siswa yang sudah ditagih dilewati, dan jumlah "dibuat"
// dihitung dari invoices.bulk_run_id.
type BulkInvoiceWorker struct {
	river.WorkerDefaults[BulkInvoiceArgs]
	DB     database.DB
	Logger *slog.Logger

	// failAfterBatches, jika > 0, membuat Work gagal setelah sekian batch
	// (hanya untuk uji).
	failAfterBatches int
}

// NewBulkInvoiceWorker mengembalikan worker tagihan massal.
func NewBulkInvoiceWorker(db database.DB, logger *slog.Logger) *BulkInvoiceWorker {
	return &BulkInvoiceWorker{DB: db, Logger: logger}
}

// Work menjalankan satu proses tagihan massal.
func (w *BulkInvoiceWorker) Work(ctx context.Context, job *river.Job[BulkInvoiceArgs]) error {
	err := w.run(ctx, job.Args)
	if err != nil && job.Attempt >= job.MaxAttempts {
		w.markFailed(job.Args, err)
	}
	return err
}

func (w *BulkInvoiceWorker) run(ctx context.Context, args BulkInvoiceArgs) error {
	run, err := GetBulkRun(ctx, w.DB, args.SchoolID, args.RunID)
	if err != nil {
		return err
	}
	if run.Finished() {
		return nil
	}
	in, err := runInput(ctx, w.DB, run)
	if err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, `UPDATE bulk_invoice_runs SET status = 'running', started_at = coalesce(started_at, now()), processed_count = 0
		WHERE id = $1`, run.ID); err != nil {
		return err
	}

	// Daftar dihitung ulang setiap percobaan; yang sudah ditagih ditandai Billed.
	targets, err := eligibleEnrollments(ctx, w.DB, run.SchoolID, run.AcademicYearID, run.ClassID, run.FeeTypeID, run.Period)
	if err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, `UPDATE bulk_invoice_runs SET total_count = $2 WHERE id = $1`, run.ID, len(targets)); err != nil {
		return err
	}
	for start, batch := 0, 0; start < len(targets); start, batch = start+bulkBatchSize, batch+1 {
		if w.failAfterBatches > 0 && batch >= w.failAfterBatches {
			return errors.New("injected failure for test")
		}
		end := min(start+bulkBatchSize, len(targets))
		err := pgx.BeginFunc(ctx, w.DB, func(tx pgx.Tx) error {
			_, err := processTargets(ctx, tx, run, in, targets[start:end])
			return err
		})
		if err != nil {
			return fmt.Errorf("billing: bulk batch %d: %w", batch, err)
		}
	}
	return pgx.BeginFunc(ctx, w.DB, func(tx pgx.Tx) error {
		return finishRun(ctx, tx, run.SchoolID, run.ID)
	})
}

// runInput menyusun ulang isian dari catatan proses.
func runInput(ctx context.Context, q database.DBTX, run BulkRun) (bulkInput, error) {
	ft, err := GetFeeType(ctx, q, run.SchoolID, run.FeeTypeID)
	if err != nil {
		return bulkInput{}, err
	}
	in := bulkInput{FeeType: ft, Period: run.Period, ClassID: run.ClassID, DueDate: run.DueDate, Amount: ft.DefaultAmount}
	if run.Amount != nil {
		in.Amount, in.Override = *run.Amount, true
	}
	return in, nil
}

// markFailed mencatat kegagalan akhir (percobaan habis).
func (w *BulkInvoiceWorker) markFailed(args BulkInvoiceArgs, cause error) {
	ctx := context.Background()
	if _, err := w.DB.Exec(ctx, `UPDATE bulk_invoice_runs SET status = 'failed', error = $3, finished_at = now()
		WHERE school_id = $1 AND id = $2`, args.SchoolID, args.RunID, cause.Error()); err != nil {
		w.Logger.Error("mark bulk run failed", "run_id", args.RunID, "error", err)
	}
}
