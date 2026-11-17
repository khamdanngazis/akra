package exam

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// EventPlan adalah gambaran lengkap agenda untuk pengawas, sesi, dan
// ringkasan kesiapan.
type EventPlan struct {
	Event        Event
	Slots        []Slot
	Rooms        []Room
	Participants []EventParticipant
	Units        []RosterUnit
	Teachers     []RosterTeacher
	Assign       map[string]string
	Issues       []RosterIssue
	loc          *time.Location
}

func unitKeyOf(room, slot string, shift int) string {
	return room + "|" + slot + "|" + strconv.Itoa(shift)
}

// unitTimes menghitung jam satu gelombang slot: gelombang k mulai di jam
// slot + (k-1) × (lama slot + jeda).
func unitTimes(ev Event, s Slot, shift int, loc *time.Location) (time.Time, time.Time) {
	a, _ := time.Parse("15:04", s.Start)
	b, _ := time.Parse("15:04", s.End)
	y, m, d := s.Day.Date()
	start := time.Date(y, m, d, a.Hour(), a.Minute(), 0, 0, loc)
	length := b.Sub(a)
	start = start.Add(time.Duration(shift-1) * (length + time.Duration(ev.ShiftGapMinutes)*time.Minute))
	return start, start.Add(length)
}

// LoadEventPlan memuat jadwal, ruang, peserta, tugas pengawas, dan masalah
// penugasan agenda.
func LoadEventPlan(ctx context.Context, q database.DBTX, ev Event) (EventPlan, error) {
	p := EventPlan{Event: ev, Assign: map[string]string{}}
	loc, err := clock.ForSchool(ctx, q, ev.SchoolID)
	if err != nil {
		return p, err
	}
	p.loc = loc
	if p.Slots, err = LoadSchedule(ctx, q, ev); err != nil {
		return p, err
	}
	if p.Rooms, err = eventRooms(ctx, q, ev); err != nil {
		return p, err
	}
	if p.Participants, err = ListEventParticipants(ctx, q, ev); err != nil {
		return p, err
	}
	// Tingkat per ruang+gelombang.
	grades := map[string]map[int]bool{}
	for _, pt := range p.Participants {
		if pt.RoomID == "" {
			continue
		}
		k := pt.RoomID + "|" + strconv.Itoa(pt.Shift)
		if grades[k] == nil {
			grades[k] = map[int]bool{}
		}
		grades[k][pt.Grade] = true
	}
	for _, s := range p.Slots {
		for _, r := range p.Rooms {
			for shift := 1; shift <= ev.MaxShifts; shift++ {
				gs := grades[r.ID+"|"+strconv.Itoa(shift)]
				var subjects []string
				for g := range gs {
					if c, ok := s.Cells[g]; ok {
						subjects = append(subjects, c.SubjectID)
					}
				}
				if len(subjects) == 0 {
					continue
				}
				sort.Strings(subjects)
				start, end := unitTimes(ev, s, shift, loc)
				p.Units = append(p.Units, RosterUnit{Key: unitKeyOf(r.ID, s.ID, shift), RoomID: r.ID, SlotID: s.ID, Shift: shift,
					Start: start, End: end, Subjects: subjects})
			}
		}
	}
	if p.Teachers, err = rosterTeachers(ctx, q, ev); err != nil {
		return p, err
	}
	rows, err := q.Query(ctx, `SELECT room_id, slot_id, shift, user_id FROM exam_event_proctors WHERE event_id = $1`, ev.ID)
	if err != nil {
		return p, fmt.Errorf("exam: load event proctors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var room, slot, user string
		var shift int16
		if err := rows.Scan(&room, &slot, &shift, &user); err != nil {
			return p, fmt.Errorf("exam: load event proctors: %w", err)
		}
		p.Assign[unitKeyOf(room, slot, int(shift))] = user
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	p.Issues = CheckRoster(p.Units, p.Teachers, p.Assign)
	return p, nil
}

// rosterTeachers mengembalikan guru aktif beserta mapel yang diampu dan
// tugas mengawasi di luar agenda ini.
func rosterTeachers(ctx context.Context, q database.DBTX, ev Event) ([]RosterTeacher, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id, u.full_name,
		       coalesce((SELECT array_agg(DISTINCT ta.subject_id::text) FROM teaching_assignments ta
		                 JOIN academic_years y ON y.id = ta.academic_year_id AND y.is_active
		                 WHERE ta.teacher_id = u.id AND ta.school_id = u.school_id), '{}')
		FROM users u
		WHERE u.school_id = $1 AND u.is_active AND EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id AND r.code = 'guru')
		ORDER BY u.full_name`, ev.SchoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: roster teachers: %w", err)
	}
	ts, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RosterTeacher, error) {
		var t RosterTeacher
		return t, r.Scan(&t.ID, &t.Name, &t.Subjects)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: roster teachers: %w", err)
	}
	idx := map[string]int{}
	for i, t := range ts {
		idx[t.ID] = i
	}
	// Tugas mengawasi lain (ulangan kelas, agenda lain) di rentang agenda.
	from := ev.StartsOn.Add(-24 * time.Hour)
	to := ev.EndsOn.Add(48 * time.Hour)
	busy, err := q.Query(ctx, `SELECT sp.user_id, x.starts_at, x.ends_at, e.name FROM exam_session_proctors sp
		JOIN exam_sessions x ON x.id = sp.session_id AND x.cancelled_at IS NULL
		JOIN exams e ON e.id = x.exam_id
		WHERE sp.school_id = $1 AND x.event_id IS DISTINCT FROM $2 AND x.starts_at < $4 AND x.ends_at > $3`, ev.SchoolID, ev.ID, from, to)
	if err != nil {
		return nil, fmt.Errorf("exam: roster busy: %w", err)
	}
	defer busy.Close()
	for busy.Next() {
		var uid, label string
		var s, e time.Time
		if err := busy.Scan(&uid, &s, &e, &label); err != nil {
			return nil, fmt.Errorf("exam: roster busy: %w", err)
		}
		if i, ok := idx[uid]; ok {
			ts[i].Busy = append(ts[i].Busy, Interval{Start: s, End: e, Label: label})
		}
	}
	return ts, busy.Err()
}

// AutoAssignProctors mengisi pengawas yang kosong secara bergilir.
// keep=false mengganti semua penugasan lama.
func AutoAssignProctors(ctx context.Context, db database.DB, sa StaffActor, eventID string, keep bool) (Roster, error) {
	if err := requireAgendaWrite(sa); err != nil {
		return Roster{}, err
	}
	var out Roster
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ev, err := GetEvent(ctx, tx, sa.SchoolID, eventID)
		if err != nil {
			return err
		}
		p, err := LoadEventPlan(ctx, tx, ev)
		if err != nil {
			return err
		}
		fixed := map[string]string{}
		if keep {
			fixed = p.Assign
		}
		out = BuildRoster(p.Units, p.Teachers, fixed)
		if _, err := tx.Exec(ctx, `DELETE FROM exam_event_proctors WHERE event_id = $1`, ev.ID); err != nil {
			return fmt.Errorf("exam: clear roster: %w", err)
		}
		for _, u := range p.Units {
			tid := out.Assign[u.Key]
			if tid == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO exam_event_proctors (school_id, event_id, room_id, slot_id, shift, user_id, assigned_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`, sa.SchoolID, ev.ID, u.RoomID, u.SlotID, u.Shift, tid, sa.UserID); err != nil {
				return fmt.Errorf("exam: save roster: %w", err)
			}
		}
		return recordAudit(ctx, tx, sa, "exam_event.roster", "exam_event", ev.ID, nil, map[string]any{"unfilled": len(out.Unfilled)})
	})
	return out, err
}

// SetProctor menetapkan atau menukar pengawas satu tugas (userID kosong
// mengosongkan).
func SetProctor(ctx context.Context, db database.DB, sa StaffActor, eventID, unitKey, userID string) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	parts := strings.Split(unitKey, "|")
	shift := 0
	if len(parts) == 3 {
		shift, _ = strconv.Atoi(parts[2])
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ev, err := GetEvent(ctx, tx, sa.SchoolID, eventID)
		if err != nil {
			return err
		}
		p, err := LoadEventPlan(ctx, tx, ev)
		if err != nil {
			return err
		}
		found := false
		for _, u := range p.Units {
			found = found || u.Key == unitKey
		}
		if !found {
			return &FieldError{Fields: map[string]string{"unit": "Tugas mengawasi tidak ditemukan. Bagi peserta ke ruang dan isi jadwal dulu."}}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exam_event_proctors WHERE event_id = $1 AND room_id = $2 AND slot_id = $3 AND shift = $4`,
			ev.ID, parts[0], parts[1], shift); err != nil {
			return fmt.Errorf("exam: set proctor: %w", err)
		}
		if userID == "" {
			return nil
		}
		ok := false
		for _, t := range p.Teachers {
			ok = ok || t.ID == userID
		}
		if !ok {
			return &FieldError{Fields: map[string]string{"user_id": "Pilih guru aktif."}}
		}
		_, err = tx.Exec(ctx, `INSERT INTO exam_event_proctors (school_id, event_id, room_id, slot_id, shift, user_id, assigned_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, sa.SchoolID, ev.ID, parts[0], parts[1], shift, userID, sa.UserID)
		if err != nil {
			return fmt.Errorf("exam: set proctor: %w", err)
		}
		return nil
	})
}

// ApplyResult adalah hasil penerapan agenda ke sesi ujian.
type ApplyResult struct {
	Created, Updated, Cancelled, Skipped int
}

// ApplyEventSessions membuat atau memperbarui sesi ujian dari agenda: satu
// sesi per sel (yang punya ujian) × ruang × gelombang, dengan peserta,
// nomor peserta, tempat duduk, dan pengawas dari agenda. Sesi yang sudah
// mulai tidak diubah; sesi agenda yang tidak lagi dibutuhkan dan belum mulai
// dibatalkan.
func ApplyEventSessions(ctx context.Context, db database.DB, sa StaffActor, eventID string, now time.Time) (ApplyResult, error) {
	if err := requireAgendaWrite(sa); err != nil {
		return ApplyResult{}, err
	}
	var res ApplyResult
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ev, err := GetEvent(ctx, tx, sa.SchoolID, eventID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM exam_events WHERE id = $1 FOR UPDATE`, ev.ID); err != nil {
			return fmt.Errorf("exam: lock event: %w", err)
		}
		p, err := LoadEventPlan(ctx, tx, ev)
		if err != nil {
			return err
		}
		roomName := map[string]string{}
		for _, r := range p.Rooms {
			roomName[r.ID] = r.Name
		}
		keep := map[string]bool{}
		exams := map[string]bool{}
		for _, u := range p.Units {
			var slot Slot
			for _, s := range p.Slots {
				if s.ID == u.SlotID {
					slot = s
				}
			}
			for g, c := range slot.Cells {
				if c.ExamID == "" {
					continue
				}
				var count int
				for _, pt := range p.Participants {
					if pt.Grade == g && pt.RoomID == u.RoomID && pt.Shift == u.Shift {
						count++
					}
				}
				if count == 0 {
					continue
				}
				label := roomName[u.RoomID]
				if u.Shift > 1 {
					label += " · Gelombang " + strconv.Itoa(u.Shift)
				}
				var sid string
				var started bool
				err := tx.QueryRow(ctx, `SELECT id, starts_at <= $5 FROM exam_sessions WHERE event_id = $1 AND cell_id = $2 AND room_id = $3 AND shift = $4
					AND cancelled_at IS NULL`, ev.ID, c.ID, u.RoomID, u.Shift, now).Scan(&sid, &started)
				switch {
				case errors.Is(err, pgx.ErrNoRows):
					err = tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, event_id, cell_id, room_id, shift, label, location,
						starts_at, ends_at, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, 'di_ruang', $8, $9, $10) RETURNING id`,
						sa.SchoolID, c.ExamID, ev.ID, c.ID, u.RoomID, u.Shift, label, u.Start, u.End, sa.UserID).Scan(&sid)
					if err != nil {
						return fmt.Errorf("exam: create event session: %w", err)
					}
					res.Created++
				case err != nil:
					return fmt.Errorf("exam: find event session: %w", err)
				case started:
					keep[sid] = true
					res.Skipped++
					continue
				default:
					if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET exam_id = $2, label = $3, starts_at = $4, ends_at = $5 WHERE id = $1`,
						sid, c.ExamID, label, u.Start, u.End); err != nil {
						return fmt.Errorf("exam: update event session: %w", err)
					}
					res.Updated++
				}
				keep[sid] = true
				exams[c.ExamID] = true
				if _, err := tx.Exec(ctx, `DELETE FROM exam_session_participants WHERE session_id = $1 AND NOT student_id IN (
					SELECT student_id FROM exam_event_participants WHERE event_id = $2 AND grade_level = $3 AND room_id = $4 AND shift = $5)`,
					sid, ev.ID, g, u.RoomID, u.Shift); err != nil {
					return fmt.Errorf("exam: sync participants: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no, seat)
					SELECT $1, $2, student_id, participant_no, seat FROM exam_event_participants
					WHERE event_id = $3 AND grade_level = $4 AND room_id = $5 AND shift = $6
					ON CONFLICT (session_id, student_id) WHERE student_id IS NOT NULL
					DO UPDATE SET participant_no = EXCLUDED.participant_no, seat = EXCLUDED.seat`,
					sa.SchoolID, sid, ev.ID, g, u.RoomID, u.Shift); err != nil {
					return fmt.Errorf("exam: sync participants: %w", err)
				}
				if _, err := tx.Exec(ctx, `DELETE FROM exam_session_proctors WHERE session_id = $1`, sid); err != nil {
					return fmt.Errorf("exam: sync proctors: %w", err)
				}
				if tid := p.Assign[u.Key]; tid != "" {
					if _, err := tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by) VALUES ($1, $2, $3, $4)`,
						sa.SchoolID, sid, tid, sa.UserID); err != nil {
						return fmt.Errorf("exam: sync proctors: %w", err)
					}
				}
			}
		}
		rows, err := tx.Query(ctx, `SELECT id FROM exam_sessions WHERE event_id = $1 AND cancelled_at IS NULL AND starts_at > $2`, ev.ID, now)
		if err != nil {
			return fmt.Errorf("exam: stale sessions: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("exam: stale sessions: %w", err)
		}
		for _, id := range ids {
			if keep[id] {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET cancelled_at = now() WHERE id = $1`, id); err != nil {
				return fmt.Errorf("exam: cancel stale session: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE student_credentials SET revoked_at = now() WHERE exam_session_id = $1 AND revoked_at IS NULL`, id); err != nil {
				return fmt.Errorf("exam: revoke stale codes: %w", err)
			}
			res.Cancelled++
		}
		for id := range exams {
			if err := assignParticipantVariants(ctx, tx, id); err != nil {
				return err
			}
		}
		return recordAudit(ctx, tx, sa, "exam_event.apply", "exam_event", ev.ID, nil,
			map[string]any{"created": res.Created, "updated": res.Updated, "cancelled": res.Cancelled})
	})
	return res, err
}

// DaySummary adalah kesiapan satu hari agenda.
type DaySummary struct {
	Label        string
	Cells, Ready int
}

// EventSummary adalah ringkasan kesiapan agenda (bisa dibuka kepala
// sekolah).
type EventSummary struct {
	Days           []DaySummary
	Cells          int
	CellsReady     int
	CellsNoExam    int
	Participants   int
	Unplaced       int
	Units          int
	UnitsNoProctor int
	OwnSubject     int
	Clashes        int
	OverCapacity   []string
	// RoomClashes: satu ruang dipakai dua gelombang/jam yang bertumpuk,
	// biasanya gelombang lab yang melewati jam berikutnya.
	RoomClashes      []string
	SessionsApplied  int
	SessionsExpected int
}

// Ready melaporkan apakah agenda tidak punya masalah merah.
func (s EventSummary) Ready() bool {
	return s.Cells > 0 && s.CellsReady == s.Cells && s.Unplaced == 0 && s.UnitsNoProctor == 0 && s.OwnSubject == 0 && s.Clashes == 0 &&
		len(s.OverCapacity) == 0 && len(s.RoomClashes) == 0 && s.SessionsApplied == s.SessionsExpected
}

// Summarize menyusun ringkasan kesiapan agenda dari rencana.
func Summarize(ctx context.Context, q database.DBTX, p EventPlan) (EventSummary, error) {
	var s EventSummary
	days := map[string]int{}
	for _, sl := range p.Slots {
		k := sl.Day.Format("2006-01-02")
		i, ok := days[k]
		if !ok {
			i = len(s.Days)
			days[k] = i
			s.Days = append(s.Days, DaySummary{Label: sl.DayLabel()})
		}
		for _, c := range sl.Cells {
			s.Days[i].Cells++
			s.Cells++
			switch c.Status {
			case CellReady:
				s.Days[i].Ready++
				s.CellsReady++
			case CellNoExam:
				s.CellsNoExam++
			}
		}
	}
	occ := map[string]int{}
	grades := map[string]map[int]bool{}
	for _, pt := range p.Participants {
		s.Participants++
		if pt.RoomID == "" {
			s.Unplaced++
			continue
		}
		k := pt.RoomID + "|" + strconv.Itoa(pt.Shift)
		occ[k]++
		if grades[k] == nil {
			grades[k] = map[int]bool{}
		}
		grades[k][pt.Grade] = true
	}
	for _, r := range p.Rooms {
		for sh := 1; sh <= p.Event.MaxShifts; sh++ {
			if n := occ[r.ID+"|"+strconv.Itoa(sh)]; n > r.Capacity {
				s.OverCapacity = append(s.OverCapacity, fmt.Sprintf("%s gelombang %d (%d/%d)", r.Name, sh, n, r.Capacity))
			}
		}
	}
	names := map[string]string{}
	for _, r := range p.Rooms {
		names[r.ID] = r.Name
	}
	s.RoomClashes = roomClashes(p.Units, names, p.loc)
	s.Units = len(p.Units)
	for _, i := range p.Issues {
		switch i.Kind {
		case IssueUnfilled:
			s.UnitsNoProctor++
		case IssueOwnSubject:
			s.OwnSubject++
		case IssueClash:
			s.Clashes++
		}
	}
	for _, u := range p.Units {
		for _, sl := range p.Slots {
			if sl.ID != u.SlotID {
				continue
			}
			for g, c := range sl.Cells {
				if c.ExamID != "" && grades[u.RoomID+"|"+strconv.Itoa(u.Shift)][g] {
					s.SessionsExpected++
				}
			}
		}
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM exam_sessions WHERE event_id = $1 AND cancelled_at IS NULL`, p.Event.ID).Scan(&s.SessionsApplied); err != nil {
		return s, fmt.Errorf("exam: count event sessions: %w", err)
	}
	return s, nil
}

// StudentScheduleItem adalah satu baris jadwal ujian siswa.
type StudentScheduleItem struct {
	Event   string
	Subject string
	Start   time.Time
	End     time.Time
	Room    string
	Number  string
	Seat    int
}

// StudentSchedule mengembalikan jadwal PTS/PAS yang sudah diumumkan untuk
// siswa, dari hari ini (zona sekolah) ke depan.
func StudentSchedule(ctx context.Context, q database.DBTX, schoolID, studentID string, now time.Time) ([]StudentScheduleItem, error) {
	loc, err := clock.ForSchool(ctx, q, schoolID)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT ev.id, ev.name, ev.shift_gap_minutes, sub.name, sl.day, to_char(sl.start_time, 'HH24:MI'), to_char(sl.end_time, 'HH24:MI'),
		       coalesce(r.name, ''), coalesce(p.shift, 1), p.participant_no, coalesce(p.seat, 0)
		FROM exam_event_participants p
		JOIN exam_events ev ON ev.id = p.event_id AND ev.announced_at IS NOT NULL AND ev.archived_at IS NULL
		JOIN exam_event_cells c ON c.event_id = ev.id AND c.grade_level = p.grade_level
		JOIN exam_event_slots sl ON sl.id = c.slot_id
		JOIN subjects sub ON sub.id = c.subject_id
		LEFT JOIN exam_rooms r ON r.id = p.room_id
		WHERE p.school_id = $1 AND p.student_id = $2 AND sl.day >= $3
		ORDER BY sl.day, sl.start_time`, schoolID, studentID, clock.TodayIn(loc))
	if err != nil {
		return nil, fmt.Errorf("exam: student schedule: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StudentScheduleItem, error) {
		var it StudentScheduleItem
		var evID string
		var gap, shift int16
		var day time.Time
		var a, b string
		err := r.Scan(&evID, &it.Event, &gap, &it.Subject, &day, &a, &b, &it.Room, &shift, &it.Number, &it.Seat)
		it.Start, it.End = unitTimes(Event{ShiftGapMinutes: int(gap)}, Slot{Day: day, Start: a, End: b}, int(shift), loc)
		return it, err
	})
}

// roomClashes melaporkan tugas di ruang yang sama dengan waktu bertumpuk.
func roomClashes(units []RosterUnit, names map[string]string, loc *time.Location) []string {
	if loc == nil {
		loc = time.UTC
	}
	byRoom := map[string][]RosterUnit{}
	for _, u := range units {
		byRoom[u.RoomID] = append(byRoom[u.RoomID], u)
	}
	var out []string
	for room, us := range byRoom {
		sort.Slice(us, func(i, j int) bool { return us[i].Start.Before(us[j].Start) })
		for i := 1; i < len(us); i++ {
			a, b := us[i-1], us[i]
			if b.Start.Before(a.End) {
				f := func(u RosterUnit) string {
					return u.Start.In(loc).Format("02/01 15.04") + "–" + u.End.In(loc).Format("15.04")
				}
				out = append(out, fmt.Sprintf("%s: %s bertumpuk dengan %s", names[room], f(a), f(b)))
			}
		}
	}
	sort.Strings(out)
	return out
}
