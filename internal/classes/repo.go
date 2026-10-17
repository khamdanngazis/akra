package classes

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

const (
	pgUniqueViolation     = "23505"
	pgCheckViolation      = "23514"
	pgForeignKeyViolation = "23503"
	pgInvalidTextRepr     = "22P02"
)

// classSelect memuat kolom kelas beserta nama wali kelas. Kondisi
// u.school_id = c.school_id menjaga agar nama dari sekolah lain tidak
// pernah ikut, walau foreign key komposit sudah menjaminnya.
const classSelect = `
SELECT c.id, c.school_id, c.academic_year_id, c.name, c.grade_level,
       coalesce(c.homeroom_teacher_id::text, ''), coalesce(u.full_name, ''),
       c.created_at, c.updated_at
FROM classes c
LEFT JOIN users u ON u.id = c.homeroom_teacher_id AND u.school_id = c.school_id`

// Semua fungsi di bawah memfilter dengan schoolID dari sesi, sehingga data
// sekolah lain diperlakukan seperti tidak ada.

// Create membuat kelas. Tahun ajaran harus milik schoolID; jika tidak,
// ErrAcademicYearNotFound.
func Create(ctx context.Context, q database.DBTX, schoolID string, in ClassInput) (Class, error) {
	if err := checkAcademicYear(ctx, q, schoolID, in.AcademicYearID); err != nil {
		return Class{}, err
	}
	if in.HomeroomTeacherID != "" && !database.IsUUID(in.HomeroomTeacherID) {
		return Class{}, ErrTeacherNotFound
	}

	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		schoolID, in.AcademicYearID, in.Name, in.GradeLevel, nullIfEmpty(in.HomeroomTeacherID)).Scan(&id)
	if err != nil {
		return Class{}, mapError("create class", err)
	}
	return GetByID(ctx, q, schoolID, id)
}

// GetByID mengembalikan satu kelas milik schoolID.
func GetByID(ctx context.Context, q database.DBTX, schoolID, id string) (Class, error) {
	if !database.IsUUID(id) {
		return Class{}, ErrNotFound
	}
	rows, err := q.Query(ctx, classSelect+` WHERE c.school_id = $1 AND c.id = $2`, schoolID, id)
	if err != nil {
		return Class{}, mapError("get class", err)
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanClass)
	if err != nil {
		return Class{}, mapError("get class", err)
	}
	return c, nil
}

// ListByYear mengembalikan kelas sekolah pada satu tahun ajaran, urut tingkat
// lalu nama. Tahun ajaran milik sekolah lain menghasilkan daftar kosong.
func ListByYear(ctx context.Context, q database.DBTX, schoolID, yearID string) ([]Class, error) {
	if !database.IsUUID(yearID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, classSelect+`
		WHERE c.school_id = $1 AND c.academic_year_id = $2
		ORDER BY c.grade_level, lower(c.name)`, schoolID, yearID)
	if err != nil {
		return nil, mapError("list classes", err)
	}
	classes, err := pgx.CollectRows(rows, scanClass)
	if err != nil {
		return nil, mapError("list classes", err)
	}
	return classes, nil
}

// checkAcademicYear memastikan yearID milik schoolID.
func checkAcademicYear(ctx context.Context, q database.DBTX, schoolID, yearID string) error {
	if !database.IsUUID(yearID) {
		return ErrAcademicYearNotFound
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT true FROM academic_years WHERE school_id = $1 AND id = $2`, schoolID, yearID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAcademicYearNotFound
	}
	if err != nil {
		return fmt.Errorf("classes: check academic year: %w", err)
	}
	return nil
}

func scanClass(row pgx.CollectableRow) (Class, error) {
	var c Class
	err := row.Scan(&c.ID, &c.SchoolID, &c.AcademicYearID, &c.Name, &c.GradeLevel,
		&c.HomeroomTeacherID, &c.HomeroomTeacherName, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// mapError mengubah error database menjadi error domain.
func mapError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "classes_academic_year_id_name_key":
			return ErrDuplicateName
		case pgErr.Code == pgCheckViolation && pgErr.ConstraintName == "classes_grade_level_check":
			return ErrInvalidGradeLevel
		case pgErr.Code == pgForeignKeyViolation && pgErr.ConstraintName == "classes_academic_year_fkey":
			return ErrAcademicYearNotFound
		case pgErr.Code == pgForeignKeyViolation && pgErr.ConstraintName == "classes_homeroom_teacher_fkey":
			return ErrTeacherNotFound
		case pgErr.Code == pgInvalidTextRepr:
			return ErrNotFound
		}
	}
	return fmt.Errorf("classes: %s: %w", op, err)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
