package admission

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

// Aksi audit pengaturan pendaftaran dan daftar ulang (T120).
const (
	ActionAdmissionSettings = "admission.settings"
	ActionReRegister        = "admission.applicant_reregister"
)

// UsesReRegistration melaporkan apakah sekolah memakai daftar ulang
// sebelum pendaftar yang diterima dijadikan siswa (bawaan: ya).
func UsesReRegistration(ctx context.Context, q database.DBTX, schoolID string) (bool, error) {
	v, ok, err := settings.Get(ctx, q, schoolID, settings.AdmissionReRegistration)
	if err != nil {
		return true, err
	}
	return !ok || v != "false", nil
}

// AdmissionSettings adalah pengaturan pendaftaran tingkat sekolah.
type AdmissionSettings struct {
	ReRegistration bool
	NISFormat      string
}

// SaveAdmissionSettings menyimpan cara menjadikan siswa dan pola NIS.
func SaveAdmissionSettings(ctx context.Context, db database.DB, actor school.Actor, in AdmissionSettings) error {
	if err := ValidateNISFormat(in.NISFormat); err != nil {
		return err
	}
	reg := "true"
	if !in.ReRegistration {
		reg = "false"
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := settings.Set(ctx, tx, actor.SchoolID, settings.AdmissionReRegistration, reg); err != nil {
			return err
		}
		if err := settings.Set(ctx, tx, actor.SchoolID, settings.AdmissionNISFormat, in.NISFormat); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionAdmissionSettings, EntityType: "school", EntityID: actor.SchoolID,
			After: map[string]any{"reregistration": in.ReRegistration, "nis_format": in.NISFormat}})
	})
}

// enrollWithoutReRegistration langsung menjadikan pendaftar siswa saat
// diterima, jika sekolah tidak memakai daftar ulang. Berjalan di dalam
// transaksi Transition sehingga gagal menjadikan siswa membatalkan
// penerimaan juga.
func (s *ApplicantService) enrollWithoutReRegistration(ctx context.Context, tx pgx.Tx, actor school.Actor, id string) (string, error) {
	uses, err := UsesReRegistration(ctx, tx, actor.SchoolID)
	if err != nil || uses {
		return "", err
	}
	txs := *s
	txs.db = tx
	_, nis, _, err := NewEnroller(tx, s.keys, &txs).Enroll(ctx, actor, id, EnrollOptions{})
	return nis, err
}

// SetReRegistered menandai (atau membatalkan tanda) pendaftar diterima
// sudah daftar ulang.
func (s *ApplicantService) SetReRegistered(ctx context.Context, actor school.Actor, id string, done bool) (Applicant, error) {
	if !database.IsUUID(id) {
		return Applicant{}, ErrNotFound
	}
	var a Applicant
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if a, err = s.load(ctx, tx, actor.SchoolID, id, true); err != nil {
			return err
		}
		var studentID *string
		if err := tx.QueryRow(ctx, `SELECT student_id::text FROM applicants WHERE id = $1`, id).Scan(&studentID); err != nil {
			return err
		}
		if a.Status != ApplicantAccepted || studentID != nil {
			return ErrNotAccepted
		}
		var at *time.Time
		if done {
			now := s.now()
			at = &now
		}
		if _, err := tx.Exec(ctx, `UPDATE applicants SET reregistered_at = $2, updated_at = now() WHERE id = $1`, id, at); err != nil {
			return err
		}
		a.ReRegisteredAt = at
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionReRegister, EntityType: "applicant", EntityID: id, After: map[string]any{"reregistered": done}})
	})
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	return a, nil
}
