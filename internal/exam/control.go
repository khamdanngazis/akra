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

// Pusat kendali koordinator (T144): kondisi semua ruang satu agenda hari
// ini dalam satu query, aksi lintas ruang, pindah peserta, ganti pengawas,
// pembatalan soal, dan rekap harian.

// ProctorFresh: pengawas dianggap sedang membuka layar pengawas bila
// layarnya menghubungi server dalam rentang ini (proctor_seen_at ditulis
// paling sering sekali per menit saat layar diperbarui).
const ProctorFresh = 3 * time.Minute

// OfflineRed: porsi peserta offline (dari yang sedang mengerjakan) yang
// membuat kartu merah.
const OfflineRed = 0.2

// ControlSession adalah satu sesi agenda di pusat kendali.
type ControlSession struct {
	ID          string
	UnitKey     string // room|slot|shift
	Room        string
	ExamID      string
	ExamName    string
	Subject     string
	Grade       int
	StartsAt    time.Time
	EndsAt      time.Time
	Extra       int
	PausedAt    *time.Time
	ClosedAt    *time.Time
	ProctorSeen *time.Time
	Signed      bool
	Proctors    []string // nama
	ProctorIDs  []string
	Total       int
	Present     int
	Working     int
	Offline     int
	Done        int
	Attention   int
}

// EndsAtEffective adalah akhir sesi termasuk waktu tambahan sesi.
func (s ControlSession) EndsAtEffective() time.Time {
	return s.EndsAt.Add(time.Duration(s.Extra) * time.Minute)
}

// Phase: belum, berlangsung, atau selesai.
func (s ControlSession) Phase(now time.Time) string {
	switch {
	case s.ClosedAt != nil || !now.Before(s.EndsAtEffective()):
		return "selesai"
	case now.Before(s.StartsAt):
		return "belum"
	}
	return "berlangsung"
}

// Warna kartu ruang.
const (
	CardGreen  = "hijau"
	CardYellow = "kuning"
	CardRed    = "merah"
	CardIdle   = "abu" // belum mulai atau sudah selesai, tanpa masalah
)

// ControlUnit adalah satu kartu ruang: ruang × jam × gelombang, berisi
// sesi tiap tingkat yang diujikan di ruang itu.
type ControlUnit struct {
	Key      string // room|slot|shift
	RoomID   string
	Room     string
	SlotID   string
	Shift    int
	StartsAt time.Time
	EndsAt   time.Time // akhir efektif terlama
	Sessions []ControlSession

	Phase       string
	Proctors    []string
	ProctorIDs  []string
	ProctorOpen bool // pengawas sedang membuka layar
	ProctorEver bool // pernah membuka layar
	Total       int
	Present     int
	Working     int
	Offline     int
	Done        int
	Attention   int
	Paused      bool
	Unsigned    int // sesi selesai yang berita acaranya belum ditandatangani
	Color       string
	Problems    []string
}

// SessionIDs mengembalikan id sesi kartu dipisah spasi (nilai checkbox).
func (u ControlUnit) SessionIDs() string {
	ids := make([]string, len(u.Sessions))
	for i, s := range u.Sessions {
		ids[i] = s.ID
	}
	return strings.Join(ids, " ")
}

// Subjects mengembalikan mapel dan tingkat sesi-sesi kartu.
func (u ControlUnit) Subjects() string {
	var parts []string
	for _, s := range u.Sessions {
		parts = append(parts, fmt.Sprintf("%s (%d)", s.Subject, s.Grade))
	}
	return strings.Join(parts, ", ")
}

// LoadControlSessions membaca sesi agenda eventID yang bersinggungan
// dengan [from, to) beserta ringkasan peserta, dalam satu query.
func LoadControlSessions(ctx context.Context, q database.DBTX, schoolID, eventID string, from, to, now time.Time) ([]ControlSession, error) {
	rows, err := q.Query(ctx, `
		SELECT x.id, x.exam_id, e.name, coalesce(sub.name, ''), coalesce(c.grade_level, 0), coalesce(x.room_id::text, ''),
			coalesce(r.name, x.label), coalesce(c.slot_id::text, ''), coalesce(x.shift, 1),
			x.starts_at, x.ends_at, x.extra_minutes, x.paused_at, x.closed_at, x.proctor_seen_at, x.minutes_signed_at IS NOT NULL,
			coalesce(pr.names, '{}'), coalesce(pr.ids, '{}'),
			agg.total, agg.present, agg.working, agg.offline, agg.done, agg.attention
		FROM exam_sessions x
		JOIN exams e ON e.id = x.exam_id
		LEFT JOIN exam_event_cells c ON c.id = x.cell_id
		LEFT JOIN subjects sub ON sub.id = c.subject_id
		LEFT JOIN exam_rooms r ON r.id = x.room_id
		LEFT JOIN LATERAL (
			SELECT array_agg(u.full_name ORDER BY u.full_name) AS names, array_agg(u.id::text ORDER BY u.full_name) AS ids
			FROM exam_session_proctors sp JOIN users u ON u.id = sp.user_id WHERE sp.session_id = x.id) pr ON true
		LEFT JOIN LATERAL (
			SELECT count(*) AS total, count(t.id) AS present,
				count(*) FILTER (WHERE t.live AND t.last_seen_at >= $5::timestamptz - $6::interval) AS working,
				count(*) FILTER (WHERE t.live AND t.last_seen_at < $5::timestamptz - $6::interval) AS offline,
				count(*) FILTER (WHERE t.id IS NOT NULL AND NOT t.live) AS done,
				count(*) FILTER (WHERE t.live AND (t.locked_at IS NOT NULL OR t.leave_count >= $7
				                                   OR t.last_seen_at < $5::timestamptz - $6::interval)) AS attention
			FROM exam_session_participants p
			LEFT JOIN LATERAL (
				SELECT t.id, t.last_seen_at, t.locked_at, t.leave_count,
					t.status = 'berjalan' AND (t.paused_at IS NOT NULL OR t.deadline_at > $5) AS live
				FROM exam_attempts t WHERE t.participant_id = p.id ORDER BY t.attempt_no DESC LIMIT 1) t ON true
			WHERE p.session_id = x.id) agg ON true
		WHERE x.school_id = $1 AND x.event_id = $2 AND x.cancelled_at IS NULL AND x.starts_at < $4 AND x.ends_at > $3
		ORDER BY x.starts_at, 7, x.shift, c.grade_level`,
		schoolID, eventID, from, to, now, OfflineAfter.String(), LeaveAttention)
	if err != nil {
		return nil, fmt.Errorf("exam: control sessions: %w", err)
	}
	defer rows.Close()
	var out []ControlSession
	for rows.Next() {
		var s ControlSession
		var room, slot string
		var shift int16
		var total, present, working, offline, done, attention int64
		if err := rows.Scan(&s.ID, &s.ExamID, &s.ExamName, &s.Subject, &s.Grade, &room, &s.Room, &slot, &shift,
			&s.StartsAt, &s.EndsAt, &s.Extra, &s.PausedAt, &s.ClosedAt, &s.ProctorSeen, &s.Signed, &s.Proctors, &s.ProctorIDs,
			&total, &present, &working, &offline, &done, &attention); err != nil {
			return nil, fmt.Errorf("exam: scan control session: %w", err)
		}
		s.Total, s.Present, s.Working, s.Offline, s.Done, s.Attention = int(total), int(present), int(working), int(offline), int(done), int(attention)
		s.UnitKey = unitKeyOf(room, slot, int(shift))
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("exam: control sessions: %w", err)
	}
	return out, nil
}

// BuildControlUnits mengelompokkan sesi menjadi kartu ruang dan menghitung
// warnanya: merah bila ruang tanpa pengawas (belum ditugaskan, atau sesi
// berlangsung dan pengawas tidak membuka layar) atau offline > 20%; kuning
// bila ada peserta perlu perhatian/offline, sesi dijeda, atau berita acara
// belum ditandatangani; hijau normal.
func BuildControlUnits(sessions []ControlSession, now time.Time) []ControlUnit {
	idx := map[string]int{}
	var units []ControlUnit
	for _, s := range sessions {
		key := s.UnitKey
		i, ok := idx[key]
		if !ok {
			parts := strings.Split(key, "|")
			u := ControlUnit{Key: key, Room: s.Room, StartsAt: s.StartsAt, EndsAt: s.EndsAtEffective()}
			if len(parts) == 3 {
				u.RoomID, u.SlotID = parts[0], parts[1]
				fmt.Sscan(parts[2], &u.Shift)
			}
			units = append(units, u)
			i = len(units) - 1
			idx[key] = i
		}
		u := &units[i]
		u.Sessions = append(u.Sessions, s)
		if s.StartsAt.Before(u.StartsAt) {
			u.StartsAt = s.StartsAt
		}
		if s.EndsAtEffective().After(u.EndsAt) {
			u.EndsAt = s.EndsAtEffective()
		}
	}
	for i := range units {
		finishUnit(&units[i], now)
	}
	return units
}

func finishUnit(u *ControlUnit, now time.Time) {
	seenP := map[string]bool{}
	phases := map[string]bool{}
	allPaused := true
	running := 0
	for _, s := range u.Sessions {
		for j, id := range s.ProctorIDs {
			if !seenP[id] {
				seenP[id] = true
				u.ProctorIDs = append(u.ProctorIDs, id)
				u.Proctors = append(u.Proctors, s.Proctors[j])
			}
		}
		if s.ProctorSeen != nil {
			u.ProctorEver = true
			if now.Sub(*s.ProctorSeen) <= ProctorFresh {
				u.ProctorOpen = true
			}
		}
		u.Total += s.Total
		u.Present += s.Present
		u.Working += s.Working
		u.Offline += s.Offline
		u.Done += s.Done
		u.Attention += s.Attention
		ph := s.Phase(now)
		phases[ph] = true
		if ph == "berlangsung" {
			running++
			allPaused = allPaused && s.PausedAt != nil
		}
		if ph == "selesai" && !s.Signed {
			u.Unsigned++
		}
	}
	u.Paused = running > 0 && allPaused
	switch {
	case phases["berlangsung"]:
		u.Phase = "berlangsung"
	case phases["belum"]:
		u.Phase = "belum"
	default:
		u.Phase = "selesai"
	}
	red, yellow := false, false
	if len(u.ProctorIDs) == 0 && u.Phase != "selesai" {
		red = true
		u.Problems = append(u.Problems, "Belum ada pengawas")
	} else if u.Phase == "berlangsung" && !u.ProctorOpen {
		red = true
		u.Problems = append(u.Problems, "Pengawas tidak membuka layar pengawas")
	} else if u.Phase == "belum" && !u.ProctorOpen && !now.Before(u.StartsAt.Add(-15*time.Minute)) {
		yellow = true
		u.Problems = append(u.Problems, "Pengawas belum membuka layar")
	}
	if live := u.Working + u.Offline; live > 0 && float64(u.Offline) > OfflineRed*float64(live) {
		red = true
		u.Problems = append(u.Problems, fmt.Sprintf("%d dari %d peserta offline", u.Offline, live))
	} else if u.Offline > 0 {
		yellow = true
	}
	if u.Attention > 0 {
		yellow = true
	}
	if u.Paused {
		yellow = true
		u.Problems = append(u.Problems, "Dijeda")
	}
	if u.Unsigned > 0 {
		yellow = true
		u.Problems = append(u.Problems, "Berita acara belum ditandatangani")
	}
	switch {
	case red:
		u.Color = CardRed
	case yellow:
		u.Color = CardYellow
	case u.Phase == "berlangsung":
		u.Color = CardGreen
	default:
		u.Color = CardIdle
	}
}

// ControlCounts adalah ringkasan seluruh agenda hari ini.
type ControlCounts struct {
	Rooms, Red, Yellow, Running            int
	Total, Present, Working, Offline, Done int
	Attention                              int
}

// CountUnits meringkas kartu.
func CountUnits(us []ControlUnit) ControlCounts {
	var c ControlCounts
	for _, u := range us {
		c.Rooms++
		switch u.Color {
		case CardRed:
			c.Red++
		case CardYellow:
			c.Yellow++
		}
		if u.Phase == "berlangsung" {
			c.Running++
		}
		c.Total += u.Total
		c.Present += u.Present
		c.Working += u.Working
		c.Offline += u.Offline
		c.Done += u.Done
		c.Attention += u.Attention
	}
	return c
}

// EventSessionIDs menyaring ids menjadi sesi agenda eventID yang tidak
// dibatalkan; id lain diabaikan.
func EventSessionIDs(ctx context.Context, q database.DBTX, schoolID, eventID string, ids []string) ([]string, error) {
	ids = uniqueUUIDs(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT id::text FROM exam_sessions WHERE school_id = $1 AND event_id = $2 AND id = ANY ($3::uuid[])
		AND cancelled_at IS NULL ORDER BY starts_at, id`, schoolID, eventID, ids)
	if err != nil {
		return nil, fmt.Errorf("exam: event sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("exam: event sessions: %w", err)
	}
	return out, nil
}

// RunningEventSessions mengembalikan sesi agenda yang sedang berlangsung.
func RunningEventSessions(ctx context.Context, q database.DBTX, schoolID, eventID string, now time.Time) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT id::text FROM exam_sessions WHERE school_id = $1 AND event_id = $2 AND cancelled_at IS NULL
		AND closed_at IS NULL AND starts_at <= $3 AND ends_at + make_interval(mins => extra_minutes) > $3 ORDER BY starts_at, id`,
		schoolID, eventID, now)
	if err != nil {
		return nil, fmt.Errorf("exam: running sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("exam: running sessions: %w", err)
	}
	return out, nil
}

// Kesalahan pindah peserta dan ganti pengawas.
var (
	ErrMoveTarget    = errors.New("exam: invalid move target")
	ErrMoveDuplicate = errors.New("exam: participant already in target session")
	ErrBadProctor    = errors.New("exam: invalid proctor")
)

// MoveResult adalah hasil pindah peserta.
type MoveResult struct {
	Name     string
	FromRoom string
	ToRoom   string
	Running  bool // attempt berjalan ikut dipindah
}

// MoveParticipant memindahkan peserta (mis. komputernya rusak) dari
// sesinya ke sesi lain agenda yang sama untuk ujian yang sama. Jawaban
// tetap karena attempt ikut pindah; sisa waktunya tetap (tenggat digeser
// shift_ms bila aturan waktu sesi tujuan berbeda). Perangkat boleh
// diganti sekali, dan kode kartu ikut pindah ke sesi tujuan. Tercatat di
// berita acara kedua sesi.
func MoveParticipant(ctx context.Context, db database.DB, sa StaffActor, eventID, participantID, toSessionID, reason string, now time.Time) (MoveResult, error) {
	var res MoveResult
	if !database.IsUUID(participantID) || !database.IsUUID(toSessionID) {
		return res, ErrNotFound
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 200 {
		return res, ErrBadNote
	}
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var fromID, examID, fromRoom string
		var studentID, applicantID *string
		err := tx.QueryRow(ctx, `SELECT p.session_id, x.exam_id, coalesce(r.name, x.label), p.student_id, p.applicant_id,
				coalesce(s.full_name, ap.full_name, '')
			FROM exam_session_participants p
			JOIN exam_sessions x ON x.id = p.session_id AND x.event_id = $3 AND x.cancelled_at IS NULL
			LEFT JOIN exam_rooms r ON r.id = x.room_id
			LEFT JOIN students s ON s.id = p.student_id
			LEFT JOIN applicants ap ON ap.id = p.applicant_id
			WHERE p.school_id = $1 AND p.id = $2
			FOR UPDATE OF p`, sa.SchoolID, participantID, eventID).Scan(&fromID, &examID, &fromRoom, &studentID, &applicantID, &res.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("exam: lock moving participant: %w", err)
		}
		if fromID == toSessionID {
			return ErrMoveTarget
		}
		// Kunci kedua sesi dalam urutan id agar tidak saling tunggu.
		var toPaused *time.Time
		var toExam, toRoom string
		var toClosed bool
		var toEnd time.Time
		lock := []string{fromID, toSessionID}
		sort.Strings(lock)
		if _, err := tx.Exec(ctx, `SELECT 1 FROM exam_sessions WHERE id = ANY ($1::uuid[]) ORDER BY id FOR UPDATE`, lock); err != nil {
			return fmt.Errorf("exam: lock sessions: %w", err)
		}
		err = tx.QueryRow(ctx, `SELECT x.exam_id, coalesce(r.name, x.label), x.paused_at, x.closed_at IS NOT NULL,
				x.ends_at + make_interval(mins => x.extra_minutes)
			FROM exam_sessions x LEFT JOIN exam_rooms r ON r.id = x.room_id
			WHERE x.school_id = $1 AND x.id = $2 AND x.event_id = $3 AND x.cancelled_at IS NULL`,
			sa.SchoolID, toSessionID, eventID).Scan(&toExam, &toRoom, &toPaused, &toClosed, &toEnd)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMoveTarget
		}
		if err != nil {
			return fmt.Errorf("exam: move target: %w", err)
		}
		if toExam != examID || toClosed || !now.Before(toEnd) {
			return ErrMoveTarget
		}
		var dup bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_session_participants WHERE session_id = $1
				AND student_id IS NOT DISTINCT FROM $2 AND applicant_id IS NOT DISTINCT FROM $3)`, toSessionID, studentID, applicantID).Scan(&dup); err != nil {
			return fmt.Errorf("exam: check duplicate: %w", err)
		}
		if dup {
			return ErrMoveDuplicate
		}
		res.FromRoom, res.ToRoom = fromRoom, toRoom
		if _, err := tx.Exec(ctx, `UPDATE exam_session_participants SET session_id = $2, seat = NULL WHERE id = $1`, participantID, toSessionID); err != nil {
			return fmt.Errorf("exam: move participant: %w", err)
		}
		// Kode kartu peserta untuk sesi asal ikut pindah.
		if _, err := tx.Exec(ctx, `UPDATE student_credentials SET exam_session_id = $2
			WHERE school_id = $1 AND exam_session_id = $3 AND revoked_at IS NULL
			  AND student_id IS NOT DISTINCT FROM $4 AND applicant_id IS NOT DISTINCT FROM $5`,
			sa.SchoolID, toSessionID, fromID, studentID, applicantID); err != nil {
			return fmt.Errorf("exam: move credential: %w", err)
		}
		// Attempt (selesai maupun berjalan) ikut pindah; jawaban tidak
		// disentuh.
		if _, err := tx.Exec(ctx, `UPDATE exam_attempts SET session_id = $2 WHERE participant_id = $1`, participantID, toSessionID); err != nil {
			return fmt.Errorf("exam: move attempts: %w", err)
		}
		var attemptID string
		var deadline time.Time
		err = tx.QueryRow(ctx, `SELECT id, deadline_at FROM exam_attempts WHERE participant_id = $1 AND status = 'berjalan' FOR UPDATE`,
			participantID).Scan(&attemptID, &deadline)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("exam: running attempt: %w", err)
		default:
			res.Running = true
			if err := keepDeadline(ctx, tx, attemptID, deadline); err != nil {
				return err
			}
			// Perangkat baru boleh dipakai; sesi tujuan yang dijeda ikut
			// menjeda peserta.
			if _, err := tx.Exec(ctx, `UPDATE exam_attempts SET device_move_allowed = true,
					paused_at = CASE WHEN $2::timestamptz IS NOT NULL AND paused_at IS NULL AND deadline_at > $3 THEN $3 ELSE paused_at END
				WHERE id = $1`, attemptID, toPaused, now); err != nil {
				return fmt.Errorf("exam: allow device after move: %w", err)
			}
		}
		why := ""
		if reason != "" {
			why = " (" + reason + ")"
		}
		for _, n := range []struct{ sid, body string }{
			{fromID, fmt.Sprintf("Dipindah ke %s oleh koordinator%s.", toRoom, why)},
			{toSessionID, fmt.Sprintf("Pindahan dari %s oleh koordinator%s.", fromRoom, why)},
		} {
			pid := any(nil)
			if n.sid == toSessionID {
				pid = participantID
			}
			if _, err := tx.Exec(ctx, `INSERT INTO exam_session_notes (school_id, session_id, participant_id, body, created_by, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)`, sa.SchoolID, n.sid, pid, left500(res.Name+": "+n.body), sa.UserID, now); err != nil {
				return fmt.Errorf("exam: move note: %w", err)
			}
		}
		return recordAudit(ctx, tx, sa, "exam.participant_move", "exam_session_participant", participantID,
			map[string]any{"session_id": fromID}, map[string]any{"session_id": toSessionID, "reason": reason})
	})
	return res, err
}

func left500(s string) string {
	if utf8.RuneCountInString(s) <= 500 {
		return s
	}
	return string([]rune(s)[:500])
}

// keepDeadline menghitung tenggat attempt di sesi barunya dan menyimpan
// selisihnya ke shift_ms sehingga tenggat (sisa waktu) tidak berubah.
func keepDeadline(ctx context.Context, tx pgx.Tx, attemptID string, deadline time.Time) error {
	var in DeadlineInput
	var pausedMS, shiftMS int64
	err := tx.QueryRow(ctx, `
		SELECT t.started_at, t.paused_ms, t.shift_ms, x.starts_at, x.ends_at, x.late_policy, x.extra_minutes, e.duration_minutes, p.extra_minutes
		FROM exam_attempts t
		JOIN exam_sessions x ON x.id = t.session_id
		JOIN exams e ON e.id = t.exam_id
		JOIN exam_session_participants p ON p.id = t.participant_id
		WHERE t.id = $1`, attemptID).Scan(&in.StartedAt, &pausedMS, &shiftMS, &in.SessionStart, &in.SessionEnd, &in.LatePolicy,
		&in.SessionExtra, &in.DurationMinutes, &in.ParticipantExtra)
	if err != nil {
		return fmt.Errorf("exam: deadline input: %w", err)
	}
	in.Paused = time.Duration(pausedMS) * time.Millisecond
	base := ComputeDeadline(in) // tanpa pergeseran
	shift := deadline.Sub(base).Milliseconds()
	if _, err := tx.Exec(ctx, `UPDATE exam_attempts SET shift_ms = $2 WHERE id = $1`, attemptID, shift); err != nil {
		return fmt.Errorf("exam: keep deadline: %w", err)
	}
	return nil
}

// ReplaceProctor mengganti pengawas satu kartu ruang (semua sesinya)
// dengan userID, juga di penugasan agenda. Pengawas lama kehilangan hak
// layar pengawas sesi itu; yang baru langsung bisa membukanya.
func ReplaceProctor(ctx context.Context, db database.DB, sa StaffActor, eventID string, sessionIDs []string, userID, reason string, now time.Time) (string, error) {
	if !database.IsUUID(userID) {
		return "", ErrBadProctor
	}
	var name string
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ev, err := GetEvent(ctx, tx, sa.SchoolID, eventID)
		if err != nil {
			return err
		}
		ids, err := EventSessionIDs(ctx, tx, sa.SchoolID, ev.ID, sessionIDs)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return ErrNotFound
		}
		teachers, err := rosterTeachers(ctx, tx, ev)
		if err != nil {
			return err
		}
		for _, t := range teachers {
			if t.ID == userID {
				name = t.Name
			}
		}
		if name == "" {
			return ErrBadProctor
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exam_session_proctors WHERE session_id = ANY ($1::uuid[])`, ids); err != nil {
			return fmt.Errorf("exam: replace proctor: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by)
			SELECT $1, id, $3, $4 FROM unnest($2::uuid[]) AS id`, sa.SchoolID, ids, userID, sa.UserID); err != nil {
			return fmt.Errorf("exam: replace proctor: %w", err)
		}
		// Penugasan agenda (ruang × jam × gelombang) ikut diganti agar
		// "Terapkan ke sesi" berikutnya tidak mengembalikan pengawas lama.
		if _, err := tx.Exec(ctx, `
			INSERT INTO exam_event_proctors (school_id, event_id, room_id, slot_id, shift, user_id, assigned_by)
			SELECT DISTINCT $1::uuid, $2::uuid, x.room_id, c.slot_id, x.shift, $4::uuid, $5::uuid
			FROM exam_sessions x JOIN exam_event_cells c ON c.id = x.cell_id
			WHERE x.id = ANY ($3::uuid[]) AND x.room_id IS NOT NULL AND x.shift IS NOT NULL
			ON CONFLICT (event_id, room_id, slot_id, shift) DO UPDATE SET user_id = excluded.user_id, assigned_by = excluded.assigned_by`,
			sa.SchoolID, ev.ID, ids, userID, sa.UserID); err != nil {
			return fmt.Errorf("exam: replace event proctor: %w", err)
		}
		body := "Pengawas diganti oleh koordinator menjadi " + name
		if r := strings.TrimSpace(reason); r != "" {
			body += " (" + r + ")"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_notes (school_id, session_id, body, created_by, created_at)
			SELECT $1, id, $3, $4, $5 FROM unnest($2::uuid[]) AS id`, sa.SchoolID, ids, left500(body+"."), sa.UserID, now); err != nil {
			return fmt.Errorf("exam: replace proctor note: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.proctor_replace", "exam_event", ev.ID, nil,
			map[string]any{"sessions": ids, "user_id": userID, "reason": reason})
	})
	return name, err
}

// MoveTarget adalah sesi tujuan pindah peserta.
type MoveTarget struct {
	SessionID string
	ExamID    string
	Label     string
}

// ControlParticipant adalah peserta untuk pilihan pindah.
type ControlParticipant struct {
	ID        string
	SessionID string
	ExamID    string
	Label     string // nomor · nama · ruang
}

// LoadMoveOptions mengembalikan peserta sesi-sesi yang belum selesai dan
// sesi tujuan yang mungkin (ujian sama, belum berakhir).
func LoadMoveOptions(ctx context.Context, q database.DBTX, schoolID string, sessions []ControlSession, now time.Time) ([]ControlParticipant, []MoveTarget, error) {
	var ids []string
	room := map[string]string{}
	for _, s := range sessions {
		if s.Phase(now) == "selesai" {
			continue
		}
		ids = append(ids, s.ID)
		room[s.ID] = s.Room
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT p.id, p.session_id, x.exam_id, coalesce(nullif(p.participant_no, ''), s.nis, ap.registration_number, ''),
			coalesce(s.full_name, ap.full_name, '')
		FROM exam_session_participants p
		JOIN exam_sessions x ON x.id = p.session_id
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants ap ON ap.id = p.applicant_id
		WHERE p.school_id = $1 AND p.session_id = ANY ($2::uuid[])
		ORDER BY 4, 5`, schoolID, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("exam: move options: %w", err)
	}
	ps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ControlParticipant, error) {
		var p ControlParticipant
		var no, name string
		err := r.Scan(&p.ID, &p.SessionID, &p.ExamID, &no, &name)
		p.Label = strings.TrimSpace(no+" "+name) + " · " + room[p.SessionID]
		return p, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("exam: move options: %w", err)
	}
	var ts []MoveTarget
	for _, s := range sessions {
		if s.Phase(now) == "selesai" {
			continue
		}
		ts = append(ts, MoveTarget{SessionID: s.ID, ExamID: s.ExamID, Label: fmt.Sprintf("%s · %s (%d)", room[s.ID], s.Subject, s.Grade)})
	}
	return ps, ts, nil
}

// ErrNotLocked: soal hanya bisa dibatalkan setelah ujian terkunci
// (dibekukan saat sesi pertama dimulai).
var ErrNotLocked = errors.New("exam: exam not locked")

// VoidInfo adalah keterangan soal yang dibatalkan.
type VoidInfo struct {
	Reason string
	At     time.Time
	By     string // nama
}

// VoidedQuestions mengembalikan soal ujian yang dibatalkan (question_id →
// keterangan).
func VoidedQuestions(ctx context.Context, q database.DBTX, schoolID, examID string) (map[string]VoidInfo, error) {
	rows, err := q.Query(ctx, `SELECT v.question_id::text, v.reason, v.voided_at, coalesce(u.full_name, '')
		FROM exam_voided_questions v LEFT JOIN users u ON u.id = v.voided_by
		WHERE v.school_id = $1 AND v.exam_id = $2`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: voided questions: %w", err)
	}
	defer rows.Close()
	out := map[string]VoidInfo{}
	for rows.Next() {
		var id string
		var v VoidInfo
		if err := rows.Scan(&id, &v.Reason, &v.At, &v.By); err != nil {
			return nil, fmt.Errorf("exam: voided questions: %w", err)
		}
		out[id] = v
	}
	return out, rows.Err()
}

// RunningExamSessions mengembalikan sesi ujian examID yang sedang
// berlangsung (untuk ralat).
func RunningExamSessions(ctx context.Context, q database.DBTX, schoolID, examID string, now time.Time) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT id::text FROM exam_sessions WHERE school_id = $1 AND exam_id = $2 AND cancelled_at IS NULL
		AND closed_at IS NULL AND starts_at <= $3 AND ends_at + make_interval(mins => extra_minutes) > $3 ORDER BY starts_at, id`,
		schoolID, examID, now)
	if err != nil {
		return nil, fmt.Errorf("exam: running exam sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("exam: running exam sessions: %w", err)
	}
	return out, nil
}

// VoidQuestion membatalkan (void=true) atau memulihkan soal questionID di
// ujian e yang sudah terkunci. Soal yang dibatalkan tidak dinilai: poinnya
// dikeluarkan dari total semua peserta (T146). notify: peserta di sesi yang
// sedang berlangsung menerima pesan berisi kutipan soal (nomor soal bisa
// berbeda per peserta karena urutan diacak). Mengembalikan jumlah sesi
// yang dikirimi pesan. Hak (pembuat ujian, koordinator, admin_tu) dicek
// pemanggil.
func VoidQuestion(ctx context.Context, db database.DB, sa StaffActor, e Exam, questionID, reason string, void, notify bool, now time.Time) (int, error) {
	if !e.Locked() {
		return 0, ErrNotLocked
	}
	if !database.IsUUID(questionID) {
		return 0, ErrNotFound
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 300 {
		return 0, ErrBadNote
	}
	sent := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT qv.snapshot FROM exam_item_snapshots s
			JOIN exam_question_versions qv ON qv.question_id = s.question_id AND qv.version = s.question_version
			WHERE s.school_id = $1 AND s.exam_id = $2 AND s.question_id = $3 LIMIT 1`, sa.SchoolID, e.ID, questionID).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("exam: void question: %w", err)
		}
		var qs Question
		if err := json.Unmarshal(raw, &qs); err != nil {
			return fmt.Errorf("exam: decode voided question: %w", err)
		}
		action := "exam.question_void"
		if void {
			_, err = tx.Exec(ctx, `INSERT INTO exam_voided_questions (school_id, exam_id, question_id, reason, voided_by, voided_at)
				VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (exam_id, question_id) DO UPDATE SET reason = excluded.reason`,
				sa.SchoolID, e.ID, questionID, reason, sa.UserID, now)
		} else {
			action = "exam.question_restore"
			_, err = tx.Exec(ctx, `DELETE FROM exam_voided_questions WHERE school_id = $1 AND exam_id = $2 AND question_id = $3`,
				sa.SchoolID, e.ID, questionID)
		}
		if err != nil {
			return fmt.Errorf("exam: void question: %w", err)
		}
		// Nilai attempt yang sudah dikumpulkan ikut berubah (T146).
		if err := regradeAfterVoid(ctx, tx, sa, e, questionID, void, reason, now); err != nil {
			return err
		}
		if void && notify {
			ids, err := RunningExamSessions(ctx, tx, sa.SchoolID, e.ID, now)
			if err != nil {
				return err
			}
			if len(ids) > 0 {
				msg := fmt.Sprintf("Soal \"%s\" dibatalkan dan tidak dinilai.", Snippet(PlainText(qs.BodySrc), 80))
				if reason != "" {
					msg += " " + reason
				}
				if utf8.RuneCountInString(msg) > MaxBroadcastLen {
					msg = string([]rune(msg)[:MaxBroadcastLen])
				}
				if err := sendBroadcastTx(ctx, tx, sa, ids, msg, now); err != nil {
					return err
				}
				sent = len(ids)
			}
		}
		return recordAudit(ctx, tx, sa, action, "exam", e.ID, nil, map[string]any{"question_id": questionID, "reason": reason, "notified": sent})
	})
	return sent, err
}

// RecapRow adalah kehadiran satu mapel × tingkat hari ini.
type RecapRow struct {
	Subject string
	Grade   int
	Total   int
	Present int
	Done    int
}

// Absent adalah peserta yang belum/tidak hadir.
func (r RecapRow) Absent() int { return r.Total - r.Present }

// UnsignedMinutes adalah sesi agenda yang sudah selesai tetapi berita
// acaranya belum ditandatangani.
type UnsignedMinutes struct {
	SessionID string
	Room      string
	Subject   string
	Grade     int
	StartsAt  time.Time
	Proctors  string
	When      string // diisi handler (zona sekolah)
}

// PendingGrading adalah ujian agenda dengan jawaban esai yang menunggu
// koreksi.
type PendingGrading struct {
	ExamID   string
	ExamName string
	Answers  int
}

// DailyRecap adalah rekap harian pusat kendali.
type DailyRecap struct {
	Attendance []RecapRow
	Unsigned   []UnsignedMinutes
	Grading    []PendingGrading
}

// LoadDailyRecap menyusun rekap: kehadiran per mapel/tingkat dari sesi
// hari ini, berita acara yang belum ditandatangani (seluruh agenda), dan
// jawaban esai yang belum dikoreksi per ujian agenda.
func LoadDailyRecap(ctx context.Context, q database.DBTX, schoolID, eventID string, today []ControlSession, now time.Time) (DailyRecap, error) {
	var rc DailyRecap
	idx := map[string]int{}
	for _, s := range today {
		k := fmt.Sprintf("%s|%d", s.Subject, s.Grade)
		i, ok := idx[k]
		if !ok {
			rc.Attendance = append(rc.Attendance, RecapRow{Subject: s.Subject, Grade: s.Grade})
			i = len(rc.Attendance) - 1
			idx[k] = i
		}
		r := &rc.Attendance[i]
		r.Total += s.Total
		r.Present += s.Present
		r.Done += s.Done
	}
	sort.SliceStable(rc.Attendance, func(i, j int) bool {
		a, b := rc.Attendance[i], rc.Attendance[j]
		if a.Grade != b.Grade {
			return a.Grade < b.Grade
		}
		return a.Subject < b.Subject
	})
	rows, err := q.Query(ctx, `
		SELECT x.id, coalesce(r.name, x.label), coalesce(sub.name, ''), coalesce(c.grade_level, 0), x.starts_at,
			coalesce((SELECT string_agg(u.full_name, ', ' ORDER BY u.full_name) FROM exam_session_proctors sp JOIN users u ON u.id = sp.user_id
			          WHERE sp.session_id = x.id), '')
		FROM exam_sessions x
		LEFT JOIN exam_event_cells c ON c.id = x.cell_id
		LEFT JOIN subjects sub ON sub.id = c.subject_id
		LEFT JOIN exam_rooms r ON r.id = x.room_id
		WHERE x.school_id = $1 AND x.event_id = $2 AND x.cancelled_at IS NULL AND x.minutes_signed_at IS NULL
		  AND (x.closed_at IS NOT NULL OR x.ends_at + make_interval(mins => x.extra_minutes) <= $3)
		ORDER BY x.starts_at, 2, 4`, schoolID, eventID, now)
	if err != nil {
		return rc, fmt.Errorf("exam: unsigned minutes: %w", err)
	}
	rc.Unsigned, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (UnsignedMinutes, error) {
		var u UnsignedMinutes
		return u, r.Scan(&u.SessionID, &u.Room, &u.Subject, &u.Grade, &u.StartsAt, &u.Proctors)
	})
	if err != nil {
		return rc, fmt.Errorf("exam: unsigned minutes: %w", err)
	}
	// Belum ada koreksi esai (T147): semua jawaban esai pada attempt yang
	// sudah dikumpulkan dihitung menunggu koreksi.
	rows, err = q.Query(ctx, `
		SELECT e.id, e.name, count(*)
		FROM exams e
		JOIN exam_attempts t ON t.exam_id = e.id AND t.status = 'dikumpulkan'
		JOIN exam_answers a ON a.attempt_id = t.id AND a.value <> 'null'::jsonb
		JOIN exam_item_snapshots s ON s.variant_id = t.variant_id AND s.question_id = a.question_id
		JOIN exam_question_versions qv ON qv.question_id = s.question_id AND qv.version = s.question_version
		WHERE e.school_id = $1 AND e.id IN (SELECT DISTINCT exam_id FROM exam_sessions WHERE event_id = $2 AND cancelled_at IS NULL)
		  AND qv.snapshot->>'Kind' = 'esai'
		GROUP BY e.id, e.name ORDER BY e.name`, schoolID, eventID)
	if err != nil {
		return rc, fmt.Errorf("exam: pending grading: %w", err)
	}
	rc.Grading, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (PendingGrading, error) {
		var g PendingGrading
		var n int64
		err := r.Scan(&g.ExamID, &g.ExamName, &n)
		g.Answers = int(n)
		return g, err
	})
	if err != nil {
		return rc, fmt.Errorf("exam: pending grading: %w", err)
	}
	return rc, nil
}
