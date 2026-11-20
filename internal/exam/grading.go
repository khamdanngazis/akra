package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Penilaian otomatis (T146). Soal objektif dinilai dari snapshot soal saat
// ujian terkunci (dengan kunci koreksi bila ada) segera setelah attempt
// dikumpulkan; esai menunggu koreksi manual (T147).

// Status nilai attempt.
const (
	ScoreFinal      = "final"
	ScoreAwaitEssay = "menunggu_esai"
	PointsUnit      = 10000 // poin disimpan dalam satuan 1/10.000 poin (integer)
	scoreScale      = 10000 // nilai attempt: integer seperseratus (100,00 = 10000)
)

// ScoreAnswer menilai satu jawaban (nilai JSON dari halaman ujian) untuk
// soal q dengan kunci key. Mengembalikan porsi benar 0..1, atau manual =
// true untuk esai berisi (dinilai guru). Jawaban kosong, berformat salah,
// atau berisi pilihan yang tidak ada bernilai 0.
//
// Aturan (T128):
//   - pg: penuh bila pilihan sama dengan kunci.
//   - pg_kompleks "semua": penuh hanya bila himpunan pilihan sama dengan
//     kunci. "proporsional": (benar dipilih − salah dipilih) / jumlah kunci,
//     paling kecil 0.
//   - benar_salah: pernyataan yang dijawab benar / jumlah pernyataan.
//   - isian: penuh bila cocok dengan salah satu jawaban diterima (teks:
//     abaikan besar/kecil, spasi, tanda baca; angka: koma/titik desimal,
//     toleransi).
//   - menjodohkan: pasangan benar / jumlah item kiri; satu pilihan kanan
//     dipakai dua kali hanya dihitung pada pasangan yang benar.
func ScoreAnswer(q Question, key AnswerKey, raw json.RawMessage) (frac float64, manual bool) {
	if isNullJSON(raw) {
		return 0, false
	}
	switch q.Kind {
	case KindMC:
		var v string
		if json.Unmarshal(raw, &v) != nil || v == "" || len(key.Correct) == 0 {
			return 0, false
		}
		if v == key.Correct[0] {
			return 1, false
		}
		return 0, false
	case KindMulti:
		var vs []string
		if json.Unmarshal(raw, &vs) != nil || len(key.Correct) == 0 {
			return 0, false
		}
		correct := setOf(key.Correct)
		picked := map[string]bool{}
		hits, misses := 0, 0
		for _, v := range vs {
			if picked[v] {
				continue
			}
			picked[v] = true
			if correct[v] {
				hits++
			} else {
				// Pilihan salah (termasuk id yang tidak ada).
				misses++
			}
		}
		if key.Scoring == ScoringProportional {
			return math.Max(0, float64(hits-misses)/float64(len(correct))), false
		}
		if hits == len(correct) && misses == 0 {
			return 1, false
		}
		return 0, false
	case KindTF:
		var v map[string]bool
		if json.Unmarshal(raw, &v) != nil || len(q.Options.Statements) == 0 {
			return 0, false
		}
		right := 0
		for _, s := range q.Options.Statements {
			want, ok := key.Truth[s.ID]
			got, answered := v[s.ID]
			if ok && answered && got == want {
				right++
			}
		}
		return float64(right) / float64(len(q.Options.Statements)), false
	case KindShort:
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return 0, false
		}
		mode := q.Options.Input
		if MatchShortAnswer(v, mode, key) {
			return 1, false
		}
		return 0, false
	case KindMatch:
		var v map[string]string
		if json.Unmarshal(raw, &v) != nil || len(q.Options.Left) == 0 {
			return 0, false
		}
		right := 0
		for _, l := range q.Options.Left {
			if want, ok := key.Pairs[l.ID]; ok && v[l.ID] == want {
				right++
			}
		}
		return float64(right) / float64(len(q.Options.Left)), false
	case KindEssay:
		var v string
		if json.Unmarshal(raw, &v) != nil || len([]rune(trimSpace(v))) == 0 {
			return 0, false
		}
		return 0, true
	}
	return 0, false
}

func isNullJSON(raw json.RawMessage) bool {
	s := trimSpace(string(raw))
	return s == "" || s == "null"
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func setOf(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// toUnits mengubah porsi benar dari poin soal menjadi satuan 1/10.000
// poin, dibulatkan.
func toUnits(frac float64, points int) int {
	return int(math.Round(frac * float64(points) * PointsUnit))
}

// GradeItem adalah satu soal untuk penilaian.
type GradeItem struct {
	Question Question
	Key      AnswerKey // kunci snapshot atau kunci koreksi
	Points   int
	Voided   bool // dibatalkan: tidak dihitung di total
}

// AnswerGrade adalah hasil penilaian satu jawaban.
type AnswerGrade struct {
	QuestionID string
	Points     *int // satuan 1/10.000 poin; nil: esai menunggu koreksi
}

// AttemptGrade adalah hasil penilaian satu attempt.
type AttemptGrade struct {
	Answers   []AnswerGrade
	Earned    int // Σ poin dalam satuan 1/10.000 poin
	Max       int // Σ poin soal yang tidak dibatalkan
	ScoreX100 int // 0..10000
	Status    string
}

// ScoreLabel menampilkan nilai dengan koma desimal, mis. "87,50".
func ScoreLabel(x100 int) string {
	return fmt.Sprintf("%d,%02d", x100/100, x100%100)
}

// GradeAnswers menilai jawaban attempt (question_id → nilai JSON) untuk
// items. fixed berisi poin tetap (question_id → poin dalam satuan
// 1/10.000): esai yang sudah dikoreksi dan nilai manual (T148), dipakai
// apa adanya tanpa menilai jawaban. Esai berisi tanpa poin membuat status
// menunggu_esai. Murni: hasil sama untuk masukan yang sama (penilaian
// idempoten).
func GradeAnswers(items []GradeItem, answers map[string]json.RawMessage, fixed map[string]int) AttemptGrade {
	var g AttemptGrade
	g.Status = ScoreFinal
	for _, it := range items {
		qid := it.Question.ID
		raw, answered := answers[qid]
		if !it.Voided {
			g.Max += it.Points
		}
		fixedPts, isFixed := fixed[qid]
		if !answered && !isFixed {
			continue
		}
		frac, manual := ScoreAnswer(it.Question, it.Key, raw)
		var pts *int
		switch {
		case isFixed:
			v := min(max(fixedPts, 0), it.Points*PointsUnit)
			pts = &v
		case manual:
			if !it.Voided {
				g.Status = ScoreAwaitEssay
			}
		default:
			v := toUnits(frac, it.Points)
			pts = &v
		}
		g.Answers = append(g.Answers, AnswerGrade{QuestionID: qid, Points: pts})
		if pts != nil && !it.Voided {
			g.Earned += *pts
		}
	}
	if g.Max > 0 {
		// Earned / (Max × PointsUnit) × 10000, dibulatkan, tanpa pecahan:
		// PointsUnit = scoreScale sehingga cukup Earned / Max.
		g.ScoreX100 = (2*g.Earned + g.Max) / (2 * g.Max)
	}
	g.ScoreX100 = max(0, min(scoreScale, g.ScoreX100))
	return g
}

// LoadGradeItems memuat soal varian variantID ujian e dari snapshot,
// dengan kunci koreksi dan tanda soal dibatalkan.
func LoadGradeItems(ctx context.Context, q database.DBTX, e Exam, variantID string) ([]GradeItem, error) {
	paper, err := LoadPaper(ctx, q, e, variantID)
	if err != nil {
		return nil, err
	}
	overrides, err := keyOverrides(ctx, q, e.SchoolID, e.ID)
	if err != nil {
		return nil, err
	}
	voided, err := VoidedQuestions(ctx, q, e.SchoolID, e.ID)
	if err != nil {
		return nil, err
	}
	items := make([]GradeItem, len(paper))
	for i, p := range paper {
		key := p.Question.Key
		if k, ok := overrides[p.Question.ID]; ok {
			key = k
		}
		_, v := voided[p.Question.ID]
		items[i] = GradeItem{Question: p.Question, Key: key, Points: p.Points, Voided: v}
	}
	return items, nil
}

func keyOverrides(ctx context.Context, q database.DBTX, schoolID, examID string) (map[string]AnswerKey, error) {
	rows, err := q.Query(ctx, `SELECT question_id::text, answer_key FROM exam_key_overrides WHERE school_id = $1 AND exam_id = $2`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: key overrides: %w", err)
	}
	defer rows.Close()
	out := map[string]AnswerKey{}
	for rows.Next() {
		var id string
		var k AnswerKey
		if err := rows.Scan(&id, &k); err != nil {
			return nil, fmt.Errorf("exam: key overrides: %w", err)
		}
		out[id] = k
	}
	return out, rows.Err()
}

// gradeCache menyimpan soal per varian selama satu putaran penilaian.
type gradeCache struct {
	exams map[string]Exam
	items map[string][]GradeItem // varian → soal
}

func newGradeCache() *gradeCache {
	return &gradeCache{exams: map[string]Exam{}, items: map[string][]GradeItem{}}
}

func (c *gradeCache) load(ctx context.Context, q database.DBTX, schoolID, examID, variantID string) ([]GradeItem, error) {
	if it, ok := c.items[variantID]; ok {
		return it, nil
	}
	e, ok := c.exams[examID]
	if !ok {
		var err error
		if e, err = GetExam(ctx, q, schoolID, examID); err != nil {
			return nil, err
		}
		c.exams[examID] = e
	}
	it, err := LoadGradeItems(ctx, q, e, variantID)
	if err != nil {
		return nil, err
	}
	c.items[variantID] = it
	return it, nil
}

// ErrNotSubmitted: attempt belum dikumpulkan.
var ErrNotSubmitted = errors.New("exam: attempt not submitted")

// GradeAttempt menilai attempt yang sudah dikumpulkan dan menyimpan poin
// per jawaban serta nilai attempt. Poin esai yang sudah dikoreksi
// dipertahankan. Idempoten: baris hanya ditulis bila nilainya berubah.
func GradeAttempt(ctx context.Context, q database.DBTX, schoolID, attemptID string, now time.Time) (AttemptGrade, error) {
	return gradeAttempt(ctx, q, newGradeCache(), schoolID, attemptID, now)
}

func gradeAttempt(ctx context.Context, q database.DBTX, cache *gradeCache, schoolID, attemptID string, now time.Time) (AttemptGrade, error) {
	var examID, variantID, status string
	err := q.QueryRow(ctx, `SELECT exam_id, variant_id, status FROM exam_attempts WHERE school_id = $1 AND id = $2`, schoolID, attemptID).
		Scan(&examID, &variantID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return AttemptGrade{}, ErrNotFound
	}
	if err != nil {
		return AttemptGrade{}, fmt.Errorf("exam: grade attempt: %w", err)
	}
	if status != AttemptSubmitted {
		return AttemptGrade{}, ErrNotSubmitted
	}
	items, err := cache.load(ctx, q, schoolID, examID, variantID)
	if err != nil {
		return AttemptGrade{}, err
	}
	answers, essay, err := loadGradingAnswers(ctx, q, attemptID, items)
	if err != nil {
		return AttemptGrade{}, err
	}
	g := GradeAnswers(items, answers, essay)
	if err := storeGrade(ctx, q, attemptID, g, now); err != nil {
		return AttemptGrade{}, err
	}
	return g, nil
}

// loadGradingAnswers membaca jawaban attempt dan poin tetap: esai yang
// sudah dikoreksi dan nilai manual.
func loadGradingAnswers(ctx context.Context, q database.DBTX, attemptID string, items []GradeItem) (map[string]json.RawMessage, map[string]int, error) {
	essayQ := map[string]bool{}
	for _, it := range items {
		if it.Question.Kind == KindEssay {
			essayQ[it.Question.ID] = true
		}
	}
	rows, err := q.Query(ctx, `SELECT question_id::text, value, points_x10000, override_by IS NOT NULL FROM exam_answers WHERE attempt_id = $1`, attemptID)
	if err != nil {
		return nil, nil, fmt.Errorf("exam: grading answers: %w", err)
	}
	defer rows.Close()
	answers := map[string]json.RawMessage{}
	essay := map[string]int{}
	for rows.Next() {
		var qid string
		var v json.RawMessage
		var pts *int
		var override bool
		if err := rows.Scan(&qid, &v, &pts, &override); err != nil {
			return nil, nil, fmt.Errorf("exam: grading answers: %w", err)
		}
		answers[qid] = v
		if (essayQ[qid] || override) && pts != nil {
			essay[qid] = *pts
		}
	}
	return answers, essay, rows.Err()
}

// storeGrade menulis poin jawaban dan nilai attempt yang berubah.
func storeGrade(ctx context.Context, q database.DBTX, attemptID string, g AttemptGrade, now time.Time) error {
	ids := make([]string, 0, len(g.Answers))
	pts := make([]*int, 0, len(g.Answers))
	for _, a := range g.Answers {
		ids = append(ids, a.QuestionID)
		pts = append(pts, a.Points)
	}
	if _, err := q.Exec(ctx, `UPDATE exam_answers x SET points_x10000 = u.p
		FROM unnest($2::uuid[], $3::int[]) AS u(q, p)
		WHERE x.attempt_id = $1 AND x.question_id = u.q AND x.points_x10000 IS DISTINCT FROM u.p`, attemptID, ids, pts); err != nil {
		return fmt.Errorf("exam: store answer points: %w", err)
	}
	if _, err := q.Exec(ctx, `UPDATE exam_attempts SET score_x100 = $2, score_status = $3, points_earned_x10000 = $4, points_max = $5, graded_at = $6
		WHERE id = $1 AND (score_x100 IS DISTINCT FROM $2 OR score_status IS DISTINCT FROM $3 OR points_earned_x10000 IS DISTINCT FROM $4
		                   OR points_max IS DISTINCT FROM $5)`,
		attemptID, g.ScoreX100, g.Status, g.Earned, g.Max, now); err != nil {
		return fmt.Errorf("exam: store attempt score: %w", err)
	}
	// Tes seleksi online (T149): nilai ikut ke nilai tes pendaftar.
	return syncAdmissionScore(ctx, q, attemptID)
}

// FinalScore adalah nilai attempt yang boleh dikirim ke modul lain.
type FinalScore struct {
	AttemptID   string
	StudentID   string
	ApplicantID string
	ScoreX100   int
}

// FinalScores mengembalikan nilai attempt terakhir yang dikumpulkan per
// peserta ujian examID, hanya bila nilainya final. Peserta yang attempt
// terakhirnya menunggu koreksi esai tidak ikut (bukan diganti attempt
// lama). Hanya fungsi ini yang dipakai seleksi (T149) dan rapor (T150).
func FinalScores(ctx context.Context, q database.DBTX, schoolID, examID string) ([]FinalScore, error) {
	rows, err := q.Query(ctx, `
		SELECT id, student_id, applicant_id, score_x100 FROM (
			SELECT DISTINCT ON (p.id) t.id, coalesce(p.student_id::text, '') AS student_id, coalesce(p.applicant_id::text, '') AS applicant_id,
				t.score_x100, t.score_status
			FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id
			WHERE t.school_id = $1 AND t.exam_id = $2 AND t.status = 'dikumpulkan'
			ORDER BY p.id, t.attempt_no DESC) last
		WHERE score_status = 'final'
		ORDER BY id`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: final scores: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (FinalScore, error) {
		var f FinalScore
		return f, r.Scan(&f.AttemptID, &f.StudentID, &f.ApplicantID, &f.ScoreX100)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: final scores: %w", err)
	}
	return out, nil
}

// gradeSafely menilai attempt di savepoint tx. Error penilaian tidak
// menggagalkan pengumpulan: savepoint dibatalkan, error dicatat, dan
// attempt dinilai susulan oleh job kumpul otomatis (GradePending).
func gradeSafely(ctx context.Context, tx pgx.Tx, cache *gradeCache, schoolID, attemptID string, now time.Time) {
	sp, err := tx.Begin(ctx)
	if err == nil {
		if _, err = gradeAttempt(ctx, sp, cache, schoolID, attemptID, now); err == nil {
			err = sp.Commit(ctx)
		} else {
			_ = sp.Rollback(ctx)
		}
	}
	if err != nil {
		slog.Default().ErrorContext(ctx, "grade attempt", "attempt_id", attemptID, "error", err)
	}
}

// GradePending menilai attempt yang sudah dikumpulkan tetapi belum
// bernilai (penilaian gagal sebelumnya, atau dikumpulkan sebelum T146),
// paling banyak limit per panggilan. Mengembalikan jumlah yang dinilai.
func GradePending(ctx context.Context, db database.DB, limit int, now time.Time) (int, error) {
	rows, err := db.Query(ctx, `SELECT school_id::text, id::text FROM exam_attempts
		WHERE status = 'dikumpulkan' AND graded_at IS NULL ORDER BY submitted_at LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("exam: pending grading: %w", err)
	}
	type pending struct{ school, id string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pending, error) {
		var p pending
		return p, r.Scan(&p.school, &p.id)
	})
	if err != nil {
		return 0, fmt.Errorf("exam: pending grading: %w", err)
	}
	cache := newGradeCache()
	n := 0
	for _, p := range list {
		if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			_, err := gradeAttempt(ctx, tx, cache, p.school, p.id, now)
			return err
		}); err != nil {
			slog.Default().ErrorContext(ctx, "grade pending attempt", "attempt_id", p.id, "error", err)
			continue
		}
		n++
	}
	return n, nil
}
