package exam

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Laporan pilot (T155): ukuran keberhasilan MVP 4 (indeks bagian 9) untuk
// satu sekolah dalam satu rentang waktu, dihitung dari data AKRA. Ukuran
// yang tidak tercatat di AKRA (tiket bantuan, "tanpa didampingi") diisi tim
// dari catatan pilot.

// Target ukuran keberhasilan (indeks MVP 4 bagian 9).
const (
	TargetUnassistedPct   = 95.0 // % attempt selesai tanpa bantuan pengawas
	TargetStartMinutes    = 5.0  // satu kelas mengerjakan sejak token dibacakan
	TargetBuildMinutes    = 15.0 // ulangan 20 soal dari nol
	TargetBuildWordMin    = 5.0  // ulangan 20 soal dari file Word
	TargetEssay30Minutes  = 20.0 // koreksi 30 esai pendek
	TargetSecondExamPct   = 70.0 // guru pilot membuat ujian kedua
	TargetTicketsPer100   = 1.0  // tiket bantuan per 100 peserta
	essayGapLimit         = 10 * time.Minute
	buildLookback         = 3 * time.Hour
	minStartSessionAttend = 5
)

// PilotReport adalah hasil ukur pilot satu sekolah.
type PilotReport struct {
	School, SchoolCode string
	From, To           time.Time

	Participants int // peserta berbeda yang memulai attempt
	Attempts     int
	Submitted    int

	// Jawaban hilang: soal yang perubahannya tiba setelah attempt ditutup.
	LostAnswers  int
	LostAttempts int

	// Attempt yang dibantu pengawas: pindah perangkat, reset login, buka
	// kunci, tambah waktu, atau dikumpulkan pengawas.
	Assisted int

	Sessions []PilotSession
	Exams    []PilotExam
	Graders  []PilotGrader

	// Guru yang membuat ujian (bukan latihan bawaan) di rentang ini.
	Teachers, TeachersSecond int

	Incidents, IncidentsOpen int
	Sample                   []PilotSample
}

// PilotSession: kecepatan mulai satu sesi.
type PilotSession struct {
	Title    string
	Location string
	Started  int
	// P90Minutes: menit sejak peserta pertama mulai sampai 90% peserta mulai.
	P90Minutes float64
}

// PilotExam: lama menyusun satu ujian.
type PilotExam struct {
	Name      string
	Questions int
	FromWord  bool
	// Minutes: dari soal/impor pertama (≤ 3 jam sebelum ujian dibuat) atau
	// ujian dibuat, sampai dijadwalkan.
	Minutes float64
}

// PilotGrader: kecepatan koreksi esai satu korektor pada satu ujian.
type PilotGrader struct {
	Exam, Grader string
	Graded       int
	// Per30Minutes: perkiraan menit untuk 30 esai (jeda > 10 menit diabaikan).
	Per30Minutes float64
}

// PilotSample: satu lembar acak untuk dicocokkan dengan koreksi manual.
type PilotSample struct {
	ExamID, AttemptID, Exam, Number string
	Score                           string
}

// UnassistedPct mengembalikan persen attempt tanpa bantuan pengawas.
func (r PilotReport) UnassistedPct() float64 {
	if r.Attempts == 0 {
		return 0
	}
	return 100 * float64(r.Attempts-r.Assisted) / float64(r.Attempts)
}

// BuildPilotReport menghitung laporan pilot sekolah schoolID untuk attempt
// dan ujian dalam [from, to). sample: jumlah lembar acak (0 = tanpa).
func BuildPilotReport(ctx context.Context, q database.DBTX, schoolID string, from, to time.Time, sample int) (PilotReport, error) {
	r := PilotReport{From: from, To: to}
	if err := q.QueryRow(ctx, `SELECT name, code FROM schools WHERE id = $1`, schoolID).Scan(&r.School, &r.SchoolCode); err != nil {
		return r, fmt.Errorf("exam: pilot school: %w", err)
	}
	// Attempt dalam rentang (bukan ujian latihan bawaan).
	const attempts = `SELECT t.id, t.participant_id, t.status, t.submit_reason FROM exam_attempts t JOIN exams e ON e.id = t.exam_id
		WHERE t.school_id = $1 AND t.started_at >= $2 AND t.started_at < $3 AND NOT e.builtin`
	err := q.QueryRow(ctx, `WITH a AS (`+attempts+`)
		SELECT count(*), count(DISTINCT participant_id), count(*) FILTER (WHERE status = 'dikumpulkan'),
			(SELECT count(*) FROM exam_lost_answers l WHERE l.attempt_id IN (SELECT id FROM a)),
			(SELECT count(DISTINCT l.attempt_id) FROM exam_lost_answers l WHERE l.attempt_id IN (SELECT id FROM a)),
			count(*) FILTER (WHERE
				EXISTS (SELECT 1 FROM exam_attempt_events v WHERE v.attempt_id = a.id AND v.kind IN ('pindah_perangkat', 'dibuka'))
				OR EXISTS (SELECT 1 FROM audit_logs g WHERE g.school_id = $1 AND g.entity_type = 'exam_attempt' AND g.entity_id = a.id::text
					AND (g.action = 'exam.attempt_unlock' OR (g.action = 'exam.attempt_submit' AND g.user_id IS NOT NULL)))
				OR EXISTS (SELECT 1 FROM audit_logs g WHERE g.school_id = $1 AND g.entity_type = 'exam_session_participant'
					AND g.entity_id = a.participant_id::text AND g.action IN ('exam.participant_reset_login', 'exam.participant_add_time')))
		FROM a`, schoolID, from, to).Scan(&r.Attempts, &r.Participants, &r.Submitted, &r.LostAnswers, &r.LostAttempts, &r.Assisted)
	if err != nil {
		return r, fmt.Errorf("exam: pilot attempts: %w", err)
	}
	if r.Sessions, err = pilotSessions(ctx, q, schoolID, from, to); err != nil {
		return r, err
	}
	if r.Exams, err = pilotExams(ctx, q, schoolID, from, to); err != nil {
		return r, err
	}
	if r.Graders, err = pilotGraders(ctx, q, schoolID, from, to); err != nil {
		return r, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE n >= 2) FROM (
			SELECT e.owner_id, count(*) AS n FROM exams e JOIN user_roles ur ON ur.user_id = e.owner_id JOIN roles ro ON ro.id = ur.role_id AND ro.code = 'guru'
			WHERE e.school_id = $1 AND NOT e.builtin AND e.created_at >= $2 AND e.created_at < $3 GROUP BY e.owner_id) x`,
		schoolID, from, to).Scan(&r.Teachers, &r.TeachersSecond); err != nil {
		return r, fmt.Errorf("exam: pilot teachers: %w", err)
	}
	// Kejadian platform (semua sekolah) di rentang ini.
	if err := q.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE resolved_at IS NULL) FROM ops_incidents WHERE opened_at >= $1 AND opened_at < $2`,
		from, to).Scan(&r.Incidents, &r.IncidentsOpen); err != nil {
		return r, fmt.Errorf("exam: pilot incidents: %w", err)
	}
	if sample > 0 {
		if r.Sample, err = pilotSample(ctx, q, schoolID, from, to, sample); err != nil {
			return r, err
		}
	}
	return r, nil
}

func pilotSessions(ctx context.Context, q database.DBTX, schoolID string, from, to time.Time) ([]PilotSession, error) {
	rows, err := q.Query(ctx, `
		SELECT e.name || coalesce(' · ' || c.name, ''), x.location, count(*),
			extract(epoch FROM percentile_disc(0.9) WITHIN GROUP (ORDER BY t.started_at) - min(t.started_at)) / 60
		FROM exam_sessions x JOIN exams e ON e.id = x.exam_id
		LEFT JOIN classes c ON c.id = x.class_id
		JOIN exam_session_participants p ON p.session_id = x.id
		JOIN exam_attempts t ON t.participant_id = p.id AND t.attempt_no = 1
		WHERE x.school_id = $1 AND x.starts_at >= $2 AND x.starts_at < $3 AND NOT e.builtin
		GROUP BY x.id, e.name, c.name, x.location, x.starts_at HAVING count(*) >= $4
		ORDER BY x.starts_at`, schoolID, from, to, minStartSessionAttend)
	if err != nil {
		return nil, fmt.Errorf("exam: pilot sessions: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PilotSession, error) {
		var s PilotSession
		return s, r.Scan(&s.Title, &s.Location, &s.Started, &s.P90Minutes)
	})
}

func pilotExams(ctx context.Context, q database.DBTX, schoolID string, from, to time.Time) ([]PilotExam, error) {
	rows, err := q.Query(ctx, `
		SELECT e.name, (SELECT count(*) FROM exam_items i WHERE i.exam_id = e.id AND i.kind = 'soal'),
			EXISTS (SELECT 1 FROM exam_imports m WHERE m.school_id = e.school_id AND m.created_by = e.owner_id AND m.source = 'word'
				AND m.created_at BETWEEN e.created_at - $4::interval AND e.scheduled_at),
			extract(epoch FROM e.scheduled_at - least(e.created_at,
				coalesce((SELECT min(qq.created_at) FROM exam_items i JOIN exam_questions qq ON qq.id = i.question_id
					WHERE i.exam_id = e.id AND qq.created_by = e.owner_id AND qq.created_at >= e.created_at - $4::interval), e.created_at),
				coalesce((SELECT min(m.created_at) FROM exam_imports m WHERE m.school_id = e.school_id AND m.created_by = e.owner_id
					AND m.created_at BETWEEN e.created_at - $4::interval AND e.scheduled_at), e.created_at))) / 60
		FROM exams e
		WHERE e.school_id = $1 AND NOT e.builtin AND e.scheduled_at IS NOT NULL AND e.created_at >= $2 AND e.created_at < $3
		ORDER BY e.created_at`, schoolID, from, to, buildLookback.String())
	if err != nil {
		return nil, fmt.Errorf("exam: pilot exams: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PilotExam, error) {
		var x PilotExam
		return x, r.Scan(&x.Name, &x.Questions, &x.FromWord, &x.Minutes)
	})
}

func pilotGraders(ctx context.Context, q database.DBTX, schoolID string, from, to time.Time) ([]PilotGrader, error) {
	rows, err := q.Query(ctx, `
		WITH g AS (
			SELECT e.id AS exam_id, e.name AS exam, u.full_name AS grader, a.essay_graded_at AS at,
				a.essay_graded_at - lag(a.essay_graded_at) OVER (PARTITION BY e.id, a.essay_graded_by ORDER BY a.essay_graded_at) AS gap
			FROM exam_answers a JOIN exam_attempts t ON t.id = a.attempt_id JOIN exams e ON e.id = t.exam_id
			JOIN users u ON u.id = a.essay_graded_by
			WHERE t.school_id = $1 AND a.essay_graded_at >= $2 AND a.essay_graded_at < $3)
		SELECT exam, grader, count(*),
			coalesce(extract(epoch FROM avg(gap) FILTER (WHERE gap <= $4::interval)) / 60 * 30, 0)
		FROM g GROUP BY exam_id, exam, grader HAVING count(*) >= 5 ORDER BY exam, grader`,
		schoolID, from, to, essayGapLimit.String())
	if err != nil {
		return nil, fmt.Errorf("exam: pilot graders: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PilotGrader, error) {
		var g PilotGrader
		return g, r.Scan(&g.Exam, &g.Grader, &g.Graded, &g.Per30Minutes)
	})
}

func pilotSample(ctx context.Context, q database.DBTX, schoolID string, from, to time.Time, n int) ([]PilotSample, error) {
	rows, err := q.Query(ctx, `
		SELECT e.id, t.id, e.name, coalesce(nullif(p.participant_no, ''), '-'), t.score_x100
		FROM exam_attempts t JOIN exams e ON e.id = t.exam_id JOIN exam_session_participants p ON p.id = t.participant_id
		WHERE t.school_id = $1 AND t.started_at >= $2 AND t.started_at < $3 AND NOT e.builtin
		  AND t.status = 'dikumpulkan' AND t.score_status = 'final'
		ORDER BY random() LIMIT $4`, schoolID, from, to, n)
	if err != nil {
		return nil, fmt.Errorf("exam: pilot sample: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PilotSample, error) {
		var s PilotSample
		var x100 *int64
		if err := r.Scan(&s.ExamID, &s.AttemptID, &s.Exam, &s.Number, &x100); err != nil {
			return s, err
		}
		if x100 != nil {
			s.Score = formatX100(*x100)
		}
		return s, nil
	})
}

// formatX100 menulis nilai skala 100 (disimpan ×100) dengan koma desimal.
func formatX100(v int64) string {
	s := fmt.Sprintf("%d,%02d", v/100, v%100)
	return strings.TrimSuffix(strings.TrimSuffix(s, "0"), ",0")
}

// mark mengembalikan ✅ bila ok, ❌ bila tidak.
func mark(ok bool) string {
	if ok {
		return "✅"
	}
	return "❌"
}

func num(f float64) string { return strings.ReplaceAll(fmt.Sprintf("%.1f", f), ".", ",") }

// WriteMarkdown menulis laporan untuk doc/uji-mvp4.md. tickets: jumlah
// tiket bantuan dari catatan tim (-1 = belum diisi). baseURL untuk tautan
// lembar sampel.
func (r PilotReport) WriteMarkdown(w io.Writer, tickets int, baseURL string) error {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	p("### Pilot %s (%s), %s – %s\n\n", r.School, r.SchoolCode, r.From.Format("2006-01-02"), r.To.Add(-time.Second).Format("2006-01-02"))
	p("%d peserta, %d attempt (%d dikumpulkan).\n\n", r.Participants, r.Attempts, r.Submitted)
	p("| Ukuran | Target | Hasil | |\n|---|---|---|---|\n")
	p("| Jawaban hilang | 0 | %d soal di %d attempt | %s |\n", r.LostAnswers, r.LostAttempts, mark(r.LostAnswers == 0))
	p("| Attempt tanpa bantuan pengawas | ≥ %s%% | %s%% (%d dibantu) | %s |\n", num(TargetUnassistedPct), num(r.UnassistedPct()), r.Assisted,
		mark(r.Attempts > 0 && r.UnassistedPct() >= TargetUnassistedPct))
	worst := 0.0
	for _, s := range r.Sessions {
		worst = max(worst, s.P90Minutes)
	}
	if len(r.Sessions) > 0 {
		p("| Kelas mulai mengerjakan (90%% peserta) | ≤ %s menit | terlama %s menit dari %d sesi | %s |\n", num(TargetStartMinutes), num(worst), len(r.Sessions), mark(worst <= TargetStartMinutes))
	} else {
		p("| Kelas mulai mengerjakan (90%% peserta) | ≤ %s menit | belum ada sesi ≥ %d peserta | – |\n", num(TargetStartMinutes), minStartSessionAttend)
	}
	p("| Guru membuat ujian kedua | ≥ %s%% | %d dari %d guru | %s |\n", num(TargetSecondExamPct), r.TeachersSecond, r.Teachers,
		mark(r.Teachers > 0 && 100*float64(r.TeachersSecond)/float64(r.Teachers) >= TargetSecondExamPct))
	if tickets >= 0 && r.Participants > 0 {
		per := 100 * float64(tickets) / float64(r.Participants)
		p("| Tiket bantuan per 100 peserta | ≤ %s | %s (%d tiket) | %s |\n", num(TargetTicketsPer100), num(per), tickets, mark(per <= TargetTicketsPer100))
	} else {
		p("| Tiket bantuan per 100 peserta | ≤ %s | isi dari catatan tim | – |\n", num(TargetTicketsPer100))
	}
	p("| Kejadian platform (semua sekolah) | 0 terbuka | %d kejadian, %d masih terbuka | %s |\n\n", r.Incidents, r.IncidentsOpen, mark(r.IncidentsOpen == 0))

	if len(r.Sessions) > 0 {
		p("**Kecepatan mulai per sesi** (menit sejak peserta pertama mulai sampai 90%% peserta mulai):\n\n| Sesi | Lokasi | Peserta | Menit |\n|---|---|---|---|\n")
		for _, s := range r.Sessions {
			p("| %s | %s | %d | %s |\n", s.Title, s.Location, s.Started, num(s.P90Minutes))
		}
		p("\n")
	}
	if len(r.Exams) > 0 {
		p("**Lama menyusun ujian** (dari soal atau impor pertama sampai dijadwalkan; target 20 soal ≤ %s menit dari nol, ≤ %s menit dari Word):\n\n| Ujian | Soal | Dari Word | Menit |\n|---|---|---|---|\n",
			num(TargetBuildMinutes), num(TargetBuildWordMin))
		for _, x := range r.Exams {
			word := "tidak"
			if x.FromWord {
				word = "ya"
			}
			p("| %s | %d | %s | %s |\n", x.Name, x.Questions, word, num(x.Minutes))
		}
		p("\n")
	}
	if len(r.Graders) > 0 {
		p("**Koreksi esai** (perkiraan menit per 30 esai; target ≤ %s):\n\n| Ujian | Korektor | Esai | Menit/30 |\n|---|---|---|---|\n", num(TargetEssay30Minutes))
		for _, g := range r.Graders {
			p("| %s | %s | %d | %s %s |\n", g.Exam, g.Grader, g.Graded, num(g.Per30Minutes), mark(g.Per30Minutes <= TargetEssay30Minutes))
		}
		p("\n")
	}
	if len(r.Sample) > 0 {
		p("**Perbandingan nilai**: koreksi manual lembar berikut tanpa melihat nilai AKRA, lalu isi kolom manual.\n\n| No | Ujian | No. peserta | Nilai AKRA | Nilai manual | Cocok |\n|---|---|---|---|---|---|\n")
		for i, s := range r.Sample {
			link := fmt.Sprintf("%s/ujian/paket/%s/hasil/peserta/%s", baseURL, s.ExamID, s.AttemptID)
			p("| %d | [%s](%s) | %s | %s | | |\n", i+1, s.Exam, link, s.Number, s.Score)
		}
		p("\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
