package attendance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

var (
	// ErrInvalidRange dikembalikan jika tanggal awal setelah tanggal akhir.
	ErrInvalidRange = errors.New("attendance: start date is after end date")
	// ErrRangeOutsideYear dikembalikan jika rentang melewati tahun ajaran
	// kelas (batas rekap: satu tahun ajaran).
	ErrRangeOutsideYear = errors.New("attendance: range must be within the class academic year")
)

// Counts adalah jumlah catatan per status.
type Counts struct {
	Hadir, Sakit, Izin, Alpa int
}

// Total mengembalikan hadir + sakit + izin + alpa.
func (c Counts) Total() int { return c.Hadir + c.Sakit + c.Izin + c.Alpa }

// PresentPercent mengembalikan persentase kehadiran dalam persepuluhan
// persen (mis. 925 = 92,5%), dengan pembagi hadir + sakit + izin + alpa.
// Definisi ini perlu dikonfirmasi ke sekolah. -1 jika belum ada catatan.
func (c Counts) PresentPercent() int {
	total := c.Total()
	if total == 0 {
		return -1
	}
	return (c.Hadir*1000 + total/2) / total
}

// StudentSummary adalah rekap satu siswa di satu kelas.
type StudentSummary struct {
	StudentID string
	NIS       string
	FullName  string
	Counts
}

// ClassSummary adalah rekap satu kelas.
type ClassSummary struct {
	Sessions int // jumlah sesi absensi kelas dalam rentang
	Counts
}

// ClassYear adalah kelas beserta batas tahun ajarannya, untuk validasi
// rentang rekap.
type ClassYear struct {
	ClassID   string
	ClassName string
	YearName  string
	YearStart time.Time
	YearEnd   time.Time
}

// GetClassYear mengembalikan kelas milik schoolID beserta tahun ajarannya.
func GetClassYear(ctx context.Context, q database.DBTX, schoolID, classID string) (ClassYear, error) {
	if !database.IsUUID(classID) {
		return ClassYear{}, ErrClassNotFound
	}
	var c ClassYear
	err := q.QueryRow(ctx, `
		SELECT c.id, c.name, y.name, y.start_date, y.end_date
		FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.school_id = c.school_id
		WHERE c.school_id = $1 AND c.id = $2`, schoolID, classID).Scan(&c.ClassID, &c.ClassName, &c.YearName, &c.YearStart, &c.YearEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassYear{}, ErrClassNotFound
	}
	if err != nil {
		return ClassYear{}, fmt.Errorf("attendance: get class year: %w", err)
	}
	return c, nil
}

// CheckRange memastikan from <= to dan keduanya di dalam tahun ajaran kelas.
func (c ClassYear) CheckRange(from, to time.Time) error {
	if from.After(to) {
		return ErrInvalidRange
	}
	if from.Before(c.YearStart) || to.After(c.YearEnd) {
		return ErrRangeOutsideYear
	}
	return nil
}

// Catatan dihitung hanya jika tanggal sesi berada di dalam masa enrollment
// siswa, sehingga siswa yang pindah kelas tidak terhitung di kelas lamanya
// setelah pindah (atau di kelas barunya sebelum masuk).
const reportFrom = `
FROM attendance_records r
JOIN attendance_sessions s ON s.id = r.session_id AND s.school_id = r.school_id
JOIN enrollments e ON e.id = r.enrollment_id AND e.school_id = r.school_id
JOIN students st ON st.id = e.student_id AND st.school_id = e.school_id
WHERE r.school_id = $1 AND s.class_id = $2
  AND s.session_date BETWEEN $3 AND $4
  AND s.session_date BETWEEN e.start_date AND coalesce(e.end_date, 'infinity'::date)`

const countColumns = `
count(*) FILTER (WHERE r.status = 'hadir'),
count(*) FILTER (WHERE r.status = 'sakit'),
count(*) FILTER (WHERE r.status = 'izin'),
count(*) FILTER (WHERE r.status = 'alpa')`

// SummaryByStudent mengembalikan jumlah hadir, sakit, izin, dan alpa per
// siswa di kelas classID antara from dan to (inklusif), urut nama. Hanya
// data sekolah schoolID.
func SummaryByStudent(ctx context.Context, q database.DBTX, schoolID, classID string, from, to time.Time) ([]StudentSummary, error) {
	if !database.IsUUID(classID) {
		return nil, ErrClassNotFound
	}
	rows, err := q.Query(ctx, `
		SELECT st.id, st.nis, st.full_name, `+countColumns+`
		`+reportFrom+`
		GROUP BY st.id, st.nis, st.full_name
		ORDER BY lower(st.full_name), st.nis`,
		schoolID, classID, from, to)
	if err != nil {
		return nil, mapError("summary by student", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (StudentSummary, error) {
		var s StudentSummary
		err := row.Scan(&s.StudentID, &s.NIS, &s.FullName, &s.Hadir, &s.Sakit, &s.Izin, &s.Alpa)
		return s, err
	})
	if err != nil {
		return nil, mapError("summary by student", err)
	}
	return list, nil
}

// SummaryByClass mengembalikan total per status kelas classID antara from
// dan to (inklusif), beserta jumlah sesinya.
func SummaryByClass(ctx context.Context, q database.DBTX, schoolID, classID string, from, to time.Time) (ClassSummary, error) {
	if !database.IsUUID(classID) {
		return ClassSummary{}, ErrClassNotFound
	}
	var c ClassSummary
	err := q.QueryRow(ctx, `SELECT `+countColumns+reportFrom, schoolID, classID, from, to).
		Scan(&c.Hadir, &c.Sakit, &c.Izin, &c.Alpa)
	if err != nil {
		return ClassSummary{}, mapError("summary by class", err)
	}
	err = q.QueryRow(ctx, `SELECT count(*) FROM attendance_sessions
		WHERE school_id = $1 AND class_id = $2 AND session_date BETWEEN $3 AND $4`,
		schoolID, classID, from, to).Scan(&c.Sessions)
	if err != nil {
		return ClassSummary{}, mapError("count sessions", err)
	}
	return c, nil
}
