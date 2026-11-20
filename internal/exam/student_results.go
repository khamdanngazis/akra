package exam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
)

// Hasil untuk siswa dan orang tua (T151). Aturan membuka hasil:
//
//   - tes seleksi tidak pernah membuka nilai maupun pembahasan untuk calon
//     siswa (hasil seleksi lewat halaman status pendaftaran);
//   - aturan ujian "tidak" menampilkan hasil: tertutup;
//   - "dibuka setelah sesi terakhir" (bawaan ulangan/PTS/PAS): terbuka bila
//     guru sudah menekan Buka hasil DAN semua sesi ujian (termasuk susulan
//     yang terjadwal kemudian) sudah berakhir, agar soal tidak bocor;
//   - tanpa "setelah sesi terakhir" (mis. latihan): terbuka begitu attempt
//     dikumpulkan.
//
// Pembahasan (soal, jawaban, kunci, pembahasan, ketuntasan TP) hanya untuk
// siswa dan hanya bila aturan "nilai dan pembahasan". Orang tua hanya
// melihat nilai, tidak pernah soal atau kunci.

// ResultsWindow adalah lama kode kartu siswa berlaku untuk melihat hasil
// setelah hasil dibuka (T124).
const ResultsWindow = 30 * 24 * time.Hour

// Error buka hasil.
var (
	ErrResultsHidden      = errors.New("exam: results are not shown to participants")
	ErrResultsImmediate   = errors.New("exam: results open right after submission")
	ErrSessionsNotEnded   = errors.New("exam: some sessions have not ended")
	ErrAdmissionNoResults = errors.New("exam: admission tests never show results")
)

// sessionsEnded melaporkan semua sesi ujian (yang tidak dibatalkan) sudah
// berakhir pada now: ditutup pengawas, atau lewat jam selesai termasuk waktu
// tambahan. Susulan yang dijadwalkan kemudian membuat ini false lagi.
func sessionsEnded(ctx context.Context, q database.DBTX, examID string, now time.Time) (bool, error) {
	var pending bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_sessions WHERE exam_id = $1 AND cancelled_at IS NULL
		AND closed_at IS NULL AND ends_at + make_interval(mins => extra_minutes) > $2)`, examID, now).Scan(&pending); err != nil {
		return false, fmt.Errorf("exam: sessions ended: %w", err)
	}
	return !pending, nil
}

// resultsOpen menerapkan aturan membuka hasil untuk ujian e.
func resultsOpen(e Exam, ended bool) bool {
	switch {
	case e.Purpose == PurposeAdmission, e.ResultsMode == ResultsNone:
		return false
	case !e.ResultsAfterAll:
		return true
	}
	return e.ResultsReleasedAt != nil && ended
}

// ResultsOpen melaporkan hasil ujian e terbuka untuk siswa dan orang tua.
func ResultsOpen(ctx context.Context, q database.DBTX, e Exam, now time.Time) (bool, error) {
	ended, err := sessionsEnded(ctx, q, e.ID, now)
	if err != nil {
		return false, err
	}
	return resultsOpen(e, ended), nil
}

// ReleaseState adalah keadaan tombol Buka hasil di halaman hasil.
type ReleaseState struct {
	Mode     string // aturan hasil ujian
	Released *time.Time
	Ended    bool // semua sesi berakhir
	Open     bool
	Never    bool // tes seleksi atau aturan "tidak"
	Instant  bool // terbuka langsung setelah dikumpulkan
}

// LoadReleaseState menyusun keadaan buka hasil ujian e.
func LoadReleaseState(ctx context.Context, q database.DBTX, e Exam, now time.Time) (ReleaseState, error) {
	ended, err := sessionsEnded(ctx, q, e.ID, now)
	if err != nil {
		return ReleaseState{}, err
	}
	return ReleaseState{Mode: e.ResultsMode, Released: e.ResultsReleasedAt, Ended: ended, Open: resultsOpen(e, ended),
		Never: e.Purpose == PurposeAdmission || e.ResultsMode == ResultsNone, Instant: !e.ResultsAfterAll}, nil
}

// ReleaseResults membuka (open true) atau menutup kembali hasil ujian e
// untuk siswa dan orang tua. Membuka hanya bila semua sesi (termasuk
// susulan) sudah berakhir; kode kartu siswa ujian ini lalu berlaku
// ResultsWindow untuk melihat hasil. Hak ubah ujian diperiksa pemanggil.
func ReleaseResults(ctx context.Context, db database.DB, sa StaffActor, e Exam, open bool, now time.Time) error {
	switch {
	case e.Purpose == PurposeAdmission:
		return ErrAdmissionNoResults
	case e.ResultsMode == ResultsNone:
		return ErrResultsHidden
	case !e.ResultsAfterAll:
		return ErrResultsImmediate
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if open {
			ended, err := sessionsEnded(ctx, tx, e.ID, now)
			if err != nil {
				return err
			}
			if !ended {
				return ErrSessionsNotEnded
			}
		}
		var at, by, until any
		if open {
			at, by, until = now, sa.UserID, now.Add(ResultsWindow)
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET results_released_at = $3, results_released_by = $4, updated_at = now() WHERE school_id = $1 AND id = $2`,
			e.SchoolID, e.ID, at, by); err != nil {
			return fmt.Errorf("exam: release results: %w", err)
		}
		// Kode kartu siswa (bukan pendaftar) untuk sesi ujian ini.
		if _, err := tx.Exec(ctx, `UPDATE student_credentials c SET results_until = $3
			FROM exam_sessions x WHERE x.id = c.exam_session_id AND x.school_id = $1 AND x.exam_id = $2
			  AND c.student_id IS NOT NULL AND c.kind = 'kode_ujian' AND c.revoked_at IS NULL`, e.SchoolID, e.ID, until); err != nil {
			return fmt.Errorf("exam: results window: %w", err)
		}
		action := "exam.results_release"
		if !open {
			action = "exam.results_hide"
		}
		return recordAudit(ctx, tx, sa, action, "exam", e.ID, nil, map[string]any{"open": open})
	})
}

// StudentResult adalah hasil satu ujian untuk siswa atau orang tuanya.
type StudentResult struct {
	ExamID    string
	ExamName  string
	Subject   string
	Purpose   Purpose
	AttemptID string
	ScoreX100 *int // nil: menunggu koreksi esai
	Review    bool // pembahasan boleh dibuka siswa
	OpenedAt  time.Time
}

// Score menampilkan nilai.
func (r StudentResult) Score() string {
	if r.ScoreX100 == nil {
		return ""
	}
	return ScoreLabel(*r.ScoreX100)
}

// PurposeLabel menampilkan jenis ujian.
func (r StudentResult) PurposeLabel() string { return r.Purpose.Label() }

// StudentResults mengembalikan hasil ujian siswa yang sudah terbuka, terbaru
// dulu: attempt terakhir yang dikumpulkan per ujian.
func StudentResults(ctx context.Context, q database.DBTX, schoolID, studentID string, now time.Time) ([]StudentResult, error) {
	if !database.IsUUID(studentID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (t.exam_id) t.exam_id::text, t.id::text, t.score_x100, t.score_status, t.submitted_at
		FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id
		WHERE t.school_id = $1 AND p.student_id = $2 AND t.status = 'dikumpulkan'
		ORDER BY t.exam_id, t.submitted_at DESC, t.attempt_no DESC`, schoolID, studentID)
	if err != nil {
		return nil, fmt.Errorf("exam: student results: %w", err)
	}
	type att struct {
		exam, id string
		score    *int
		status   *string
		at       time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (att, error) {
		var a att
		return a, r.Scan(&a.exam, &a.id, &a.score, &a.status, &a.at)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: student results: %w", err)
	}
	var out []StudentResult
	for _, a := range list {
		e, err := GetExam(ctx, q, schoolID, a.exam)
		if err != nil {
			return nil, err
		}
		if e.ArchivedAt != nil {
			continue
		}
		open, err := ResultsOpen(ctx, q, e, now)
		if err != nil {
			return nil, err
		}
		if !open {
			continue
		}
		r := StudentResult{ExamID: e.ID, ExamName: e.Name, Subject: e.SubjectName, Purpose: e.Purpose, AttemptID: a.id,
			Review: e.ResultsMode == ResultsExplanation, OpenedAt: a.at}
		if e.ResultsReleasedAt != nil && e.ResultsAfterAll {
			r.OpenedAt = *e.ResultsReleasedAt
		}
		if a.status != nil && *a.status == ScoreFinal {
			r.ScoreX100 = a.score
		}
		out = append(out, r)
	}
	// Terbaru dulu.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].OpenedAt.After(out[j-1].OpenedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// ObjectiveProgress adalah ketuntasan satu TP di pembahasan siswa.
type ObjectiveProgress struct {
	ObjectiveRef
	Percent  int
	Mastered bool
}

// StudentReview adalah pembahasan satu attempt untuk siswa.
type StudentReview struct {
	Exam       Exam
	Result     StudentResult
	Answers    []AnswerDetail
	Objectives []ObjectiveProgress
	KKTP       KKTP
}

// LoadStudentReview memuat pembahasan attempt attemptID milik peserta p:
// hanya siswa (bukan pendaftar), hanya bila hasil ujian terbuka dengan
// aturan "nilai dan pembahasan". ErrNotFound untuk selain itu, agar
// keberadaan pembahasan tidak bocor lewat URL.
func LoadStudentReview(ctx context.Context, q database.DBTX, p auth.Participant, attemptID string, now time.Time) (StudentReview, error) {
	var rv StudentReview
	if p.StudentID == "" || p.ApplicantID != "" {
		return rv, ErrNotFound
	}
	a, err := GetAttempt(ctx, q, p, attemptID)
	if err != nil {
		return rv, err
	}
	if a.Running() {
		return rv, ErrNotFound
	}
	e, err := GetExam(ctx, q, p.SchoolID, a.ExamID)
	if err != nil {
		return rv, err
	}
	results, err := StudentResults(ctx, q, p.SchoolID, p.StudentID, now)
	if err != nil {
		return rv, err
	}
	for _, r := range results {
		if r.ExamID == e.ID {
			rv.Result = r
		}
	}
	// Hanya attempt terakhir yang ditampilkan di hasil.
	if rv.Result.AttemptID != a.ID || !rv.Result.Review {
		return rv, ErrNotFound
	}
	rv.Exam = e
	if _, rv.Answers, err = loadAttemptDetail(ctx, q, e, a.ID, func(id string) string { return "/u/a/" + a.ID + "/gambar/" + id }); err != nil {
		return rv, err
	}
	if rv.KKTP, err = ResolveKKTP(ctx, q, e); err != nil {
		return rv, err
	}
	// Ketuntasan TP dari soal yang dikerjakan siswa ini.
	items := map[string][]GradeItem{"": nil}
	type acc struct{ earned, points int }
	sums := map[string]*acc{}
	var order []string
	for _, d := range rv.Answers {
		it := d.Item
		items[""] = append(items[""], it)
		oid := it.Question.ObjectiveID
		if oid == "" || it.Voided {
			continue
		}
		if sums[oid] == nil {
			sums[oid] = &acc{}
			order = append(order, oid)
		}
		sums[oid].points += it.Points
		if d.Earned != nil {
			sums[oid].earned += *d.Earned
		}
	}
	refs, err := objectiveRefs(ctx, q, e.SchoolID, items)
	if err != nil {
		return rv, err
	}
	byID := map[string]ObjectiveRef{}
	for _, r := range refs {
		byID[r.ID] = r
	}
	for _, id := range order {
		s := sums[id]
		ref, ok := byID[id]
		if !ok {
			ref = ObjectiveRef{ID: id, Code: "TP"}
		}
		pct := 0
		if s.points > 0 {
			pct = (2*s.earned*100/PointsUnit + s.points) / (2 * s.points)
		}
		rv.Objectives = append(rv.Objectives, ObjectiveProgress{ObjectiveRef: ref, Percent: pct, Mastered: mastered(s.earned, s.points, rv.KKTP.Value)})
	}
	return rv, nil
}

// ReviewAllowed melaporkan pembahasan attempt terbuka untuk peserta p
// (dipakai route gambar soal setelah attempt selesai).
func ReviewAllowed(ctx context.Context, q database.DBTX, p auth.Participant, attemptID string, now time.Time) bool {
	_, err := LoadStudentReview(ctx, q, p, attemptID, now)
	return err == nil
}

// --- Portal orang tua ---

// ChildExamItem adalah satu jadwal ujian anak.
type ChildExamItem struct {
	Name, Subject string
	Start, End    time.Time
	Makeup        bool
	Room, Number  string
}

// ChildAbsence adalah ujian yang tidak diikuti anak.
type ChildAbsence struct {
	ExamName, Subject string
	Makeup            *time.Time // jadwal susulan bila ada
}

// ChildExams adalah ringkasan ujian anak untuk orang tua: tanpa soal,
// jawaban, maupun kunci.
type ChildExams struct {
	Upcoming []ChildExamItem
	Results  []StudentResult
	Absent   []ChildAbsence
}

// LoadChildExams menyusun jadwal, nilai yang sudah dibuka, dan ketidakhadiran
// siswa studentID. Kepemilikan anak diperiksa pemanggil (portal).
func LoadChildExams(ctx context.Context, q database.DBTX, schoolID, studentID string, now time.Time) (ChildExams, error) {
	var c ChildExams
	// Jadwal PTS/PAS yang sudah diumumkan (T134).
	sched, err := StudentSchedule(ctx, q, schoolID, studentID, now)
	if err != nil {
		return c, err
	}
	for _, it := range sched {
		if it.End.After(now) {
			c.Upcoming = append(c.Upcoming, ChildExamItem{Name: it.Event, Subject: it.Subject, Start: it.Start, End: it.End, Room: it.Room, Number: it.Number})
		}
	}
	// Sesi ulangan dan susulan anak yang belum berakhir (di luar agenda).
	rows, err := q.Query(ctx, `
		SELECT e.name, coalesce(s.name, ''), x.starts_at, x.ends_at + make_interval(mins => x.extra_minutes), x.kind = 'susulan'
		FROM exam_session_participants p
		JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL AND x.event_id IS NULL
		JOIN exams e ON e.id = x.exam_id AND e.archived_at IS NULL AND e.purpose IN ('ulangan', 'pts', 'pas')
		LEFT JOIN subjects s ON s.id = e.subject_id
		WHERE p.school_id = $1 AND p.student_id = $2 AND x.ends_at + make_interval(mins => x.extra_minutes) > $3
		ORDER BY x.starts_at`, schoolID, studentID, now)
	if err != nil {
		return c, fmt.Errorf("exam: child sessions: %w", err)
	}
	more, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ChildExamItem, error) {
		var it ChildExamItem
		return it, r.Scan(&it.Name, &it.Subject, &it.Start, &it.End, &it.Makeup)
	})
	if err != nil {
		return c, fmt.Errorf("exam: child sessions: %w", err)
	}
	c.Upcoming = append(c.Upcoming, more...)
	for i := 1; i < len(c.Upcoming); i++ {
		for j := i; j > 0 && c.Upcoming[j].Start.Before(c.Upcoming[j-1].Start); j-- {
			c.Upcoming[j], c.Upcoming[j-1] = c.Upcoming[j-1], c.Upcoming[j]
		}
	}
	if c.Results, err = StudentResults(ctx, q, schoolID, studentID, now); err != nil {
		return c, err
	}
	// Tidak hadir: semua sesi anak untuk ujian itu sudah berakhir tanpa
	// attempt; susulan yang belum berakhir ditampilkan sebagai jadwalnya.
	rows, err = q.Query(ctx, `
		SELECT e.name, coalesce(s.name, ''),
		       (SELECT min(x2.starts_at) FROM exam_session_participants p2 JOIN exam_sessions x2 ON x2.id = p2.session_id AND x2.cancelled_at IS NULL
		        WHERE x2.exam_id = e.id AND p2.student_id = $2 AND x2.kind = 'susulan' AND x2.ends_at > $3)
		FROM exams e LEFT JOIN subjects s ON s.id = e.subject_id
		WHERE e.school_id = $1 AND e.archived_at IS NULL AND e.purpose IN ('ulangan', 'pts', 'pas')
		  AND EXISTS (SELECT 1 FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
		              WHERE x.exam_id = e.id AND p.student_id = $2 AND x.ends_at + make_interval(mins => x.extra_minutes) <= $3)
		  AND NOT EXISTS (SELECT 1 FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id
		                  WHERE t.exam_id = e.id AND p.student_id = $2)
		ORDER BY e.created_at DESC LIMIT 20`, schoolID, studentID, now)
	if err != nil {
		return c, fmt.Errorf("exam: child absences: %w", err)
	}
	c.Absent, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ChildAbsence, error) {
		var a ChildAbsence
		return a, r.Scan(&a.ExamName, &a.Subject, &a.Makeup)
	})
	if err != nil {
		return c, fmt.Errorf("exam: child absences: %w", err)
	}
	return c, nil
}
