package exam

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Data lintas sekolah untuk tim platform (T145): ujian aktif (jendela
// deploy), tunggakan kumpul otomatis (alert), dan jadwal musim ujian
// (dasbor internal). Bukan untuk halaman sekolah.

// Nama endpoint peserta di metrics (T145).
const (
	EndpointLogin  = "peserta_login"
	EndpointOpen   = "peserta_buka"
	EndpointStart  = "peserta_mulai"
	EndpointSave   = "peserta_simpan"
	EndpointSubmit = "peserta_kumpul"
)

// DeployLead: sesi yang mulai dalam rentang ini ikut menahan deploy.
const DeployLead = 30 * time.Minute

// ActiveSession adalah satu sesi yang sedang atau segera berlangsung.
type ActiveSession struct {
	SchoolCode string    `json:"school"`
	ExamName   string    `json:"exam"`
	Location   string    `json:"location"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Running    int       `json:"running_attempts"`
	Upcoming   bool      `json:"upcoming"`
}

// ActiveExams adalah ringkasan ujian aktif seluruh platform.
type ActiveExams struct {
	Active          int             `json:"active_sessions"`
	Upcoming        int             `json:"upcoming_sessions"`
	RunningAttempts int             `json:"running_attempts"`
	Sessions        []ActiveSession `json:"sessions"`
}

// Blocking melaporkan apakah deploy sebaiknya ditunda.
func (a ActiveExams) Blocking() bool { return a.Active > 0 || a.Upcoming > 0 || a.RunningAttempts > 0 }

// LoadActiveExams mengembalikan sesi yang sedang berlangsung dan sesi yang
// mulai dalam lead. Sesi berlangsung dihitung bila di ruang (PTS/PAS,
// ulangan di kelas) atau ada peserta yang sedang mengerjakan; sesi jarak
// jauh yang jendelanya berhari-hari tidak menahan deploy selama tidak ada
// yang mengerjakan. RunningAttempts juga menghitung attempt yang masih
// dalam tenggang simpan.
func LoadActiveExams(ctx context.Context, q database.DBTX, now time.Time, lead time.Duration) (ActiveExams, error) {
	var out ActiveExams
	rows, err := q.Query(ctx, `
		WITH run AS (
			SELECT session_id, count(*) AS n FROM exam_attempts
			WHERE status = 'berjalan' AND (paused_at IS NOT NULL OR deadline_at + $3::interval > $1)
			GROUP BY session_id)
		SELECT s.code, e.name, x.location, x.starts_at, x.ends_at + make_interval(mins => x.extra_minutes),
			coalesce(run.n, 0), x.starts_at > $1
		FROM exam_sessions x
		JOIN exams e ON e.id = x.exam_id AND e.archived_at IS NULL
		JOIN schools s ON s.id = x.school_id
		LEFT JOIN run ON run.session_id = x.id
		WHERE x.cancelled_at IS NULL AND (
			run.n > 0
			OR (x.closed_at IS NULL AND x.starts_at <= $1 AND x.ends_at + make_interval(mins => x.extra_minutes) > $1 AND x.location = 'di_ruang')
			OR (x.closed_at IS NULL AND x.starts_at > $1 AND x.starts_at <= $1::timestamptz + $2::interval))
		ORDER BY x.starts_at, s.code`, now, lead.String(), SaveGrace.String())
	if err != nil {
		return out, fmt.Errorf("exam: active exams: %w", err)
	}
	out.Sessions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ActiveSession, error) {
		var s ActiveSession
		var n int64
		err := r.Scan(&s.SchoolCode, &s.ExamName, &s.Location, &s.StartsAt, &s.EndsAt, &n, &s.Upcoming)
		s.Running = int(n)
		return s, err
	})
	if err != nil {
		return out, fmt.Errorf("exam: active exams: %w", err)
	}
	for _, s := range out.Sessions {
		if s.Upcoming {
			out.Upcoming++
		} else {
			out.Active++
		}
		out.RunningAttempts += s.Running
	}
	return out, nil
}

// AutoSubmitBacklog mengembalikan jumlah attempt yang seharusnya sudah
// dikumpulkan job kumpul otomatis (tenggat + tenggang lewat, tidak dijeda)
// dan berapa lama yang tertua tertunda.
func AutoSubmitBacklog(ctx context.Context, q database.DBTX, now time.Time) (int, time.Duration, error) {
	var n int64
	var oldest *time.Time
	err := q.QueryRow(ctx, `SELECT count(*), min(deadline_at) FROM exam_attempts
		WHERE status = 'berjalan' AND paused_at IS NULL AND deadline_at + $2::interval <= $1`, now, SaveGrace.String()).Scan(&n, &oldest)
	if err != nil {
		return 0, 0, fmt.Errorf("exam: auto submit backlog: %w", err)
	}
	if oldest == nil {
		return 0, 0, nil
	}
	return int(n), now.Sub(oldest.Add(SaveGrace)), nil
}

// RunningAttemptsBySchool mengembalikan jumlah attempt berjalan per kode
// sekolah (metrik).
func RunningAttemptsBySchool(ctx context.Context, q database.DBTX, now time.Time) (map[string]int, error) {
	rows, err := q.Query(ctx, `SELECT s.code, count(*) FROM exam_attempts t JOIN schools s ON s.id = t.school_id
		WHERE t.status = 'berjalan' AND (t.paused_at IS NOT NULL OR t.deadline_at + $2::interval > $1) GROUP BY s.code`, now, SaveGrace.String())
	if err != nil {
		return nil, fmt.Errorf("exam: running attempts: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var code string
		var n int64
		if err := rows.Scan(&code, &n); err != nil {
			return nil, fmt.Errorf("exam: running attempts: %w", err)
		}
		out[code] = int(n)
	}
	return out, rows.Err()
}

// SeasonDay adalah jadwal ujian satu sekolah pada satu hari (zona
// sekolah).
type SeasonDay struct {
	Day        time.Time
	SchoolCode string
	SchoolName string
	Sessions   int
	InRoom     int // sesi di ruang (PTS/PAS, ulangan kelas)
	FirstStart time.Time
	LastEnd    time.Time
}

// LoadExamSeason mengembalikan jadwal ujian semua sekolah dari from
// sampai from+days, per hari dan sekolah, untuk merencanakan rilis.
func LoadExamSeason(ctx context.Context, q database.DBTX, from time.Time, days int) ([]SeasonDay, error) {
	rows, err := q.Query(ctx, `
		SELECT (x.starts_at AT TIME ZONE s.timezone)::date AS day, s.code, s.name, count(*),
			count(*) FILTER (WHERE x.location = 'di_ruang'), min(x.starts_at), max(x.ends_at + make_interval(mins => x.extra_minutes))
		FROM exam_sessions x
		JOIN exams e ON e.id = x.exam_id AND e.archived_at IS NULL
		JOIN schools s ON s.id = x.school_id
		WHERE x.cancelled_at IS NULL AND x.ends_at > $1 AND x.starts_at < $1::timestamptz + make_interval(days => $2)
		GROUP BY 1, 2, 3 ORDER BY 1, 2`, from, days)
	if err != nil {
		return nil, fmt.Errorf("exam: season: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SeasonDay, error) {
		var d SeasonDay
		var n, inRoom int64
		err := r.Scan(&d.Day, &d.SchoolCode, &d.SchoolName, &n, &inRoom, &d.FirstStart, &d.LastEnd)
		d.Sessions, d.InRoom = int(n), int(inRoom)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: season: %w", err)
	}
	return out, nil
}
