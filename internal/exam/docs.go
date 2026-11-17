package exam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/settings"
)

// Dokumen ujian (T135): kartu peserta, daftar hadir, denah, tempelan pintu,
// berita acara, naskah cadangan, dan kunci jawaban.

// CardLead adalah batas paling cepat kartu (dan kodenya) dicetak sebelum
// sesi pertama: H-3, agar kode tidak beredar terlalu lama.
const CardLead = 72 * time.Hour

// TooEarlyError: kartu belum boleh dicetak.
type TooEarlyError struct{ Opens time.Time }

func (e *TooEarlyError) Error() string { return "exam: cards not yet available" }

// ErrCardsIssued: sebagian peserta sudah punya kartu; mencetak ulang
// membuat kode baru dan membatalkan kartu lama, jadi perlu konfirmasi.
var ErrCardsIssued = errors.New("exam: cards already issued")

// ErrNoSessions: tidak ada sesi (atau peserta) di cakupan dokumen.
var ErrNoSessions = errors.New("exam: no sessions in scope")

// Letterhead adalah kop dokumen dari pengaturan rapor sekolah.
type Letterhead struct {
	School      string
	Address     string
	Principal   string
	SignerTitle string
	Code        string // kode sekolah untuk tautan login peserta
	Loc         *time.Location
}

// LoadLetterhead membaca kop sekolah dan zona waktunya.
func LoadLetterhead(ctx context.Context, q database.DBTX, schoolID string) (Letterhead, error) {
	var l Letterhead
	var tz string
	if err := q.QueryRow(ctx, `SELECT name, coalesce(address, ''), code, timezone FROM schools WHERE id = $1`, schoolID).
		Scan(&l.School, &l.Address, &l.Code, &tz); err != nil {
		return l, fmt.Errorf("exam: letterhead: %w", err)
	}
	l.Loc = clock.Location(tz)
	l.Principal, _, _ = settings.Get(ctx, q, schoolID, settings.ReportPrincipalName)
	l.SignerTitle, _, _ = settings.Get(ctx, q, schoolID, settings.ReportSignerTitle)
	if l.SignerTitle == "" {
		l.SignerTitle = "Kepala Sekolah"
	}
	return l, nil
}

// Card adalah satu kartu peserta.
type Card struct {
	Title      string
	Name       string
	Identifier string // NIS atau nomor pendaftaran
	Number     string // nomor peserta
	ClassName  string
	Room       string
	Seat       int
	Lines      []string // jadwal
	Code       string
	LoginURL   string
}

// cardOwner adalah satu peserta beserta sesi-sesinya di cakupan kartu.
type cardOwner struct {
	owner      Owner
	name, id   string
	number     string
	className  string
	room       string
	seat       int
	windows    []CardWindow
	lines      []string
	firstStart time.Time
}

// CardScope adalah cakupan cetak kartu.
type CardScope struct {
	ExamID    string // kartu ulangan/tes: sesi ujian ini
	SessionID string // opsional: satu sesi
	EventID   string // kartu PTS/PAS: semua sesi agenda peserta
	RoomID    string // opsional (agenda): peserta satu ruang
	ClassID   string // opsional (agenda): peserta satu kelas
}

// loadCardOwners mengumpulkan peserta di cakupan beserta sesi dan baris
// jadwalnya.
func loadCardOwners(ctx context.Context, q database.DBTX, schoolID string, sc CardScope, loc *time.Location) ([]*cardOwner, error) {
	where := []string{"x.school_id = $1", "x.cancelled_at IS NULL"}
	args := []any{schoolID}
	add := func(cond, v string) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	switch {
	case database.IsUUID(sc.EventID):
		add("x.event_id = ?", sc.EventID)
		if database.IsUUID(sc.RoomID) {
			add("x.room_id = ?", sc.RoomID)
		}
		if database.IsUUID(sc.ClassID) {
			add("p.student_id IN (SELECT student_id FROM exam_event_participants WHERE class_id = ?)", sc.ClassID)
		}
	case database.IsUUID(sc.ExamID):
		add("x.exam_id = ?", sc.ExamID)
		if database.IsUUID(sc.SessionID) {
			add("x.id = ?", sc.SessionID)
		}
	default:
		return nil, ErrNoSessions
	}
	rows, err := q.Query(ctx, `
		SELECT x.id, x.starts_at, x.ends_at, coalesce(p.student_id::text, ''), coalesce(p.applicant_id::text, ''),
		       coalesce(s.full_name, a.full_name, ''), coalesce(s.nis, a.registration_number, ''), p.participant_no, coalesce(p.seat, 0),
		       coalesce(r.name, ''), coalesce(sub.name, e.name),
		       coalesce((SELECT c.name FROM enrollments en JOIN classes c ON c.id = en.class_id
		                 WHERE en.student_id = p.student_id AND en.end_date IS NULL ORDER BY en.start_date DESC LIMIT 1), '')
		FROM exam_sessions x
		JOIN exams e ON e.id = x.exam_id
		JOIN exam_session_participants p ON p.session_id = x.id
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants a ON a.id = p.applicant_id
		LEFT JOIN exam_rooms r ON r.id = x.room_id
		LEFT JOIN subjects sub ON sub.id = e.subject_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY x.starts_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("exam: card participants: %w", err)
	}
	defer rows.Close()
	byOwner := map[string]*cardOwner{}
	var out []*cardOwner
	for rows.Next() {
		var sid, stu, app, name, ident, no, room, subject, class string
		var seat int16
		var start, end time.Time
		if err := rows.Scan(&sid, &start, &end, &stu, &app, &name, &ident, &no, &seat, &room, &subject, &class); err != nil {
			return nil, fmt.Errorf("exam: card participants: %w", err)
		}
		key := stu + app
		co := byOwner[key]
		if co == nil {
			co = &cardOwner{owner: Owner{StudentID: stu, ApplicantID: app}, name: name, id: ident, number: no, className: class,
				room: room, seat: int(seat), firstStart: start}
			byOwner[key] = co
			out = append(out, co)
		}
		co.windows = append(co.windows, CardWindow{SessionID: sid, Start: start, End: end})
		a, b := start.In(loc), end.In(loc)
		co.lines = append(co.lines, fmt.Sprintf("%s %s %s–%s %s", dayNames[a.Weekday()][:3], a.Format("02/01"), a.Format("15.04"), b.Format("15.04"), subject))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNoSessions
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].room != out[j].room {
			return out[i].room < out[j].room
		}
		if out[i].number != out[j].number {
			return out[i].number < out[j].number
		}
		return out[i].name < out[j].name
	})
	return out, nil
}

// ParticipantLoginURL adalah tautan login peserta dengan NIS/nomor
// pendaftaran terisi (isi QR kartu). Kode ujian tidak pernah masuk QR.
func ParticipantLoginURL(baseURL, schoolCode, identifier string) string {
	return strings.TrimRight(baseURL, "/") + "/u/" + url.PathEscape(schoolCode) + "?nis=" + url.QueryEscape(identifier)
}

// IssueCards membuat kartu peserta untuk cakupan sc. Setiap peserta
// mendapat satu kode baru yang berlaku di semua sesinya dalam cakupan.
// Kartu tersedia paling cepat CardLead sebelum sesi pertama; bila sebagian
// peserta sudah punya kode, reissue harus true (kartu lama tidak berlaku).
func IssueCards(ctx context.Context, db database.DB, sa StaffActor, is *Issuer, sc CardScope, title, baseURL string, reissue bool, now time.Time) ([]Card, error) {
	var out []Card
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		lh, err := LoadLetterhead(ctx, tx, sa.SchoolID)
		if err != nil {
			return err
		}
		owners, err := loadCardOwners(ctx, tx, sa.SchoolID, sc, lh.Loc)
		if err != nil {
			return err
		}
		first := owners[0].firstStart
		var sessions []string
		for _, o := range owners {
			if o.firstStart.Before(first) {
				first = o.firstStart
			}
			for _, w := range o.windows {
				sessions = append(sessions, w.SessionID)
			}
		}
		if first.Sub(now) > CardLead {
			return &TooEarlyError{Opens: first.Add(-CardLead)}
		}
		if !reissue {
			var issued bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM student_credentials WHERE school_id = $1 AND exam_session_id = ANY($2)
				AND revoked_at IS NULL)`, sa.SchoolID, sessions).Scan(&issued); err != nil {
				return fmt.Errorf("exam: check issued: %w", err)
			}
			if issued {
				return ErrCardsIssued
			}
		}
		for _, o := range owners {
			// Sesi yang sudah berakhir tidak perlu kode baru.
			var ws []CardWindow
			for _, w := range o.windows {
				if w.End.After(now) {
					ws = append(ws, w)
				}
			}
			c := Card{Title: title, Name: o.name, Identifier: o.id, Number: o.number, ClassName: o.className, Room: o.room, Seat: o.seat,
				Lines: o.lines, LoginURL: ParticipantLoginURL(baseURL, lh.Code, o.id)}
			if len(ws) > 0 {
				if c.Code, err = is.IssueCard(ctx, tx, sa.SchoolID, sa.UserID, o.owner, ws); err != nil {
					return err
				}
			}
			out = append(out, c)
		}
		return recordAudit(ctx, tx, sa, "exam.cards_print", "exam_scope", firstNonEmpty(sc.EventID, sc.ExamID), nil,
			map[string]any{"cards": len(out), "reissue": reissue, "session": sc.SessionID, "room": sc.RoomID, "class": sc.ClassID})
	})
	return out, err
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// SessionDoc adalah data satu sesi untuk daftar hadir dan berita acara.
type SessionDoc struct {
	Session      Session
	ExamName     string
	Subject      string
	Purpose      Purpose
	Room         string
	When         string
	Day          string
	Participants []ParticipantRow
}

// LoadSessionDocs memuat sesi (urut waktu dan ruang) beserta pesertanya.
// sessionIDs kosong berarti semua sesi di cakupan examID atau eventID
// (opsional roomID).
func LoadSessionDocs(ctx context.Context, q database.DBTX, schoolID, examID, eventID, roomID, sessionID string, loc *time.Location) ([]SessionDoc, error) {
	where := []string{"x.school_id = $1", "x.cancelled_at IS NULL"}
	args := []any{schoolID}
	add := func(cond, v string) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	switch {
	case database.IsUUID(eventID):
		add("x.event_id = ?", eventID)
	case database.IsUUID(examID):
		add("x.exam_id = ?", examID)
	default:
		return nil, ErrNoSessions
	}
	if database.IsUUID(roomID) {
		add("x.room_id = ?", roomID)
	}
	if database.IsUUID(sessionID) {
		add("x.id = ?", sessionID)
	}
	rows, err := q.Query(ctx, `SELECT x.id, e.name, coalesce(sub.name, ''), e.purpose, coalesce(r.name, '')
		FROM exam_sessions x JOIN exams e ON e.id = x.exam_id
		LEFT JOIN subjects sub ON sub.id = e.subject_id LEFT JOIN exam_rooms r ON r.id = x.room_id
		WHERE `+strings.Join(where, " AND ")+` ORDER BY x.starts_at, r.name, x.label`, args...)
	if err != nil {
		return nil, fmt.Errorf("exam: session docs: %w", err)
	}
	type head struct {
		id, exam, subject string
		purpose           Purpose
		room              string
	}
	heads, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (head, error) {
		var h head
		return h, r.Scan(&h.id, &h.exam, &h.subject, &h.purpose, &h.room)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: session docs: %w", err)
	}
	if len(heads) == 0 {
		return nil, ErrNoSessions
	}
	var out []SessionDoc
	for _, h := range heads {
		s, err := GetSession(ctx, q, schoolID, h.id)
		if err != nil {
			return nil, err
		}
		ps, err := ListParticipants(ctx, q, schoolID, h.id)
		if err != nil {
			return nil, err
		}
		a := s.StartsAt.In(loc)
		out = append(out, SessionDoc{Session: s, ExamName: h.exam, Subject: h.subject, Purpose: h.purpose, Room: h.room,
			When: sessionWhen(s, loc), Day: dayNames[a.Weekday()] + ", " + clock.FormatDateID(a), Participants: ps})
	}
	return out, nil
}

// InterleaveByClass mengurutkan peserta agar siswa sekelas tidak duduk
// berdampingan: bergilir antarkelas (urutan di dalam kelas tetap).
func InterleaveByClass[T any](items []T, class func(T) string) []T {
	var order []string
	groups := map[string][]T{}
	for _, it := range items {
		c := class(it)
		if _, ok := groups[c]; !ok {
			order = append(order, c)
		}
		groups[c] = append(groups[c], it)
	}
	out := make([]T, 0, len(items))
	for len(out) < len(items) {
		for _, c := range order {
			if g := groups[c]; len(g) > 0 {
				out = append(out, g[0])
				groups[c] = g[1:]
			}
		}
	}
	return out
}

// ReseatEvent mengatur ulang tempat duduk peserta agenda per ruang+
// gelombang: urut kelas lalu nama, atau berselang antarkelas bila
// interleave. Peserta sesi yang belum mulai ikut diperbarui saat agenda
// diterapkan lagi.
func ReseatEvent(ctx context.Context, db database.DB, sa StaffActor, eventID string, interleave bool) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ev, err := GetEvent(ctx, tx, sa.SchoolID, eventID)
		if err != nil {
			return err
		}
		if !interleave {
			return reseat(ctx, tx, ev.ID)
		}
		ps, err := ListEventParticipants(ctx, tx, ev)
		if err != nil {
			return err
		}
		groups := map[string][]EventParticipant{}
		var keys []string
		for _, p := range ps {
			if p.RoomID == "" {
				continue
			}
			k := fmt.Sprintf("%s|%d", p.RoomID, p.Shift)
			if _, ok := groups[k]; !ok {
				keys = append(keys, k)
			}
			groups[k] = append(groups[k], p)
		}
		for _, k := range keys {
			for i, p := range InterleaveByClass(groups[k], func(p EventParticipant) string { return p.ClassID }) {
				if _, err := tx.Exec(ctx, `UPDATE exam_event_participants SET seat = $3 WHERE event_id = $1 AND student_id = $2`,
					ev.ID, p.StudentID, i+1); err != nil {
					return fmt.Errorf("exam: reseat: %w", err)
				}
			}
		}
		return nil
	})
}

// RoomPlan adalah isi satu ruang+gelombang untuk denah dan tempelan pintu.
type RoomPlan struct {
	Room     Room
	Shift    int
	Seats    []EventParticipant // urut tempat duduk
	Times    []string           // jadwal ruang ini
	FirstNo  string
	LastNo   string
	Capacity int
}

// LoadRoomPlans mengembalikan ruang+gelombang agenda (opsional satu ruang)
// beserta peserta urut tempat duduk dan jadwalnya.
func LoadRoomPlans(ctx context.Context, q database.DBTX, ev Event, roomID string) ([]RoomPlan, error) {
	p, err := LoadEventPlan(ctx, q, ev)
	if err != nil {
		return nil, err
	}
	var out []RoomPlan
	for _, r := range p.Rooms {
		if roomID != "" && r.ID != roomID {
			continue
		}
		for sh := 1; sh <= ev.MaxShifts; sh++ {
			var seats []EventParticipant
			for _, pt := range p.Participants {
				if pt.RoomID == r.ID && pt.Shift == sh {
					seats = append(seats, pt)
				}
			}
			if len(seats) == 0 {
				continue
			}
			sort.SliceStable(seats, func(i, j int) bool { return seats[i].Seat < seats[j].Seat })
			rp := RoomPlan{Room: r, Shift: sh, Seats: seats, Capacity: r.Capacity}
			nos := make([]string, len(seats))
			for i, s := range seats {
				nos[i] = s.Number
			}
			sort.Strings(nos)
			rp.FirstNo, rp.LastNo = nos[0], nos[len(nos)-1]
			for _, u := range p.Units {
				if u.RoomID != r.ID || u.Shift != sh {
					continue
				}
				a, b := u.Start.In(p.loc), u.End.In(p.loc)
				rp.Times = append(rp.Times, fmt.Sprintf("%s %s %s–%s", dayNames[a.Weekday()], a.Format("02/01"), a.Format("15.04"), b.Format("15.04")))
			}
			out = append(out, rp)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoSessions
	}
	return out, nil
}
