package exam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Timer ujian (T140): rumus tenggat, aturan terlambat masuk, jeda, dan
// waktu tambahan. Tenggat disimpan di exam_attempts.deadline_at agar simpan
// jawaban dan job kumpul otomatis cukup membaca satu kolom; tenggat
// dihitung ulang setiap kali jeda selesai atau waktu ditambah.
//
// Hak menjeda dan menambah waktu (pengawas sesi, koordinator) diperiksa
// pemanggil (layar pengawas T143, pusat kendali T144).

// Aturan peserta yang terlambat masuk.
const (
	// LateFull: waktu mengerjakan dihitung dari saat peserta mulai (tetap
	// dibatasi akhir sesi).
	LateFull = "penuh"
	// LateCut: waktu dihitung dari jam mulai sesi; keterlambatan memotong
	// waktu mengerjakan.
	LateCut = "potong"
)

// LatePolicyLabel mengembalikan penjelasan aturan terlambat.
func LatePolicyLabel(p string) string {
	if p == LateCut {
		return "Dipotong sesuai keterlambatan"
	}
	return "Durasi penuh"
}

// MaxExtraMinutes adalah batas total waktu tambahan peserta atau sesi.
const MaxExtraMinutes = 240

// Error timer.
var (
	// ErrBadExtra: waktu tambahan di luar 1–60 menit atau total melebihi
	// batas.
	ErrBadExtra = errors.New("exam: invalid extra time")
	// ErrBadLatePolicy: aturan terlambat tidak dikenal.
	ErrBadLatePolicy = errors.New("exam: invalid late policy")
)

// DeadlineInput adalah bahan perhitungan tenggat satu attempt.
type DeadlineInput struct {
	StartedAt        time.Time // peserta menekan Mulai
	SessionStart     time.Time
	SessionEnd       time.Time
	LatePolicy       string
	DurationMinutes  int
	ParticipantExtra int // menit, peserta berkebutuhan khusus atau tambahan pengawas
	SessionExtra     int // menit, untuk semua peserta sesi
	Paused           time.Duration
}

// ComputeDeadline menghitung tenggat:
//
//	min(mulai + durasi + tambahan peserta, akhir sesi) + tambahan sesi + jeda
//
// mulai = StartedAt, atau jam mulai sesi bila aturan LateCut. Tambahan sesi
// dan jeda menggeser akhir sesi juga, sehingga jeda 10 menit selalu
// memperpanjang tenggat tepat 10 menit.
func ComputeDeadline(in DeadlineInput) time.Time {
	start := in.StartedAt
	if in.LatePolicy == LateCut && in.SessionStart.Before(start) {
		start = in.SessionStart
	}
	d := start.Add(time.Duration(in.DurationMinutes+in.ParticipantExtra) * time.Minute)
	if d.After(in.SessionEnd) {
		d = in.SessionEnd
	}
	return d.Add(time.Duration(in.SessionExtra) * time.Minute).Add(in.Paused)
}

// adjustAttempts menghitung ulang tenggat attempt berjalan di sesi
// sessionID (atau hanya attemptID bila diisi) yang belum kedaluwarsa.
// resume: jeda yang sedang berlangsung diakhiri dan lamanya ditambahkan.
// Mengembalikan jumlah attempt yang berubah.
func adjustAttempts(ctx context.Context, tx pgx.Tx, schoolID, sessionID, attemptID string, resume bool, now time.Time) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.started_at, t.deadline_at, t.paused_at, t.paused_ms, x.starts_at, x.ends_at, x.late_policy, x.extra_minutes,
			e.duration_minutes, p.extra_minutes
		FROM exam_attempts t
		JOIN exam_sessions x ON x.id = t.session_id
		JOIN exams e ON e.id = t.exam_id
		JOIN exam_session_participants p ON p.id = t.participant_id
		WHERE t.school_id = $1 AND t.session_id = $2 AND ($3::uuid IS NULL OR t.id = $3::uuid) AND t.status = 'berjalan'
		  AND (t.paused_at IS NOT NULL OR t.deadline_at + $4::interval > $5)
		ORDER BY t.id
		FOR UPDATE OF t`, schoolID, sessionID, nilIfEmpty(attemptID), SaveGrace.String(), now)
	if err != nil {
		return 0, fmt.Errorf("exam: load running attempts: %w", err)
	}
	type change struct {
		id       string
		deadline time.Time
		pausedAt *time.Time
		pausedMS int64
	}
	var changes []change
	for rows.Next() {
		var id string
		var in DeadlineInput
		var deadline time.Time
		var pausedAt *time.Time
		var pausedMS int64
		if err := rows.Scan(&id, &in.StartedAt, &deadline, &pausedAt, &pausedMS, &in.SessionStart, &in.SessionEnd, &in.LatePolicy,
			&in.SessionExtra, &in.DurationMinutes, &in.ParticipantExtra); err != nil {
			rows.Close()
			return 0, fmt.Errorf("exam: scan running attempt: %w", err)
		}
		if resume && pausedAt != nil {
			if now.After(*pausedAt) {
				pausedMS += now.Sub(*pausedAt).Milliseconds()
			}
			pausedAt = nil
		}
		in.Paused = time.Duration(pausedMS) * time.Millisecond
		d := ComputeDeadline(in)
		if !d.Equal(deadline) || resume {
			changes = append(changes, change{id, d, pausedAt, pausedMS})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("exam: running attempts: %w", err)
	}
	for _, c := range changes {
		if _, err := tx.Exec(ctx, `UPDATE exam_attempts SET deadline_at = $2, paused_at = $3, paused_ms = $4 WHERE id = $1`,
			c.id, c.deadline, c.pausedAt, c.pausedMS); err != nil {
			return 0, fmt.Errorf("exam: update deadline: %w", err)
		}
	}
	return len(changes), nil
}

// sessionForTimer mengunci sesi yang tidak dibatalkan milik sekolah.
func sessionForTimer(ctx context.Context, tx pgx.Tx, schoolID, sessionID string) error {
	if !database.IsUUID(sessionID) {
		return ErrNotFound
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT true FROM exam_sessions WHERE school_id = $1 AND id = $2 AND cancelled_at IS NULL FOR UPDATE`,
		schoolID, sessionID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("exam: lock session: %w", err)
	}
	return nil
}

// attemptForTimer mengunci attempt berjalan milik sekolah dan mengembalikan
// sesinya. ErrAttemptClosed bila sudah dikumpulkan atau kedaluwarsa.
func attemptForTimer(ctx context.Context, tx pgx.Tx, schoolID, attemptID string, now time.Time) (Attempt, error) {
	if !database.IsUUID(attemptID) {
		return Attempt{}, ErrNotFound
	}
	a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM exam_attempts WHERE school_id = $1 AND id = $2 FOR UPDATE`,
		schoolID, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Attempt{}, ErrNotFound
	}
	if err != nil {
		return Attempt{}, fmt.Errorf("exam: lock attempt: %w", err)
	}
	if !a.Running() || a.Expired(now) {
		return a, ErrAttemptClosed
	}
	return a, nil
}

// PauseSession menjeda sesi: semua attempt berjalan yang belum habis
// waktunya berhenti dan layar peserta terkunci; peserta yang belum mulai
// tidak bisa mulai sampai jeda selesai.
func PauseSession(ctx context.Context, db database.DB, sa StaffActor, sessionID string, now time.Time) (int, error) {
	var n int
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := sessionForTimer(ctx, tx, sa.SchoolID, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET paused_at = coalesce(paused_at, $2) WHERE id = $1`, sessionID, now); err != nil {
			return fmt.Errorf("exam: pause session: %w", err)
		}
		tag, err := tx.Exec(ctx, `UPDATE exam_attempts SET paused_at = $3
			WHERE school_id = $1 AND session_id = $2 AND status = 'berjalan' AND paused_at IS NULL AND deadline_at > $3`,
			sa.SchoolID, sessionID, now)
		if err != nil {
			return fmt.Errorf("exam: pause attempts: %w", err)
		}
		n = int(tag.RowsAffected())
		return recordAudit(ctx, tx, sa, "exam.session_pause", "exam_session", sessionID, nil, map[string]any{"attempts": n})
	})
	return n, err
}

// ResumeSession mengakhiri jeda sesi dan jeda semua attempt di sesi itu
// (termasuk yang dijeda per peserta). Lama jeda ditambahkan ke tenggat
// masing-masing attempt.
func ResumeSession(ctx context.Context, db database.DB, sa StaffActor, sessionID string, now time.Time) (int, error) {
	var n int
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := sessionForTimer(ctx, tx, sa.SchoolID, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET paused_at = NULL WHERE id = $1`, sessionID); err != nil {
			return fmt.Errorf("exam: resume session: %w", err)
		}
		var err error
		if n, err = adjustAttempts(ctx, tx, sa.SchoolID, sessionID, "", true, now); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.session_resume", "exam_session", sessionID, nil, map[string]any{"attempts": n})
	})
	return n, err
}

// PauseAttempt menjeda satu peserta (mis. perangkatnya bermasalah).
func PauseAttempt(ctx context.Context, db database.DB, sa StaffActor, attemptID string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		a, err := attemptForTimer(ctx, tx, sa.SchoolID, attemptID, now)
		if err != nil {
			return err
		}
		if a.PausedAt != nil {
			return nil
		}
		if !now.Before(a.DeadlineAt) {
			return ErrAttemptClosed
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_attempts SET paused_at = $2 WHERE id = $1`, a.ID, now); err != nil {
			return fmt.Errorf("exam: pause attempt: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.attempt_pause", "exam_attempt", a.ID, nil, nil)
	})
}

// ResumeAttempt mengakhiri jeda satu peserta; lama jeda ditambahkan ke
// tenggatnya.
func ResumeAttempt(ctx context.Context, db database.DB, sa StaffActor, attemptID string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		a, err := attemptForTimer(ctx, tx, sa.SchoolID, attemptID, now)
		if err != nil {
			return err
		}
		if a.PausedAt == nil {
			return nil
		}
		if _, err := adjustAttempts(ctx, tx, sa.SchoolID, a.SessionID, a.ID, true, now); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.attempt_resume", "exam_attempt", a.ID, nil,
			map[string]any{"paused_seconds": int(now.Sub(*a.PausedAt).Seconds())})
	})
}

// AddSessionTime menambah waktu semua peserta sesi (1–60 menit sekali
// tambah). Tenggat attempt berjalan langsung diperbarui; perangkat
// menerimanya pada balasan simpan berikutnya.
func AddSessionTime(ctx context.Context, db database.DB, sa StaffActor, sessionID string, minutes int, now time.Time) error {
	if minutes < 1 || minutes > 60 {
		return ErrBadExtra
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := sessionForTimer(ctx, tx, sa.SchoolID, sessionID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE exam_sessions SET extra_minutes = extra_minutes + $2 WHERE id = $1 AND extra_minutes + $2 <= $3`,
			sessionID, minutes, MaxExtraMinutes)
		if err != nil {
			return fmt.Errorf("exam: add session time: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrBadExtra
		}
		n, err := adjustAttempts(ctx, tx, sa.SchoolID, sessionID, "", false, now)
		if err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.session_add_time", "exam_session", sessionID, nil,
			map[string]any{"minutes": minutes, "attempts": n})
	})
}

// AddParticipantTime menambah waktu satu peserta sesi (1–60 menit sekali
// tambah). Seperti waktu tambahan peserta berkebutuhan khusus, tetap
// dibatasi akhir sesi.
func AddParticipantTime(ctx context.Context, db database.DB, sa StaffActor, participantID string, minutes int, now time.Time) error {
	if minutes < 1 || minutes > 60 {
		return ErrBadExtra
	}
	if !database.IsUUID(participantID) {
		return ErrNotFound
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var sessionID string
		var extra int
		err := tx.QueryRow(ctx, `SELECT session_id, extra_minutes FROM exam_session_participants WHERE school_id = $1 AND id = $2 FOR UPDATE`,
			sa.SchoolID, participantID).Scan(&sessionID, &extra)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("exam: lock participant: %w", err)
		}
		if extra+minutes > MaxExtraMinutes {
			return ErrBadExtra
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_session_participants SET extra_minutes = extra_minutes + $2 WHERE id = $1`,
			participantID, minutes); err != nil {
			return fmt.Errorf("exam: add participant time: %w", err)
		}
		var attemptID string
		err = tx.QueryRow(ctx, `SELECT id FROM exam_attempts WHERE participant_id = $1 AND status = 'berjalan'`, participantID).Scan(&attemptID)
		switch {
		case err == nil:
			if _, err := adjustAttempts(ctx, tx, sa.SchoolID, sessionID, attemptID, false, now); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("exam: running attempt: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.participant_add_time", "exam_session_participant", participantID,
			map[string]any{"extra_minutes": extra}, map[string]any{"extra_minutes": extra + minutes})
	})
}

// SetLatePolicy mengubah aturan terlambat sesi yang belum mulai.
func SetLatePolicy(ctx context.Context, db database.DB, sa StaffActor, examID, sessionID, policy string, now time.Time) error {
	if policy != LateFull && policy != LateCut {
		return ErrBadLatePolicy
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, examID, now, false)
		if err != nil {
			return err
		}
		s, err := GetSession(ctx, tx, sa.SchoolID, sessionID)
		if err != nil || s.ExamID != e.ID || s.CancelledAt != nil {
			return ErrNotFound
		}
		if !now.Before(s.StartsAt) {
			return ErrSessionStarted
		}
		if s.LatePolicy == policy {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET late_policy = $2 WHERE id = $1`, s.ID, policy); err != nil {
			return fmt.Errorf("exam: set late policy: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.session_late_policy", "exam_session", s.ID,
			map[string]any{"late_policy": s.LatePolicy}, map[string]any{"late_policy": policy})
	})
}
