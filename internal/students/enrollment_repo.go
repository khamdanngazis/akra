package students

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

var (
	// ErrClassNotFound dikembalikan jika kelas tidak ada, milik sekolah lain,
	// atau bukan di tahun ajaran yang diminta.
	ErrClassNotFound = errors.New("students: class not found in this school and academic year")
	// ErrInvalidPlacementDate dikembalikan jika tanggal pindah sebelum
	// tanggal mulai kelas saat ini, atau setelah tahun ajaran berakhir.
	ErrInvalidPlacementDate = errors.New("students: invalid placement date")
	// ErrAlreadyEnrolled dikembalikan jika database menolak enrollment aktif
	// kedua (cadangan; Place mengunci siswa agar ini tidak terjadi).
	ErrAlreadyEnrolled = errors.New("students: student already has an active enrollment in this academic year")
)

// Enrollment adalah satu penempatan siswa di kelas pada tahun ajaran.
type Enrollment struct {
	ID             string
	SchoolID       string
	StudentID      string
	ClassID        string
	ClassName      string // untuk tampilan
	AcademicYearID string
	StartDate      time.Time
	EndDate        *time.Time // nil berarti masih aktif
	CreatedAt      time.Time
}

// Active melaporkan apakah enrollment masih berjalan.
func (e Enrollment) Active() bool { return e.EndDate == nil }

const enrollmentSelect = `
SELECT e.id, e.school_id, e.student_id, e.class_id, c.name, e.academic_year_id,
       e.start_date, e.end_date, e.created_at
FROM enrollments e
JOIN classes c ON c.id = e.class_id AND c.school_id = e.school_id`

// ActiveEnrollment mengembalikan enrollment aktif siswa pada tahun ajaran,
// atau ErrNotFound.
func ActiveEnrollment(ctx context.Context, q database.DBTX, schoolID, studentID, yearID string) (Enrollment, error) {
	if !database.IsUUID(studentID) || !database.IsUUID(yearID) {
		return Enrollment{}, ErrNotFound
	}
	rows, err := q.Query(ctx, enrollmentSelect+`
		WHERE e.school_id = $1 AND e.student_id = $2 AND e.academic_year_id = $3 AND e.end_date IS NULL`,
		schoolID, studentID, yearID)
	if err != nil {
		return Enrollment{}, mapEnrollmentError("get active enrollment", err)
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanEnrollment)
	if err != nil {
		return Enrollment{}, mapEnrollmentError("get active enrollment", err)
	}
	return e, nil
}

// ListEnrollments mengembalikan seluruh riwayat penempatan siswa, terbaru
// dulu.
func ListEnrollments(ctx context.Context, q database.DBTX, schoolID, studentID string) ([]Enrollment, error) {
	if !database.IsUUID(studentID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, enrollmentSelect+`
		WHERE e.school_id = $1 AND e.student_id = $2
		ORDER BY e.start_date DESC, e.created_at DESC`, schoolID, studentID)
	if err != nil {
		return nil, mapEnrollmentError("list enrollments", err)
	}
	list, err := pgx.CollectRows(rows, scanEnrollment)
	if err != nil {
		return nil, mapEnrollmentError("list enrollments", err)
	}
	return list, nil
}

// PlaceResult adalah hasil Place.
type PlaceResult struct {
	Enrollment Enrollment  // enrollment aktif setelah Place
	Closed     *Enrollment // enrollment lama yang ditutup, nil jika tidak ada
	Changed    bool        // false jika siswa sudah di kelas tersebut
}

// Place menempatkan siswa di kelas classID pada tahun ajaran yearID mulai
// tanggal date. Harus dipanggil di dalam transaksi tx:
//
//  1. Kunci baris siswa (FOR UPDATE) agar penempatan bersamaan untuk siswa
//     yang sama berjalan bergantian.
//  2. Pastikan kelas milik sekolah dan tahun ajaran tersebut.
//  3. Tutup enrollment aktif (end_date = date - 1), lalu buat enrollment baru
//     mulai date. Enrollment lama tetap ada sebagai riwayat.
//
// date sebelum awal tahun ajaran dimajukan ke awal tahun ajaran (persiapan
// kelas sebelum tahun dimulai). date setelah akhir tahun ajaran, atau
// sebelum tanggal mulai kelas saat ini, ditolak dengan
// ErrInvalidPlacementDate. Jika siswa sudah aktif di classID, tidak ada
// perubahan.
func Place(ctx context.Context, tx pgx.Tx, schoolID, studentID, classID, yearID string, date time.Time) (PlaceResult, error) {
	if !database.IsUUID(studentID) {
		return PlaceResult{}, ErrNotFound
	}
	if !database.IsUUID(classID) || !database.IsUUID(yearID) {
		return PlaceResult{}, ErrClassNotFound
	}

	var locked bool
	err := tx.QueryRow(ctx, `SELECT true FROM students WHERE school_id = $1 AND id = $2 FOR UPDATE`, schoolID, studentID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaceResult{}, ErrNotFound
	}
	if err != nil {
		return PlaceResult{}, mapEnrollmentError("lock student", err)
	}

	var yearStart, yearEnd time.Time
	err = tx.QueryRow(ctx, `
		SELECT y.start_date, y.end_date
		FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.school_id = c.school_id
		WHERE c.school_id = $1 AND c.id = $2 AND c.academic_year_id = $3`,
		schoolID, classID, yearID).Scan(&yearStart, &yearEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaceResult{}, ErrClassNotFound
	}
	if err != nil {
		return PlaceResult{}, mapEnrollmentError("check class", err)
	}
	if date.Before(yearStart) {
		date = yearStart
	}
	if date.After(yearEnd) {
		return PlaceResult{}, ErrInvalidPlacementDate
	}

	var res PlaceResult
	current, err := ActiveEnrollment(ctx, tx, schoolID, studentID, yearID)
	switch {
	case err == nil:
		if current.ClassID == classID {
			return PlaceResult{Enrollment: current}, nil
		}
		if date.Before(current.StartDate) {
			return PlaceResult{}, ErrInvalidPlacementDate
		}
		end := date.AddDate(0, 0, -1)
		if _, err := tx.Exec(ctx, `UPDATE enrollments SET end_date = $3 WHERE school_id = $1 AND id = $2`,
			schoolID, current.ID, end); err != nil {
			return PlaceResult{}, mapEnrollmentError("close enrollment", err)
		}
		current.EndDate = &end
		res.Closed = &current
	case errors.Is(err, ErrNotFound):
	default:
		return PlaceResult{}, err
	}

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		schoolID, studentID, classID, yearID, date).Scan(&id)
	if err != nil {
		return PlaceResult{}, mapEnrollmentError("create enrollment", err)
	}
	if res.Enrollment, err = ActiveEnrollment(ctx, tx, schoolID, studentID, yearID); err != nil {
		return PlaceResult{}, err
	}
	res.Changed = true
	return res, nil
}

func scanEnrollment(row pgx.CollectableRow) (Enrollment, error) {
	var e Enrollment
	err := row.Scan(&e.ID, &e.SchoolID, &e.StudentID, &e.ClassID, &e.ClassName, &e.AcademicYearID,
		&e.StartDate, &e.EndDate, &e.CreatedAt)
	return e, err
}

func mapEnrollmentError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "enrollments_one_active":
			return ErrAlreadyEnrolled
		case pgErr.Code == "23503" && pgErr.ConstraintName == "enrollments_class_fkey":
			return ErrClassNotFound
		case pgErr.Code == "23503" && pgErr.ConstraintName == "enrollments_student_fkey":
			return ErrNotFound
		case pgErr.Code == pgCheckViolation && pgErr.ConstraintName == "enrollments_dates_check":
			return ErrInvalidPlacementDate
		}
		return fmt.Errorf("students: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("students: %s: %w", op, err)
}
