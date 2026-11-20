package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Nilai ulang (T146): kunci yang salah dibetulkan setelah ujian lalu semua
// attempt yang sudah dikumpulkan dinilai ulang, dengan pratinjau dampak
// dan alasan yang tercatat di audit.

// Kesalahan nilai ulang.
var (
	ErrBadKey       = errors.New("exam: invalid answer key")
	ErrKeyUnchanged = errors.New("exam: answer key unchanged")
	ErrNeedReason   = errors.New("exam: regrade reason required")
	ErrNotObjective = errors.New("exam: question is not auto-graded")
)

// ValidateKey memeriksa kunci k untuk soal q (jenis objektif).
func ValidateKey(q Question, k AnswerKey) error {
	ids := func(cs []Choice) map[string]bool {
		m := map[string]bool{}
		for _, c := range cs {
			m[c.ID] = true
		}
		return m
	}
	switch q.Kind {
	case KindMC:
		if len(k.Correct) != 1 || !ids(q.Options.Choices)[k.Correct[0]] {
			return ErrBadKey
		}
	case KindMulti:
		valid := ids(q.Options.Choices)
		if len(k.Correct) == 0 || (k.Scoring != ScoringAll && k.Scoring != ScoringProportional) {
			return ErrBadKey
		}
		seen := map[string]bool{}
		for _, c := range k.Correct {
			if !valid[c] || seen[c] {
				return ErrBadKey
			}
			seen[c] = true
		}
	case KindTF:
		if len(k.Truth) != len(q.Options.Statements) {
			return ErrBadKey
		}
		for _, s := range q.Options.Statements {
			if _, ok := k.Truth[s.ID]; !ok {
				return ErrBadKey
			}
		}
	case KindShort:
		n := 0
		for _, a := range k.Accepted {
			if strings.TrimSpace(a) != "" {
				n++
				if q.Options.Input == "angka" {
					if _, ok := parseNumber(a); !ok {
						return ErrBadKey
					}
				}
			}
		}
		if n == 0 || k.Tolerance < 0 {
			return ErrBadKey
		}
	case KindMatch:
		right := ids(q.Options.Right)
		if len(k.Pairs) != len(q.Options.Left) {
			return ErrBadKey
		}
		used := map[string]bool{}
		for _, l := range q.Options.Left {
			r, ok := k.Pairs[l.ID]
			if !ok || !right[r] || used[r] {
				return ErrBadKey
			}
			used[r] = true
		}
	default:
		return ErrNotObjective
	}
	return nil
}

// keyEqual membandingkan dua kunci lewat JSON (urutan pilihan diabaikan).
func keyEqual(a, b AnswerKey) bool {
	norm := func(k AnswerKey) string {
		k.Correct = append([]string(nil), k.Correct...)
		sort.Strings(k.Correct)
		var acc []string
		for _, x := range k.Accepted {
			if x = strings.TrimSpace(x); x != "" {
				acc = append(acc, x)
			}
		}
		k.Accepted = acc
		k.RubricSrc, k.RubricHTML = "", ""
		b, _ := json.Marshal(k)
		return string(b)
	}
	return norm(a) == norm(b)
}

// RegradeRow adalah dampak nilai ulang pada satu attempt.
type RegradeRow struct {
	AttemptID string
	Name      string
	Number    string
	Before    int // nilai x100; -1 = belum bernilai
	After     int
	Status    string
}

// Delta mengembalikan perubahan nilai x100.
func (r RegradeRow) Delta() int { return r.After - r.Before }

// RegradeImpact adalah pratinjau atau hasil nilai ulang.
type RegradeImpact struct {
	Attempts int
	Raised   int
	Lowered  int
	Rows     []RegradeRow // yang berubah, perubahan terbesar dulu
}

// QuestionKey mengembalikan soal ujian e (snapshot) beserta kunci yang
// berlaku saat ini (koreksi bila ada).
func QuestionKey(ctx context.Context, q database.DBTX, e Exam, questionID string) (Question, AnswerKey, bool, error) {
	if !e.Locked() {
		return Question{}, AnswerKey{}, false, ErrNotLocked
	}
	if !database.IsUUID(questionID) {
		return Question{}, AnswerKey{}, false, ErrNotFound
	}
	var raw []byte
	err := q.QueryRow(ctx, `SELECT qv.snapshot FROM exam_item_snapshots s
		JOIN exam_question_versions qv ON qv.question_id = s.question_id AND qv.version = s.question_version
		WHERE s.school_id = $1 AND s.exam_id = $2 AND s.question_id = $3 LIMIT 1`, e.SchoolID, e.ID, questionID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, AnswerKey{}, false, ErrNotFound
	}
	if err != nil {
		return Question{}, AnswerKey{}, false, fmt.Errorf("exam: question key: %w", err)
	}
	var qs Question
	if err := json.Unmarshal(raw, &qs); err != nil {
		return Question{}, AnswerKey{}, false, fmt.Errorf("exam: decode question: %w", err)
	}
	ov, err := keyOverrides(ctx, q, e.SchoolID, e.ID)
	if err != nil {
		return Question{}, AnswerKey{}, false, err
	}
	if k, ok := ov[questionID]; ok {
		return qs, k, true, nil
	}
	return qs, qs.Key, false, nil
}

// regradeAll menilai ulang semua attempt ujian e yang sudah dikumpulkan
// dengan kunci pengganti (question_id → kunci) di atas kunci yang berlaku.
// store false: hanya menghitung (pratinjau).
func regradeAll(ctx context.Context, q database.DBTX, e Exam, replace map[string]AnswerKey, store bool, now time.Time) (RegradeImpact, error) {
	var imp RegradeImpact
	rows, err := q.Query(ctx, `
		SELECT t.id, t.variant_id, coalesce(t.score_x100, -1), coalesce(s.full_name, ap.full_name, ''),
			coalesce(nullif(p.participant_no, ''), s.nis, ap.registration_number, '')
		FROM exam_attempts t
		JOIN exam_session_participants p ON p.id = t.participant_id
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants ap ON ap.id = p.applicant_id
		WHERE t.school_id = $1 AND t.exam_id = $2 AND t.status = 'dikumpulkan'
		ORDER BY t.id`, e.SchoolID, e.ID)
	if err != nil {
		return imp, fmt.Errorf("exam: regrade attempts: %w", err)
	}
	type att struct {
		id, variant string
		row         RegradeRow
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (att, error) {
		var a att
		err := r.Scan(&a.id, &a.variant, &a.row.Before, &a.row.Name, &a.row.Number)
		a.row.AttemptID = a.id
		return a, err
	})
	if err != nil {
		return imp, fmt.Errorf("exam: regrade attempts: %w", err)
	}
	items := map[string][]GradeItem{}
	for _, a := range list {
		it, ok := items[a.variant]
		if !ok {
			if it, err = LoadGradeItems(ctx, q, e, a.variant); err != nil {
				return imp, err
			}
			for i := range it {
				if k, ok := replace[it[i].Question.ID]; ok {
					it[i].Key = k
				}
			}
			items[a.variant] = it
		}
		answers, essay, err := loadGradingAnswers(ctx, q, a.id, it)
		if err != nil {
			return imp, err
		}
		g := GradeAnswers(it, answers, essay)
		if store {
			if err := storeGrade(ctx, q, a.id, g, now); err != nil {
				return imp, err
			}
		}
		imp.Attempts++
		a.row.After, a.row.Status = g.ScoreX100, g.Status
		if a.row.Before < 0 || a.row.After == a.row.Before {
			continue
		}
		if a.row.After > a.row.Before {
			imp.Raised++
		} else {
			imp.Lowered++
		}
		imp.Rows = append(imp.Rows, a.row)
	}
	sort.SliceStable(imp.Rows, func(i, j int) bool {
		di, dj := abs(imp.Rows[i].Delta()), abs(imp.Rows[j].Delta())
		if di != dj {
			return di > dj
		}
		return imp.Rows[i].Name < imp.Rows[j].Name
	})
	return imp, nil
}

// PreviewKeyFix menghitung dampak kunci baru tanpa menyimpan apa pun.
func PreviewKeyFix(ctx context.Context, q database.DBTX, e Exam, questionID string, key AnswerKey, now time.Time) (RegradeImpact, error) {
	qs, cur, _, err := QuestionKey(ctx, q, e, questionID)
	if err != nil {
		return RegradeImpact{}, err
	}
	if err := ValidateKey(qs, key); err != nil {
		return RegradeImpact{}, err
	}
	if keyEqual(cur, key) {
		return RegradeImpact{}, ErrKeyUnchanged
	}
	return regradeAll(ctx, q, e, map[string]AnswerKey{questionID: key}, false, now)
}

// ApplyKeyFix menyimpan kunci koreksi soal questionID dan menilai ulang
// semua attempt yang sudah dikumpulkan. Alasan wajib; tercatat di audit
// dan riwayat nilai ulang. Hak (pembuat ujian, koordinator, admin_tu)
// dicek pemanggil.
func ApplyKeyFix(ctx context.Context, db database.DB, sa StaffActor, e Exam, questionID string, key AnswerKey, reason string, now time.Time) (RegradeImpact, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 300 {
		return RegradeImpact{}, ErrNeedReason
	}
	var imp RegradeImpact
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// Satu nilai ulang per ujian pada satu waktu.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM exams WHERE id = $1 FOR UPDATE`, e.ID); err != nil {
			return fmt.Errorf("exam: lock exam: %w", err)
		}
		qs, cur, _, err := QuestionKey(ctx, tx, e, questionID)
		if err != nil {
			return err
		}
		if err := ValidateKey(qs, key); err != nil {
			return err
		}
		if keyEqual(cur, key) {
			return ErrKeyUnchanged
		}
		raw, _ := json.Marshal(key)
		if _, err := tx.Exec(ctx, `INSERT INTO exam_key_overrides (school_id, exam_id, question_id, answer_key, reason, created_by, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (exam_id, question_id) DO UPDATE SET answer_key = excluded.answer_key, reason = excluded.reason,
				created_by = excluded.created_by, created_at = excluded.created_at`,
			sa.SchoolID, e.ID, questionID, raw, reason, sa.UserID, now); err != nil {
			return fmt.Errorf("exam: save key override: %w", err)
		}
		if imp, err = regradeAll(ctx, tx, e, nil, true, now); err != nil {
			return err
		}
		if err := logRegrade(ctx, tx, sa, e.ID, questionID, "kunci", reason, imp, now); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.regrade", "exam", e.ID, map[string]any{"question_id": questionID, "answer_key": cur},
			map[string]any{"question_id": questionID, "answer_key": key, "reason": reason, "attempts": imp.Attempts,
				"raised": imp.Raised, "lowered": imp.Lowered})
	})
	return imp, err
}

// regradeAfterVoid menilai ulang setelah soal dibatalkan/dipulihkan.
func regradeAfterVoid(ctx context.Context, tx pgx.Tx, sa StaffActor, e Exam, questionID string, void bool, reason string, now time.Time) error {
	imp, err := regradeAll(ctx, tx, e, nil, true, now)
	if err != nil {
		return err
	}
	if imp.Attempts == 0 {
		return nil
	}
	cause := "batal"
	if !void {
		cause = "pulih"
	}
	return logRegrade(ctx, tx, sa, e.ID, questionID, cause, reason, imp, now)
}

func logRegrade(ctx context.Context, tx pgx.Tx, sa StaffActor, examID, questionID, cause, reason string, imp RegradeImpact, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO exam_regrades (school_id, exam_id, question_id, cause, reason, attempts, raised, lowered, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		sa.SchoolID, examID, nilIfEmpty(questionID), cause, reason, imp.Attempts, imp.Raised, imp.Lowered, sa.UserID, now)
	if err != nil {
		return fmt.Errorf("exam: log regrade: %w", err)
	}
	return nil
}

// Regrade adalah satu baris riwayat nilai ulang.
type Regrade struct {
	Cause    string
	Reason   string
	Attempts int
	Raised   int
	Lowered  int
	By       string
	At       time.Time
	Question string // kutipan soal
}

// CauseLabel untuk tampilan.
func (r Regrade) CauseLabel() string {
	return map[string]string{"kunci": "Kunci dibetulkan", "batal": "Soal dibatalkan", "pulih": "Soal dipulihkan"}[r.Cause]
}

// ListRegrades mengembalikan riwayat nilai ulang ujian, terbaru dulu.
func ListRegrades(ctx context.Context, q database.DBTX, schoolID, examID string) ([]Regrade, error) {
	rows, err := q.Query(ctx, `
		SELECT r.cause, r.reason, r.attempts, r.raised, r.lowered, coalesce(u.full_name, ''), r.created_at,
			coalesce((SELECT qv.snapshot->>'BodySrc' FROM exam_item_snapshots s
			          JOIN exam_question_versions qv ON qv.question_id = s.question_id AND qv.version = s.question_version
			          WHERE s.exam_id = r.exam_id AND s.question_id = r.question_id LIMIT 1), '')
		FROM exam_regrades r LEFT JOIN users u ON u.id = r.created_by
		WHERE r.school_id = $1 AND r.exam_id = $2 ORDER BY r.id DESC LIMIT 50`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: list regrades: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Regrade, error) {
		var g Regrade
		var body string
		err := r.Scan(&g.Cause, &g.Reason, &g.Attempts, &g.Raised, &g.Lowered, &g.By, &g.At, &body)
		g.Question = Snippet(PlainText(body), 80)
		return g, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: list regrades: %w", err)
	}
	return out, nil
}

// BeforeLabel dan AfterLabel menampilkan nilai untuk tabel dampak.
func (r RegradeRow) BeforeLabel() string { return ScoreLabel(r.Before) }

// AfterLabel menampilkan nilai sesudah nilai ulang.
func (r RegradeRow) AfterLabel() string { return ScoreLabel(r.After) }
