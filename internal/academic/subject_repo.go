// Package academic berisi data akademik sekolah di luar tahun ajaran dan
// kelas: mata pelajaran (T64), lalu penugasan guru dan jadwal (T65, T66).
package academic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

var (
	// ErrSubjectNotFound dikembalikan jika mapel tidak ada atau milik sekolah
	// lain; keduanya sengaja tidak dibedakan.
	ErrSubjectNotFound = errors.New("academic: subject not found")
	// ErrDuplicateSubjectName dikembalikan jika nama sudah dipakai di sekolah.
	ErrDuplicateSubjectName = errors.New("academic: subject name already exists")
	// ErrDuplicateSubjectCode dikembalikan jika kode sudah dipakai di sekolah.
	ErrDuplicateSubjectCode = errors.New("academic: subject code already exists")
)

// Subject adalah satu mata pelajaran sekolah.
type Subject struct {
	ID        string
	SchoolID  string
	Name      string
	Code      string // kosong jika tidak diisi
	KKM       *int   // KKM mapel (T73); nil = memakai report.kkm_default sekolah
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SubjectInput adalah data mapel yang sudah divalidasi service.
type SubjectInput struct {
	Name string
	Code string // kosong disimpan sebagai NULL
	KKM  *int   // nil disimpan sebagai NULL
}

const subjectColumns = `id, school_id, name, coalesce(code, ''), kkm, is_active, created_at, updated_at`

// Semua fungsi di bawah memfilter dengan schoolID dari sesi. Mapel sekolah
// lain diperlakukan seperti tidak ada (ErrSubjectNotFound).

// CreateSubject membuat mapel aktif.
func CreateSubject(ctx context.Context, q database.DBTX, schoolID string, in SubjectInput) (Subject, error) {
	return oneSubject(ctx, q, "create subject", `
		INSERT INTO subjects (school_id, name, code, kkm)
		VALUES ($1, $2, $3, $4) RETURNING `+subjectColumns,
		schoolID, in.Name, codeArg(in.Code), in.KKM)
}

// UpdateSubject mengubah nama, kode, dan KKM. Status aktif diubah lewat
// SetSubjectActive.
func UpdateSubject(ctx context.Context, q database.DBTX, schoolID, id string, in SubjectInput) (Subject, error) {
	if !database.IsUUID(id) {
		return Subject{}, ErrSubjectNotFound
	}
	return oneSubject(ctx, q, "update subject", `
		UPDATE subjects SET name = $3, code = $4, kkm = $5, updated_at = now()
		WHERE school_id = $1 AND id = $2 RETURNING `+subjectColumns,
		schoolID, id, in.Name, codeArg(in.Code), in.KKM)
}

// SetSubjectActive mengaktifkan atau menonaktifkan mapel. Mapel tidak pernah
// dihapus.
func SetSubjectActive(ctx context.Context, q database.DBTX, schoolID, id string, active bool) (Subject, error) {
	if !database.IsUUID(id) {
		return Subject{}, ErrSubjectNotFound
	}
	return oneSubject(ctx, q, "set subject active", `
		UPDATE subjects SET is_active = $3, updated_at = now()
		WHERE school_id = $1 AND id = $2 RETURNING `+subjectColumns, schoolID, id, active)
}

// GetSubjectByID mengembalikan satu mapel milik schoolID, aktif atau tidak.
func GetSubjectByID(ctx context.Context, q database.DBTX, schoolID, id string) (Subject, error) {
	if !database.IsUUID(id) {
		return Subject{}, ErrSubjectNotFound
	}
	return oneSubject(ctx, q, "get subject", `SELECT `+subjectColumns+` FROM subjects WHERE school_id = $1 AND id = $2`, schoolID, id)
}

// ListActiveSubjects mengembalikan mapel aktif sekolah, urut nama. Hanya ini
// yang boleh dipakai untuk pilihan penugasan guru dan jadwal.
func ListActiveSubjects(ctx context.Context, q database.DBTX, schoolID string) ([]Subject, error) {
	return listSubjects(ctx, q, `SELECT `+subjectColumns+` FROM subjects WHERE school_id = $1 AND is_active
		ORDER BY lower(name)`, schoolID)
}

// ListAllSubjects mengembalikan semua mapel sekolah untuk halaman
// pengaturan: aktif dulu, lalu urut nama.
func ListAllSubjects(ctx context.Context, q database.DBTX, schoolID string) ([]Subject, error) {
	return listSubjects(ctx, q, `SELECT `+subjectColumns+` FROM subjects WHERE school_id = $1
		ORDER BY is_active DESC, lower(name)`, schoolID)
}

func oneSubject(ctx context.Context, q database.DBTX, op, sql string, args ...any) (Subject, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return Subject{}, mapSubjectError(op, err)
	}
	s, err := pgx.CollectExactlyOneRow(rows, scanSubject)
	if err != nil {
		return Subject{}, mapSubjectError(op, err)
	}
	return s, nil
}

func listSubjects(ctx context.Context, q database.DBTX, sql string, args ...any) ([]Subject, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapSubjectError("list subjects", err)
	}
	list, err := pgx.CollectRows(rows, scanSubject)
	if err != nil {
		return nil, mapSubjectError("list subjects", err)
	}
	return list, nil
}

func scanSubject(row pgx.CollectableRow) (Subject, error) {
	var s Subject
	var kkm *int16
	err := row.Scan(&s.ID, &s.SchoolID, &s.Name, &s.Code, &kkm, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if kkm != nil {
		k := int(*kkm)
		s.KKM = &k
	}
	return s, err
}

// codeArg mengubah kode kosong menjadi NULL.
func codeArg(code string) any {
	if code == "" {
		return nil
	}
	return code
}

// mapSubjectError menerjemahkan error database. Pesan PostgreSQL tidak
// diteruskan karena bisa memuat nilai baris; hanya kode dan nama constraint.
func mapSubjectError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSubjectNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "subjects_school_id_name_key":
			return ErrDuplicateSubjectName
		case pgErr.Code == "23505" && pgErr.ConstraintName == "subjects_school_id_code_key":
			return ErrDuplicateSubjectCode
		}
		return fmt.Errorf("academic: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("academic: %s: %w", op, err)
}
