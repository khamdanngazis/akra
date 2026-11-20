package exam

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"akra/internal/audit"
	"akra/internal/platform/database"
)

// Kumpul otomatis (T140): attempt yang lewat tenggat + tenggang tetapi
// tidak pernah dikumpulkan perangkat (HP mati, browser ditutup, baterai
// habis) dikumpulkan server dengan jawaban yang sudah tersimpan. Attempt
// yang dijeda dilewati karena waktunya berhenti.

// autoSubmitBatch adalah jumlah attempt per transaksi.
const autoSubmitBatch = 500

// AutoSubmitExpired mengumpulkan semua attempt kedaluwarsa pada waktu now
// dengan alasan "sistem". Mengembalikan jumlah attempt yang dikumpulkan.
func AutoSubmitExpired(ctx context.Context, db database.DB, now time.Time) (int, error) {
	total := 0
	for {
		n, err := autoSubmitBatchOnce(ctx, db, now)
		if err != nil {
			return total, err
		}
		total += n
		if n < autoSubmitBatch {
			return total, nil
		}
	}
}

func autoSubmitBatchOnce(ctx context.Context, db database.DB, now time.Time) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// SKIP LOCKED: attempt yang sedang dikumpulkan peserta atau dijeda
		// pengawas pada saat yang sama tidak ditunggu; putaran berikutnya
		// memeriksanya lagi.
		rows, err := tx.Query(ctx, `
			WITH due AS (
				SELECT id FROM exam_attempts
				WHERE status = 'berjalan' AND paused_at IS NULL AND deadline_at + $2::interval <= $1
				ORDER BY deadline_at
				LIMIT $3
				FOR UPDATE SKIP LOCKED)
			UPDATE exam_attempts t SET status = 'dikumpulkan', submitted_at = $1, submit_reason = 'sistem'
			FROM due WHERE t.id = due.id
			RETURNING t.school_id, t.id, t.session_id, cardinality(t.question_order),
				(SELECT count(*) FROM exam_answers a WHERE a.attempt_id = t.id AND a.value <> 'null'::jsonb)`,
			now, SaveGrace.String(), autoSubmitBatch)
		if err != nil {
			return fmt.Errorf("exam: auto submit: %w", err)
		}
		type done struct {
			school, id, session string
			questions, answered int64
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (done, error) {
			var d done
			var questions int32
			err := r.Scan(&d.school, &d.id, &d.session, &questions, &d.answered)
			d.questions = int64(questions)
			return d, err
		})
		if err != nil {
			return fmt.Errorf("exam: auto submit: %w", err)
		}
		cache := newGradeCache()
		for _, d := range list {
			gradeSafely(ctx, tx, cache, d.school, d.id, now)
			if err := audit.Record(ctx, tx, audit.Entry{SchoolID: d.school, Action: "exam.attempt_submit", EntityType: "exam_attempt",
				EntityID: d.id, After: map[string]any{"reason": SubmitBySystem, "session_id": d.session, "answered": d.answered,
					"questions": d.questions}}); err != nil {
				return err
			}
		}
		n = len(list)
		return nil
	})
	return n, err
}

// AutoSubmitArgs adalah job berkala kumpul otomatis.
type AutoSubmitArgs struct{}

// Kind mengembalikan nama job.
func (AutoSubmitArgs) Kind() string { return "exam_auto_submit" }

// AutoSubmitWorker menjalankan AutoSubmitExpired.
type AutoSubmitWorker struct {
	river.WorkerDefaults[AutoSubmitArgs]
	DB     database.DB
	Logger *slog.Logger
	Now    func() time.Time
}

// Work menjalankan satu putaran.
func (w *AutoSubmitWorker) Work(ctx context.Context, _ *river.Job[AutoSubmitArgs]) error {
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	n, err := AutoSubmitExpired(ctx, w.DB, now())
	if n > 0 {
		w.Logger.InfoContext(ctx, "expired attempts submitted", "count", n)
	}
	if err != nil {
		return err
	}
	// Penilaian susulan (T146): attempt yang belum bernilai.
	g, err := GradePending(ctx, w.DB, gradePendingBatch, now())
	if g > 0 {
		w.Logger.InfoContext(ctx, "pending attempts graded", "count", g)
	}
	return err
}

// gradePendingBatch: attempt yang dinilai susulan per putaran job.
const gradePendingBatch = 500

// AutoSubmitPeriodicJob menjalankan AutoSubmitWorker setiap menit.
func AutoSubmitPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Minute),
		func() (river.JobArgs, *river.InsertOpts) { return AutoSubmitArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
