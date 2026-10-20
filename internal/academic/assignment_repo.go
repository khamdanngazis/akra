package academic

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

// ErrAssignmentConflict dikembalikan jika mapel di kelas itu sudah punya guru
// (UNIQUE (class_id, subject_id)), mis. dua admin menyimpan bersamaan.
var ErrAssignmentConflict = errors.New("academic: subject already has a teacher in this class")

// Assignment adalah penugasan satu guru untuk satu mapel di satu kelas.
type Assignment struct {
	ID             string
	AcademicYearID string
	YearName       string
	ClassID        string
	ClassName      string
	SubjectID      string
	SubjectName    string
	SubjectCode    string
	SubjectActive  bool
	TeacherID      string
	TeacherName    string
	TeacherActive  bool // pengguna aktif dan masih berperan guru
}

const assignmentSelect = `
SELECT ta.id, ta.academic_year_id, y.name, ta.class_id, c.name, ta.subject_id, s.name, coalesce(s.code, ''), s.is_active,
       ta.teacher_id, u.full_name,
       u.is_active AND EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id AND r.code = 'guru')
FROM teaching_assignments ta
JOIN academic_years y ON y.school_id = ta.school_id AND y.id = ta.academic_year_id
JOIN classes c ON c.school_id = ta.school_id AND c.id = ta.class_id
JOIN subjects s ON s.school_id = ta.school_id AND s.id = ta.subject_id
JOIN users u ON u.school_id = ta.school_id AND u.id = ta.teacher_id`

// ListClassAssignments mengembalikan penugasan di kelas classID, urut nama
// mapel.
func ListClassAssignments(ctx context.Context, q database.DBTX, schoolID, classID string) ([]Assignment, error) {
	if !database.IsUUID(classID) {
		return nil, nil
	}
	return listAssignments(ctx, q, assignmentSelect+` WHERE ta.school_id = $1 AND ta.class_id = $2 ORDER BY lower(s.name)`, schoolID, classID)
}

// ListTeacherAssignments mengembalikan penugasan guru teacherID di tahun
// ajaran aktif, urut kelas lalu mapel.
func ListTeacherAssignments(ctx context.Context, q database.DBTX, schoolID, teacherID string) ([]Assignment, error) {
	return listAssignments(ctx, q, assignmentSelect+` WHERE ta.school_id = $1 AND ta.teacher_id = $2 AND y.is_active
		ORDER BY c.grade_level, lower(c.name), lower(s.name)`, schoolID, teacherID)
}

// assignTeacher membuat atau mengganti guru mapel subjectID di kelas classID.
// previous adalah guru sebelumnya ("" jika baru).
func assignTeacher(ctx context.Context, q database.DBTX, schoolID, yearID, classID, subjectID, teacherID string) (previous string, err error) {
	err = q.QueryRow(ctx, `SELECT teacher_id FROM teaching_assignments WHERE school_id = $1 AND class_id = $2 AND subject_id = $3 FOR UPDATE`,
		schoolID, classID, subjectID).Scan(&previous)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, err = q.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id)
			VALUES ($1, $2, $3, $4, $5)`, schoolID, yearID, classID, subjectID, teacherID)
		return "", mapAssignmentError("create assignment", err)
	case err != nil:
		return "", mapAssignmentError("get assignment", err)
	}
	_, err = q.Exec(ctx, `UPDATE teaching_assignments SET teacher_id = $4, updated_at = now()
		WHERE school_id = $1 AND class_id = $2 AND subject_id = $3`, schoolID, classID, subjectID, teacherID)
	return previous, mapAssignmentError("update assignment", err)
}

// unassignTeacher menghapus penugasan mapel subjectID di kelas classID.
// Mengembalikan guru yang dilepas ("" jika tidak ada penugasan).
func unassignTeacher(ctx context.Context, q database.DBTX, schoolID, classID, subjectID string) (string, error) {
	var teacher string
	err := q.QueryRow(ctx, `DELETE FROM teaching_assignments WHERE school_id = $1 AND class_id = $2 AND subject_id = $3 RETURNING teacher_id`,
		schoolID, classID, subjectID).Scan(&teacher)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return teacher, mapAssignmentError("delete assignment", err)
}

func listAssignments(ctx context.Context, q database.DBTX, sql string, args ...any) ([]Assignment, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapAssignmentError("list assignments", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Assignment, error) {
		var a Assignment
		err := row.Scan(&a.ID, &a.AcademicYearID, &a.YearName, &a.ClassID, &a.ClassName, &a.SubjectID, &a.SubjectName, &a.SubjectCode,
			&a.SubjectActive, &a.TeacherID, &a.TeacherName, &a.TeacherActive)
		return a, err
	})
	if err != nil {
		return nil, mapAssignmentError("list assignments", err)
	}
	return list, nil
}

func mapAssignmentError(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && pgErr.ConstraintName == "teaching_assignments_class_subject_key" {
			return ErrAssignmentConflict
		}
		return fmt.Errorf("academic: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("academic: %s: %w", op, err)
}
