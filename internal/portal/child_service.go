package portal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/attendance"
	"akra/internal/platform/database"
)

// ErrNotFound dikembalikan jika siswa tidak ada, milik sekolah lain, atau
// bukan anak akun yang login. Ketiganya sengaja tidak dibedakan agar id
// siswa lain tidak bisa ditebak.
var ErrNotFound = errors.New("portal: child not found")

// ChildDetail adalah data anak yang boleh dilihat orang tua. Status internal
// sekolah, catatan staf, dan data wali lain sengaja tidak ada.
type ChildDetail struct {
	ID        string
	NIS       string
	FullName  string
	BirthDate *time.Time
	Gender    string // L, P, atau kosong
	// Kelas aktif di tahun ajaran aktif; kosong jika belum ditempatkan.
	ClassName string
	Homeroom  string // nama wali kelas; kosong jika belum ditentukan
	YearName  string
	// Rekap absensi tahun ajaran aktif, dari awal tahun ajaran sampai
	// RecapTo. HasRecap false jika belum ada tahun ajaran aktif.
	HasRecap   bool
	RecapFrom  time.Time
	RecapTo    time.Time
	Attendance attendance.Counts
}

// checkChild memastikan studentID adalah anak akun userID di sekolah
// schoolID (keduanya dari sesi). Semua halaman per anak memanggilnya lebih
// dulu; selain itu ErrNotFound.
func (s *Service) checkChild(ctx context.Context, schoolID, userID, studentID string) error {
	if !database.IsUUID(schoolID) || !database.IsUUID(userID) || !database.IsUUID(studentID) {
		return ErrNotFound
	}
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM users u
			JOIN student_guardians sg ON sg.school_id = u.school_id AND sg.guardian_id = u.guardian_id
			WHERE u.school_id = $1 AND u.id = $2 AND u.guardian_id IS NOT NULL AND sg.student_id = $3)`,
		schoolID, userID, studentID).Scan(&ok)
	if err != nil {
		return fmt.Errorf("portal: check child: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// GetChildDetail mengembalikan detail anak studentID milik akun userID di
// sekolah schoolID (keduanya dari sesi). today (Asia/Jakarta) membatasi
// akhir rekap absensi, sama dengan default rekap kelas (T27).
func (s *Service) GetChildDetail(ctx context.Context, schoolID, userID, studentID string, today time.Time) (ChildDetail, error) {
	if err := s.checkChild(ctx, schoolID, userID, studentID); err != nil {
		return ChildDetail{}, err
	}
	var d ChildDetail
	var gender *string
	err := s.db.QueryRow(ctx, `SELECT id, nis, full_name, birth_date, gender FROM students WHERE school_id = $1 AND id = $2`,
		schoolID, studentID).Scan(&d.ID, &d.NIS, &d.FullName, &d.BirthDate, &gender)
	if err != nil {
		return ChildDetail{}, fmt.Errorf("portal: get child: %w", err)
	}
	if gender != nil {
		d.Gender = *gender
	}

	var (
		yearID             string
		yearStart, yearEnd time.Time
	)
	err = s.db.QueryRow(ctx, `SELECT id, name, start_date, end_date FROM academic_years WHERE school_id = $1 AND is_active`,
		schoolID).Scan(&yearID, &d.YearName, &yearStart, &yearEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return ChildDetail{}, fmt.Errorf("portal: active year: %w", err)
	}
	err = s.db.QueryRow(ctx, `
		SELECT c.name, coalesce(u.full_name, '')
		FROM enrollments e
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		LEFT JOIN users u ON u.school_id = c.school_id AND u.id = c.homeroom_teacher_id
		WHERE e.school_id = $1 AND e.student_id = $2 AND e.academic_year_id = $3 AND e.end_date IS NULL`,
		schoolID, studentID, yearID).Scan(&d.ClassName, &d.Homeroom)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ChildDetail{}, fmt.Errorf("portal: current class: %w", err)
	}

	// Rentang sama dengan default rekap kelas: awal tahun ajaran sampai hari
	// ini (atau akhir tahun ajaran jika sudah lewat).
	d.HasRecap, d.RecapFrom, d.RecapTo = true, yearStart, yearEnd
	if today.Before(yearEnd) {
		d.RecapTo = today
	}
	if d.RecapTo.Before(yearStart) {
		d.RecapTo = yearEnd
	}
	// Rekap memakai attendance.SummaryByStudent (T27) per kelas yang pernah
	// ditempati anak di tahun ajaran aktif, lalu dijumlahkan, agar angkanya
	// sama dengan rekap kelas.
	rows, err := s.db.Query(ctx, `SELECT DISTINCT class_id FROM enrollments
		WHERE school_id = $1 AND student_id = $2 AND academic_year_id = $3`, schoolID, studentID, yearID)
	if err != nil {
		return ChildDetail{}, fmt.Errorf("portal: child classes: %w", err)
	}
	classIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return ChildDetail{}, fmt.Errorf("portal: child classes: %w", err)
	}
	for _, classID := range classIDs {
		list, err := attendance.SummaryByStudent(ctx, s.db, schoolID, classID, d.RecapFrom, d.RecapTo)
		if err != nil {
			return ChildDetail{}, err
		}
		for _, row := range list {
			if row.StudentID == studentID {
				d.Attendance.Hadir += row.Hadir
				d.Attendance.Sakit += row.Sakit
				d.Attendance.Izin += row.Izin
				d.Attendance.Alpa += row.Alpa
			}
		}
	}
	return d, nil
}
