package admission

import (
	"context"
	"sort"
	"strings"
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
	// TrackScore (perseratus) untuk metode nilai, ManualRank untuk metode
	// manual (0 = belum diisi) (T118).
	TrackScore int
	ManualRank int
	Complete   bool // data urutan lengkap: tes dinilai, nilai diisi, atau urutan manual diisi
	Rank       int  // 0 jika belum bisa diperingkat
	Proposal   string
}

// TrackRanking adalah peringkat satu jalur.
type TrackRanking struct {
	Track     Track
	Accepted  int // sudah diterima sebelumnya
	Remaining int // sisa kuota (0 jika kuota tanpa batas)
	Rows      []Ranked
	// Pending adalah pendaftar jalur ini yang belum diverifikasi (dikirim
	// atau perlu perbaikan), sehingga belum ikut peringkat.
	Pending int
}

// Basis menjelaskan dasar urutan jalur untuk ditampilkan.
func (r TrackRanking) Basis() string {
	switch r.Track.Method {
	case MethodTest:
		return "Diurutkan dari skor tes tertinggi; yang nilainya belum lengkap di bawah tanpa peringkat."
	case MethodScore:
		return "Diurutkan dari " + strings.ToLower(r.Track.ScoreLabel) + " tertinggi; yang belum mengisi nilai di bawah tanpa peringkat."
	case MethodManual:
		return "Diurutkan dari urutan yang diisi TU di kolom Urutan; yang belum diberi urutan di bawah tanpa peringkat."
	}
	return "Diurutkan dari waktu kirim formulir paling awal (siapa cepat dia dapat)."
}

// ScoreText menampilkan nilai jalur metode nilai.
func (r Ranked) ScoreText() string {
	if !r.Complete {
		return ""
	}
	return formatScore(r.TrackScore)
}

// RankTrack mengurutkan pendaftar satu jalur sesuai metodenya (T118) dan
// membuat usulan:
//   - tes: skor akhir tes menurun;
//   - nilai: nilai jalur menurun;
//   - manual: urutan dari TU menaik;
//   - urutan daftar: waktu kirim paling awal.
//
// Pendaftar yang data urutannya belum lengkap ada di bawah tanpa peringkat.
// Seri diurutkan dari waktu kirim paling awal. Peringkat 1 sampai sisa kuota
// diusulkan diterima, sisanya cadangan; kuota 0 berarti tanpa batas.
func RankTrack(t Track, accepted int, rows []Ranked) TrackRanking {
	if t.Method == "" { // jalur dari data lama tanpa metode
		t.Method = MethodOrder
		if t.UsesTest {
			t.Method = MethodTest
		}
	}
	if t.Method == MethodOrder {
		for i := range rows {
			rows[i].Complete = true
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Complete != b.Complete {
			return a.Complete
		}
		switch t.Method {
		case MethodTest:
			if a.Score != b.Score {
				return a.Score > b.Score
			}
		case MethodScore:
			if a.TrackScore != b.TrackScore {
				return a.TrackScore > b.TrackScore
			}
		case MethodManual:
			if a.ManualRank != b.ManualRank {
				return a.ManualRank < b.ManualRank
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
		if !rows[i].Complete {
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
		SELECT a.id, coalesce(a.registration_number, ''), a.full_name, a.status, a.track_id, a.submitted_at, a.track_score, a.manual_rank
		FROM applicants a WHERE a.school_id = $1 AND a.period_id = $2 AND a.status IN ('diverifikasi', 'cadangan')`, schoolID, periodID)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	byTrack := map[string][]Ranked{}
	scoreSet := map[string]bool{}
	for rows.Next() {
		var r Ranked
		var trackID string
		var submitted *time.Time
		var score, rank *int
		if err := rows.Scan(&r.ApplicantID, &r.RegistrationNumber, &r.FullName, &r.Status, &trackID, &submitted, &score, &rank); err != nil {
			rows.Close()
			return nil, mapApplicantError(err)
		}
		if submitted != nil {
			r.SubmittedAt = *submitted
		}
		if score != nil {
			r.TrackScore = *score
			scoreSet[r.ApplicantID] = true
		}
		if rank != nil {
			r.ManualRank = *rank
		}
		byTrack[trackID] = append(byTrack[trackID], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapApplicantError(err)
	}

	accepted, pending := map[string]int{}, map[string]int{}
	arows, err := s.db.Query(ctx, `SELECT track_id, count(*) FILTER (WHERE status = 'diterima'), count(*) FILTER (WHERE status IN ('dikirim', 'perlu_perbaikan'))
		FROM applicants WHERE school_id = $1 AND period_id = $2 GROUP BY track_id`, schoolID, periodID)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	for arows.Next() {
		var t string
		var n, p int
		if err := arows.Scan(&t, &n, &p); err != nil {
			arows.Close()
			return nil, mapApplicantError(err)
		}
		accepted[t], pending[t] = n, p
	}
	arows.Close()

	var out []TrackRanking
	for _, t := range p.Tracks {
		list := byTrack[t.ID]
		for i := range list {
			switch t.Method {
			case MethodTest:
				list[i].Score, list[i].Complete = FinalScore(tests, scores[list[i].ApplicantID])
				list[i].Complete = list[i].Complete && weightsOK
			case MethodScore:
				list[i].Complete = scoreSet[list[i].ApplicantID]
			case MethodManual:
				list[i].Complete = list[i].ManualRank > 0
			default:
				list[i].Complete = true
			}
		}
		tr := RankTrack(t, accepted[t.ID], list)
		tr.Pending = pending[t.ID]
		out = append(out, tr)
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
