package grades

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Score adalah nilai dalam perseratus (85,5 = 8550), agar tidak memakai
// float. Rentang 0–10000.
type Score int

// MaxScore adalah nilai tertinggi (100).
const MaxScore Score = 10000

// ParseScore membaca isian nilai: "85", "85,5", "85.25", atau kosong.
// empty true untuk isian kosong (belum dinilai). Maksimal dua angka di
// belakang koma; di luar 0–100 ditolak.
func ParseScore(s string) (score Score, empty, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true, true
	}
	s = strings.Replace(s, ",", ".", 1)
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" || len(whole) > 3 || (hasFrac && (frac == "" || len(frac) > 2)) {
		return 0, false, false
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, false, false
		}
	}
	w, _ := strconv.Atoi(whole)
	f := 0
	if hasFrac {
		f, _ = strconv.Atoi(frac)
		if len(frac) == 1 {
			f *= 10
		}
	}
	score = Score(w*100 + f)
	if score > MaxScore {
		return 0, false, false
	}
	return score, false, true
}

// String memformat nilai gaya Indonesia tanpa nol di belakang: "85",
// "85,5", "85,25".
func (s Score) String() string {
	whole, frac := int(s)/100, int(s)%100
	switch {
	case frac == 0:
		return strconv.Itoa(whole)
	case frac%10 == 0:
		return fmt.Sprintf("%d,%d", whole, frac/10)
	}
	return fmt.Sprintf("%d,%02d", whole, frac)
}

// decimal memformat nilai untuk kolom numeric: "85.50".
func (s Score) decimal() string { return fmt.Sprintf("%d.%02d", int(s)/100, int(s)%100) }

// Grade adalah nilai satu enrollment untuk satu komponen. Score nil berarti
// belum dinilai.
type Grade struct {
	ID           string
	EnrollmentID string
	ComponentID  string
	Score        *Score
	UpdatedAt    time.Time
}

// StudentRow adalah satu siswa di tabel input nilai beserta nilainya untuk
// satu komponen.
type StudentRow struct {
	EnrollmentID string
	StudentID    string
	NIS          string
	FullName     string
	Active       bool // masih terdaftar di kelas; false = sudah pindah, nilai lama tetap tampil
	Grade        *Grade
}

// ClassComponentRows mengembalikan siswa kelas classID untuk komponen
// componentID: siswa yang masih terdaftar di kelas, ditambah siswa yang
// sudah pindah tetapi punya nilai untuk komponen ini. Urut nama.
func ClassComponentRows(ctx context.Context, q database.DBTX, schoolID, classID, componentID string) ([]StudentRow, error) {
	rows, err := q.Query(ctx, `
		SELECT e.id, st.id, st.nis, st.full_name, e.end_date IS NULL,
		       g.id, (g.score * 100)::int, g.updated_at
		FROM enrollments e
		JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		LEFT JOIN grades g ON g.school_id = e.school_id AND g.enrollment_id = e.id AND g.component_id = $3
		WHERE e.school_id = $1 AND e.class_id = $2 AND (e.end_date IS NULL OR g.id IS NOT NULL)
		ORDER BY lower(st.full_name), st.nis`, schoolID, classID, componentID)
	if err != nil {
		return nil, fmt.Errorf("grades: list class rows: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (StudentRow, error) {
		var r StudentRow
		var gradeID *string
		var score *int32
		var updated *time.Time
		if err := row.Scan(&r.EnrollmentID, &r.StudentID, &r.NIS, &r.FullName, &r.Active, &gradeID, &score, &updated); err != nil {
			return r, err
		}
		if gradeID != nil {
			r.Grade = &Grade{ID: *gradeID, EnrollmentID: r.EnrollmentID, ComponentID: componentID, UpdatedAt: *updated}
			if score != nil {
				s := Score(*score)
				r.Grade.Score = &s
			}
		}
		return r, nil
	})
}

// scoreArg mengubah nilai menjadi argumen numeric (nil = NULL).
func scoreArg(s *Score) any {
	if s == nil {
		return nil
	}
	return s.decimal()
}

func insertGrade(ctx context.Context, q database.DBTX, schoolID, yearID, enrollmentID, componentID string, score *Score, userID string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO grades (school_id, academic_year_id, enrollment_id, component_id, score, recorded_by, updated_by)
		VALUES ($1, $2, $3, $4, $5::numeric, $6, $6) RETURNING id`,
		schoolID, yearID, enrollmentID, componentID, scoreArg(score), userID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("grades: insert grade: %w", err)
	}
	return id, nil
}

func updateGrade(ctx context.Context, q database.DBTX, schoolID, id string, score *Score, userID string) error {
	if _, err := q.Exec(ctx, `UPDATE grades SET score = $3::numeric, updated_by = $4, updated_at = now() WHERE school_id = $1 AND id = $2`,
		schoolID, id, scoreArg(score), userID); err != nil {
		return fmt.Errorf("grades: update grade: %w", err)
	}
	return nil
}
