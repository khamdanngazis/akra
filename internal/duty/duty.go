// Package duty mengelola tugas tambahan staf per tahun ajaran (T125),
// mis. koordinator ujian. Tugas bukan peran login; haknya diterjemahkan
// oleh modul yang memakainya (mis. exam.LoadActor).
package duty

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Kode tugas. Nilainya sama dengan CHECK di migrasi 000061.
const (
	ExamCoordinator = "koordinator_ujian"
)

// Labels adalah nama tugas untuk tampilan.
var Labels = map[string]string{
	ExamCoordinator: "Koordinator ujian",
}

// Hints menjelaskan hak setiap tugas.
var Hints = map[string]string{
	ExamCoordinator: "Mengatur agenda PTS/PAS, ruang, sesi, dan pengawas; membuka semua bank soal; memantau semua ruang.",
}

// Aksi audit.
const (
	ActionAssign = "staff_duty.assign"
	ActionRemove = "staff_duty.remove"
)

// Errors.
var (
	ErrNotFound      = errors.New("duty: not found")
	ErrInvalidDuty   = errors.New("duty: invalid duty")
	ErrNotTeacher    = errors.New("duty: user is not an active teacher")
	ErrAlreadyExists = errors.New("duty: already assigned")
)

// Assignment adalah satu tugas tambahan.
type Assignment struct {
	ID       string
	YearID   string
	UserID   string
	FullName string
	Email    string
	Duty     string
}

// Label mengembalikan nama tugas.
func (a Assignment) Label() string { return Labels[a.Duty] }

// Actor adalah pengguna yang melakukan aksi (dari sesi).
type Actor struct {
	SchoolID string
	UserID   string
	IP       netip.Addr
}

// List mengembalikan tugas tambahan di tahun ajaran yearID, urut tugas lalu
// nama.
func List(ctx context.Context, q database.DBTX, schoolID, yearID string) ([]Assignment, error) {
	rows, err := q.Query(ctx, `
		SELECT d.id, d.academic_year_id, d.user_id, u.full_name, u.email, d.duty
		FROM staff_duties d
		JOIN users u ON u.id = d.user_id AND u.school_id = d.school_id
		WHERE d.school_id = $1 AND d.academic_year_id = $2
		ORDER BY d.duty, lower(u.full_name)`, schoolID, yearID)
	if err != nil {
		return nil, fmt.Errorf("duty: list: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Assignment, error) {
		var a Assignment
		err := r.Scan(&a.ID, &a.YearID, &a.UserID, &a.FullName, &a.Email, &a.Duty)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("duty: list: %w", err)
	}
	return out, nil
}

// HasActive melaporkan apakah userID punya tugas d di tahun ajaran aktif
// sekolah. Tugas tahun ajaran lain tidak berlaku.
func HasActive(ctx context.Context, q database.DBTX, schoolID, userID, d string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM staff_duties sd
			JOIN academic_years y ON y.id = sd.academic_year_id AND y.school_id = sd.school_id AND y.is_active
			WHERE sd.school_id = $1 AND sd.user_id = $2 AND sd.duty = $3)`, schoolID, userID, d).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("duty: check: %w", err)
	}
	return ok, nil
}

// Assign memberi tugas d kepada guru userID di tahun ajaran yearID.
func Assign(ctx context.Context, db database.DB, actor Actor, yearID, userID, d string) (Assignment, error) {
	if _, ok := Labels[d]; !ok {
		return Assignment{}, ErrInvalidDuty
	}
	var out Assignment
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := school.GetAcademicYear(ctx, tx, actor.SchoolID, yearID); err != nil {
			if errors.Is(err, school.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		ok, err := classes.IsActiveTeacher(ctx, tx, actor.SchoolID, userID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotTeacher
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO staff_duties (school_id, academic_year_id, user_id, duty, created_by)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`, actor.SchoolID, yearID, userID, d, actor.UserID).Scan(&out.ID)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("duty: assign: %w", err)
		}
		out.YearID, out.UserID, out.Duty = yearID, userID, d
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, Action: ActionAssign, EntityType: "staff_duty", EntityID: out.ID,
			After: map[string]string{"academic_year_id": yearID, "user_id": userID, "duty": d}, IP: actor.IP,
		})
	})
	return out, err
}

// Remove mencabut tugas id milik sekolah actor.
func Remove(ctx context.Context, db database.DB, actor Actor, id string) error {
	if !database.IsUUID(id) {
		return ErrNotFound
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var yearID, userID, d string
		err := tx.QueryRow(ctx, `DELETE FROM staff_duties WHERE school_id = $1 AND id = $2
			RETURNING academic_year_id, user_id, duty`, actor.SchoolID, id).Scan(&yearID, &userID, &d)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("duty: remove: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, Action: ActionRemove, EntityType: "staff_duty", EntityID: id,
			Before: map[string]string{"academic_year_id": yearID, "user_id": userID, "duty": d}, IP: actor.IP,
		})
	})
}
