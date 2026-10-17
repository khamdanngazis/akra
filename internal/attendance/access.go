package attendance

import (
	"context"
	"fmt"

	"akra/internal/auth"
	"akra/internal/platform/database"
)

// CanEditClass melaporkan apakah id boleh membuka dan mengisi absensi kelas
// classID.
//
// Di MVP 1 belum ada penugasan guru mata pelajaran, jadi hanya wali kelas
// (classes.homeroom_teacher_id) dengan peran guru di sekolah yang sama yang
// boleh. Admin/TU tidak mengisi absensi.
func CanEditClass(ctx context.Context, q database.DBTX, id auth.Identity, classID string) (bool, error) {
	if !id.HasRole(auth.RoleGuru) || !database.IsUUID(classID) {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM classes WHERE school_id = $1 AND id = $2 AND homeroom_teacher_id = $3)`,
		id.SchoolID, classID, id.UserID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("attendance: check class access: %w", err)
	}
	return ok, nil
}

// HomeroomClass adalah kelas yang diwalikan seorang guru.
type HomeroomClass struct {
	ID       string
	Name     string
	YearName string
}

// HomeroomClasses mengembalikan kelas yang diwalikan userID pada tahun
// ajaran aktif sekolah, urut nama.
func HomeroomClasses(ctx context.Context, q database.DBTX, schoolID, userID string) ([]HomeroomClass, error) {
	rows, err := q.Query(ctx, `
		SELECT c.id, c.name, y.name
		FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.school_id = c.school_id
		WHERE c.school_id = $1 AND c.homeroom_teacher_id = $2 AND y.is_active
		ORDER BY c.grade_level, lower(c.name)`, schoolID, userID)
	if err != nil {
		return nil, fmt.Errorf("attendance: list homeroom classes: %w", err)
	}
	defer rows.Close()
	var list []HomeroomClass
	for rows.Next() {
		var c HomeroomClass
		if err := rows.Scan(&c.ID, &c.Name, &c.YearName); err != nil {
			return nil, fmt.Errorf("attendance: list homeroom classes: %w", err)
		}
		list = append(list, c)
	}
	return list, rows.Err()
}
