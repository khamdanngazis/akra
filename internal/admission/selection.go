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
	"akra/internal/exam"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/module"
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
	// Rubric adalah acuan penilaian; PassScore nilai minimal lulus (0 =
	// tanpa batas) (T119).
	Rubric    string
	PassScore int
	// ExamID: ujian seleksi online (T149); kosong = di kertas/wawancara.
	ExamID   string
	ExamName string
}

// Online melaporkan tes dikerjakan sebagai ujian online.
func (t SelectionTest) Online() bool { return t.ExamID != "" }

// Cara tes (T149).
const (
	TestMethodPaper  = "kertas"
	TestMethodOnline = "online"
)

// ErrOnlineScores: nilai tes online tidak bisa diketik; koreksi lewat
// nilai ulang ujian.
var ErrOnlineScores = errors.New("admission: online test scores are read-only")

// TestForm adalah isian form tes apa adanya.
type TestForm struct {
	Name        string
	Weight      string
	MaxScore    string
	ScheduledAt string // YYYY-MM-DDTHH:MM (WIB), boleh kosong
	Location    string
	Rubric      string
	PassScore   string // kosong = tanpa nilai minimal
	Method      string // kertas atau online (T149)
	ExamID      string
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
	rows, err := s.db.Query(ctx, `SELECT t.id, t.period_id, t.name, t.weight, t.max_score, t.scheduled_at, coalesce(t.location, ''), t.sort_order,
		       coalesce(t.rubric, ''), coalesce(t.pass_score, 0), coalesce(t.exam_id::text, ''), coalesce(e.name, '')
		FROM admission_tests t LEFT JOIN exams e ON e.id = t.exam_id AND e.school_id = t.school_id
		WHERE t.school_id = $1 AND t.period_id = $2 ORDER BY t.sort_order, t.name`, schoolID, periodID)
	if err != nil {
		return nil, mapError("list tests", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SelectionTest, error) {
		var t SelectionTest
		var w, m int16
		err := r.Scan(&t.ID, &t.PeriodID, &t.Name, &w, &m, &t.ScheduledAt, &t.Location, &t.SortOrder, &t.Rubric, &t.PassScore, &t.ExamID, &t.ExamName)
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
	Rubric      *string
	PassScore   *int
	ExamID      *string
}

// validateTest memvalidasi form tes. Jadwal dibaca di zona waktu sekolah
// loc (T126).
func validateTest(f TestForm, loc *time.Location) (testInput, error) {
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
		t, err := time.ParseInLocation(scheduleLayout, v, loc)
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
	if rubric := strings.TrimSpace(f.Rubric); rubric != "" {
		if utf8.RuneCountInString(rubric) > 2000 {
			fields["rubric"] = "Acuan penilaian maksimal 2.000 karakter."
		}
		in.Rubric = &rubric
	}
	if v := strings.TrimSpace(f.PassScore); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 0 || (fields["max_score"] == "" && n > in.MaxScore) {
			fields["pass_score"] = "Nilai minimal harus angka 0 sampai nilai maksimal."
		} else {
			in.PassScore = &n
		}
	}
	switch f.Method {
	case TestMethodOnline:
		if id := strings.TrimSpace(f.ExamID); id == "" {
			fields["exam_id"] = "Pilih ujian seleksi online, atau buat ujian baru."
		} else {
			in.ExamID = &id
		}
	case "", TestMethodPaper:
	default:
		fields["method"] = "Pilih cara tes."
	}
	if len(fields) > 0 {
		return testInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

// linkExam memeriksa dan menautkan ujian online ke tes (T149): ujian harus
// ujian seleksi sekolah ini; tes yang sudah punya nilai ketikan tidak bisa
// diubah menjadi online. Nilai online lama dihapus lalu ditulis ulang dari
// ujian yang ditautkan (juga saat nilai maksimal berubah).
// examActive melaporkan apakah modul ujian aktif untuk sekolah request
// staf ini (T166). Tanpa modul ujian, tes baru hanya "di kertas" dan nilainya
// diketik TU; tes online yang sudah ada tetap tertaut.
func examActive(ctx context.Context) bool { return module.From(ctx).Has(module.Exam) }

// errExamInactive: ujian online dipilih padahal modul ujian tidak aktif.
var errExamInactive = fieldErr("method", "Ujian online aktif bila modul Ujian Online dipakai. Pilih tes di kertas atau wawancara.")

func linkExam(ctx context.Context, tx pgx.Tx, schoolID, testID string, examID *string) error {
	if examID != nil {
		if err := exam.CheckAdmissionExam(ctx, tx, schoolID, *examID); errors.Is(err, exam.ErrNotFound) {
			return fieldErr("exam_id", "Ujian tidak ditemukan. Pilih ujian seleksi sekolah ini.")
		} else if err != nil {
			return err
		}
		var typed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM applicant_test_scores WHERE test_id = $1 AND source = 'manual' AND score IS NOT NULL)`,
			testID).Scan(&typed); err != nil {
			return mapError("check typed scores", err)
		}
		if typed {
			return fieldErr("method", "Tes ini sudah punya nilai yang diketik. Kosongkan nilainya di Isi nilai dulu sebelum memakai ujian online.")
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM applicant_test_scores WHERE test_id = $1 AND (source = 'ujian_online' OR score IS NULL)`, testID); err != nil {
		return mapError("clear online scores", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE admission_tests SET exam_id = $3 WHERE school_id = $1 AND id = $2`, schoolID, testID, examID); err != nil {
		return mapError("link exam", err)
	}
	if examID == nil {
		return nil
	}
	_, err := exam.SyncAdmissionExam(ctx, tx, schoolID, *examID)
	return err
}

// AddTest menambah tes ke periode.
func (s *SelectionService) AddTest(ctx context.Context, actor school.Actor, periodID string, f TestForm) (SelectionTest, error) {
	loc, err := clock.ForSchool(ctx, s.db, actor.SchoolID)
	if err != nil {
		return SelectionTest{}, err
	}
	in, err := validateTest(f, loc)
	if err != nil {
		return SelectionTest{}, err
	}
	var t SelectionTest
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO admission_tests (school_id, period_id, name, weight, max_score, scheduled_at, location, rubric, pass_score, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, (SELECT coalesce(max(sort_order), 0) + 1 FROM admission_tests WHERE period_id = $2))
			RETURNING id`, actor.SchoolID, periodID, in.Name, in.Weight, in.MaxScore, in.ScheduledAt, in.Location, in.Rubric, in.PassScore).Scan(&t.ID); err != nil {
			return mapError("add test", err)
		}
		t = SelectionTest{ID: t.ID, PeriodID: periodID, Name: in.Name, Weight: in.Weight, MaxScore: in.MaxScore, ScheduledAt: in.ScheduledAt}
		if in.ExamID != nil && !examActive(ctx) {
			return errExamInactive
		}
		if in.ExamID != nil {
			if err := linkExam(ctx, tx, actor.SchoolID, t.ID, in.ExamID); err != nil {
				return err
			}
			t.ExamID = *in.ExamID
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTestCreate, periodID, nil, map[string]any{"test_id": t.ID, "name": t.Name, "weight": t.Weight,
			"max_score": t.MaxScore, "exam_id": in.ExamID}))
	})
	if err != nil {
		return SelectionTest{}, testFieldError(err)
	}
	return t, nil
}

// UpdateTest mengubah tes. Nilai maksimal tidak boleh lebih kecil dari
// nilai yang sudah tercatat.
func (s *SelectionService) UpdateTest(ctx context.Context, actor school.Actor, periodID, testID string, f TestForm) error {
	loc, err := clock.ForSchool(ctx, s.db, actor.SchoolID)
	if err != nil {
		return err
	}
	in, err := validateTest(f, loc)
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
		if err := tx.QueryRow(ctx, `SELECT max(score)::float8 FROM applicant_test_scores WHERE test_id = $1 AND source = 'manual'`, testID).Scan(&top); err != nil {
			return err
		}
		if top != nil && *top > float64(in.MaxScore) {
			return fieldErr("max_score", "Sudah ada nilai di atas nilai maksimal baru.")
		}
		tag, err := tx.Exec(ctx, `UPDATE admission_tests SET name = $4, weight = $5, max_score = $6, scheduled_at = $7, location = $8,
			       rubric = $9, pass_score = $10
			WHERE school_id = $1 AND period_id = $2 AND id = $3`, actor.SchoolID, periodID, testID, in.Name, in.Weight, in.MaxScore, in.ScheduledAt, in.Location, in.Rubric, in.PassScore)
		if err != nil {
			return mapError("update test", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// Tanpa modul ujian (T166): tautan ujian yang sudah ada dibiarkan
		// (nilai online tetap), tautan baru ditolak; beralih ke kertas boleh.
		if !examActive(ctx) && in.ExamID != nil {
			var cur string
			if err := tx.QueryRow(ctx, `SELECT coalesce(exam_id::text, '') FROM admission_tests WHERE school_id = $1 AND id = $2`, actor.SchoolID, testID).Scan(&cur); err != nil {
				return mapError("current exam", err)
			}
			if cur != *in.ExamID {
				return errExamInactive
			}
		} else if err := linkExam(ctx, tx, actor.SchoolID, testID, in.ExamID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTestUpdate, periodID, nil, map[string]any{"test_id": testID, "name": in.Name, "weight": in.Weight,
			"max_score": in.MaxScore, "exam_id": in.ExamID}))
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
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "admission_tests_exam_key" {
		return fieldErr("exam_id", "Ujian ini sudah dipakai tes lain. Satu ujian online untuk satu tes.")
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
	// Online: nilai dari ujian online (T149), tidak bisa disunting;
	// AttemptID untuk tautan ke detail hasil.
	Online    bool
	AttemptID string
}

// Grid mengembalikan pendaftar yang bisa dinilai untuk tes testID:
// berstatus diverifikasi di jalur yang memakai tes.
func (s *SelectionService) Grid(ctx context.Context, schoolID, periodID, testID string) ([]ScoreRow, error) {
	if !database.IsUUID(periodID) || !database.IsUUID(testID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT a.id, coalesce(a.registration_number, ''), a.full_name, t.name, coalesce(sc.score::text, ''),
		       coalesce(sc.source = 'ujian_online', false), coalesce(sc.attempt_id::text, '')
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
		err := r.Scan(&x.ApplicantID, &x.RegistrationNumber, &x.FullName, &x.TrackName, &x.Score, &x.Online, &x.AttemptID)
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
		if test.Online() {
			return ErrOnlineScores
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
