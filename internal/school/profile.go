package school

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// ActionSchoolTimezone adalah aksi audit perubahan zona waktu sekolah.
const ActionSchoolTimezone = "school.timezone"

// Profile adalah data profil sekolah yang tampil di Pengaturan → Profil
// sekolah (T126). Nama dan NPSN hanya dibaca; diatur pengelola platform.
type Profile struct {
	Name     string
	NPSN     string
	Code     string
	Timezone string
}

// ProfileService membaca dan mengubah profil sekolah.
type ProfileService struct {
	db database.DB
}

// NewProfileService mengembalikan ProfileService.
func NewProfileService(db database.DB) *ProfileService {
	return &ProfileService{db: db}
}

// Get mengembalikan profil sekolah schoolID.
func (s *ProfileService) Get(ctx context.Context, schoolID string) (Profile, error) {
	var p Profile
	err := s.db.QueryRow(ctx, `SELECT name, coalesce(npsn, ''), coalesce(code, ''), timezone FROM schools WHERE id = $1`, schoolID).
		Scan(&p.Name, &p.NPSN, &p.Code, &p.Timezone)
	if err != nil {
		return Profile{}, fmt.Errorf("school: get profile: %w", err)
	}
	return p, nil
}

// ErrInvalidTimezone dikembalikan bila zona waktu bukan WIB/WITA/WIT.
var ErrInvalidTimezone = errors.New("school: invalid timezone")

// SetTimezone mengubah zona waktu sekolah beserta auditnya. changed false
// bila zona tidak berubah (tanpa audit).
func (s *ProfileService) SetTimezone(ctx context.Context, actor Actor, name string) (changed bool, err error) {
	if !clock.ValidZone(name) {
		return false, ErrInvalidTimezone
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var before string
		if err := tx.QueryRow(ctx, `SELECT timezone FROM schools WHERE id = $1 FOR UPDATE`, actor.SchoolID).Scan(&before); err != nil {
			return fmt.Errorf("school: lock profile: %w", err)
		}
		if before == name {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE schools SET timezone = $2, updated_at = now() WHERE id = $1`, actor.SchoolID, name); err != nil {
			return fmt.Errorf("school: set timezone: %w", err)
		}
		changed = true
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, Action: ActionSchoolTimezone,
			EntityType: "school", EntityID: actor.SchoolID,
			Before: map[string]string{"timezone": before}, After: map[string]string{"timezone": name}, IP: actor.IP,
		})
	})
	return changed, err
}
