package exam

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Data layar pengawas (T143): satu query agregat per sesi, dipanggil tiap
// 10 detik oleh layar pengawas.

// OfflineAfter: peserta yang mengerjakan dianggap offline bila perangkatnya
// tidak menghubungi server selama ini (ping tiap 10 detik, last_seen_at
// ditulis paling sering tiap 30 detik).
const OfflineAfter = 75 * time.Second

// LeaveAttention: keluar halaman sebanyak ini menandai "perlu perhatian".
const LeaveAttention = 3

// ProctorLevel mengembalikan hak a atas layar pengawas sesi: Write untuk
// pengawas sesi dalam jendela waktu, koordinator, dan admin_tu; Read untuk
// kepala sekolah dan pengawas sesi di luar jendela waktu; None untuk yang
// lain (handler menjawab 404).
func ProctorLevel(ctx context.Context, q database.DBTX, a Actor, schoolID, sessionID string, now time.Time) (Level, error) {
	if lv := a.Can(ActProctor, Resource{SessionID: sessionID}, now); lv > None {
		return lv, nil
	}
	var assigned bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_session_proctors WHERE school_id = $1 AND session_id = $2 AND user_id = $3)`,
		schoolID, sessionID, a.UserID).Scan(&assigned); err != nil {
		return None, fmt.Errorf("exam: proctor assignment: %w", err)
	}
	if assigned {
		return Read, nil
	}
	return None, nil
}

// Status peserta di layar pengawas.
const (
	MonNotStarted = "belum"
	MonWorking    = "mengerjakan"
	MonOffline    = "offline"
	MonDone       = "selesai"
)

// MonitorRow adalah satu peserta di layar pengawas.
type MonitorRow struct {
	ParticipantID string
	Name          string
	Number        string // nomor peserta atau NIS
	Seat          int
	LoggedIn      bool // kode sudah dipakai di perangkat
	CodeLocked    bool // terkunci karena salah kode
	AttemptID     string
	SubmitReason  string
	Deadline      time.Time
	Paused        bool
	Locked        bool
	LastSeen      time.Time
	Leaves        int
	Away          time.Duration
	MoveAllowed   bool
	Questions     int
	Answered      int
	LastEvent     int64 // id kejadian keluar halaman/perangkat lain terakhir
	OtherDevice   bool  // mencoba masuk dari perangkat lain (15 menit terakhir)
	ExtraMinutes  int

	State     string
	Attention []string // alasan perlu perhatian, urut terpenting
	Left      time.Duration
}

// NeedsAttention melaporkan apakah peserta perlu perhatian pengawas.
func (r MonitorRow) NeedsAttention() bool { return len(r.Attention) > 0 }

// Monitor adalah isi layar pengawas satu sesi.
type Monitor struct {
	Rows   []MonitorRow
	Counts map[string]int // belum, mengerjakan, offline, perhatian, selesai
}

// LoadMonitor membaca semua peserta sesi beserta attempt terakhir,
// progres, kejadian, dan status kode dalam satu query.
func LoadMonitor(ctx context.Context, q database.DBTX, schoolID, sessionID string, now time.Time) (Monitor, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, coalesce(s.full_name, ap.full_name, ''), coalesce(nullif(p.participant_no, ''), s.nis, ap.registration_number, ''),
			coalesce(p.seat, 0), p.extra_minutes,
			coalesce(c.device_id IS NOT NULL, false), coalesce(c.locked_until > $3, false),
			coalesce(t.id::text, ''), coalesce(t.status, ''), coalesce(t.submit_reason, ''), coalesce(t.deadline_at, $3),
			coalesce(t.paused_at IS NOT NULL, false), coalesce(t.locked_at IS NOT NULL, false), coalesce(t.last_seen_at, $3),
			coalesce(t.leave_count, 0), coalesce(t.away_ms, 0), coalesce(t.device_move_allowed, false),
			coalesce(cardinality(t.question_order), 0),
			coalesce((SELECT count(*) FROM exam_answers x WHERE x.attempt_id = t.id AND x.value <> 'null'::jsonb), 0),
			coalesce((SELECT max(ev.id) FROM exam_attempt_events ev WHERE ev.attempt_id = t.id AND ev.kind IN ('keluar', 'perangkat_lain')), 0),
			EXISTS (SELECT 1 FROM exam_attempt_events ev WHERE ev.attempt_id = t.id AND ev.kind = 'perangkat_lain'
			        AND ev.occurred_at > $3::timestamptz - interval '15 minutes')
		FROM exam_session_participants p
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants ap ON ap.id = p.applicant_id
		LEFT JOIN LATERAL (SELECT * FROM exam_attempts t WHERE t.participant_id = p.id ORDER BY t.attempt_no DESC LIMIT 1) t ON true
		LEFT JOIN LATERAL (SELECT c.device_id, c.locked_until FROM student_credentials c
			WHERE c.school_id = p.school_id AND c.exam_session_id = p.session_id AND c.revoked_at IS NULL AND c.kind = 'kode_ujian'
			  AND c.student_id IS NOT DISTINCT FROM p.student_id AND c.applicant_id IS NOT DISTINCT FROM p.applicant_id
			ORDER BY c.created_at DESC LIMIT 1) c ON true
		WHERE p.school_id = $1 AND p.session_id = $2`, schoolID, sessionID, now)
	if err != nil {
		return Monitor{}, fmt.Errorf("exam: load monitor: %w", err)
	}
	m := Monitor{Counts: map[string]int{}}
	for rows.Next() {
		var r MonitorRow
		var status string
		var away int64
		if err := rows.Scan(&r.ParticipantID, &r.Name, &r.Number, &r.Seat, &r.ExtraMinutes, &r.LoggedIn, &r.CodeLocked,
			&r.AttemptID, &status, &r.SubmitReason, &r.Deadline, &r.Paused, &r.Locked, &r.LastSeen, &r.Leaves, &away, &r.MoveAllowed,
			&r.Questions, &r.Answered, &r.LastEvent, &r.OtherDevice); err != nil {
			rows.Close()
			return Monitor{}, fmt.Errorf("exam: scan monitor: %w", err)
		}
		r.Away = time.Duration(away) * time.Millisecond
		classify(&r, status, now)
		m.Counts[r.State]++
		if r.NeedsAttention() {
			m.Counts["perhatian"]++
		}
		m.Rows = append(m.Rows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Monitor{}, fmt.Errorf("exam: load monitor: %w", err)
	}
	sortMonitor(m.Rows)
	return m, nil
}

func classify(r *MonitorRow, status string, now time.Time) {
	switch {
	case status == "":
		r.State = MonNotStarted
	case status == AttemptSubmitted:
		r.State = MonDone
	case now.Sub(r.LastSeen) > OfflineAfter:
		r.State = MonOffline
	default:
		r.State = MonWorking
	}
	if r.State == MonWorking || r.State == MonOffline {
		r.Left = r.Deadline.Sub(now)
		if r.Left < 0 {
			r.Left = 0
		}
	}
	// Urutan alasan: terkunci, perangkat lain, keluar berulang, offline.
	if r.Locked && r.State != MonDone {
		r.Attention = append(r.Attention, "Terkunci")
	}
	if r.CodeLocked {
		r.Attention = append(r.Attention, "Kode terkunci")
	}
	if r.OtherDevice && !r.MoveAllowed && r.State != MonDone {
		r.Attention = append(r.Attention, "Minta pindah perangkat")
	}
	if r.Leaves >= LeaveAttention && r.State != MonDone {
		r.Attention = append(r.Attention, fmt.Sprintf("Keluar %d kali", r.Leaves))
	}
	if r.State == MonOffline {
		r.Attention = append(r.Attention, "Offline")
	}
}

// attentionRank: makin kecil makin di atas.
func attentionRank(r MonitorRow) int {
	switch {
	case r.Locked && r.State != MonDone, r.CodeLocked:
		return 0
	case r.OtherDevice && !r.MoveAllowed && r.State != MonDone:
		return 1
	case r.Leaves >= LeaveAttention && r.State != MonDone:
		return 2
	case r.State == MonOffline:
		return 3
	case r.State == MonWorking:
		return 4
	case r.State == MonNotStarted:
		return 5
	}
	return 6
}

func sortMonitor(rs []MonitorRow) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := attentionRank(rs[i]), attentionRank(rs[j])
		if a != b {
			return a < b
		}
		if rs[i].Seat != rs[j].Seat && rs[i].Seat > 0 && rs[j].Seat > 0 {
			return rs[i].Seat < rs[j].Seat
		}
		if rs[i].Number != rs[j].Number {
			return rs[i].Number < rs[j].Number
		}
		return rs[i].Name < rs[j].Name
	})
}

// ProctorSession adalah sesi di layar pengawas: data sesi, ujian, dan
// status.
type ProctorSession struct {
	Session
	ExamName  string
	Subject   string
	Duration  int
	Room      string
	ClosedAt  *time.Time
	SignedAt  *time.Time
	TokenReq  bool
	ExamID    string
	EventID   string
	Minutes   string
	Proctored bool // pengguna ini pengawasnya
}

// Phase adalah keadaan sesi: belum, berlangsung, atau selesai.
func (s ProctorSession) Phase(now time.Time) string {
	switch {
	case s.ClosedAt != nil || !now.Before(s.EndsAt.Add(time.Duration(s.ExtraMinutes)*time.Minute)):
		return "selesai"
	case now.Before(s.StartsAt):
		return "belum"
	}
	return "berlangsung"
}

// EndsAtEffective adalah akhir sesi termasuk waktu tambahan sesi.
func (s ProctorSession) EndsAtEffective() time.Time {
	return s.EndsAt.Add(time.Duration(s.ExtraMinutes) * time.Minute)
}

// GetProctorSession membaca sesi untuk layar pengawas.
func GetProctorSession(ctx context.Context, q database.DBTX, schoolID, sessionID, userID string) (ProctorSession, error) {
	s, err := GetSession(ctx, q, schoolID, sessionID)
	if err != nil {
		return ProctorSession{}, err
	}
	if s.CancelledAt != nil {
		return ProctorSession{}, ErrNotFound
	}
	ps := ProctorSession{Session: s}
	err = q.QueryRow(ctx, `SELECT e.id, e.name, coalesce(sub.name, ''), e.duration_minutes, coalesce(r.name, ''), x.closed_at,
			x.minutes_signed_at, x.token_required, coalesce(x.event_id::text, ''), x.minutes_notes,
			EXISTS (SELECT 1 FROM exam_session_proctors sp WHERE sp.session_id = x.id AND sp.user_id = $3)
		FROM exam_sessions x JOIN exams e ON e.id = x.exam_id
		LEFT JOIN subjects sub ON sub.id = e.subject_id
		LEFT JOIN exam_rooms r ON r.id = x.room_id
		WHERE x.school_id = $1 AND x.id = $2`, schoolID, sessionID, userID).
		Scan(&ps.ExamID, &ps.ExamName, &ps.Subject, &ps.Duration, &ps.Room, &ps.ClosedAt, &ps.SignedAt, &ps.TokenReq, &ps.EventID,
			&ps.Minutes, &ps.Proctored)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProctorSession{}, ErrNotFound
	}
	if err != nil {
		return ProctorSession{}, fmt.Errorf("exam: proctor session: %w", err)
	}
	return ps, nil
}

// MarkProctorSeen mencatat pengawas sedang membuka layar (pusat kendali
// T144). Ditulis paling sering sekali per menit.
func MarkProctorSeen(ctx context.Context, q database.DBTX, schoolID, sessionID string, now time.Time) error {
	_, err := q.Exec(ctx, `UPDATE exam_sessions SET proctor_seen_at = $3 WHERE school_id = $1 AND id = $2
		AND (proctor_seen_at IS NULL OR proctor_seen_at < $3::timestamptz - interval '1 minute')`, schoolID, sessionID, now)
	if err != nil {
		return fmt.Errorf("exam: mark proctor seen: %w", err)
	}
	return nil
}

// MySession adalah sesi yang diawasi pengguna hari ini (beranda).
type MySession struct {
	ID           string
	ExamName     string
	Subject      string
	Title        string // ruang/kelas/label
	StartsAt     time.Time
	EndsAt       time.Time
	Participants int
	ClosedAt     *time.Time
}

// MyProctorSessions mengembalikan sesi yang diawasi userID dan berlangsung
// pada rentang [from, to) (mis. hari ini di zona sekolah).
func MyProctorSessions(ctx context.Context, q database.DBTX, schoolID, userID string, from, to time.Time) ([]MySession, error) {
	rows, err := q.Query(ctx, `
		SELECT x.id, e.name, coalesce(sub.name, ''),
			coalesce(nullif(r.name, ''), nullif(x.label, ''), 'Kelas ' || c.name, 'Sesi'), x.starts_at, x.ends_at,
			(SELECT count(*) FROM exam_session_participants p WHERE p.session_id = x.id), x.closed_at
		FROM exam_session_proctors sp
		JOIN exam_sessions x ON x.id = sp.session_id AND x.cancelled_at IS NULL
		JOIN exams e ON e.id = x.exam_id AND e.archived_at IS NULL
		LEFT JOIN subjects sub ON sub.id = e.subject_id
		LEFT JOIN exam_rooms r ON r.id = x.room_id
		LEFT JOIN classes c ON c.id = x.class_id
		WHERE sp.school_id = $1 AND sp.user_id = $2 AND x.starts_at < $4 AND x.ends_at > $3
		ORDER BY x.starts_at, e.name`, schoolID, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("exam: my proctor sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MySession, error) {
		var s MySession
		var n int64
		err := r.Scan(&s.ID, &s.ExamName, &s.Subject, &s.Title, &s.StartsAt, &s.EndsAt, &n, &s.ClosedAt)
		s.Participants = int(n)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: my proctor sessions: %w", err)
	}
	return out, nil
}
