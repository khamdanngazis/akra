// Package school mengelola data tingkat sekolah: profil dan tahun ajaran.
package school

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
	// ErrNotFound dikembalikan jika data tidak ada atau milik sekolah lain.
	ErrNotFound = errors.New("school: not found")
	// ErrDuplicateName dikembalikan jika nama tahun ajaran sudah dipakai di
	// sekolah yang sama.
	ErrDuplicateName = errors.New("school: academic year name already exists")
	// ErrInvalidDates dikembalikan jika end_date tidak setelah start_date.
	ErrInvalidDates = errors.New("school: end date must be after start date")
)

// Kode error PostgreSQL yang dipetakan ke error domain.
const (
	pgUniqueViolation     = "23505"
	pgCheckViolation      = "23514"
	pgInvalidTextRepr     = "22P02"
	constraintUniqueName  = "academic_years_school_id_name_key"
	constraintDatesCheck  = "academic_years_dates_check"
	academicYearSelectCol = `id, school_id, name, start_date, end_date, is_active, created_at, updated_at`
)

// AcademicYear adalah satu tahun ajaran milik sebuah sekolah.
type AcademicYear struct {
	ID        string
	SchoolID  string
	Name      string // mis. "2026/2027"
	StartDate time.Time
	EndDate   time.Time
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AcademicYearInput adalah data yang bisa diisi saat membuat atau mengubah
// tahun ajaran. Validasi input pengguna dilakukan di service (T17); database
// tetap menolak tanggal terbalik dan nama ganda.
type AcademicYearInput struct {
	Name      string
	StartDate time.Time
	EndDate   time.Time
}

// Semua fungsi di bawah selalu memfilter dengan schoolID, sehingga ID milik
// sekolah lain diperlakukan seperti tidak ada (ErrNotFound).

// ListAcademicYears mengembalikan tahun ajaran sekolah, terbaru dulu.
func ListAcademicYears(ctx context.Context, q database.DBTX, schoolID string) ([]AcademicYear, error) {
	rows, err := q.Query(ctx, `SELECT `+academicYearSelectCol+` FROM academic_years
		WHERE school_id = $1 ORDER BY start_date DESC, name DESC`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("school: list academic years: %w", err)
	}
	years, err := pgx.CollectRows(rows, scanAcademicYear)
	if err != nil {
		return nil, fmt.Errorf("school: list academic years: %w", err)
	}
	return years, nil
}

// GetAcademicYear mengembalikan satu tahun ajaran milik schoolID.
func GetAcademicYear(ctx context.Context, q database.DBTX, schoolID, id string) (AcademicYear, error) {
	rows, err := q.Query(ctx, `SELECT `+academicYearSelectCol+` FROM academic_years
		WHERE school_id = $1 AND id = $2`, schoolID, id)
	if err != nil {
		return AcademicYear{}, mapError("get academic year", err)
	}
	y, err := pgx.CollectExactlyOneRow(rows, scanAcademicYear)
	if err != nil {
		return AcademicYear{}, mapError("get academic year", err)
	}
	return y, nil
}

// GetActiveAcademicYear mengembalikan tahun ajaran aktif sekolah, atau
// ErrNotFound jika belum ada yang aktif.
func GetActiveAcademicYear(ctx context.Context, q database.DBTX, schoolID string) (AcademicYear, error) {
	rows, err := q.Query(ctx, `SELECT `+academicYearSelectCol+` FROM academic_years
		WHERE school_id = $1 AND is_active`, schoolID)
	if err != nil {
		return AcademicYear{}, mapError("get active academic year", err)
	}
	y, err := pgx.CollectExactlyOneRow(rows, scanAcademicYear)
	if err != nil {
		return AcademicYear{}, mapError("get active academic year", err)
	}
	return y, nil
}

// CreateAcademicYear membuat tahun ajaran nonaktif.
func CreateAcademicYear(ctx context.Context, q database.DBTX, schoolID string, in AcademicYearInput) (AcademicYear, error) {
	rows, err := q.Query(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date)
		VALUES ($1, $2, $3, $4) RETURNING `+academicYearSelectCol,
		schoolID, in.Name, in.StartDate, in.EndDate)
	if err != nil {
		return AcademicYear{}, mapError("create academic year", err)
	}
	y, err := pgx.CollectExactlyOneRow(rows, scanAcademicYear)
	if err != nil {
		return AcademicYear{}, mapError("create academic year", err)
	}
	return y, nil
}

// UpdateAcademicYear mengubah nama dan tanggal. Status aktif hanya berubah
// lewat SetActive.
func UpdateAcademicYear(ctx context.Context, q database.DBTX, schoolID, id string, in AcademicYearInput) (AcademicYear, error) {
	rows, err := q.Query(ctx, `UPDATE academic_years
		SET name = $3, start_date = $4, end_date = $5, updated_at = now()
		WHERE school_id = $1 AND id = $2
		RETURNING `+academicYearSelectCol,
		schoolID, id, in.Name, in.StartDate, in.EndDate)
	if err != nil {
		return AcademicYear{}, mapError("update academic year", err)
	}
	y, err := pgx.CollectExactlyOneRow(rows, scanAcademicYear)
	if err != nil {
		return AcademicYear{}, mapError("update academic year", err)
	}
	return y, nil
}

// SetActive menjadikan yearID satu-satunya tahun ajaran aktif di schoolID.
// Harus dipanggil di dalam transaksi tx agar langkah-langkahnya atomik.
//
// Urutan:
//  1. Kunci semua tahun ajaran sekolah (FOR UPDATE), sehingga dua aktivasi
//     bersamaan di sekolah yang sama berjalan bergantian, sekaligus
//     memastikan yearID milik sekolah ini.
//  2. Nonaktifkan tahun aktif lain lebih dulu, agar unique index
//     academic_years_one_active tidak terlanggar di tengah transaksi.
//  3. Aktifkan yearID.
//
// Mengaktifkan tahun yang sudah aktif tidak mengubah apa pun.
func SetActive(ctx context.Context, tx pgx.Tx, schoolID, yearID string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM academic_years WHERE school_id = $1 ORDER BY id FOR UPDATE`, schoolID)
	if err != nil {
		return mapError("lock academic years", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return mapError("lock academic years", err)
	}
	found := false
	for _, id := range ids {
		if id == yearID {
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `UPDATE academic_years SET is_active = false, updated_at = now()
		WHERE school_id = $1 AND is_active AND id <> $2`, schoolID, yearID); err != nil {
		return mapError("deactivate academic years", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE academic_years SET is_active = true, updated_at = now()
		WHERE school_id = $1 AND id = $2 AND NOT is_active`, schoolID, yearID); err != nil {
		return mapError("activate academic year", err)
	}
	return nil
}

func scanAcademicYear(row pgx.CollectableRow) (AcademicYear, error) {
	var y AcademicYear
	err := row.Scan(&y.ID, &y.SchoolID, &y.Name, &y.StartDate, &y.EndDate, &y.IsActive, &y.CreatedAt, &y.UpdatedAt)
	return y, err
}

// mapError mengubah error database menjadi error domain.
func mapError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgInvalidTextRepr:
			// ID dari URL yang bukan UUID valid.
			return ErrNotFound
		case pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == constraintUniqueName:
			return ErrDuplicateName
		case pgErr.Code == pgCheckViolation && pgErr.ConstraintName == constraintDatesCheck:
			return ErrInvalidDates
		}
	}
	return fmt.Errorf("school: %s: %w", op, err)
}
