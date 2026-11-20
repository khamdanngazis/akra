package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Koreksi esai (T147): antrian per soal, skor dan komentar, penugasan ke
// guru lain, dan kemiripan jawaban.

// GraderScope adalah bagian esai ujian yang boleh dikoreksi seorang guru.
type GraderScope struct {
	// Full: pemegang ujian, koordinator, admin_tu (semua soal dan sesi,
	// juga mengatur penugasan).
	Full  bool
	rules []graderRule
}

type graderRule struct{ question, session string } // kosong = semua

// Any melaporkan apakah ada yang boleh dikoreksi.
func (s GraderScope) Any() bool { return s.Full || len(s.rules) > 0 }

// Allows melaporkan apakah jawaban soal questionID di sesi sessionID
// termasuk cakupan.
func (s GraderScope) Allows(questionID, sessionID string) bool {
	if s.Full {
		return true
	}
	for _, r := range s.rules {
		if (r.question == "" || r.question == questionID) && (r.session == "" || r.session == sessionID) {
			return true
		}
	}
	return false
}

// AllowsQuestion melaporkan apakah ada bagian soal questionID dalam
// cakupan.
func (s GraderScope) AllowsQuestion(questionID string) bool {
	if s.Full {
		return true
	}
	for _, r := range s.rules {
		if r.question == "" || r.question == questionID {
			return true
		}
	}
	return false
}

// LoadGraderScope menentukan cakupan koreksi a untuk ujian e: penuh bila
// a boleh mengubah ujian (pemegang, koordinator, admin_tu), selain itu
// dari penugasan koreksi. Kepala sekolah tidak mengoreksi.
func LoadGraderScope(ctx context.Context, q database.DBTX, a Actor, e Exam, now time.Time) (GraderScope, error) {
	if e.Access(a, now) >= Write {
		return GraderScope{Full: true}, nil
	}
	if !a.HasAnyRole(roleTeacher, roleAdmin) {
		return GraderScope{}, nil
	}
	rows, err := q.Query(ctx, `SELECT coalesce(question_id::text, ''), coalesce(session_id::text, '') FROM exam_grader_assignments
		WHERE school_id = $1 AND exam_id = $2 AND user_id = $3`, e.SchoolID, e.ID, a.UserID)
	if err != nil {
		return GraderScope{}, fmt.Errorf("exam: grader scope: %w", err)
	}
	defer rows.Close()
	var s GraderScope
	for rows.Next() {
		var r graderRule
		if err := rows.Scan(&r.question, &r.session); err != nil {
			return GraderScope{}, fmt.Errorf("exam: grader scope: %w", err)
		}
		s.rules = append(s.rules, r)
	}
	return s, rows.Err()
}

// EssayQuestion adalah satu soal esai ujian di antrian koreksi.
type EssayQuestion struct {
	Question Question
	Number   int // nomor di paket pertama
	Points   int
	Total    int // jawaban berisi dalam cakupan
	Graded   int
}

// Pending adalah jawaban yang belum dikoreksi.
func (q EssayQuestion) Pending() int { return q.Total - q.Graded }

// essaySnapshot adalah esai di snapshot ujian: soal, poin, dan nomor.
func essaySnapshot(ctx context.Context, q database.DBTX, e Exam) ([]EssayQuestion, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (s.question_id) qv.snapshot, s.points, s.position
		FROM exam_item_snapshots s
		JOIN exam_variants v ON v.id = s.variant_id
		JOIN exam_question_versions qv ON qv.question_id = s.question_id AND qv.version = s.question_version
		WHERE s.school_id = $1 AND s.exam_id = $2 AND qv.snapshot->>'Kind' = 'esai'
		ORDER BY s.question_id, v.label, s.position`, e.SchoolID, e.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: essay questions: %w", err)
	}
	defer rows.Close()
	var out []EssayQuestion
	for rows.Next() {
		var raw []byte
		var pts int16
		var pos int
		if err := rows.Scan(&raw, &pts, &pos); err != nil {
			return nil, fmt.Errorf("exam: essay questions: %w", err)
		}
		var eq EssayQuestion
		if err := json.Unmarshal(raw, &eq.Question); err != nil {
			return nil, fmt.Errorf("exam: decode essay question: %w", err)
		}
		eq.Points, eq.Number = int(pts), pos+1
		out = append(out, eq)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("exam: essay questions: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// EssayAnswer adalah satu jawaban esai yang dikumpulkan.
type EssayAnswer struct {
	AttemptID    string
	SessionID    string
	SessionLabel string
	Name         string
	Number       string // nomor peserta atau NIS
	Text         string
	PointsX10000 *int
	Comment      string
	GradedBy     string
	InScope      bool
	Index        int // urutan dalam cakupan, mulai 1 ("Jawaban 7")
	Similar      []SimilarRef
}

// SimilarRef menunjuk jawaban lain yang mirip.
type SimilarRef struct {
	AttemptID string
	Index     int // 0: di luar cakupan korektor
	Percent   int
}

// Graded melaporkan apakah jawaban sudah dikoreksi.
func (a EssayAnswer) Graded() bool { return a.PointsX10000 != nil }

// PointsLabel menampilkan skor, mis. "2,5".
func (a EssayAnswer) PointsLabel() string {
	if a.PointsX10000 == nil {
		return ""
	}
	return PointsLabel(*a.PointsX10000)
}

// PointsLabel menampilkan poin dalam satuan 1/10.000 dengan koma desimal
// tanpa nol di belakang (20000 → "2", 25000 → "2,5").
func PointsLabel(x int) string {
	s := fmt.Sprintf("%d,%04d", x/PointsUnit, x%PointsUnit)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ",")
}

// LoadEssayAnswers mengembalikan semua jawaban esai berisi untuk soal
// questionID dari attempt yang sudah dikumpulkan, urut waktu kumpul, dengan
// tanda cakupan dan kemiripan. Jawaban di luar cakupan tetap dimuat untuk
// perbandingan kemiripan, tetapi isinya tidak ditampilkan.
func LoadEssayAnswers(ctx context.Context, q database.DBTX, e Exam, questionID string, scope GraderScope) ([]EssayAnswer, error) {
	rows, err := q.Query(ctx, `
		SELECT t.id, t.session_id, coalesce(nullif(x.label, ''), r.name, c.name, ''), coalesce(s.full_name, ap.full_name, ''),
			coalesce(nullif(p.participant_no, ''), s.nis, ap.registration_number, ''), a.value #>> '{}', a.points_x10000, a.essay_comment,
			coalesce(u.full_name, '')
		FROM exam_answers a
		JOIN exam_attempts t ON t.id = a.attempt_id AND t.status = 'dikumpulkan'
		JOIN exam_sessions x ON x.id = t.session_id
		LEFT JOIN exam_rooms r ON r.id = x.room_id
		LEFT JOIN classes c ON c.id = x.class_id
		JOIN exam_session_participants p ON p.id = t.participant_id
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants ap ON ap.id = p.applicant_id
		LEFT JOIN users u ON u.id = a.essay_graded_by
		WHERE t.school_id = $1 AND t.exam_id = $2 AND a.question_id = $3
		  AND jsonb_typeof(a.value) = 'string' AND btrim(a.value #>> '{}') <> ''
		ORDER BY t.submitted_at, 5, t.id`, e.SchoolID, e.ID, questionID)
	if err != nil {
		return nil, fmt.Errorf("exam: essay answers: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (EssayAnswer, error) {
		var a EssayAnswer
		return a, r.Scan(&a.AttemptID, &a.SessionID, &a.SessionLabel, &a.Name, &a.Number, &a.Text, &a.PointsX10000, &a.Comment, &a.GradedBy)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: essay answers: %w", err)
	}
	texts := make([]string, len(out))
	n := 0
	for i := range out {
		texts[i] = out[i].Text
		if scope.Allows(questionID, out[i].SessionID) {
			n++
			out[i].InScope, out[i].Index = true, n
		}
	}
	for _, p := range FindSimilar(texts) {
		a, b := &out[p.A], &out[p.B]
		a.Similar = append(a.Similar, SimilarRef{AttemptID: b.AttemptID, Index: b.Index, Percent: p.Percent()})
		b.Similar = append(b.Similar, SimilarRef{AttemptID: a.AttemptID, Index: a.Index, Percent: p.Percent()})
	}
	return out, nil
}

// LoadEssayQueue mengembalikan soal esai ujian dalam cakupan dengan
// jumlah jawaban dan yang sudah dikoreksi.
func LoadEssayQueue(ctx context.Context, q database.DBTX, e Exam, scope GraderScope) ([]EssayQuestion, error) {
	qs, err := essaySnapshot(ctx, q, e)
	if err != nil {
		return nil, err
	}
	var out []EssayQuestion
	for _, eq := range qs {
		if !scope.AllowsQuestion(eq.Question.ID) {
			continue
		}
		answers, err := LoadEssayAnswers(ctx, q, e, eq.Question.ID, scope)
		if err != nil {
			return nil, err
		}
		for _, a := range answers {
			if !a.InScope {
				continue
			}
			eq.Total++
			if a.Graded() {
				eq.Graded++
			}
		}
		out = append(out, eq)
	}
	return out, nil
}

// Kesalahan koreksi esai.
var (
	ErrBadScore    = errors.New("exam: invalid essay score")
	ErrBadComment  = errors.New("exam: essay comment too long")
	ErrOutOfScope  = errors.New("exam: essay outside grader scope")
	ErrNoEssayText = errors.New("exam: essay answer empty or not submitted")
)

// ParseEssayScore membaca skor esai (koma atau titik desimal, paling
// banyak 2 desimal) dalam satuan 1/10.000 poin, 0..maxPoints.
func ParseEssayScore(s string, maxPoints int) (int, error) {
	v, ok := parseNumber(s)
	if !ok || v < 0 || v > float64(maxPoints)+1e-9 {
		return 0, ErrBadScore
	}
	hund := math.Round(v * 100)
	if math.Abs(v*100-hund) > 1e-6 {
		return 0, ErrBadScore
	}
	return int(hund) * (PointsUnit / 100), nil
}

// GradeEssay menyimpan skor dan komentar satu jawaban esai, lalu menilai
// ulang attempt-nya (menjadi final bila tidak ada esai lain yang menunggu).
func GradeEssay(ctx context.Context, db database.DB, sa StaffActor, e Exam, scope GraderScope, questionID, attemptID, score, comment string, now time.Time) (AttemptGrade, error) {
	if !database.IsUUID(questionID) || !database.IsUUID(attemptID) {
		return AttemptGrade{}, ErrNotFound
	}
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > 1000 {
		return AttemptGrade{}, ErrBadComment
	}
	var g AttemptGrade
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var sessionID string
		var points int16
		var before *int
		err := tx.QueryRow(ctx, `
			SELECT t.session_id, s.points, a.points_x10000
			FROM exam_attempts t
			JOIN exam_answers a ON a.attempt_id = t.id AND a.question_id = $3
			JOIN exam_item_snapshots s ON s.variant_id = t.variant_id AND s.question_id = $3
			JOIN exam_question_versions qv ON qv.question_id = s.question_id AND qv.version = s.question_version
			WHERE t.school_id = $1 AND t.exam_id = $2 AND t.id = $4 AND t.status = 'dikumpulkan' AND qv.snapshot->>'Kind' = 'esai'
			  AND jsonb_typeof(a.value) = 'string' AND btrim(a.value #>> '{}') <> ''
			FOR UPDATE OF t, a`, sa.SchoolID, e.ID, questionID, attemptID).Scan(&sessionID, &points, &before)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoEssayText
		}
		if err != nil {
			return fmt.Errorf("exam: load essay answer: %w", err)
		}
		if !scope.Allows(questionID, sessionID) {
			return ErrOutOfScope
		}
		pts, err := ParseEssayScore(score, int(points))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_answers SET points_x10000 = $3, essay_comment = $4, essay_graded_by = $5, essay_graded_at = $6
			WHERE attempt_id = $1 AND question_id = $2`, attemptID, questionID, pts, comment, sa.UserID, now); err != nil {
			return fmt.Errorf("exam: grade essay: %w", err)
		}
		if g, err = GradeAttempt(ctx, tx, sa.SchoolID, attemptID, now); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.essay_grade", "exam_attempt", attemptID, map[string]any{"points_x10000": before},
			map[string]any{"question_id": questionID, "points_x10000": pts, "comment": comment != "", "score_x100": g.ScoreX100, "status": g.Status})
	})
	return g, err
}

// PendingEssays mengembalikan jumlah jawaban esai ujian yang belum
// dikoreksi (seluruh peserta).
func PendingEssays(ctx context.Context, q database.DBTX, schoolID, examID string) (int, error) {
	var n int64
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM exam_answers a JOIN exam_attempts t ON t.id = a.attempt_id
		WHERE t.school_id = $1 AND t.exam_id = $2 AND t.status = 'dikumpulkan' AND t.score_status = 'menunggu_esai'
		  AND a.points_x10000 IS NULL AND jsonb_typeof(a.value) = 'string' AND btrim(a.value #>> '{}') <> ''`, schoolID, examID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("exam: pending essays: %w", err)
	}
	return int(n), nil
}

// GraderAssignment adalah satu penugasan koreksi.
type GraderAssignment struct {
	ID       int64
	UserID   string
	UserName string
	Question string // kutipan; kosong = semua soal esai
	Session  string // label; kosong = semua sesi
}

// ListGraderAssignments mengembalikan penugasan koreksi ujian.
func ListGraderAssignments(ctx context.Context, q database.DBTX, schoolID, examID string) ([]GraderAssignment, error) {
	rows, err := q.Query(ctx, `
		SELECT g.id, g.user_id, u.full_name,
			coalesce((SELECT qv.snapshot->>'BodySrc' FROM exam_item_snapshots s JOIN exam_question_versions qv
			          ON qv.question_id = s.question_id AND qv.version = s.question_version
			          WHERE s.exam_id = g.exam_id AND s.question_id = g.question_id LIMIT 1), ''),
			coalesce(nullif(x.label, ''), r.name, c.name, '')
		FROM exam_grader_assignments g
		JOIN users u ON u.id = g.user_id
		LEFT JOIN exam_sessions x ON x.id = g.session_id
		LEFT JOIN exam_rooms r ON r.id = x.room_id
		LEFT JOIN classes c ON c.id = x.class_id
		WHERE g.school_id = $1 AND g.exam_id = $2
		ORDER BY u.full_name, g.id`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: grader assignments: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (GraderAssignment, error) {
		var g GraderAssignment
		var body string
		err := r.Scan(&g.ID, &g.UserID, &g.UserName, &body, &g.Session)
		if body != "" {
			g.Question = Snippet(PlainText(body), 60)
		}
		return g, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: grader assignments: %w", err)
	}
	return out, nil
}

// ErrBadAssignment: guru, soal, atau sesi penugasan tidak valid.
var ErrBadAssignment = errors.New("exam: invalid grader assignment")

// AssignGrader menugaskan guru userID mengoreksi soal esai questionID
// (kosong = semua) di sesi sessionID (kosong = semua). Hak (pengelola
// ujian) dicek pemanggil.
func AssignGrader(ctx context.Context, db database.DB, sa StaffActor, e Exam, userID, questionID, sessionID string) error {
	if !database.IsUUID(userID) || (questionID != "" && !database.IsUUID(questionID)) || (sessionID != "" && !database.IsUUID(sessionID)) {
		return ErrBadAssignment
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
			WHERE u.school_id = $1 AND u.id = $2 AND u.is_active AND r.code = 'guru')`, sa.SchoolID, userID).Scan(&ok); err != nil || !ok {
			return ErrBadAssignment
		}
		if questionID != "" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_item_snapshots s JOIN exam_question_versions qv
				ON qv.question_id = s.question_id AND qv.version = s.question_version
				WHERE s.exam_id = $1 AND s.question_id = $2 AND qv.snapshot->>'Kind' = 'esai')`, e.ID, questionID).Scan(&ok); err != nil || !ok {
				return ErrBadAssignment
			}
		}
		if sessionID != "" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_sessions WHERE school_id = $1 AND exam_id = $2 AND id = $3)`,
				sa.SchoolID, e.ID, sessionID).Scan(&ok); err != nil || !ok {
				return ErrBadAssignment
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_grader_assignments (school_id, exam_id, user_id, question_id, session_id, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT ON CONSTRAINT exam_grader_assignments_key DO NOTHING`,
			sa.SchoolID, e.ID, userID, nilIfEmpty(questionID), nilIfEmpty(sessionID), sa.UserID); err != nil {
			return fmt.Errorf("exam: assign grader: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.grader_assign", "exam", e.ID, nil,
			map[string]any{"user_id": userID, "question_id": questionID, "session_id": sessionID})
	})
}

// RemoveGrader menghapus penugasan koreksi id.
func RemoveGrader(ctx context.Context, db database.DB, sa StaffActor, e Exam, id int64) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM exam_grader_assignments WHERE school_id = $1 AND exam_id = $2 AND id = $3`, sa.SchoolID, e.ID, id)
		if err != nil {
			return fmt.Errorf("exam: remove grader: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return recordAudit(ctx, tx, sa, "exam.grader_remove", "exam", e.ID, map[string]any{"assignment_id": id}, nil)
	})
}

// EssayWork adalah pekerjaan koreksi guru di beranda.
type EssayWork struct {
	ExamID   string
	ExamName string
	Pending  int  // jawaban esai dalam cakupan yang belum dikoreksi
	Ready    bool // pemegang ujian: semua esai sudah dikoreksi, nilai final
}

// ReadyNotice: kabar "nilai siap" tampil sekian lama setelah esai terakhir
// dikoreksi.
const ReadyNotice = 14 * 24 * time.Hour

// MyEssayWork mengembalikan ujian dengan esai yang menunggu koreksi oleh
// userID (sebagai pemegang ujian atau lewat penugasan), dan ujian milik
// userID yang esainya baru selesai dikoreksi (nilai siap dikirim ke rapor).
func MyEssayWork(ctx context.Context, q database.DBTX, schoolID, userID string, now time.Time) ([]EssayWork, error) {
	rows, err := q.Query(ctx, `
		WITH pend AS (
			SELECT t.exam_id, t.session_id, a.question_id
			FROM exam_answers a JOIN exam_attempts t ON t.id = a.attempt_id
			WHERE t.school_id = $1 AND t.status = 'dikumpulkan' AND t.score_status = 'menunggu_esai'
			  AND a.points_x10000 IS NULL AND jsonb_typeof(a.value) = 'string' AND btrim(a.value #>> '{}') <> ''),
		mine AS (
			SELECT pend.exam_id, count(*) AS n FROM pend JOIN exams e ON e.id = pend.exam_id
			WHERE e.owner_id = $2 AND e.archived_at IS NULL
			   OR EXISTS (SELECT 1 FROM exam_grader_assignments g WHERE g.exam_id = pend.exam_id AND g.user_id = $2
			              AND (g.question_id IS NULL OR g.question_id = pend.question_id)
			              AND (g.session_id IS NULL OR g.session_id = pend.session_id))
			GROUP BY pend.exam_id),
		ready AS (
			SELECT e.id AS exam_id FROM exams e
			WHERE e.school_id = $1 AND e.owner_id = $2 AND e.archived_at IS NULL
			  AND NOT EXISTS (SELECT 1 FROM pend WHERE pend.exam_id = e.id)
			  AND EXISTS (SELECT 1 FROM exam_answers a JOIN exam_attempts t ON t.id = a.attempt_id
			              WHERE t.exam_id = e.id AND a.essay_graded_at > $3))
		SELECT e.id, e.name, coalesce(mine.n, 0), ready.exam_id IS NOT NULL
		FROM exams e LEFT JOIN mine ON mine.exam_id = e.id LEFT JOIN ready ON ready.exam_id = e.id
		WHERE e.school_id = $1 AND (mine.n > 0 OR ready.exam_id IS NOT NULL)
		ORDER BY ready.exam_id IS NOT NULL, e.name`, schoolID, userID, now.Add(-ReadyNotice))
	if err != nil {
		return nil, fmt.Errorf("exam: my essay work: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (EssayWork, error) {
		var w EssayWork
		var n int64
		err := r.Scan(&w.ExamID, &w.ExamName, &n, &w.Ready)
		w.Pending = int(n)
		return w, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: my essay work: %w", err)
	}
	return out, nil
}
