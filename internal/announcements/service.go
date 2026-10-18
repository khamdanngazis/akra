// Package announcements mengelola pengumuman sekolah untuk satu kelas atau
// semua kelas di tahun ajaran aktif, dan menampilkannya di portal orang tua.
package announcements

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// ActionCreate adalah aksi audit pembuatan pengumuman.
const ActionCreate = "announcement.create"

// Batas panjang (sama dengan constraint database).
const (
	MaxTitleLen = 150
	MaxBodyLen  = 5000
)

var (
	// ErrNoActiveYear dikembalikan jika belum ada tahun ajaran aktif.
	ErrNoActiveYear = errors.New("announcements: no active academic year")
	// ErrClassNotFound dikembalikan jika kelas tidak ada di tahun ajaran
	// aktif sekolah ini.
	ErrClassNotFound = errors.New("announcements: class not found in the active academic year")
)

// Announcement adalah satu pengumuman.
type Announcement struct {
	ID          string
	Title       string
	Body        string
	ClassID     string // kosong = semua kelas
	ClassName   string
	YearName    string
	PublishedAt time.Time
	AuthorName  string
}

// Form adalah isian form pengumuman apa adanya.
type Form struct {
	Title   string
	Body    string
	ClassID string // kosong = semua kelas
}

// ValidationError berisi pesan per field.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "announcements: invalid form" }

// Service menjalankan aturan pengumuman.
type Service struct {
	db database.DB
}

// NewService mengembalikan Service.
func NewService(db database.DB) *Service {
	return &Service{db: db}
}

// ClassOption adalah kelas tahun ajaran aktif untuk pilihan form.
type ClassOption struct {
	ID, Name string
}

// ActiveYear mengembalikan tahun ajaran aktif dan kelas-kelasnya.
func (s *Service) ActiveYear(ctx context.Context, schoolID string) (school.AcademicYear, []ClassOption, error) {
	y, err := school.GetActiveAcademicYear(ctx, s.db, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return school.AcademicYear{}, nil, ErrNoActiveYear
	}
	if err != nil {
		return school.AcademicYear{}, nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, name FROM classes WHERE school_id = $1 AND academic_year_id = $2 ORDER BY grade_level, lower(name)`, schoolID, y.ID)
	if err != nil {
		return y, nil, fmt.Errorf("announcements: list classes: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ClassOption, error) {
		var c ClassOption
		err := row.Scan(&c.ID, &c.Name)
		return c, err
	})
	if err != nil {
		return y, nil, fmt.Errorf("announcements: list classes: %w", err)
	}
	return y, list, nil
}

// Create membuat pengumuman di tahun ajaran aktif beserta audit
// announcement.create. Judul dan isi disimpan sebagai teks biasa; HTML
// tidak diproses (di-escape saat tampil).
func (s *Service) Create(ctx context.Context, actor school.Actor, f Form) (Announcement, error) {
	title := strings.Join(strings.Fields(f.Title), " ")
	body := strings.TrimSpace(strings.ReplaceAll(f.Body, "\r\n", "\n"))
	fields := map[string]string{}
	switch {
	case title == "":
		fields["title"] = "Judul wajib diisi."
	case utf8.RuneCountInString(title) > MaxTitleLen:
		fields["title"] = fmt.Sprintf("Judul paling banyak %d karakter.", MaxTitleLen)
	}
	switch {
	case body == "":
		fields["body"] = "Isi pengumuman wajib diisi."
	case utf8.RuneCountInString(body) > MaxBodyLen:
		fields["body"] = fmt.Sprintf("Isi paling banyak %d karakter.", MaxBodyLen)
	}
	if len(fields) > 0 {
		return Announcement{}, &ValidationError{Fields: fields}
	}
	y, err := school.GetActiveAcademicYear(ctx, s.db, actor.SchoolID)
	if errors.Is(err, school.ErrNotFound) {
		return Announcement{}, ErrNoActiveYear
	}
	if err != nil {
		return Announcement{}, err
	}
	var classID any
	if f.ClassID != "" {
		if !database.IsUUID(f.ClassID) {
			return Announcement{}, ErrClassNotFound
		}
		classID = f.ClassID
	}
	var a Announcement
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO announcements (school_id, academic_year_id, class_id, title, body, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, published_at`, actor.SchoolID, y.ID, classID, title, body, actor.UserID).Scan(&a.ID, &a.PublishedAt)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "announcements_class_fkey" {
			return ErrClassNotFound
		}
		if err != nil {
			return fmt.Errorf("announcements: insert: %w", err)
		}
		a.Title, a.Body, a.ClassID, a.YearName = title, body, f.ClassID, y.Name
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionCreate, EntityType: "announcement", EntityID: a.ID,
			After: map[string]any{"title": title, "class_id": f.ClassID, "academic_year_id": y.ID, "body_length": utf8.RuneCountInString(body)},
		})
	})
	return a, err
}

const selectColumns = `a.id, a.title, a.body, coalesce(a.class_id::text, ''), coalesce(c.name, ''), y.name, a.published_at, coalesce(u.full_name, '')`

func scan(row pgx.CollectableRow) (Announcement, error) {
	var a Announcement
	err := row.Scan(&a.ID, &a.Title, &a.Body, &a.ClassID, &a.ClassName, &a.YearName, &a.PublishedAt, &a.AuthorName)
	return a, err
}

const fromJoins = `FROM announcements a
	JOIN academic_years y ON y.school_id = a.school_id AND y.id = a.academic_year_id
	LEFT JOIN classes c ON c.school_id = a.school_id AND c.id = a.class_id
	LEFT JOIN users u ON u.school_id = a.school_id AND u.id = a.created_by`

// List mengembalikan pengumuman sekolah (semua tahun ajaran), terbaru dulu,
// untuk halaman staf.
func (s *Service) List(ctx context.Context, schoolID string, limit int) ([]Announcement, error) {
	rows, err := s.db.Query(ctx, `SELECT `+selectColumns+` `+fromJoins+`
		WHERE a.school_id = $1 ORDER BY a.published_at DESC, a.id LIMIT $2`, schoolID, limit)
	if err != nil {
		return nil, fmt.Errorf("announcements: list: %w", err)
	}
	return pgx.CollectRows(rows, scan)
}

// ListForParent mengembalikan pengumuman yang relevan untuk akun orang tua
// userID di sekolah schoolID (keduanya dari sesi), terbaru dulu: pengumuman
// tahun ajaran aktif untuk semua kelas, atau untuk kelas tempat anaknya
// terdaftar (enrollment aktif) di tahun ajaran aktif.
func ListForParent(ctx context.Context, q database.DBTX, schoolID, userID string, limit int) ([]Announcement, error) {
	if !database.IsUUID(schoolID) || !database.IsUUID(userID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT `+selectColumns+` `+fromJoins+`
		WHERE a.school_id = $1 AND y.is_active
		  AND EXISTS (SELECT 1 FROM users pu WHERE pu.school_id = $1 AND pu.id = $2 AND pu.guardian_id IS NOT NULL)
		  AND (a.class_id IS NULL OR a.class_id IN (
			SELECT e.class_id FROM users pu
			JOIN student_guardians sg ON sg.school_id = pu.school_id AND sg.guardian_id = pu.guardian_id
			JOIN enrollments e ON e.school_id = sg.school_id AND e.student_id = sg.student_id
			WHERE pu.school_id = $1 AND pu.id = $2 AND e.academic_year_id = a.academic_year_id AND e.end_date IS NULL))
		ORDER BY a.published_at DESC, a.id LIMIT $3`, schoolID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("announcements: list for parent: %w", err)
	}
	return pgx.CollectRows(rows, scan)
}
