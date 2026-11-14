package admission

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/school"
)

// Usulan hasil seleksi (T93b).
const (
	ProposalAccept     = ApplicantAccepted
	ProposalReserve    = ApplicantReserve
	ProposalIncomplete = "" // nilai tes belum lengkap: tidak diusulkan
)

// Ranked adalah satu pendaftar di peringkat jalurnya.
type Ranked struct {
	ApplicantID        string
	RegistrationNumber string
	FullName           string
	Status             string
	SubmittedAt        time.Time
	Score              float64
	Complete           bool // semua tes sudah dinilai (atau jalur tanpa tes)
	Rank               int  // 0 jika belum bisa diperingkat
	Proposal           string
}

// TrackRanking adalah peringkat satu jalur.
type TrackRanking struct {
	Track     Track
	Accepted  int // sudah diterima sebelumnya
	Remaining int // sisa kuota (0 jika kuota tanpa batas)
	Rows      []Ranked
}

// RankTrack mengurutkan pendaftar satu jalur dan membuat usulan:
//   - jalur dengan tes: skor akhir menurun, lalu waktu kirim paling awal;
//     pendaftar dengan nilai belum lengkap di bawah tanpa peringkat;
//   - jalur tanpa tes: waktu kirim paling awal.
//
// Peringkat 1 sampai sisa kuota diusulkan diterima, sisanya cadangan.
// Kuota 0 berarti tanpa batas: semua yang bisa diperingkat diusulkan diterima.
func RankTrack(t Track, accepted int, rows []Ranked) TrackRanking {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if t.UsesTest {
			if a.Complete != b.Complete {
				return a.Complete
			}
			if a.Score != b.Score {
				return a.Score > b.Score
			}
		}
		if !a.SubmittedAt.Equal(b.SubmittedAt) {
			return a.SubmittedAt.Before(b.SubmittedAt)
		}
		return a.RegistrationNumber < b.RegistrationNumber
	})
	out := TrackRanking{Track: t, Accepted: accepted}
	remaining := -1 // tanpa batas
	if t.Quota > 0 {
		remaining = t.Quota - accepted
		if remaining < 0 {
			remaining = 0
		}
		out.Remaining = remaining
	}
	rank := 0
	for i := range rows {
		if t.UsesTest && !rows[i].Complete {
			rows[i].Proposal = ProposalIncomplete
			continue
		}
		rank++
		rows[i].Rank = rank
		if remaining < 0 || rank <= remaining {
			rows[i].Proposal = ProposalAccept
		} else {
			rows[i].Proposal = ProposalReserve
		}
	}
	out.Rows = rows
	return out
}

// Ranking menyusun peringkat semua jalur periode, untuk pendaftar berstatus
// diverifikasi atau cadangan.
func (s *ApplicantService) Ranking(ctx context.Context, schoolID, periodID string) ([]TrackRanking, error) {
	p, err := getPeriod(ctx, s.db, schoolID, periodID, false)
	if err != nil {
		return nil, err
	}
	sel := NewSelectionService(s.db)
	tests, err := sel.Tests(ctx, schoolID, periodID)
	if err != nil {
		return nil, err
	}
	scores, err := sel.Scores(ctx, schoolID, periodID)
	if err != nil {
		return nil, err
	}
	weightsOK := WeightsTotal(tests) == 100

	rows, err := s.db.Query(ctx, `
		SELECT a.id, coalesce(a.registration_number, ''), a.full_name, a.status, a.track_id, a.submitted_at
		FROM applicants a WHERE a.school_id = $1 AND a.period_id = $2 AND a.status IN ('diverifikasi', 'cadangan')`, schoolID, periodID)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	byTrack := map[string][]Ranked{}
	for rows.Next() {
		var r Ranked
		var trackID string
		var submitted *time.Time
		if err := rows.Scan(&r.ApplicantID, &r.RegistrationNumber, &r.FullName, &r.Status, &trackID, &submitted); err != nil {
			rows.Close()
			return nil, mapApplicantError(err)
		}
		if submitted != nil {
			r.SubmittedAt = *submitted
		}
		byTrack[trackID] = append(byTrack[trackID], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapApplicantError(err)
	}

	accepted := map[string]int{}
	arows, err := s.db.Query(ctx, `SELECT track_id, count(*) FROM applicants WHERE school_id = $1 AND period_id = $2 AND status = 'diterima' GROUP BY track_id`, schoolID, periodID)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	for arows.Next() {
		var t string
		var n int
		if err := arows.Scan(&t, &n); err != nil {
			arows.Close()
			return nil, mapApplicantError(err)
		}
		accepted[t] = n
	}
	arows.Close()

	var out []TrackRanking
	for _, t := range p.Tracks {
		list := byTrack[t.ID]
		for i := range list {
			if t.UsesTest {
				list[i].Score, list[i].Complete = FinalScore(tests, scores[list[i].ApplicantID])
				list[i].Complete = list[i].Complete && weightsOK
			} else {
				list[i].Complete = true
			}
		}
		out = append(out, RankTrack(t, accepted[t.ID], list))
	}
	return out, nil
}

// ApplyResults menetapkan hasil untuk banyak pendaftar sekaligus dalam satu
// transaksi. results berisi status tujuan per pendaftar; pendaftar yang
// statusnya sudah sama dilewati (tidak membuat riwayat ganda). Mengembalikan
// jumlah pendaftar yang berubah dan apakah ada jalur yang melebihi kuota.
func (s *ApplicantService) ApplyResults(ctx context.Context, actor school.Actor, periodID string, results map[string]string) (changed int, overQuota bool, err error) {
	if !database.IsUUID(periodID) {
		return 0, false, ErrNotFound
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true); err != nil {
			return err
		}
		txs := *s
		txs.db = tx
		for id, to := range results {
			if to == "" {
				continue
			}
			if to != ApplicantAccepted && to != ApplicantReserve && to != ApplicantRejected {
				return ErrBadTransition
			}
			a, err := txs.load(ctx, tx, actor.SchoolID, id, false)
			if err != nil {
				return err
			}
			if a.PeriodID != periodID {
				return ErrNotFound
			}
			if a.Status == to {
				continue
			}
			res, err := txs.Transition(ctx, actor, id, to, "Penetapan hasil seleksi")
			if err != nil {
				return err
			}
			changed++
			overQuota = overQuota || res.OverQuota
		}
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return changed, overQuota, nil
}
