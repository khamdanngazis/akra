package students

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

const (
	pgUniqueViolation   = "23505"
	pgCheckViolation    = "23514"
	pgStringTooLong     = "22001"
	pgInvalidTextRepr   = "22P02"
	defaultSearchLimit  = 50
	studentSelectColumn = `id, school_id, nis, full_name, birth_date, coalesce(gender, ''), status, created_at, updated_at`
)

// Semua fungsi di bawah memfilter dengan schoolID dari sesi, sehingga data
// sekolah lain diperlakukan seperti tidak ada.

// Create membuat siswa.
func Create(ctx context.Context, q database.DBTX, schoolID string, in StudentInput) (Student, error) {
	rows, err := q.Query(ctx, `
		INSERT INTO students (school_id, nis, full_name, birth_date, gender, status)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+studentSelectColumn,
		schoolID, in.NIS, in.FullName, in.BirthDate, nullIfEmpty(in.Gender), statusOrDefault(in.Status))
	if err != nil {
		return Student{}, mapError("create student", err)
	}
	s, err := pgx.CollectExactlyOneRow(rows, scanStudent)
	if err != nil {
		return Student{}, mapError("create student", err)
	}
	return s, nil
}

// GetByID mengembalikan satu siswa milik schoolID.
func GetByID(ctx context.Context, q database.DBTX, schoolID, id string) (Student, error) {
	if !database.IsUUID(id) {
		return Student{}, ErrNotFound
	}
	rows, err := q.Query(ctx, `SELECT `+studentSelectColumn+` FROM students WHERE school_id = $1 AND id = $2`, schoolID, id)
	if err != nil {
		return Student{}, mapError("get student", err)
	}
	s, err := pgx.CollectExactlyOneRow(rows, scanStudent)
	if err != nil {
		return Student{}, mapError("get student", err)
	}
	return s, nil
}

// Update mengganti seluruh data pribadi siswa milik schoolID.
func Update(ctx context.Context, q database.DBTX, schoolID, id string, in StudentInput) (Student, error) {
	if !database.IsUUID(id) {
		return Student{}, ErrNotFound
	}
	rows, err := q.Query(ctx, `
		UPDATE students
		SET nis = $3, full_name = $4, birth_date = $5, gender = $6, status = $7, updated_at = now()
		WHERE school_id = $1 AND id = $2
		RETURNING `+studentSelectColumn,
		schoolID, id, in.NIS, in.FullName, in.BirthDate, nullIfEmpty(in.Gender), statusOrDefault(in.Status))
	if err != nil {
		return Student{}, mapError("update student", err)
	}
	s, err := pgx.CollectExactlyOneRow(rows, scanStudent)
	if err != nil {
		return Student{}, mapError("update student", err)
	}
	return s, nil
}

// Search mencari siswa sekolah yang namanya mengandung query, atau NIS-nya
// diawali query, tanpa membedakan huruf besar/kecil. query kosong
// mengembalikan semua siswa. Hasil urut nama, paling banyak limit baris
// (limit <= 0 berarti 50). Paginasi lengkap dikerjakan di T22.
func Search(ctx context.Context, q database.DBTX, schoolID, query string, limit int) ([]Student, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	query = strings.TrimSpace(query)
	pattern := escapeLike(query)
	rows, err := q.Query(ctx, `
		SELECT `+studentSelectColumn+` FROM students
		WHERE school_id = $1
		  AND ($2 = '' OR full_name ILIKE '%' || $3 || '%' OR nis ILIKE $3 || '%')
		ORDER BY lower(full_name), nis
		LIMIT $4`, schoolID, query, pattern, limit)
	if err != nil {
		return nil, mapError("search students", err)
	}
	list, err := pgx.CollectRows(rows, scanStudent)
	if err != nil {
		return nil, mapError("search students", err)
	}
	return list, nil
}

// escapeLike meloloskan karakter khusus LIKE (\, %, _) agar query pengguna
// dicocokkan apa adanya. Backslash adalah karakter escape bawaan ILIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func scanStudent(row pgx.CollectableRow) (Student, error) {
	var s Student
	err := row.Scan(&s.ID, &s.SchoolID, &s.NIS, &s.FullName, &s.BirthDate, &s.Gender, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// mapError mengubah error database menjadi error domain. Pesan error
// PostgreSQL bisa memuat nilai baris (data pribadi), jadi error yang tidak
// dikenali hanya dibungkus dengan kode SQLSTATE dan nama constraint.
func mapError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "students_school_id_nis_key":
			return ErrDuplicateNIS
		case pgErr.Code == pgCheckViolation && pgErr.ConstraintName == "students_nis_check":
			return ErrInvalidNIS
		case pgErr.Code == pgCheckViolation && pgErr.ConstraintName == "students_gender_check",
			pgErr.Code == pgStringTooLong && strings.Contains(pgErr.Message, "character(1)"):
			return ErrInvalidGender
		case pgErr.Code == pgCheckViolation && pgErr.ConstraintName == "students_status_check":
			return ErrInvalidStatus
		case pgErr.Code == pgInvalidTextRepr:
			return ErrNotFound
		}
		return fmt.Errorf("students: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("students: %s: %w", op, err)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func statusOrDefault(s string) string {
	if s == "" {
		return StatusActive
	}
	return s
}
