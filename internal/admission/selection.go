package admission

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit tes seleksi (T93a).
const (
	ActionTestCreate = "admission.test_create"
	ActionTestUpdate = "admission.test_update"
	ActionTestDelete = "admission.test_delete"
	ActionScoresSave = "admission.scores_save"
	scheduleLayout   = "2006-01-02T15:04"
	maxTestLocation  = 120
)

// ErrWeights dikembalikan jika jumlah bobot tes periode bukan 100.
var ErrWeights = errors.New("admission: test weights must total 100")

// SelectionTest adalah satu tes seleksi di sebuah periode.
type SelectionTest struct {
	ID          string
	PeriodID    string
	Name        string
	Weight      int
	MaxScore    int
	ScheduledAt *time.Time
	Location    string
	SortOrder   int
}

// TestForm adalah isian form tes apa adanya.
type TestForm struct {
	Name        string
	Weight      string
	MaxScore    string
	ScheduledAt string // YYYY-MM-DDTHH:MM (WIB), boleh kosong
	Location    string
}

// SelectionService mengelola tes seleksi dan nilainya.
type SelectionService struct {
	db database.DB
}

// NewSelectionService mengembalikan SelectionService.
func NewSelectionService(db database.DB) *SelectionService {
	return &SelectionService{db: db}
}

// Tests mengembalikan tes periode, urut tampil.
func (s *SelectionService) Tests(ctx context.Context, schoolID, periodID string) ([]SelectionTest, error) {
	if !database.IsUUID(periodID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT id, period_id, name, weight, max_score, scheduled_at, coalesce(location, ''), sort_order
		FROM admission_tests WHERE school_id = $1 AND period_id = $2 ORDER BY sort_order, name`, schoolID, periodID)
	if err != nil {
		return nil, mapError("list tests", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SelectionTest, error) {
		var t SelectionTest
		var w, m int16
		err := r.Scan(&t.ID, &t.PeriodID, &t.Name, &w, &m, &t.ScheduledAt, &t.Location, &t.SortOrder)
		t.Weight, t.MaxScore = int(w), int(m)
		return t, err
	})
	if err != nil {
		return nil, mapError("list tests", err)
	}
	return list, nil
}

// WeightsTotal mengembalikan jumlah bobot tes.
func WeightsTotal(tests []SelectionTest) int {
	n := 0
	for _, t := range tests {
		n += t.Weight
	}
	return n
}

type testInput struct {
	Name        string
	Weight      int
	MaxScore    int
	ScheduledAt *time.Time
	Location    *string
}

func validateTest(f TestForm) (testInput, error) {
	fields := map[string]string{}
	in := testInput{Name: strings.Join(strings.Fields(f.Name), " ")}
	switch {
	case in.Name == "":
		fields["test_name"] = "Nama tes wajib diisi."
	case utf8.RuneCountInString(in.Name) > 60:
		fields["test_name"] = "Nama tes maksimal 60 karakter."
	}
	if w, err := strconv.Atoi(strings.TrimSpace(f.Weight)); err != nil || w < 1 || w > 100 {
		fields["weight"] = "Bobot harus 1–100 (persen)."
	} else {
		in.Weight = w
	}
	in.MaxScore = 100
	if m := strings.TrimSpace(f.MaxScore); m != "" {
		if n, err := strconv.Atoi(m); err != nil || n < 1 || n > 1000 {
			fields["max_score"] = "Nilai maksimal harus 1–1.000."
		} else {
			in.MaxScore = n
		}
	}
	if v := strings.TrimSpace(f.ScheduledAt); v != "" {
		t, err := time.ParseInLocation(scheduleLayout, v, clock.Jakarta)
		if err != nil {
			fields["scheduled_at"] = "Jadwal tidak valid."
		} else {
			in.ScheduledAt = &t
		}
	}
	if loc := strings.Join(strings.Fields(f.Location), " "); loc != "" {
		if utf8.RuneCountInString(loc) > maxTestLocation {
			fields["location"] = fmt.Sprintf("Lokasi maksimal %d karakter.", maxTestLocation)
		}
		in.Location = &loc
	}
	if len(fields) > 0 {
		return testInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

// AddTest menambah tes ke periode.
func (s *SelectionService) AddTest(ctx context.Context, actor school.Actor, periodID string, f TestForm) (SelectionTest, error) {
	in, err := validateTest(f)
	if err != nil {
		return SelectionTest{}, err
	}
	var t SelectionTest
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO admission_tests (school_id, period_id, name, weight, max_score, scheduled_at, location, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7, (SELECT coalesce(max(sort_order), 0) + 1 FROM admission_tests WHERE period_id = $2))
			RETURNING id`, actor.SchoolID, periodID, in.Name, in.Weight, in.MaxScore, in.ScheduledAt, in.Location).Scan(&t.ID); err != nil {
			return mapError("add test", err)
		}
		t = SelectionTest{ID: t.ID, PeriodID: periodID, Name: in.Name, Weight: in.Weight, MaxScore: in.MaxScore, ScheduledAt: in.ScheduledAt}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTestCreate, periodID, nil, map[string]any{"test_id": t.ID, "name": t.Name, "weight": t.Weight, "max_score": t.MaxScore}))
	})
	if err != nil {
		return SelectionTest{}, testFieldError(err)
	}
	return t, nil
}

// UpdateTest mengubah tes. Nilai maksimal tidak boleh lebih kecil dari
// nilai yang sudah tercatat.
func (s *SelectionService) UpdateTest(ctx context.Context, actor school.Actor, periodID, testID string, f TestForm) error {
	in, err := validateTest(f)
	if err != nil {
		return err
	}
	if !database.IsUUID(testID) {
		return ErrNotFound
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true); err != nil {
			return err
		}
		var top *float64
		if err := tx.QueryRow(ctx, `SELECT max(score)::float8 FROM applicant_test_scores WHERE test_id = $1`, testID).Scan(&top); err != nil {
			return err
		}
		if top != nil && *top > float64(in.MaxScore) {
			return fieldErr("max_score", "Sudah ada nilai di atas nilai maksimal baru.")
		}
		tag, err := tx.Exec(ctx, `UPDATE admission_tests SET name = $4, weight = $5, max_score = $6, scheduled_at = $7, location = $8
			WHERE school_id = $1 AND period_id = $2 AND id = $3`, actor.SchoolID, periodID, testID, in.Name, in.Weight, in.MaxScore, in.ScheduledAt, in.Location)
		if err != nil {
			return mapError("update test", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTestUpdate, periodID, nil, map[string]any{"test_id": testID, "name": in.Name, "weight": in.Weight, "max_score": in.MaxScore}))
	})
	return testFieldError(err)
}

// DeleteTest menghapus tes yang belum punya nilai terisi.
func (s *SelectionService) DeleteTest(ctx context.Context, actor school.Actor, periodID, testID string) error {
	if !database.IsUUID(testID) {
		return ErrNotFound
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true); err != nil {
			return err
		}
		// Baris nilai kosong (NULL) bukan nilai; hapus agar tes bisa dihapus.
		if _, err := tx.Exec(ctx, `DELETE FROM applicant_test_scores WHERE school_id = $1 AND test_id = $2 AND score IS NULL`, actor.SchoolID, testID); err != nil {
			return mapError("delete empty scores", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM admission_tests WHERE school_id = $1 AND period_id = $2 AND id = $3`, actor.SchoolID, periodID, testID)
		if err != nil {
			return deleteError(mapError("delete test", err))
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTestDelete, periodID, map[string]any{"test_id": testID}, nil))
	})
}

func testFieldError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "admission_tests_name_key" {
		return fieldErr("test_name", "Nama tes sudah dipakai di gelombang ini.")
	}
	return err
}

// ScoreRow adalah satu baris grid nilai.
type ScoreRow struct {
	ApplicantID        string
	RegistrationNumber string
	FullName           string
	TrackName          string
	Score              string // "" = belum dinilai
}

// Grid mengembalikan pendaftar yang bisa dinilai untuk tes testID:
// berstatus diverifikasi di jalur yang memakai tes.
func (s *SelectionService) Grid(ctx context.Context, schoolID, periodID, testID string) ([]ScoreRow, error) {
	if !database.IsUUID(periodID) || !database.IsUUID(testID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT a.id, coalesce(a.registration_number, ''), a.full_name, t.name, coalesce(sc.score::text, '')
		FROM applicants a
		JOIN admission_tracks t ON t.id = a.track_id AND t.uses_test
		LEFT JOIN applicant_test_scores sc ON sc.applicant_id = a.id AND sc.test_id = $3
		WHERE a.school_id = $1 AND a.period_id = $2 AND a.status = 'diverifikasi'
		  AND EXISTS (SELECT 1 FROM admission_tests x WHERE x.id = $3 AND x.period_id = $2 AND x.school_id = $1)
		ORDER BY t.sort_order, a.registration_number`, schoolID, periodID, testID)
	if err != nil {
		return nil, mapError("score grid", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ScoreRow, error) {
		var x ScoreRow
		err := r.Scan(&x.ApplicantID, &x.RegistrationNumber, &x.FullName, &x.TrackName, &x.Score)
		x.Score = trimScore(x.Score)
		return x, err
	})
	if err != nil {
		return nil, mapError("score grid", err)
	}
	return list, nil
}

// trimScore menulis "87.50" sebagai "87.5" dan "90.00" sebagai "90".
func trimScore(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// SaveScores menyimpan nilai satu tes untuk banyak pendaftar dalam satu
// transaksi. Nilai kosong disimpan sebagai NULL (belum dinilai). Pendaftar
// di luar grid (belum diverifikasi, jalur tanpa tes, sekolah lain) ditolak.
func (s *SelectionService) SaveScores(ctx context.Context, actor school.Actor, periodID, testID string, scores map[string]string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tests, err := (&SelectionService{db: tx}).Tests(ctx, actor.SchoolID, periodID)
		if err != nil {
			return err
		}
		var test *SelectionTest
		for i := range tests {
			if tests[i].ID == testID {
				test = &tests[i]
			}
		}
		if test == nil {
			return ErrNotFound
		}
		grid, err := (&SelectionService{db: tx}).Grid(ctx, actor.SchoolID, periodID, testID)
		if err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, g := range grid {
			allowed[g.ApplicantID] = true
		}
		fields := map[string]string{}
		changed := 0
		for id, raw := range scores {
			if !allowed[id] {
				return ErrNotFound
			}
			v := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
			var score any
			if v != "" {
				x, err := strconv.ParseFloat(v, 64)
				_, frac, _ := strings.Cut(v, ".")
				if err != nil || x < 0 || x > float64(test.MaxScore) || len(frac) > 2 || strings.ContainsAny(v, "eE+-") {
					fields["score_"+id] = fmt.Sprintf("Nilai harus 0–%d, paling banyak dua angka di belakang koma.", test.MaxScore)
					continue
				}
				score = v
			}
			tag, err := tx.Exec(ctx, `
				INSERT INTO applicant_test_scores (school_id, applicant_id, test_id, score, recorded_by)
				VALUES ($1, $2, $3, $4::numeric, $5)
				ON CONFLICT (applicant_id, test_id) DO UPDATE SET score = EXCLUDED.score, recorded_by = EXCLUDED.recorded_by, updated_at = now()
				WHERE applicant_test_scores.score IS DISTINCT FROM EXCLUDED.score`,
				actor.SchoolID, id, testID, score, actor.UserID)
			if err != nil {
				return mapError("save score", err)
			}
			changed += int(tag.RowsAffected())
		}
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}
		if changed == 0 {
			return nil
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionScoresSave, periodID, nil, map[string]any{"test_id": testID, "changed": changed}))
	})
}

// FinalScore menghitung skor akhir (0–100) dari nilai per tes. complete
// bernilai false jika ada tes yang belum dinilai; skornya tetap dihitung
// dari tes yang ada, tetapi tidak boleh dipakai untuk peringkat.
func FinalScore(tests []SelectionTest, scores map[string]float64) (score float64, complete bool) {
	complete = len(tests) > 0
	for _, t := range tests {
		v, ok := scores[t.ID]
		if !ok {
			complete = false
			continue
		}
		score += v / float64(t.MaxScore) * float64(t.Weight)
	}
	return math.Round(score*100) / 100, complete
}

// Scores mengembalikan nilai per pendaftar per tes untuk satu periode:
// map[applicantID]map[testID]nilai (hanya yang sudah dinilai).
func (s *SelectionService) Scores(ctx context.Context, schoolID, periodID string) (map[string]map[string]float64, error) {
	rows, err := s.db.Query(ctx, `SELECT sc.applicant_id, sc.test_id, sc.score::float8
		FROM applicant_test_scores sc JOIN admission_tests t ON t.id = sc.test_id
		WHERE sc.school_id = $1 AND t.period_id = $2 AND sc.score IS NOT NULL`, schoolID, periodID)
	if err != nil {
		return nil, mapError("list scores", err)
	}
	defer rows.Close()
	out := map[string]map[string]float64{}
	for rows.Next() {
		var a, t string
		var v float64
		if err := rows.Scan(&a, &t, &v); err != nil {
			return nil, mapError("list scores", err)
		}
		if out[a] == nil {
			out[a] = map[string]float64{}
		}
		out[a][t] = v
	}
	return out, rows.Err()
}
