package portal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Child adalah anak yang terhubung dengan akun orang tua.
type Child struct {
	ID           string
	NIS          string
	FullName     string
	Status       string
	Relationship string // ayah, ibu, wali, lainnya
	// ClassName dan YearName dari enrollment aktif di tahun ajaran aktif;
	// kosong jika belum ditempatkan.
	ClassName string
	YearName  string
}

// Service membaca data portal orang tua.
type Service struct {
	db database.DBTX
}

// NewService mengembalikan Service.
func NewService(db database.DBTX) *Service {
	return &Service{db: db}
}

// ListChildren mengembalikan anak yang terhubung dengan akun userID di
// sekolah schoolID (keduanya dari sesi). Wali dicari dari users.guardian_id
// setiap kali, bukan dari parameter, sehingga perubahan hubungan wali di
// sekolah langsung berlaku. Kelas hanya dari enrollment aktif tahun ajaran
// aktif.
func (s *Service) ListChildren(ctx context.Context, schoolID, userID string) ([]Child, error) {
	if !database.IsUUID(schoolID) || !database.IsUUID(userID) {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT st.id, st.nis, st.full_name, st.status, sg.relationship,
		       coalesce(cur.class_name, ''), coalesce(cur.year_name, '')
		FROM users u
		JOIN student_guardians sg ON sg.school_id = u.school_id AND sg.guardian_id = u.guardian_id
		JOIN students st ON st.school_id = sg.school_id AND st.id = sg.student_id
		LEFT JOIN LATERAL (
			SELECT c.name AS class_name, y.name AS year_name
			FROM academic_years y
			JOIN enrollments e ON e.school_id = y.school_id AND e.academic_year_id = y.id
			JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
			WHERE y.school_id = st.school_id AND y.is_active AND e.student_id = st.id AND e.end_date IS NULL
		) cur ON true
		WHERE u.school_id = $1 AND u.id = $2 AND u.guardian_id IS NOT NULL
		ORDER BY lower(st.full_name), st.nis`, schoolID, userID)
	if err != nil {
		return nil, fmt.Errorf("portal: list children: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Child, error) {
		var c Child
		err := row.Scan(&c.ID, &c.NIS, &c.FullName, &c.Status, &c.Relationship, &c.ClassName, &c.YearName)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("portal: list children: %w", err)
	}
	return list, nil
}
