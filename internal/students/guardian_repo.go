package students

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

// Hubungan wali dengan siswa.
const (
	RelationFather   = "ayah"
	RelationMother   = "ibu"
	RelationGuardian = "wali"
	RelationOther    = "lainnya"
)

// Relationships berisi semua hubungan yang valid.
var Relationships = []string{RelationFather, RelationMother, RelationGuardian, RelationOther}

var (
	// ErrGuardianNotFound dikembalikan jika wali tidak ada atau milik sekolah lain.
	ErrGuardianNotFound = errors.New("students: guardian not found")
	// ErrNotLinked dikembalikan jika wali tidak terhubung ke siswa tersebut.
	ErrNotLinked = errors.New("students: guardian is not linked to this student")
	// ErrInvalidRelationship dikembalikan jika hubungan tidak dikenal.
	ErrInvalidRelationship = errors.New("students: unknown relationship")
	// ErrDuplicatePrimary dikembalikan jika database menolak penanggung jawab
	// utama kedua (cadangan; LinkToStudent melepas yang lama lebih dulu).
	ErrDuplicatePrimary = errors.New("students: student already has a primary guardian")
)

// Guardian adalah orang tua atau wali siswa.
type Guardian struct {
	ID        string
	SchoolID  string
	FullName  string
	Phone     string // format +62xxxxxxxx, kosong jika tidak diisi
	Email     string // huruf kecil, kosong jika tidak diisi
	Address   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GuardianInput adalah data wali yang sudah dinormalisasi oleh service.
type GuardianInput struct {
	FullName string
	Phone    string
	Email    string
	Address  string
}

// StudentGuardian adalah wali beserta hubungannya dengan satu siswa.
type StudentGuardian struct {
	Guardian
	Relationship string
	IsPrimary    bool
}

// GuardianChild adalah siswa yang diasuh seorang wali.
type GuardianChild struct {
	StudentID    string
	NIS          string
	FullName     string
	Relationship string
	IsPrimary    bool
}

const guardianColumns = `g.id, g.school_id, g.full_name, coalesce(g.phone, ''), coalesce(g.email, ''), coalesce(g.address, ''), g.created_at, g.updated_at`

// CreateGuardian membuat wali.
func CreateGuardian(ctx context.Context, q database.DBTX, schoolID string, in GuardianInput) (Guardian, error) {
	rows, err := q.Query(ctx, `
		INSERT INTO guardians AS g (school_id, full_name, phone, email, address)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+guardianColumns,
		schoolID, in.FullName, nullIfEmpty(in.Phone), nullIfEmpty(in.Email), nullIfEmpty(in.Address))
	if err != nil {
		return Guardian{}, mapGuardianError("create guardian", err)
	}
	g, err := pgx.CollectExactlyOneRow(rows, scanGuardian)
	if err != nil {
		return Guardian{}, mapGuardianError("create guardian", err)
	}
	return g, nil
}

// GetGuardian mengembalikan satu wali milik schoolID.
func GetGuardian(ctx context.Context, q database.DBTX, schoolID, id string) (Guardian, error) {
	if !database.IsUUID(id) {
		return Guardian{}, ErrGuardianNotFound
	}
	rows, err := q.Query(ctx, `SELECT `+guardianColumns+` FROM guardians g WHERE g.school_id = $1 AND g.id = $2`, schoolID, id)
	if err != nil {
		return Guardian{}, mapGuardianError("get guardian", err)
	}
	g, err := pgx.CollectExactlyOneRow(rows, scanGuardian)
	if err != nil {
		return Guardian{}, mapGuardianError("get guardian", err)
	}
	return g, nil
}

// LinkToStudent menghubungkan wali ke siswa. Jika sudah terhubung, hubungan
// dan status utama diperbarui. Jika isPrimary, penanggung jawab utama lama
// dilepas lebih dulu, jadi harus dipanggil di dalam transaksi tx.
func LinkToStudent(ctx context.Context, tx pgx.Tx, schoolID, studentID, guardianID, relationship string, isPrimary bool) error {
	if !database.IsUUID(studentID) {
		return ErrNotFound
	}
	if !database.IsUUID(guardianID) {
		return ErrGuardianNotFound
	}
	// Pastikan keduanya milik sekolah ini (pesan error jelas); foreign key
	// komposit menjaga hal yang sama di database.
	var studentOK, guardianOK bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM students WHERE school_id = $1 AND id = $2),
		       EXISTS (SELECT 1 FROM guardians WHERE school_id = $1 AND id = $3)`,
		schoolID, studentID, guardianID).Scan(&studentOK, &guardianOK)
	if err != nil {
		return mapGuardianError("check link", err)
	}
	if !studentOK {
		return ErrNotFound
	}
	if !guardianOK {
		return ErrGuardianNotFound
	}

	if isPrimary {
		if _, err := tx.Exec(ctx, `UPDATE student_guardians SET is_primary = false
			WHERE school_id = $1 AND student_id = $2 AND is_primary AND guardian_id <> $3`,
			schoolID, studentID, guardianID); err != nil {
			return mapGuardianError("clear primary guardian", err)
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (student_id, guardian_id)
		DO UPDATE SET relationship = excluded.relationship, is_primary = excluded.is_primary`,
		schoolID, studentID, guardianID, relationship, isPrimary)
	if err != nil {
		return mapGuardianError("link guardian", err)
	}
	return nil
}

// UnlinkFromStudent memutus hubungan wali dan siswa. Data wali tetap ada.
func UnlinkFromStudent(ctx context.Context, q database.DBTX, schoolID, studentID, guardianID string) error {
	if !database.IsUUID(studentID) || !database.IsUUID(guardianID) {
		return ErrNotLinked
	}
	tag, err := q.Exec(ctx, `DELETE FROM student_guardians WHERE school_id = $1 AND student_id = $2 AND guardian_id = $3`,
		schoolID, studentID, guardianID)
	if err != nil {
		return mapGuardianError("unlink guardian", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotLinked
	}
	return nil
}

// ListByStudent mengembalikan wali seorang siswa, penanggung jawab utama
// dulu. Hanya data sekolah schoolID.
func ListByStudent(ctx context.Context, q database.DBTX, schoolID, studentID string) ([]StudentGuardian, error) {
	if !database.IsUUID(studentID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT `+guardianColumns+`, sg.relationship, sg.is_primary
		FROM student_guardians sg
		JOIN guardians g ON g.id = sg.guardian_id AND g.school_id = sg.school_id
		WHERE sg.school_id = $1 AND sg.student_id = $2
		ORDER BY sg.is_primary DESC, lower(g.full_name)`, schoolID, studentID)
	if err != nil {
		return nil, mapGuardianError("list guardians", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (StudentGuardian, error) {
		var sg StudentGuardian
		g := &sg.Guardian
		err := row.Scan(&g.ID, &g.SchoolID, &g.FullName, &g.Phone, &g.Email, &g.Address, &g.CreatedAt, &g.UpdatedAt,
			&sg.Relationship, &sg.IsPrimary)
		return sg, err
	})
	if err != nil {
		return nil, mapGuardianError("list guardians", err)
	}
	return list, nil
}

// ListByGuardian mengembalikan anak-anak seorang wali, urut nama. Hanya data
// sekolah schoolID.
func ListByGuardian(ctx context.Context, q database.DBTX, schoolID, guardianID string) ([]GuardianChild, error) {
	if !database.IsUUID(guardianID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT s.id, s.nis, s.full_name, sg.relationship, sg.is_primary
		FROM student_guardians sg
		JOIN students s ON s.id = sg.student_id AND s.school_id = sg.school_id
		WHERE sg.school_id = $1 AND sg.guardian_id = $2
		ORDER BY lower(s.full_name), s.nis`, schoolID, guardianID)
	if err != nil {
		return nil, mapGuardianError("list children", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GuardianChild, error) {
		var c GuardianChild
		err := row.Scan(&c.StudentID, &c.NIS, &c.FullName, &c.Relationship, &c.IsPrimary)
		return c, err
	})
	if err != nil {
		return nil, mapGuardianError("list children", err)
	}
	return list, nil
}

func scanGuardian(row pgx.CollectableRow) (Guardian, error) {
	var g Guardian
	err := row.Scan(&g.ID, &g.SchoolID, &g.FullName, &g.Phone, &g.Email, &g.Address, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

// mapGuardianError mengubah error database menjadi error domain tanpa
// membawa pesan PostgreSQL (yang bisa memuat data pribadi).
func mapGuardianError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGuardianNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "student_one_primary":
			return ErrDuplicatePrimary
		case pgErr.Code == pgCheckViolation && pgErr.ConstraintName == "student_guardians_relationship_check":
			return ErrInvalidRelationship
		case pgErr.Code == "23503" && pgErr.ConstraintName == "student_guardians_student_fkey":
			return ErrNotFound
		case pgErr.Code == "23503" && pgErr.ConstraintName == "student_guardians_guardian_fkey":
			return ErrGuardianNotFound
		}
		return fmt.Errorf("students: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("students: %s: %w", op, err)
}
