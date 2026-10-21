// Package grades mengelola penilaian: komponen nilai per mapel (T69), lalu
// nilai siswa dan rekapnya (T70–T75).
package grades

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

// ErrDuplicateName: database menolak nama komponen yang sudah dipakai di
// mapel dan tahun ajaran yang sama (unique index, tanpa membedakan huruf).
var ErrDuplicateName = errors.New("grades: component name already used for this subject and year")

// Component adalah satu komponen penilaian, mis. "UTS" berbobot 30%.
type Component struct {
	ID             string
	AcademicYearID string
	SubjectID      string
	Name           string
	Weight         int
	SortOrder      int
	IsActive       bool
}

const componentColumns = `id, academic_year_id, subject_id, name, weight_percent, sort_order, is_active`

// ListComponents mengembalikan komponen mapel subjectID di tahun ajaran
// yearID, aktif dan nonaktif, urut sort_order lalu nama.
func ListComponents(ctx context.Context, q database.DBTX, schoolID, yearID, subjectID string) ([]Component, error) {
	if !database.IsUUID(yearID) || !database.IsUUID(subjectID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT `+componentColumns+` FROM grade_components
		WHERE school_id = $1 AND academic_year_id = $2 AND subject_id = $3
		ORDER BY sort_order, lower(name)`, schoolID, yearID, subjectID)
	if err != nil {
		return nil, mapError("list components", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Component, error) {
		var c Component
		err := row.Scan(&c.ID, &c.AcademicYearID, &c.SubjectID, &c.Name, &c.Weight, &c.SortOrder, &c.IsActive)
		return c, err
	})
	if err != nil {
		return nil, mapError("list components", err)
	}
	return list, nil
}

// ActiveWeightTotal mengembalikan jumlah komponen aktif dan total bobotnya.
func ActiveWeightTotal(ctx context.Context, q database.DBTX, schoolID, yearID, subjectID string) (count, total int, err error) {
	err = q.QueryRow(ctx, `SELECT count(*), coalesce(sum(weight_percent), 0) FROM grade_components
		WHERE school_id = $1 AND academic_year_id = $2 AND subject_id = $3 AND is_active`,
		schoolID, yearID, subjectID).Scan(&count, &total)
	if err != nil {
		return 0, 0, mapError("active weight total", err)
	}
	return count, total, nil
}

func insertComponent(ctx context.Context, q database.DBTX, schoolID string, c Component) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, sort_order, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		schoolID, c.AcademicYearID, c.SubjectID, c.Name, c.Weight, c.SortOrder, c.IsActive).Scan(&id)
	return id, mapError("insert component", err)
}

func updateComponent(ctx context.Context, q database.DBTX, schoolID string, c Component) error {
	_, err := q.Exec(ctx, `
		UPDATE grade_components SET name = $3, weight_percent = $4, sort_order = $5, is_active = $6, updated_at = now()
		WHERE school_id = $1 AND id = $2`, schoolID, c.ID, c.Name, c.Weight, c.SortOrder, c.IsActive)
	return mapError("update component", err)
}

func mapError(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && pgErr.ConstraintName == "grade_components_subject_year_name_key" {
			return ErrDuplicateName
		}
		return fmt.Errorf("grades: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("grades: %s: %w", op, err)
}
