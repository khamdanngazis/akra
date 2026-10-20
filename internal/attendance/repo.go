// Package attendance mengelola absensi per kelas: satu sesi harian per kelas
// per tanggal, dan (jika sekolah mengaktifkannya, T68) satu sesi per mapel
// per kelas per tanggal. Setiap sesi punya satu catatan status per siswa.
package attendance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

// Status kehadiran.
const (
	StatusPresent = "hadir"
	StatusSick    = "sakit"
	StatusExcused = "izin"
	StatusAbsent  = "alpa"
)

// Statuses berisi semua status yang valid, dalam urutan tampilan.
var Statuses = []string{StatusPresent, StatusSick, StatusExcused, StatusAbsent}

var (
	// ErrClassNotFound dikembalikan jika kelas tidak ada atau milik sekolah lain.
	ErrClassNotFound = errors.New("attendance: class not found")
	// ErrSessionNotFound dikembalikan jika sesi tidak ada atau milik sekolah lain.
	ErrSessionNotFound = errors.New("attendance: session not found")
	// ErrDateOutsideYear dikembalikan jika tanggal di luar tahun ajaran kelas.
	ErrDateOutsideYear = errors.New("attendance: date is outside the class academic year")
	// ErrInvalidStatus dikembalikan jika status bukan salah satu Statuses.
	ErrInvalidStatus = errors.New("attendance: invalid status")
	// ErrSubjectNotFound dikembalikan jika mapel tidak ada atau milik sekolah lain.
	ErrSubjectNotFound = errors.New("attendance: subject not found")
)

// Session adalah sesi absensi satu kelas pada satu tanggal: harian jika
// SubjectID kosong, atau untuk satu mapel.
type Session struct {
	ID             string
	SchoolID       string
	AcademicYearID string
	ClassID        string
	ClassName      string
	SubjectID      string // kosong untuk sesi harian
	SubjectName    string
	SessionDate    time.Time
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Record adalah status kehadiran satu siswa di satu sesi.
type Record struct {
	EnrollmentID string
	StudentID    string
	NIS          string
	FullName     string
	Status       string
	Note         string
	UpdatedAt    time.Time
}

const sessionSelect = `
SELECT s.id, s.school_id, s.academic_year_id, s.class_id, c.name, coalesce(s.subject_id::text, ''), coalesce(sub.name, ''),
       s.session_date, s.created_by, s.created_at, s.updated_at
FROM attendance_sessions s
JOIN classes c ON c.id = s.class_id AND c.school_id = s.school_id
LEFT JOIN subjects sub ON sub.id = s.subject_id AND sub.school_id = s.school_id`

// GetOrCreateSession mengembalikan sesi kelas classID pada tanggal date
// untuk mapel subjectID (kosong = sesi harian), membuatnya jika belum ada. INSERT ... ON CONFLICT DO NOTHING lalu SELECT
// membuat dua permintaan bersamaan tetap menghasilkan satu sesi.
//
// Kelas harus milik schoolID dan date harus di dalam tahun ajaran kelas.
// userID dicatat sebagai pembuat jika sesi baru dibuat.
func GetOrCreateSession(ctx context.Context, q database.DBTX, schoolID, classID, subjectID string, date time.Time, userID string) (Session, error) {
	if !database.IsUUID(classID) {
		return Session{}, ErrClassNotFound
	}
	if subjectID != "" && !database.IsUUID(subjectID) {
		return Session{}, ErrSubjectNotFound
	}
	var yearStart, yearEnd time.Time
	err := q.QueryRow(ctx, `
		SELECT y.start_date, y.end_date
		FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.school_id = c.school_id
		WHERE c.school_id = $1 AND c.id = $2`, schoolID, classID).Scan(&yearStart, &yearEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrClassNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("attendance: check class: %w", err)
	}
	if date.Before(yearStart) || date.After(yearEnd) {
		return Session{}, ErrDateOutsideYear
	}

	subject := nullUUID(subjectID)
	if _, err := q.Exec(ctx, `
		INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, subject_id, session_date, created_by)
		SELECT school_id, academic_year_id, id, $5::uuid, $3, $4 FROM classes WHERE school_id = $1 AND id = $2
		ON CONFLICT (class_id, session_date, (coalesce(subject_id, '00000000-0000-0000-0000-000000000000'::uuid))) DO NOTHING`,
		schoolID, classID, date, userID, subject); err != nil {
		return Session{}, mapError("create session", err)
	}
	rows, err := q.Query(ctx, sessionSelect+` WHERE s.school_id = $1 AND s.class_id = $2 AND s.session_date = $3
		AND s.subject_id IS NOT DISTINCT FROM $4::uuid`, schoolID, classID, date, subject)
	if err != nil {
		return Session{}, mapError("get session", err)
	}
	s, err := pgx.CollectExactlyOneRow(rows, scanSession)
	if err != nil {
		return Session{}, mapError("get session", err)
	}
	return s, nil
}

// GetSession mengembalikan sesi milik schoolID.
func GetSession(ctx context.Context, q database.DBTX, schoolID, sessionID string) (Session, error) {
	if !database.IsUUID(sessionID) {
		return Session{}, ErrSessionNotFound
	}
	rows, err := q.Query(ctx, sessionSelect+` WHERE s.school_id = $1 AND s.id = $2`, schoolID, sessionID)
	if err != nil {
		return Session{}, mapError("get session", err)
	}
	s, err := pgx.CollectExactlyOneRow(rows, scanSession)
	if err != nil {
		return Session{}, mapError("get session", err)
	}
	return s, nil
}

// EnsureRecords membuat catatan berstatus hadir untuk setiap siswa yang
// terdaftar di kelas sesi PADA TANGGAL SESI (start_date <= tanggal dan
// belum berakhir pada tanggal itu). Siswa yang pindah ke kelas ini setelah
// tanggal sesi tidak ikut; siswa yang pindah keluar setelah tanggal sesi
// tetap ikut. Catatan yang sudah ada tidak diubah, dan tidak ada catatan
// yang dihapus. Mengembalikan jumlah catatan baru.
func EnsureRecords(ctx context.Context, tx pgx.Tx, schoolID, sessionID string) (int64, error) {
	if !database.IsUUID(sessionID) {
		return 0, ErrSessionNotFound
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO attendance_records (school_id, session_id, enrollment_id, status)
		SELECT s.school_id, s.id, e.id, 'hadir'
		FROM attendance_sessions s
		JOIN enrollments e
		  ON e.school_id = s.school_id AND e.class_id = s.class_id
		 AND e.start_date <= s.session_date
		 AND (e.end_date IS NULL OR e.end_date >= s.session_date)
		WHERE s.school_id = $1 AND s.id = $2
		ON CONFLICT (session_id, enrollment_id) DO NOTHING`, schoolID, sessionID)
	if err != nil {
		return 0, mapError("ensure records", err)
	}
	return tag.RowsAffected(), nil
}

// ListRecords mengembalikan catatan sesi beserta data siswa, urut nama.
func ListRecords(ctx context.Context, q database.DBTX, schoolID, sessionID string) ([]Record, error) {
	if !database.IsUUID(sessionID) {
		return nil, ErrSessionNotFound
	}
	rows, err := q.Query(ctx, `
		SELECT r.enrollment_id, st.id, st.nis, st.full_name, r.status, coalesce(r.note, ''), r.updated_at
		FROM attendance_records r
		JOIN enrollments e ON e.id = r.enrollment_id AND e.school_id = r.school_id
		JOIN students st ON st.id = e.student_id AND st.school_id = e.school_id
		WHERE r.school_id = $1 AND r.session_id = $2
		ORDER BY lower(st.full_name), st.nis`, schoolID, sessionID)
	if err != nil {
		return nil, mapError("list records", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Record, error) {
		var r Record
		err := row.Scan(&r.EnrollmentID, &r.StudentID, &r.NIS, &r.FullName, &r.Status, &r.Note, &r.UpdatedAt)
		return r, err
	})
	if err != nil {
		return nil, mapError("list records", err)
	}
	return list, nil
}

func scanSession(row pgx.CollectableRow) (Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.SchoolID, &s.AcademicYearID, &s.ClassID, &s.ClassName, &s.SubjectID, &s.SubjectName,
		&s.SessionDate, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// nullUUID mengubah id kosong menjadi NULL.
func nullUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// mapError mengubah error database menjadi error domain tanpa membawa pesan
// PostgreSQL (yang bisa memuat data siswa).
func mapError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSessionNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23514" && pgErr.ConstraintName == "attendance_records_status_check":
			return ErrInvalidStatus
		case pgErr.Code == "23503" && pgErr.ConstraintName == "attendance_sessions_class_fkey":
			return ErrClassNotFound
		case pgErr.Code == "23503" && pgErr.ConstraintName == "attendance_sessions_subject_fkey":
			return ErrSubjectNotFound
		}
		return fmt.Errorf("attendance: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("attendance: %s: %w", op, err)
}
