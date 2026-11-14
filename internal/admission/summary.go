package admission

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Count adalah satu baris rekap.
type Count struct {
	Label string
	N     int
}

// TrackSummary adalah rekap satu jalur.
type TrackSummary struct {
	Period    string
	Track     string
	Quota     int
	Submitted int // semua yang sudah mengirim (bukan draf, bukan mundur)
	Accepted  int
	Remaining int // -1 = tanpa batas
}

// Summary adalah rekap pendaftaran (T97). Hanya pendaftar yang sudah
// mengirim formulir (draf tidak dihitung).
type Summary struct {
	Total     int
	ByStatus  []Count
	ByTrack   []TrackSummary
	ByGender  []Count
	BySchool  []Count // sekolah asal terbanyak
	ByDay     []Count // jumlah kiriman per hari, 30 hari terakhir
	Drafts    int
	Enrolled  int
	PeriodIDs []string
}

func countRows(ctx context.Context, q database.DBTX, sql string, args ...any) ([]Count, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Count, error) {
		var c Count
		return c, r.Scan(&c.Label, &c.N)
	})
	if err != nil {
		return nil, mapApplicantError(err)
	}
	return list, nil
}

// Summarize menyusun rekap pendaftaran sekolah, opsional untuk satu periode.
func (s *ApplicantService) Summarize(ctx context.Context, schoolID, periodID string, today time.Time) (Summary, error) {
	var sum Summary
	pid := uuidOrNil(periodID)
	base := `FROM applicants a WHERE a.school_id = $1 AND ($2::uuid IS NULL OR a.period_id = $2::uuid) AND a.status <> 'draf'`
	if err := s.db.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE a.student_id IS NOT NULL) `+base, schoolID, pid).Scan(&sum.Total, &sum.Enrolled); err != nil {
		return Summary{}, mapApplicantError(err)
	}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM applicants a WHERE a.school_id = $1 AND ($2::uuid IS NULL OR a.period_id = $2::uuid) AND a.status = 'draf'`,
		schoolID, pid).Scan(&sum.Drafts); err != nil {
		return Summary{}, mapApplicantError(err)
	}
	byStatus, err := countRows(ctx, s.db, `SELECT a.status, count(*) `+base+` GROUP BY a.status`, schoolID, pid)
	if err != nil {
		return Summary{}, err
	}
	counts := map[string]int{}
	for _, c := range byStatus {
		counts[c.Label] = c.N
	}
	for _, st := range ApplicantStatuses {
		if st != ApplicantDraft {
			sum.ByStatus = append(sum.ByStatus, Count{Label: StatusLabels[st], N: counts[st]})
		}
	}
	if sum.ByGender, err = countRows(ctx, s.db, `SELECT CASE a.gender WHEN 'L' THEN 'Laki-laki' WHEN 'P' THEN 'Perempuan' ELSE 'Tidak diisi' END, count(*) `+base+` GROUP BY 1 ORDER BY 1`, schoolID, pid); err != nil {
		return Summary{}, err
	}
	if sum.BySchool, err = countRows(ctx, s.db, `SELECT coalesce(nullif(a.profile->>'previous_school', ''), 'Tidak diisi'), count(*) `+base+` GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 20`, schoolID, pid); err != nil {
		return Summary{}, err
	}
	if sum.ByDay, err = countRows(ctx, s.db, `SELECT to_char((a.submitted_at AT TIME ZONE 'Asia/Jakarta')::date, 'YYYY-MM-DD'), count(*) `+base+`
		AND a.submitted_at >= $3 GROUP BY 1 ORDER BY 1`, schoolID, pid, today.AddDate(0, 0, -29)); err != nil {
		return Summary{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.name, t.name, coalesce(t.quota, 0),
		       count(a.id) FILTER (WHERE a.status NOT IN ('draf', 'mundur')),
		       count(a.id) FILTER (WHERE a.status = 'diterima')
		FROM admission_tracks t JOIN admission_periods p ON p.id = t.period_id
		LEFT JOIN applicants a ON a.track_id = t.id
		WHERE t.school_id = $1 AND ($2::uuid IS NULL OR t.period_id = $2::uuid)
		GROUP BY p.name, p.opens_on, t.name, t.quota, t.sort_order ORDER BY p.opens_on, t.sort_order`, schoolID, pid)
	if err != nil {
		return Summary{}, mapApplicantError(err)
	}
	sum.ByTrack, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TrackSummary, error) {
		var t TrackSummary
		err := r.Scan(&t.Period, &t.Track, &t.Quota, &t.Submitted, &t.Accepted)
		t.Remaining = -1
		if t.Quota > 0 {
			t.Remaining = max(t.Quota-t.Accepted, 0)
		}
		return t, err
	})
	if err != nil {
		return Summary{}, mapApplicantError(err)
	}
	return sum, nil
}
