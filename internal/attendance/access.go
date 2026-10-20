package attendance

import (
	"context"
	"fmt"

	"akra/internal/auth"
	"akra/internal/platform/database"
)

// CanEditClass melaporkan apakah id boleh membuka dan mengisi absensi kelas
// classID: pengguna berperan guru yang menjadi wali kelas
// (classes.homeroom_teacher_id) atau punya penugasan mapel di kelas itu
// (teaching_assignments, T65), di sekolah yang sama. Admin/TU tidak mengisi
// absensi.
func CanEditClass(ctx context.Context, q database.DBTX, id auth.Identity, classID string) (bool, error) {
	if !id.HasRole(auth.RoleGuru) || !database.IsUUID(classID) {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM classes WHERE school_id = $1 AND id = $2 AND homeroom_teacher_id = $3
		UNION ALL
		SELECT 1 FROM teaching_assignments WHERE school_id = $1 AND class_id = $2 AND teacher_id = $3)`,
		id.SchoolID, classID, id.UserID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("attendance: check class access: %w", err)
	}
	return ok, nil
}

// TeacherClass adalah kelas yang boleh diisi absensinya oleh seorang guru.
type TeacherClass struct {
	ID       string
	Name     string
	YearName string
	Homeroom bool   // guru adalah wali kelas
	Subjects string // mapel yang diajarnya di kelas ini, dipisah koma
}

// TeacherClasses mengembalikan kelas tahun ajaran aktif yang diwalikan
// userID atau tempat ia mengajar mapel (sama dengan CanEditClass), urut
// tingkat lalu nama.
func TeacherClasses(ctx context.Context, q database.DBTX, schoolID, userID string) ([]TeacherClass, error) {
	rows, err := q.Query(ctx, `
		SELECT c.id, c.name, y.name, c.homeroom_teacher_id IS NOT DISTINCT FROM $2::uuid,
		       coalesce((SELECT string_agg(s.name, ', ' ORDER BY lower(s.name))
		                 FROM teaching_assignments ta JOIN subjects s ON s.school_id = ta.school_id AND s.id = ta.subject_id
		                 WHERE ta.school_id = c.school_id AND ta.class_id = c.id AND ta.teacher_id = $2), '')
		FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.school_id = c.school_id
		WHERE c.school_id = $1 AND y.is_active
		  AND (c.homeroom_teacher_id = $2 OR EXISTS (
		        SELECT 1 FROM teaching_assignments ta WHERE ta.school_id = c.school_id AND ta.class_id = c.id AND ta.teacher_id = $2))
		ORDER BY c.grade_level, lower(c.name)`, schoolID, userID)
	if err != nil {
		return nil, fmt.Errorf("attendance: list teacher classes: %w", err)
	}
	defer rows.Close()
	var list []TeacherClass
	for rows.Next() {
		var c TeacherClass
		if err := rows.Scan(&c.ID, &c.Name, &c.YearName, &c.Homeroom, &c.Subjects); err != nil {
			return nil, fmt.Errorf("attendance: list teacher classes: %w", err)
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// CanEditSubject melaporkan apakah id boleh membuka dan mengisi absensi mapel
// subjectID di kelas classID: hanya guru yang punya penugasan mapel itu di
// kelas itu (T65). Wali kelas tanpa penugasan mapel tersebut tidak boleh.
func CanEditSubject(ctx context.Context, q database.DBTX, id auth.Identity, classID, subjectID string) (bool, error) {
	if !id.HasRole(auth.RoleGuru) || !database.IsUUID(classID) || !database.IsUUID(subjectID) {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM teaching_assignments
		WHERE school_id = $1 AND class_id = $2 AND subject_id = $3 AND teacher_id = $4)`,
		id.SchoolID, classID, subjectID, id.UserID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("attendance: check subject access: %w", err)
	}
	return ok, nil
}

// SubjectOption adalah mapel untuk pilihan absensi atau filter rekap.
type SubjectOption struct {
	ID, Name string
}

// TeacherSubjects mengembalikan mapel aktif yang diajar userID di kelas
// classID, urut nama: pilihan absensi per mapel di form.
func TeacherSubjects(ctx context.Context, q database.DBTX, schoolID, userID, classID string) ([]SubjectOption, error) {
	if !database.IsUUID(classID) || !database.IsUUID(userID) {
		return nil, nil
	}
	return listSubjects(ctx, q, `
		SELECT s.id, s.name FROM teaching_assignments ta
		JOIN subjects s ON s.school_id = ta.school_id AND s.id = ta.subject_id
		WHERE ta.school_id = $1 AND ta.class_id = $2 AND ta.teacher_id = $3 AND s.is_active
		ORDER BY lower(s.name)`, schoolID, classID, userID)
}

// ClassSubjects mengembalikan mapel yang punya penugasan atau sesi absensi
// di kelas classID, urut nama: pilihan filter rekap.
func ClassSubjects(ctx context.Context, q database.DBTX, schoolID, classID string) ([]SubjectOption, error) {
	if !database.IsUUID(classID) {
		return nil, nil
	}
	return listSubjects(ctx, q, `
		SELECT s.id, s.name FROM subjects s
		WHERE s.school_id = $1 AND (
			EXISTS (SELECT 1 FROM teaching_assignments ta WHERE ta.school_id = s.school_id AND ta.class_id = $2 AND ta.subject_id = s.id)
			OR EXISTS (SELECT 1 FROM attendance_sessions a WHERE a.school_id = s.school_id AND a.class_id = $2 AND a.subject_id = s.id))
		ORDER BY lower(s.name)`, schoolID, classID)
}

func listSubjects(ctx context.Context, q database.DBTX, sql string, args ...any) ([]SubjectOption, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("attendance: list subjects: %w", err)
	}
	defer rows.Close()
	var list []SubjectOption
	for rows.Next() {
		var s SubjectOption
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			return nil, fmt.Errorf("attendance: list subjects: %w", err)
		}
		list = append(list, s)
	}
	return list, rows.Err()
}
