package admission

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// TrackScoreKey adalah nama isian nilai jalur metode nilai (T118).
const TrackScoreKey = "track_score"

// Aksi audit nilai jalur dan urutan manual (T118).
const (
	ActionTrackScore  = "admission.applicant_track_score"
	ActionManualRanks = "admission.manual_ranks"
)

// parseScore membaca nilai seperti "85", "85,5", atau "85.50" menjadi
// perseratus (8550). Paling banyak dua angka di belakang koma.
func parseScore(raw string) (int, bool) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if raw == "" {
		return 0, false
	}
	whole, frac, _ := strings.Cut(raw, ".")
	if whole == "" || len(frac) > 2 || len(whole) > 5 {
		return 0, false
	}
	w, err := strconv.Atoi(whole)
	if err != nil || w < 0 {
		return 0, false
	}
	f := 0
	if frac != "" {
		if f, err = strconv.Atoi(frac); err != nil || f < 0 {
			return 0, false
		}
		if len(frac) == 1 {
			f *= 10
		}
	}
	return w*100 + f, true
}

// formatScore menampilkan nilai perseratus dengan koma desimal, tanpa nol
// di belakang (8550 → "85,5", 9000 → "90").
func formatScore(v int) string {
	s := strconv.Itoa(v / 100)
	if f := v % 100; f != 0 {
		frac := strings.TrimRight(strconv.Itoa(100 + f)[1:], "0")
		s += "," + frac
	}
	return s
}

// ScoreText mengembalikan nilai jalur pendaftar untuk ditampilkan ("" jika
// belum diisi).
func (a Applicant) ScoreText() string {
	if a.TrackScore == nil {
		return ""
	}
	return formatScore(*a.TrackScore)
}

// validateTrackScore memeriksa isian nilai untuk jalur t. Kosong berarti
// belum diisi (nil); wajib diisi saat dikirim lewat Missing.
func validateTrackScore(t Track, raw string) (*int, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	v, ok := parseScore(raw)
	if !ok || v > t.ScoreMax*100 {
		return nil, fieldErr(TrackScoreKey, t.ScoreLabel+" harus angka 0–"+strconv.Itoa(t.ScoreMax)+", paling banyak dua angka di belakang koma.")
	}
	return &v, nil
}

// SetTrackScore mengoreksi nilai jalur pendaftar oleh TU saat verifikasi.
// Hanya untuk jalur metode nilai dan pendaftar yang belum diterima.
func (s *ApplicantService) SetTrackScore(ctx context.Context, actor school.Actor, id, raw string) (Applicant, error) {
	var a Applicant
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if a, err = s.load(ctx, tx, actor.SchoolID, id, true); err != nil {
			return err
		}
		if a.Track.Method != MethodScore || a.AnonymizedAt != nil || a.StudentID != "" ||
			(a.Status != ApplicantSubmitted && a.Status != ApplicantFix && a.Status != ApplicantVerified && a.Status != ApplicantReserve) {
			return ErrBadTransition
		}
		v, err := validateTrackScore(a.Track, raw)
		if err != nil {
			return err
		}
		if v == nil {
			return fieldErr(TrackScoreKey, a.Track.ScoreLabel+" wajib diisi.")
		}
		before := a.ScoreText()
		if _, err := tx.Exec(ctx, `UPDATE applicants SET track_score = $2, updated_at = now() WHERE id = $1`, id, *v); err != nil {
			return err
		}
		a.TrackScore = v
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionTrackScore, EntityType: "applicant", EntityID: id,
			Before: map[string]any{"score": before}, After: map[string]any{"score": a.ScoreText()}})
	})
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	return a, nil
}

// SetManualRanks menyimpan urutan manual pendaftar jalur metode manual di
// periode periodID. Nilai kosong menghapus urutan. Urutan 1–9999.
func (s *ApplicantService) SetManualRanks(ctx context.Context, actor school.Actor, periodID string, ranks map[string]string) (int, error) {
	if !database.IsUUID(periodID) {
		return 0, ErrNotFound
	}
	changed := 0
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true); err != nil {
			return err
		}
		fields := map[string]string{}
		for id, raw := range ranks {
			var rank *int
			if raw = strings.TrimSpace(raw); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 1 || n > 9999 {
					fields["rank_"+id] = "Urutan harus angka 1–9999."
					continue
				}
				rank = &n
			}
			if !database.IsUUID(id) {
				return ErrNotFound
			}
			tag, err := tx.Exec(ctx, `UPDATE applicants a SET manual_rank = $4, updated_at = now()
				FROM admission_tracks t
				WHERE a.school_id = $1 AND a.period_id = $2 AND a.id = $3 AND t.id = a.track_id AND t.method = 'manual'
				  AND a.status IN ('diverifikasi', 'cadangan') AND a.manual_rank IS DISTINCT FROM $4`,
				actor.SchoolID, periodID, id, rank)
			if err != nil {
				return err
			}
			changed += int(tag.RowsAffected())
		}
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}
		if changed == 0 {
			return nil
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionManualRanks, periodID, nil, map[string]any{"changed": changed}))
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}
