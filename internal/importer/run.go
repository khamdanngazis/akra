package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
	"akra/internal/students"
)

const (
	// SyncLimit: impor dengan baris valid sebanyak ini atau kurang disimpan
	// langsung; di atasnya dijalankan sebagai job River.
	SyncLimit = 50
	// runChunkSize adalah jumlah baris per transaksi.
	runChunkSize = 100
	// runMaxAttempts membatasi percobaan job impor.
	runMaxAttempts = 5
	// ActionImportRun dicatat di audit setelah impor selesai.
	ActionImportRun = "import.run"
)

// Hasil penyimpanan baris.
const (
	ResultCreated = "baru"
	ResultUpdated = "diperbarui"
	ResultFailed  = "gagal"
)

var (
	// ErrNotRunnable: batch belum divalidasi atau sudah disimpan. Batch yang
	// selesai tidak bisa dijalankan ulang; unggah file sebagai batch baru.
	ErrNotRunnable = errors.New("importer: batch is not ready to run")
	// ErrNothingToImport: tidak ada baris valid.
	ErrNothingToImport = errors.New("importer: no valid rows")
)

// JobInserter memasukkan job River di dalam transaksi (dipenuhi
// *river.Client[pgx.Tx]).
type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// ImportStudentsArgs adalah argumen job impor siswa.
type ImportStudentsArgs struct {
	SchoolID string `json:"school_id"`
	BatchID  string `json:"batch_id"`
}

// Kind mengembalikan nama jenis job.
func (ImportStudentsArgs) Kind() string { return "import_students" }

// Run menyimpan baris valid batch ke data siswa. Batch dengan baris valid
// lebih dari SyncLimit dijalankan sebagai job (status berjalan); sisanya
// langsung diselesaikan di request ini.
func (s *Service) Run(ctx context.Context, actor school.Actor, batchID string, today time.Time) error {
	if !database.IsUUID(batchID) {
		return ErrBatchNotFound
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var status string
		var valid int
		err := tx.QueryRow(ctx, `SELECT status, coalesce(valid_rows, 0) FROM import_batches WHERE school_id = $1 AND id = $2 FOR UPDATE`,
			actor.SchoolID, batchID).Scan(&status, &valid)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBatchNotFound
		}
		if err != nil {
			return fmt.Errorf("importer: lock batch: %w", err)
		}
		switch {
		case status != StatusValidated:
			return ErrNotRunnable
		case valid == 0:
			return ErrNothingToImport
		}
		mode := "sync"
		if valid > SyncLimit {
			mode = "async"
		}
		_, err = tx.Exec(ctx, `
			UPDATE import_batches SET status = $3, mode = $4, run_by = $5, run_on = $6, started_at = now()
			WHERE school_id = $1 AND id = $2`, actor.SchoolID, batchID, StatusRunning, mode, actor.UserID, today)
		if err != nil {
			return fmt.Errorf("importer: start batch: %w", err)
		}
		if mode == "async" {
			if s.jobs == nil {
				return errors.New("importer: job client not configured")
			}
			res, err := s.jobs.InsertTx(ctx, tx, ImportStudentsArgs{SchoolID: actor.SchoolID, BatchID: batchID}, &river.InsertOpts{MaxAttempts: runMaxAttempts})
			if err != nil {
				return fmt.Errorf("importer: enqueue import job: %w", err)
			}
			_, err = tx.Exec(ctx, `UPDATE import_batches SET river_job_id = $3 WHERE school_id = $1 AND id = $2`, actor.SchoolID, batchID, res.Job.ID)
			return err
		}
		for {
			n, err := processChunk(ctx, tx, actor.SchoolID, batchID, today, 0)
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}
		}
		return finishBatch(ctx, tx, actor.SchoolID, batchID)
	})
}

// ImportStudentsWorker menjalankan impor besar di latar belakang, per
// runChunkSize baris per transaksi. Setiap baris yang selesai ditandai
// processed_at, jadi jika job gagal di tengah dan diulang River, baris yang
// sudah tersimpan tidak diproses lagi.
type ImportStudentsWorker struct {
	river.WorkerDefaults[ImportStudentsArgs]
	DB     database.DB
	Logger *slog.Logger

	// failAfterChunks, jika > 0, membuat Work gagal setelah sekian
	// transaksi (hanya untuk uji).
	failAfterChunks int
	// failAtLine, jika > 0, membuat transaksi gagal di tengah saat akan
	// memproses baris Excel tersebut (hanya untuk uji).
	failAtLine int
}

// NewImportStudentsWorker mengembalikan worker impor siswa.
func NewImportStudentsWorker(db database.DB, logger *slog.Logger) *ImportStudentsWorker {
	return &ImportStudentsWorker{DB: db, Logger: logger}
}

// Work menjalankan satu job impor.
func (w *ImportStudentsWorker) Work(ctx context.Context, job *river.Job[ImportStudentsArgs]) error {
	err := w.run(ctx, job.Args)
	if err != nil && job.Attempt >= job.MaxAttempts {
		w.markFailed(job.Args, err)
	}
	return err
}

func (w *ImportStudentsWorker) run(ctx context.Context, args ImportStudentsArgs) error {
	var (
		status string
		runOn  time.Time
	)
	err := w.DB.QueryRow(ctx, `SELECT status, coalesce(run_on, current_date) FROM import_batches WHERE school_id = $1 AND id = $2`,
		args.SchoolID, args.BatchID).Scan(&status, &runOn)
	if errors.Is(err, pgx.ErrNoRows) {
		w.Logger.Warn("import job for missing batch", "batch_id", args.BatchID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("importer: get batch: %w", err)
	}
	if status != StatusRunning {
		return nil
	}
	for chunk := 0; ; chunk++ {
		if w.failAfterChunks > 0 && chunk >= w.failAfterChunks {
			return errors.New("injected failure for test")
		}
		var n int
		err := pgx.BeginFunc(ctx, w.DB, func(tx pgx.Tx) error {
			var err error
			n, err = processChunk(ctx, tx, args.SchoolID, args.BatchID, runOn, w.failAtLine)
			return err
		})
		if err != nil {
			return fmt.Errorf("importer: chunk %d: %w", chunk, err)
		}
		if n == 0 {
			break
		}
	}
	return pgx.BeginFunc(ctx, w.DB, func(tx pgx.Tx) error {
		return finishBatch(ctx, tx, args.SchoolID, args.BatchID)
	})
}

// markFailed mencatat kegagalan akhir (percobaan habis). Baris yang sudah
// tersimpan tetap ada.
func (w *ImportStudentsWorker) markFailed(args ImportStudentsArgs, cause error) {
	w.Logger.Error("import job failed", "batch_id", args.BatchID, "error", cause)
	_, err := w.DB.Exec(context.Background(), `
		UPDATE import_batches SET status = $3, error = $4, finished_at = now()
		WHERE school_id = $1 AND id = $2 AND status = $5`,
		args.SchoolID, args.BatchID, StatusFailed, "Impor berhenti karena gangguan sistem. Baris yang sudah tersimpan tetap ada.", StatusRunning)
	if err != nil {
		w.Logger.Error("mark import batch failed", "batch_id", args.BatchID, "error", err)
	}
}

// processChunk menyimpan paling banyak runChunkSize baris valid yang belum
// diproses, lalu memperbarui hitungan batch. Mengembalikan jumlah baris yang
// diproses (0 = selesai). Jika gagal, seluruh transaksi batal sehingga tidak
// ada baris setengah tersimpan; percobaan berikutnya mengulang dari baris
// pertama yang belum ditandai processed_at.
func processChunk(ctx context.Context, tx pgx.Tx, schoolID, batchID string, runOn time.Time, failAtLine int) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, line, data FROM import_rows
		WHERE school_id = $1 AND batch_id = $2 AND status = 'valid' AND processed_at IS NULL
		ORDER BY line LIMIT $3 FOR UPDATE`, schoolID, batchID, runChunkSize)
	if err != nil {
		return 0, fmt.Errorf("importer: pending rows: %w", err)
	}
	type pending struct {
		id   string
		line int
		data ValidRow
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pending, error) {
		var p pending
		var raw []byte
		if err := r.Scan(&p.id, &p.line, &raw); err != nil {
			return p, err
		}
		return p, json.Unmarshal(raw, &p.data)
	})
	if err != nil {
		return 0, fmt.Errorf("importer: pending rows: %w", err)
	}
	for _, p := range list {
		if failAtLine > 0 && p.line == failAtLine {
			return 0, errors.New("injected failure for test")
		}
		result, msg, err := saveRow(ctx, tx, schoolID, p.data, runOn)
		if err != nil {
			return 0, fmt.Errorf("importer: line %d: %w", p.line, err)
		}
		_, err = tx.Exec(ctx, `UPDATE import_rows SET processed_at = now(), result = $2, result_error = $3 WHERE id = $1`,
			p.id, result, nullIfEmpty(msg))
		if err != nil {
			return 0, fmt.Errorf("importer: mark row: %w", err)
		}
	}
	if len(list) > 0 {
		if err := refreshCounts(ctx, tx, schoolID, batchID); err != nil {
			return 0, err
		}
	}
	return len(list), nil
}

// rowFailure adalah kegagalan yang hanya mengenai satu baris; baris ditandai
// gagal dan baris lain tetap diproses.
type rowFailure struct{ msg string }

func (e *rowFailure) Error() string { return e.msg }

// saveRow menyimpan satu baris dalam savepoint: upsert siswa berdasarkan
// NIS, penempatan kelas, dan wali. Kegagalan per baris (mis. kelas sudah
// dihapus) membatalkan savepoint dan dikembalikan sebagai result gagal.
func saveRow(ctx context.Context, tx pgx.Tx, schoolID string, d ValidRow, runOn time.Time) (result, msg string, err error) {
	err = pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
		var e error
		result, e = upsertRow(ctx, sp, schoolID, d, runOn)
		return e
	})
	var rf *rowFailure
	if errors.As(err, &rf) {
		return ResultFailed, rf.msg, nil
	}
	return result, "", err
}

func upsertRow(ctx context.Context, tx pgx.Tx, schoolID string, d ValidRow, runOn time.Time) (string, error) {
	var (
		studentID string
		inserted  bool
	)
	err := tx.QueryRow(ctx, `
		INSERT INTO students (school_id, nis, full_name, birth_date, gender)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (school_id, nis) DO UPDATE SET
			full_name  = EXCLUDED.full_name,
			birth_date = coalesce(EXCLUDED.birth_date, students.birth_date),
			gender     = coalesce(EXCLUDED.gender, students.gender),
			updated_at = now()
		RETURNING id, xmax = 0`,
		schoolID, d.NIS, d.FullName, nullIfEmpty(d.BirthDate), nullIfEmpty(d.Gender)).Scan(&studentID, &inserted)
	if err != nil {
		return "", fmt.Errorf("upsert student: %w", err)
	}

	// Kelas harus masih ada di tahun ajaran aktif saat disimpan.
	var yearID string
	err = tx.QueryRow(ctx, `
		SELECT c.academic_year_id FROM classes c
		JOIN academic_years y ON y.school_id = c.school_id AND y.id = c.academic_year_id
		WHERE c.school_id = $1 AND c.id = $2 AND y.is_active`, schoolID, d.ClassID).Scan(&yearID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &rowFailure{fmt.Sprintf("Kelas %s sudah tidak ada di tahun ajaran aktif. Unggah ulang file.", d.ClassName)}
	}
	if err != nil {
		return "", fmt.Errorf("class year: %w", err)
	}
	if _, err := students.Place(ctx, tx, schoolID, studentID, d.ClassID, yearID, runOn); err != nil {
		switch {
		case errors.Is(err, students.ErrInvalidPlacementDate):
			return "", &rowFailure{fmt.Sprintf("Siswa tidak bisa dipindahkan ke kelas %s pada tanggal ini. Atur kelasnya lewat halaman siswa.", d.ClassName)}
		case errors.Is(err, students.ErrClassNotFound):
			return "", &rowFailure{fmt.Sprintf("Kelas %s sudah tidak ada di tahun ajaran aktif. Unggah ulang file.", d.ClassName)}
		}
		return "", fmt.Errorf("place student: %w", err)
	}

	if d.GuardianName != "" {
		if err := upsertGuardian(ctx, tx, schoolID, studentID, d); err != nil {
			return "", err
		}
	}
	if inserted {
		return ResultCreated, nil
	}
	return ResultUpdated, nil
}

// upsertGuardian memakai wali yang sama jika ada (students.FindMatchingGuardian),
// atau membuat wali baru, lalu menautkannya ke siswa. Telepon dan email
// kosong tidak menghapus data wali yang sudah ada.
func upsertGuardian(ctx context.Context, tx pgx.Tx, schoolID, studentID string, d ValidRow) error {
	var guardianID string
	g, err := students.FindMatchingGuardian(ctx, tx, schoolID, studentID, d.GuardianName, d.GuardianPhone)
	switch {
	case errors.Is(err, students.ErrGuardianNotFound):
		g, err = students.CreateGuardian(ctx, tx, schoolID, students.GuardianInput{FullName: d.GuardianName, Phone: d.GuardianPhone, Email: d.GuardianEmail})
		if err != nil {
			return fmt.Errorf("create guardian: %w", err)
		}
		guardianID = g.ID
	case err != nil:
		return fmt.Errorf("find guardian: %w", err)
	default:
		guardianID = g.ID
		_, err = tx.Exec(ctx, `
			UPDATE guardians SET phone = coalesce($3, phone), email = coalesce($4, email), updated_at = now()
			WHERE school_id = $1 AND id = $2`, schoolID, guardianID, nullIfEmpty(d.GuardianPhone), nullIfEmpty(d.GuardianEmail))
		if err != nil {
			return fmt.Errorf("update guardian: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary)
		VALUES ($1, $2, $3, 'wali', NOT EXISTS (SELECT 1 FROM student_guardians WHERE student_id = $2 AND is_primary))
		ON CONFLICT (student_id, guardian_id) DO NOTHING`, schoolID, studentID, guardianID)
	if err != nil {
		return fmt.Errorf("link guardian: %w", err)
	}
	return nil
}

// refreshCounts menghitung ulang progres batch dari import_rows, jadi tetap
// benar walau job diulang.
func refreshCounts(ctx context.Context, tx pgx.Tx, schoolID, batchID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE import_batches b SET
			processed_rows = c.processed, created_count = c.created, updated_count = c.updated, failed_count = c.failed
		FROM (SELECT count(*) FILTER (WHERE processed_at IS NOT NULL) AS processed,
		             count(*) FILTER (WHERE result = 'baru') AS created,
		             count(*) FILTER (WHERE result = 'diperbarui') AS updated,
		             count(*) FILTER (WHERE result = 'gagal') AS failed
		      FROM import_rows WHERE school_id = $1 AND batch_id = $2 AND status = 'valid') c
		WHERE b.school_id = $1 AND b.id = $2`, schoolID, batchID)
	if err != nil {
		return fmt.Errorf("importer: refresh counts: %w", err)
	}
	return nil
}

// finishBatch menandai batch selesai dan mencatat satu audit import.run
// berisi ringkasan jumlah (tanpa data pribadi).
func finishBatch(ctx context.Context, tx pgx.Tx, schoolID, batchID string) error {
	if err := refreshCounts(ctx, tx, schoolID, batchID); err != nil {
		return err
	}
	var (
		runBy                                            string
		status, mode                                     string
		total, valid, errorsN, skipped, created, updated int
		failed                                           int
	)
	err := tx.QueryRow(ctx, `
		UPDATE import_batches SET status = $3, finished_at = now()
		WHERE school_id = $1 AND id = $2
		RETURNING run_by, status, mode, total_rows, coalesce(valid_rows, 0), coalesce(error_rows, 0), skipped_rows,
		          created_count, updated_count, failed_count`, schoolID, batchID, StatusDone).
		Scan(&runBy, &status, &mode, &total, &valid, &errorsN, &skipped, &created, &updated, &failed)
	if err != nil {
		return fmt.Errorf("importer: finish batch: %w", err)
	}
	return audit.Record(ctx, tx, audit.Entry{
		SchoolID: schoolID, UserID: runBy,
		Action: ActionImportRun, EntityType: "import_batch", EntityID: batchID,
		After: map[string]any{"mode": mode, "total_rows": total, "valid_rows": valid, "error_rows": errorsN, "skipped_rows": skipped,
			"created": created, "updated": updated, "failed": failed},
	})
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
