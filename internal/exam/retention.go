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

// Masa simpan data ujian (T153, UU PDP). Attempt, jawaban, dan kejadian
// disimpan sampai akhir tahun ajaran berikutnya setelah tahun ajaran ujian
// (bila ujian tanpa tahun ajaran, mis. latihan bawaan: 365 hari setelah
// sesi berakhir). Setelah itu per sesi:
//
//   - jawaban (termasuk komentar esai), kejadian, kode akses, dan catatan
//     kirim-rapor dihapus;
//   - peserta dilepas dari identitas siswa/pendaftar;
//   - nilai dan durasi attempt tetap, tanpa identitas, untuk statistik.
//
// Nilai yang sudah masuk rapor mengikuti aturan rapor (tabel grades tidak
// disentuh). Tes seleksi mengikuti masa simpan pendaftar (T113): data
// ujiannya dihapus saat pendaftar dianonimkan (EraseApplicantExamData).

// retentionDays adalah masa simpan untuk ujian tanpa tahun ajaran.
const retentionDays = 365

// dueSessions mengembalikan sesi yang masa simpannya sudah lewat pada today.
func dueSessions(ctx context.Context, q database.DBTX, today time.Time, limit int) ([][2]string, error) {
	rows, err := q.Query(ctx, `
		SELECT x.school_id::text, x.id::text FROM exam_sessions x
		JOIN exams e ON e.id = x.exam_id AND e.purpose <> 'seleksi'
		LEFT JOIN academic_years y ON y.id = e.academic_year_id AND y.school_id = e.school_id
		WHERE x.data_anonymized_at IS NULL AND x.ends_at < $1
		  AND coalesce(
		        (SELECT min(y2.end_date) FROM academic_years y2 WHERE y2.school_id = e.school_id AND y2.start_date > y.end_date),
		        y.end_date + $3::int,
		        (x.ends_at AT TIME ZONE 'UTC')::date + $3::int) < $1::date
		ORDER BY x.ends_at LIMIT $2`, today, limit, retentionDays)
	if err != nil {
		return nil, fmt.Errorf("exam: due sessions: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) ([2]string, error) {
		var s [2]string
		return s, r.Scan(&s[0], &s[1])
	})
}

// eraseParticipants menghapus data ujian pribadi peserta yang dipilih
// subquery sel (kolom id peserta) dan melepas identitasnya.
func eraseParticipants(ctx context.Context, tx pgx.Tx, sel string, args ...any) (int, error) {
	steps := []string{
		`DELETE FROM exam_answers WHERE attempt_id IN (SELECT t.id FROM exam_attempts t WHERE t.participant_id IN (` + sel + `))`,
		`DELETE FROM exam_attempt_events WHERE attempt_id IN (SELECT t.id FROM exam_attempts t WHERE t.participant_id IN (` + sel + `))`,
		`DELETE FROM exam_grade_sent g USING exam_session_participants p
			WHERE p.id IN (` + sel + `) AND g.student_id = p.student_id AND g.exam_id = (SELECT exam_id FROM exam_sessions x WHERE x.id = p.session_id)`,
		`DELETE FROM student_credentials c USING exam_session_participants p
			WHERE p.id IN (` + sel + `) AND c.exam_session_id = p.session_id
			  AND c.student_id IS NOT DISTINCT FROM p.student_id AND c.applicant_id IS NOT DISTINCT FROM p.applicant_id`,
		// Perangkat diganti nilai nol: id perangkat bisa menautkan ke orang.
		`UPDATE exam_attempts SET device_id = '00000000-0000-0000-0000-000000000000' WHERE participant_id IN (` + sel + `)`,
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s, args...); err != nil {
			return 0, fmt.Errorf("exam: erase participant data: %w", err)
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE exam_session_participants SET student_id = NULL, applicant_id = NULL, participant_no = '', anonymized_at = now()
		WHERE id IN (`+sel+`) AND anonymized_at IS NULL`, args...)
	if err != nil {
		return 0, fmt.Errorf("exam: unlink participants: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// AnonymizeSession menjalankan masa simpan untuk satu sesi. Aman diulang.
func AnonymizeSession(ctx context.Context, db database.DB, schoolID, sessionID string, now time.Time) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var done *time.Time
		if err := tx.QueryRow(ctx, `SELECT data_anonymized_at FROM exam_sessions WHERE school_id = $1 AND id = $2 FOR UPDATE`, schoolID, sessionID).
			Scan(&done); err != nil {
			return fmt.Errorf("exam: lock session: %w", err)
		}
		if done != nil {
			return nil
		}
		var err error
		if n, err = eraseParticipants(ctx, tx, `SELECT id FROM exam_session_participants WHERE school_id = $1 AND session_id = $2`, schoolID, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET data_anonymized_at = $3 WHERE school_id = $1 AND id = $2`, schoolID, sessionID, now); err != nil {
			return fmt.Errorf("exam: mark session anonymized: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: schoolID, Action: "exam.data_retention", EntityType: "exam_session", EntityID: sessionID,
			After: map[string]any{"participants": n}})
	})
	return n, err
}

// EraseApplicantExamData menghapus data ujian pendaftar (jawaban, kejadian,
// kode akses) dan melepas identitasnya dari peserta tes seleksi. Dipanggil
// saat pendaftar dianonimkan (T113); nilai tes tetap.
func EraseApplicantExamData(ctx context.Context, tx pgx.Tx, schoolID, applicantID string) error {
	if _, err := eraseParticipants(ctx, tx, `SELECT id FROM exam_session_participants WHERE school_id = $1 AND applicant_id = $2`,
		schoolID, applicantID); err != nil {
		return err
	}
	// Kode akses tanpa sesi (mis. sudah dibatalkan).
	if _, err := tx.Exec(ctx, `DELETE FROM student_credentials WHERE school_id = $1 AND applicant_id = $2`, schoolID, applicantID); err != nil {
		return fmt.Errorf("exam: erase applicant credentials: %w", err)
	}
	return nil
}

// RetentionArgs adalah job harian masa simpan data ujian.
type RetentionArgs struct{}

// Kind mengembalikan jenis job.
func (RetentionArgs) Kind() string { return "exam_retention" }

// RetentionWorker menjalankan masa simpan data ujian.
type RetentionWorker struct {
	river.WorkerDefaults[RetentionArgs]
	DB     database.DB
	Logger *slog.Logger
	Now    func() time.Time
}

// RetentionPeriodicJob menjalankan RetentionWorker sehari sekali.
func RetentionPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return RetentionArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// Work memproses semua sesi yang jatuh tempo, per batch; satu sesi gagal
// tidak menghentikan yang lain.
func (w *RetentionWorker) Work(ctx context.Context, _ *river.Job[RetentionArgs]) error {
	_, err := w.Run(ctx)
	return err
}

// Run menjalankan satu putaran dan mengembalikan jumlah sesi yang diproses.
func (w *RetentionWorker) Run(ctx context.Context) (int, error) {
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	done := 0
	for {
		due, err := dueSessions(ctx, w.DB, now(), 200)
		if err != nil {
			return done, err
		}
		progressed := false
		for _, s := range due {
			if _, err := AnonymizeSession(ctx, w.DB, s[0], s[1], now()); err != nil {
				w.Logger.ErrorContext(ctx, "exam retention", "session_id", s[1], "error", err)
				continue
			}
			done++
			progressed = true
		}
		if !progressed || len(due) < 200 {
			break
		}
	}
	if done > 0 {
		w.Logger.InfoContext(ctx, "exam data anonymized after retention period", "sessions", done)
	}
	return done, nil
}
