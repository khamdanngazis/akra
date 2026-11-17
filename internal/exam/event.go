package exam

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// Agenda ujian sekolah PTS/PAS (T134).

// requireAgendaWrite memeriksa hak tulis agenda (admin_tu, koordinator).
func requireAgendaWrite(sa StaffActor) error {
	if !sa.CanAny(ActAgenda, Write) {
		return ErrForbidden
	}
	return nil
}

// Room adalah ruang ujian.
type Room struct {
	ID         string
	Name       string
	Kind       string
	Capacity   int
	ArchivedAt *time.Time
}

// KindLabel mengembalikan nama jenis ruang.
func (r Room) KindLabel() string {
	if r.Kind == RoomLab {
		return "Lab komputer"
	}
	return "Ruang kelas (HP)"
}

// ListRooms mengembalikan ruang aktif sekolah, urut nama.
func ListRooms(ctx context.Context, q database.DBTX, schoolID string) ([]Room, error) {
	rows, err := q.Query(ctx, `SELECT id, name, kind, capacity, archived_at FROM exam_rooms WHERE school_id = $1 AND archived_at IS NULL
		ORDER BY kind, lower(name)`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: list rooms: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Room, error) {
		var x Room
		var c int16
		err := r.Scan(&x.ID, &x.Name, &x.Kind, &c, &x.ArchivedAt)
		x.Capacity = int(c)
		return x, err
	})
}

// RoomForm adalah isian ruang.
type RoomForm struct{ Name, Kind, Capacity string }

// SaveRoom membuat (id kosong) atau mengubah ruang.
func SaveRoom(ctx context.Context, db database.DB, sa StaffActor, id string, f RoomForm) (string, error) {
	if err := requireAgendaWrite(sa); err != nil {
		return "", err
	}
	errs := map[string]string{}
	name := strings.TrimSpace(f.Name)
	if name == "" || utf8.RuneCountInString(name) > 60 {
		errs["name"] = "Nama ruang 1–60 karakter."
	}
	if f.Kind != RoomLab && f.Kind != RoomClass {
		errs["kind"] = "Pilih jenis ruang."
	}
	c, err := strconv.Atoi(strings.TrimSpace(f.Capacity))
	if err != nil || c < 1 || c > 500 {
		errs["capacity"] = "Kapasitas 1–500."
	}
	if len(errs) > 0 {
		return "", &FieldError{Fields: errs}
	}
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var dup bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_rooms WHERE school_id = $1 AND lower(name) = lower($2) AND archived_at IS NULL
			AND id::text <> $3)`, sa.SchoolID, name, id).Scan(&dup); err != nil {
			return fmt.Errorf("exam: check room: %w", err)
		}
		if dup {
			return &FieldError{Fields: map[string]string{"name": "Nama ruang sudah dipakai."}}
		}
		if id == "" {
			return tx.QueryRow(ctx, `INSERT INTO exam_rooms (school_id, name, kind, capacity) VALUES ($1, $2, $3, $4) RETURNING id`,
				sa.SchoolID, name, f.Kind, c).Scan(&id)
		}
		if !database.IsUUID(id) {
			return ErrNotFound
		}
		tag, err := tx.Exec(ctx, `UPDATE exam_rooms SET name = $3, kind = $4, capacity = $5 WHERE school_id = $1 AND id = $2 AND archived_at IS NULL`,
			sa.SchoolID, id, name, f.Kind, c)
		if err != nil {
			return fmt.Errorf("exam: update room: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	return id, err
}

// ArchiveRoom mengarsipkan ruang. Agenda lama tetap merujuknya.
func ArchiveRoom(ctx context.Context, db database.DB, sa StaffActor, id string) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	if !database.IsUUID(id) {
		return ErrNotFound
	}
	tag, err := db.Exec(ctx, `UPDATE exam_rooms SET archived_at = now() WHERE school_id = $1 AND id = $2 AND archived_at IS NULL`, sa.SchoolID, id)
	if err != nil {
		return fmt.Errorf("exam: archive room: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Event adalah satu agenda ujian sekolah.
type Event struct {
	ID              string
	SchoolID        string
	Name            string
	Purpose         Purpose
	StartsOn        time.Time
	EndsOn          time.Time
	Grades          []int
	NumberFormat    string
	ShiftGapMinutes int
	MaxShifts       int
	AnnouncedAt     *time.Time
	CreatedAt       time.Time
	ArchivedAt      *time.Time
}

// GradesLabel mengembalikan daftar tingkat, mis. "7, 8, 9".
func (e Event) GradesLabel() string {
	s := make([]string, len(e.Grades))
	for i, g := range e.Grades {
		s[i] = strconv.Itoa(g)
	}
	return strings.Join(s, ", ")
}

// DatesLabel mengembalikan rentang tanggal.
func (e Event) DatesLabel() string {
	if e.StartsOn.Equal(e.EndsOn) {
		return clock.FormatDateID(e.StartsOn)
	}
	return clock.FormatDateID(e.StartsOn) + " – " + clock.FormatDateID(e.EndsOn)
}

const eventCols = `id, school_id, name, purpose, starts_on, ends_on, grades, number_format, shift_gap_minutes, max_shifts, announced_at, created_at, archived_at`

func scanEvent(r pgx.Row) (Event, error) {
	var e Event
	var grades []int16
	var gap, shifts int16
	err := r.Scan(&e.ID, &e.SchoolID, &e.Name, &e.Purpose, &e.StartsOn, &e.EndsOn, &grades, &e.NumberFormat, &gap, &shifts, &e.AnnouncedAt,
		&e.CreatedAt, &e.ArchivedAt)
	for _, g := range grades {
		e.Grades = append(e.Grades, int(g))
	}
	e.ShiftGapMinutes, e.MaxShifts = int(gap), int(shifts)
	return e, err
}

// GetEvent mengembalikan agenda milik sekolah.
func GetEvent(ctx context.Context, q database.DBTX, schoolID, id string) (Event, error) {
	if !database.IsUUID(id) {
		return Event{}, ErrNotFound
	}
	e, err := scanEvent(q.QueryRow(ctx, `SELECT `+eventCols+` FROM exam_events WHERE school_id = $1 AND id = $2`, schoolID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("exam: get event: %w", err)
	}
	return e, nil
}

// ListEvents mengembalikan agenda aktif, terbaru dulu.
func ListEvents(ctx context.Context, q database.DBTX, schoolID string) ([]Event, error) {
	rows, err := q.Query(ctx, `SELECT `+eventCols+` FROM exam_events WHERE school_id = $1 AND archived_at IS NULL ORDER BY starts_on DESC LIMIT 100`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: list events: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) { return scanEvent(r) })
}

// EventForm adalah isian agenda.
type EventForm struct {
	Name, Purpose, StartsOn, EndsOn string
	Grades                          []string
	NumberFormat                    string
	ShiftGap, MaxShifts             string
}

// EventFormOf mengubah agenda menjadi isian.
func EventFormOf(e Event) EventForm {
	f := EventForm{Name: e.Name, Purpose: string(e.Purpose), StartsOn: e.StartsOn.Format("2006-01-02"), EndsOn: e.EndsOn.Format("2006-01-02"),
		NumberFormat: e.NumberFormat, ShiftGap: strconv.Itoa(e.ShiftGapMinutes), MaxShifts: strconv.Itoa(e.MaxShifts)}
	for _, g := range e.Grades {
		f.Grades = append(f.Grades, strconv.Itoa(g))
	}
	return f
}

func validateEvent(f EventForm) (Event, error) {
	errs := map[string]string{}
	e := Event{Name: strings.TrimSpace(f.Name), Purpose: Purpose(f.Purpose), NumberFormat: strings.TrimSpace(f.NumberFormat)}
	if e.Name == "" || utf8.RuneCountInString(e.Name) > 120 {
		errs["name"] = "Nama agenda 1–120 karakter."
	}
	if !e.Purpose.SchoolWide() {
		errs["purpose"] = "Pilih PTS atau PAS."
	}
	var err1, err2 error
	e.StartsOn, err1 = time.Parse("2006-01-02", f.StartsOn)
	e.EndsOn, err2 = time.Parse("2006-01-02", f.EndsOn)
	switch {
	case err1 != nil || err2 != nil:
		errs["dates"] = "Isi tanggal mulai dan selesai."
	case e.EndsOn.Before(e.StartsOn):
		errs["dates"] = "Tanggal selesai harus setelah tanggal mulai."
	case e.EndsOn.Sub(e.StartsOn) > 31*24*time.Hour:
		errs["dates"] = "Agenda maksimal 31 hari."
	}
	for _, g := range f.Grades {
		n, ok := parseGrade(g)
		if ok && !slices.Contains(e.Grades, n) {
			e.Grades = append(e.Grades, n)
		}
	}
	sort.Ints(e.Grades)
	if len(e.Grades) == 0 {
		errs["grades"] = "Pilih minimal satu tingkat."
	}
	if e.NumberFormat == "" {
		e.NumberFormat = "{tahun}-{tingkat}-{urut:3}"
	}
	if !ValidNumberFormat(e.NumberFormat) {
		errs["number_format"] = "Format harus memuat {urut}, mis. {tahun}-{tingkat}-{urut:3}."
	}
	gap, err := strconv.Atoi(strings.TrimSpace(f.ShiftGap))
	if f.ShiftGap == "" {
		gap, err = 15, nil
	}
	if err != nil || gap < 0 || gap > 240 {
		errs["shift_gap"] = "Jeda 0–240 menit."
	}
	e.ShiftGapMinutes = gap
	shifts, err := strconv.Atoi(strings.TrimSpace(f.MaxShifts))
	if f.MaxShifts == "" {
		shifts, err = 3, nil
	}
	if err != nil || shifts < 1 || shifts > 6 {
		errs["max_shifts"] = "Gelombang lab 1–6."
	}
	e.MaxShifts = shifts
	if len(errs) > 0 {
		return e, &FieldError{Fields: errs}
	}
	return e, nil
}

func int16s(xs []int) []int16 {
	out := make([]int16, len(xs))
	for i, x := range xs {
		out[i] = int16(x)
	}
	return out
}

// CreateEvent membuat agenda baru. Semua ruang aktif sekolah langsung
// dipilih untuk agenda (bisa diubah).
func CreateEvent(ctx context.Context, db database.DB, sa StaffActor, f EventForm) (Event, error) {
	if err := requireAgendaWrite(sa); err != nil {
		return Event{}, err
	}
	e, err := validateEvent(f)
	if err != nil {
		return Event{}, err
	}
	var out Event
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO exam_events (school_id, academic_year_id, name, purpose, starts_on, ends_on, grades, number_format,
			shift_gap_minutes, max_shifts, created_by)
			VALUES ($1, (SELECT id FROM academic_years WHERE school_id = $1 AND is_active LIMIT 1), $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			sa.SchoolID, e.Name, e.Purpose, e.StartsOn, e.EndsOn, int16s(e.Grades), e.NumberFormat, e.ShiftGapMinutes, e.MaxShifts, sa.UserID).Scan(&id)
		if err != nil {
			return fmt.Errorf("exam: create event: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_event_rooms (school_id, event_id, room_id)
			SELECT $1, $2, id FROM exam_rooms WHERE school_id = $1 AND archived_at IS NULL`, sa.SchoolID, id); err != nil {
			return fmt.Errorf("exam: event rooms: %w", err)
		}
		if out, err = GetEvent(ctx, tx, sa.SchoolID, id); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam_event.create", "exam_event", id, nil, map[string]any{"name": e.Name})
	})
	return out, err
}

// UpdateEvent mengubah dasar agenda. Tujuan tidak berubah.
func UpdateEvent(ctx context.Context, db database.DB, sa StaffActor, id string, f EventForm) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	old, err := GetEvent(ctx, db, sa.SchoolID, id)
	if err != nil {
		return err
	}
	f.Purpose = string(old.Purpose)
	e, err := validateEvent(f)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `UPDATE exam_events SET name = $3, starts_on = $4, ends_on = $5, grades = $6, number_format = $7,
		shift_gap_minutes = $8, max_shifts = $9, updated_at = now() WHERE school_id = $1 AND id = $2`,
		sa.SchoolID, id, e.Name, e.StartsOn, e.EndsOn, int16s(e.Grades), e.NumberFormat, e.ShiftGapMinutes, e.MaxShifts)
	if err != nil {
		return fmt.Errorf("exam: update event: %w", err)
	}
	return nil
}

// EventRoomIDs mengembalikan ruang yang dipakai agenda.
func EventRoomIDs(ctx context.Context, q database.DBTX, eventID string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT room_id::text FROM exam_event_rooms WHERE event_id = $1`, eventID)
	if err != nil {
		return nil, fmt.Errorf("exam: event rooms: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// SetEventRooms mengatur ruang yang dipakai agenda.
func SetEventRooms(ctx context.Context, db database.DB, sa StaffActor, eventID string, roomIDs []string) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := GetEvent(ctx, tx, sa.SchoolID, eventID); err != nil {
			return err
		}
		var ids []string
		for _, id := range roomIDs {
			if database.IsUUID(id) {
				ids = append(ids, id)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exam_event_rooms WHERE event_id = $1 AND NOT (room_id = ANY($2))`, eventID, ids); err != nil {
			return fmt.Errorf("exam: event rooms: %w", err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO exam_event_rooms (school_id, event_id, room_id)
			SELECT $1, $2, id FROM exam_rooms WHERE school_id = $1 AND id = ANY($3) AND archived_at IS NULL ON CONFLICT DO NOTHING`,
			sa.SchoolID, eventID, ids)
		if err != nil {
			return fmt.Errorf("exam: event rooms: %w", err)
		}
		// Peserta di ruang yang dilepas kehilangan tempat.
		_, err = tx.Exec(ctx, `UPDATE exam_event_participants SET room_id = NULL, shift = NULL, seat = NULL
			WHERE event_id = $1 AND room_id IS NOT NULL AND NOT (room_id = ANY($2))`, eventID, ids)
		if err != nil {
			return fmt.Errorf("exam: release participants: %w", err)
		}
		return nil
	})
}

// Slot adalah satu baris jadwal (hari dan jam) beserta selnya.
type Slot struct {
	ID         string
	Day        time.Time
	Start, End string // HH:MM waktu sekolah
	Cells      map[int]Cell
}

// Minutes mengembalikan lama slot.
func (s Slot) Minutes() int {
	a, _ := time.Parse("15:04", s.Start)
	b, _ := time.Parse("15:04", s.End)
	return int(b.Sub(a).Minutes())
}

// DayLabel mengembalikan nama hari dan tanggal.
func (s Slot) DayLabel() string {
	return dayNames[s.Day.Weekday()] + ", " + clock.FormatDateID(s.Day)
}

var dayNames = [...]string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}

// Status sel jadwal.
const (
	CellNoExam   = "belum_ada"
	CellDrafting = "disusun"
	CellInReview = "ditelaah"
	CellReady    = "siap"
)

var cellStatusLabels = map[string]string{
	CellNoExam: "Soal belum ada", CellDrafting: "Disusun", CellInReview: "Ditelaah", CellReady: "Siap",
}

// Cell adalah satu sel jadwal: mapel untuk satu tingkat.
type Cell struct {
	ID          string
	SlotID      string
	Grade       int
	SubjectID   string
	SubjectName string
	ExamID      string
	ExamName    string
	OwnerName   string
	Status      string
	Questions   int
}

// StatusLabel mengembalikan nama status sel.
func (c Cell) StatusLabel() string { return cellStatusLabels[c.Status] }

// LoadSchedule mengembalikan slot agenda beserta selnya, urut waktu.
func LoadSchedule(ctx context.Context, q database.DBTX, ev Event) ([]Slot, error) {
	rows, err := q.Query(ctx, `SELECT id, day, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI') FROM exam_event_slots
		WHERE school_id = $1 AND event_id = $2 ORDER BY day, start_time`, ev.SchoolID, ev.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: load slots: %w", err)
	}
	slots, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Slot, error) {
		s := Slot{Cells: map[int]Cell{}}
		return s, r.Scan(&s.ID, &s.Day, &s.Start, &s.End)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: load slots: %w", err)
	}
	idx := map[string]int{}
	for i, s := range slots {
		idx[s.ID] = i
	}
	rows, err = q.Query(ctx, `
		SELECT c.id, c.slot_id, c.grade_level, c.subject_id, s.name, coalesce(c.exam_id::text, ''), coalesce(e.name, ''), coalesce(u.full_name, ''),
		       coalesce(e.status, ''),
		       (SELECT count(*) FROM exam_items i WHERE i.exam_id = e.id),
		       EXISTS (SELECT 1 FROM exam_variant_items vi JOIN exam_variants v ON v.id = vi.variant_id JOIN exam_questions q ON q.id = vi.question_id
		               WHERE v.exam_id = e.id AND q.status = 'ditelaah')
		FROM exam_event_cells c
		JOIN subjects s ON s.id = c.subject_id
		LEFT JOIN exams e ON e.id = c.exam_id AND e.archived_at IS NULL
		LEFT JOIN users u ON u.id = e.owner_id
		WHERE c.school_id = $1 AND c.event_id = $2`, ev.SchoolID, ev.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: load cells: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Cell
		var g int16
		var status string
		var review bool
		if err := rows.Scan(&c.ID, &c.SlotID, &g, &c.SubjectID, &c.SubjectName, &c.ExamID, &c.ExamName, &c.OwnerName, &status, &c.Questions, &review); err != nil {
			return nil, fmt.Errorf("exam: load cells: %w", err)
		}
		c.Grade = int(g)
		switch {
		case c.ExamName == "":
			c.ExamID, c.Status = "", CellNoExam
		case status == ExamScheduled || status == ExamRunning || status == ExamDone:
			c.Status = CellReady
		case review:
			c.Status = CellInReview
		case c.Questions == 0:
			c.Status = CellNoExam
		default:
			c.Status = CellDrafting
		}
		if i, ok := idx[c.SlotID]; ok {
			slots[i].Cells[c.Grade] = c
		}
	}
	return slots, rows.Err()
}

// SlotForm adalah isian baris jadwal.
type SlotForm struct{ Day, Start, End string }

// AddSlot menambahkan baris jadwal (hari dan jam) ke agenda.
func AddSlot(ctx context.Context, db database.DB, sa StaffActor, eventID string, f SlotForm) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	ev, err := GetEvent(ctx, db, sa.SchoolID, eventID)
	if err != nil {
		return err
	}
	day, err1 := time.Parse("2006-01-02", f.Day)
	a, err2 := time.Parse("15:04", f.Start)
	b, err3 := time.Parse("15:04", f.End)
	switch {
	case err1 != nil || err2 != nil || err3 != nil:
		return &FieldError{Fields: map[string]string{"slot": "Isi tanggal, jam mulai, dan jam selesai."}}
	case day.Before(ev.StartsOn) || day.After(ev.EndsOn):
		return &FieldError{Fields: map[string]string{"slot": "Tanggal di luar rentang agenda."}}
	case !b.After(a):
		return &FieldError{Fields: map[string]string{"slot": "Jam selesai harus setelah jam mulai."}}
	}
	_, err = db.Exec(ctx, `INSERT INTO exam_event_slots (school_id, event_id, day, start_time, end_time) VALUES ($1, $2, $3, $4, $5)`,
		sa.SchoolID, eventID, day, f.Start, f.End)
	if err != nil {
		return fmt.Errorf("exam: add slot: %w", err)
	}
	return nil
}

// ErrSlotUsed: baris jadwal sudah punya sesi yang dibuat.
var ErrSlotUsed = errors.New("exam: slot has sessions")

// RemoveSlot menghapus baris jadwal beserta selnya, bila belum ada sesi.
func RemoveSlot(ctx context.Context, db database.DB, sa StaffActor, eventID, slotID string) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	if !database.IsUUID(slotID) {
		return ErrNotFound
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_sessions x JOIN exam_event_cells c ON c.id = x.cell_id
			WHERE c.slot_id = $1 AND x.cancelled_at IS NULL)`, slotID).Scan(&used); err != nil {
			return fmt.Errorf("exam: check slot: %w", err)
		}
		if used {
			return ErrSlotUsed
		}
		for _, q := range []string{
			`DELETE FROM exam_event_proctors WHERE slot_id = $2 AND event_id = $1`,
			`DELETE FROM exam_event_cells WHERE slot_id = $2 AND event_id = $1`,
		} {
			if _, err := tx.Exec(ctx, q, eventID, slotID); err != nil {
				return fmt.Errorf("exam: remove slot: %w", err)
			}
		}
		tag, err := tx.Exec(ctx, `DELETE FROM exam_event_slots WHERE school_id = $1 AND event_id = $2 AND id = $3`, sa.SchoolID, eventID, slotID)
		if err != nil {
			return fmt.Errorf("exam: remove slot: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// SetCell mengisi sel jadwal dengan mapel (subjectID kosong menghapus sel
// yang belum punya ujian).
func SetCell(ctx context.Context, db database.DB, sa StaffActor, eventID, slotID string, grade int, subjectID string) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	ev, err := GetEvent(ctx, db, sa.SchoolID, eventID)
	if err != nil {
		return err
	}
	if !slices.Contains(ev.Grades, grade) || !database.IsUUID(slotID) {
		return ErrNotFound
	}
	var ok bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_event_slots WHERE school_id = $1 AND event_id = $2 AND id = $3)`,
		sa.SchoolID, eventID, slotID).Scan(&ok); err != nil {
		return fmt.Errorf("exam: check slot: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	if subjectID == "" {
		_, err := db.Exec(ctx, `DELETE FROM exam_event_cells WHERE event_id = $1 AND slot_id = $2 AND grade_level = $3 AND exam_id IS NULL`,
			eventID, slotID, grade)
		if err != nil {
			return fmt.Errorf("exam: clear cell: %w", err)
		}
		return nil
	}
	if !database.IsUUID(subjectID) {
		return &FieldError{Fields: map[string]string{"subject_id": "Pilih mapel."}}
	}
	_, err = db.Exec(ctx, `INSERT INTO exam_event_cells (school_id, event_id, slot_id, grade_level, subject_id)
		SELECT $1, $2, $3, $4, id FROM subjects WHERE school_id = $1 AND id = $5
		ON CONFLICT (slot_id, grade_level) DO UPDATE SET subject_id = EXCLUDED.subject_id
		WHERE exam_event_cells.exam_id IS NULL`, sa.SchoolID, eventID, slotID, grade, subjectID)
	if err != nil {
		return fmt.Errorf("exam: set cell: %w", err)
	}
	return nil
}

// CreateCellExam membuat ujian PTS/PAS untuk sel jadwal dan menautkannya.
// Pemegang soal: ownerID, atau guru yang mengampu mapel itu di tingkat sel
// (bila ada), atau pembuat.
func CreateCellExam(ctx context.Context, db database.DB, sa StaffActor, eventID, cellID, ownerID string, now time.Time) (Exam, error) {
	if err := requireAgendaWrite(sa); err != nil {
		return Exam{}, err
	}
	ev, err := GetEvent(ctx, db, sa.SchoolID, eventID)
	if err != nil {
		return Exam{}, err
	}
	if !database.IsUUID(cellID) {
		return Exam{}, ErrNotFound
	}
	var subjectID, subjectName, existing string
	var grade int16
	var slotMinutes int
	err = db.QueryRow(ctx, `SELECT c.subject_id, s.name, c.grade_level, coalesce(c.exam_id::text, ''),
		(extract(epoch FROM sl.end_time - sl.start_time) / 60)::int
		FROM exam_event_cells c JOIN subjects s ON s.id = c.subject_id JOIN exam_event_slots sl ON sl.id = c.slot_id
		WHERE c.school_id = $1 AND c.event_id = $2 AND c.id = $3`, sa.SchoolID, eventID, cellID).Scan(&subjectID, &subjectName, &grade, &existing, &slotMinutes)
	if errors.Is(err, pgx.ErrNoRows) {
		return Exam{}, ErrNotFound
	}
	if err != nil {
		return Exam{}, fmt.Errorf("exam: load cell: %w", err)
	}
	if existing != "" {
		return GetExam(ctx, db, sa.SchoolID, existing)
	}
	if ownerID == "" {
		_ = db.QueryRow(ctx, `SELECT ta.teacher_id FROM teaching_assignments ta
			JOIN classes c ON c.id = ta.class_id JOIN academic_years y ON y.id = ta.academic_year_id AND y.is_active
			WHERE ta.school_id = $1 AND ta.subject_id = $2 AND c.grade_level = $3 ORDER BY c.name LIMIT 1`,
			sa.SchoolID, subjectID, grade).Scan(&ownerID)
	}
	dur := min(max(slotMinutes, 5), 600)
	f := BasicsForm{Name: fmt.Sprintf("%s %s Kelas %d", ev.Purpose.Label(), subjectName, grade), Purpose: string(ev.Purpose),
		SubjectID: subjectID, GradeLevel: strconv.Itoa(int(grade)), Duration: strconv.Itoa(dur), OwnerID: ownerID}
	e, err := CreateExam(ctx, db, sa, f, now)
	if err != nil {
		return Exam{}, err
	}
	if _, err := db.Exec(ctx, `UPDATE exam_event_cells SET exam_id = $2 WHERE id = $1 AND exam_id IS NULL`, cellID, e.ID); err != nil {
		return Exam{}, fmt.Errorf("exam: link cell exam: %w", err)
	}
	return e, nil
}

// EventParticipant adalah satu peserta agenda.
type EventParticipant struct {
	StudentID string
	Name      string
	NIS       string
	ClassID   string
	ClassName string
	Grade     int
	RoomID    string
	Shift     int
	Seat      int
	Number    string
}

// ListEventParticipants mengembalikan peserta agenda, urut tingkat, kelas,
// nama.
func ListEventParticipants(ctx context.Context, q database.DBTX, ev Event) ([]EventParticipant, error) {
	rows, err := q.Query(ctx, `
		SELECT p.student_id, s.full_name, s.nis, p.class_id, c.name, p.grade_level, coalesce(p.room_id::text, ''), coalesce(p.shift, 0),
		       coalesce(p.seat, 0), p.participant_no
		FROM exam_event_participants p JOIN students s ON s.id = p.student_id JOIN classes c ON c.id = p.class_id
		WHERE p.school_id = $1 AND p.event_id = $2
		ORDER BY p.grade_level, c.name, s.full_name`, ev.SchoolID, ev.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: list event participants: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (EventParticipant, error) {
		var p EventParticipant
		var g, shift, seat int16
		err := r.Scan(&p.StudentID, &p.Name, &p.NIS, &p.ClassID, &p.ClassName, &g, &p.RoomID, &shift, &seat, &p.Number)
		p.Grade, p.Shift, p.Seat = int(g), int(shift), int(seat)
		return p, err
	})
}

// AllocateEvent membagi semua siswa aktif tingkat agenda ke ruang dan
// gelombang (Allocate), lalu memberi nomor peserta. Pembagian lama diganti.
func AllocateEvent(ctx context.Context, db database.DB, sa StaffActor, eventID string) (Allocation, error) {
	if err := requireAgendaWrite(sa); err != nil {
		return Allocation{}, err
	}
	var out Allocation
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ev, err := GetEvent(ctx, tx, sa.SchoolID, eventID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT c.id, c.name, c.grade_level, s.id, s.full_name, s.nis
			FROM classes c
			JOIN academic_years y ON y.id = c.academic_year_id AND y.is_active
			JOIN enrollments en ON en.class_id = c.id AND en.end_date IS NULL
			JOIN students s ON s.id = en.student_id AND s.status = 'aktif'
			WHERE c.school_id = $1 AND c.grade_level = ANY($2)
			ORDER BY c.grade_level, c.name, s.full_name, s.nis`, sa.SchoolID, int16s(ev.Grades))
		if err != nil {
			return fmt.Errorf("exam: load students: %w", err)
		}
		var classes []AllocClass
		idx := map[string]int{}
		classOf := map[string]AllocClass{}
		for rows.Next() {
			var cid, cname, sid, sname, nis string
			var g int16
			if err := rows.Scan(&cid, &cname, &g, &sid, &sname, &nis); err != nil {
				rows.Close()
				return fmt.Errorf("exam: load students: %w", err)
			}
			i, ok := idx[cid]
			if !ok {
				i = len(classes)
				idx[cid] = i
				classes = append(classes, AllocClass{ID: cid, Name: cname, Grade: int(g)})
			}
			classes[i].Students = append(classes[i].Students, AllocStudent{ID: sid, Name: sname, NIS: nis})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("exam: load students: %w", err)
		}
		for _, c := range classes {
			for _, s := range c.Students {
				classOf[s.ID] = c
			}
		}
		rooms, err := eventRooms(ctx, tx, ev)
		if err != nil {
			return err
		}
		var ar []AllocRoom
		for _, r := range rooms {
			ar = append(ar, AllocRoom{ID: r.ID, Name: r.Name, Kind: r.Kind, Capacity: r.Capacity})
		}
		out = Allocate(classes, ar, ev.MaxShifts)
		if _, err := tx.Exec(ctx, `DELETE FROM exam_event_participants WHERE event_id = $1`, ev.ID); err != nil {
			return fmt.Errorf("exam: clear participants: %w", err)
		}
		seq := map[int]int{}
		var batch [][]any
		for _, c := range classes {
			for _, s := range c.Students {
				seq[c.Grade]++
				p, placed := out.Places[s.ID]
				var room, shift, seat any
				if placed {
					room, shift, seat = p.RoomID, p.Shift, p.Seat
				}
				no := ParticipantNumber(ev.NumberFormat, ev.StartsOn.Year(), c.Grade, seq[c.Grade], p.Shift)
				batch = append(batch, []any{sa.SchoolID, ev.ID, s.ID, c.ID, int16(c.Grade), room, shift, seat, no})
			}
		}
		if len(batch) > 0 {
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{"exam_event_participants"},
				[]string{"school_id", "event_id", "student_id", "class_id", "grade_level", "room_id", "shift", "seat", "participant_no"},
				pgx.CopyFromRows(batch)); err != nil {
				return fmt.Errorf("exam: save participants: %w", err)
			}
		}
		return recordAudit(ctx, tx, sa, "exam_event.allocate", "exam_event", ev.ID, nil,
			map[string]any{"placed": len(out.Places), "unplaced": len(out.Unplaced)})
	})
	return out, err
}

// eventRooms mengembalikan ruang agenda, urut lab dulu lalu nama.
func eventRooms(ctx context.Context, q database.DBTX, ev Event) ([]Room, error) {
	rows, err := q.Query(ctx, `SELECT r.id, r.name, r.kind, r.capacity, r.archived_at FROM exam_event_rooms er
		JOIN exam_rooms r ON r.id = er.room_id WHERE er.event_id = $1 ORDER BY r.kind, lower(r.name)`, ev.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: event rooms: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Room, error) {
		var x Room
		var c int16
		err := r.Scan(&x.ID, &x.Name, &x.Kind, &c, &x.ArchivedAt)
		x.Capacity = int(c)
		return x, err
	})
}

// OverCapacityError: pemindahan membuat ruang+gelombang melebihi
// kapasitas.
type OverCapacityError struct {
	Room           string
	Shift          int
	Want, Capacity int
}

func (e *OverCapacityError) Error() string {
	return fmt.Sprintf("exam: %s shift %d over capacity (%d/%d)", e.Room, e.Shift, e.Want, e.Capacity)
}

// MoveRequest adalah pemindahan peserta: satu siswa, atau semua siswa satu
// kelas yang sekarang di ruang+gelombang asal (FromRoom/FromShift).
type MoveRequest struct {
	StudentID string
	ClassID   string
	FromRoom  string
	FromShift int
	ToRoom    string
	ToShift   int
}

// MoveParticipants memindahkan peserta ke ruang+gelombang lain. Kapasitas
// dijaga: pemindahan yang melebihi kapasitas ditolak dengan
// OverCapacityError. Tempat duduk ruang tujuan dan asal diurutkan ulang.
func MoveParticipants(ctx context.Context, db database.DB, sa StaffActor, eventID string, m MoveRequest) (int, error) {
	if err := requireAgendaWrite(sa); err != nil {
		return 0, err
	}
	moved := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		ev, err := GetEvent(ctx, tx, sa.SchoolID, eventID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM exam_events WHERE id = $1 FOR UPDATE`, ev.ID); err != nil {
			return fmt.Errorf("exam: lock event: %w", err)
		}
		var room Room
		var c int16
		err = tx.QueryRow(ctx, `SELECT r.id, r.name, r.kind, r.capacity FROM exam_event_rooms er JOIN exam_rooms r ON r.id = er.room_id
			WHERE er.event_id = $1 AND r.id::text = $2`, ev.ID, m.ToRoom).Scan(&room.ID, &room.Name, &room.Kind, &c)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("exam: load room: %w", err)
		}
		room.Capacity = int(c)
		if m.ToShift < 1 || m.ToShift > ev.MaxShifts || (room.Kind != RoomLab && m.ToShift > 1) {
			return &FieldError{Fields: map[string]string{"shift": "Gelombang tidak tersedia untuk ruang ini."}}
		}
		var ids []string
		switch {
		case database.IsUUID(m.StudentID):
			ids = []string{m.StudentID}
		case database.IsUUID(m.ClassID):
			rows, err := tx.Query(ctx, `SELECT student_id::text FROM exam_event_participants WHERE event_id = $1 AND class_id = $2
				AND room_id IS NOT DISTINCT FROM $3 AND shift IS NOT DISTINCT FROM $4`, ev.ID, m.ClassID, nullable(m.FromRoom), nullInt(m.FromShift))
			if err != nil {
				return fmt.Errorf("exam: load class participants: %w", err)
			}
			if ids, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
				return fmt.Errorf("exam: load class participants: %w", err)
			}
		}
		if len(ids) == 0 {
			return ErrNotFound
		}
		var already, current int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE student_id = ANY($4)), count(*) FROM exam_event_participants
			WHERE event_id = $1 AND room_id = $2 AND shift = $3`, ev.ID, room.ID, m.ToShift, ids).Scan(&already, &current); err != nil {
			return fmt.Errorf("exam: count room: %w", err)
		}
		want := current - already + len(ids)
		if want > room.Capacity {
			return &OverCapacityError{Room: room.Name, Shift: m.ToShift, Want: want, Capacity: room.Capacity}
		}
		tag, err := tx.Exec(ctx, `UPDATE exam_event_participants SET room_id = $3, shift = $4, seat = NULL
			WHERE event_id = $1 AND student_id = ANY($2)`, ev.ID, ids, room.ID, m.ToShift)
		if err != nil {
			return fmt.Errorf("exam: move participants: %w", err)
		}
		moved = int(tag.RowsAffected())
		return reseat(ctx, tx, ev.ID)
	})
	return moved, err
}

// reseat mengurutkan ulang tempat duduk semua ruang+gelombang agenda:
// urut kelas lalu nama.
func reseat(ctx context.Context, tx pgx.Tx, eventID string) error {
	_, err := tx.Exec(ctx, `
		WITH r AS (
			SELECT p.id, row_number() OVER (PARTITION BY p.room_id, p.shift ORDER BY c.name, s.full_name, s.nis) AS n
			FROM exam_event_participants p JOIN classes c ON c.id = p.class_id JOIN students s ON s.id = p.student_id
			WHERE p.event_id = $1 AND p.room_id IS NOT NULL)
		UPDATE exam_event_participants p SET seat = r.n FROM r WHERE r.id = p.id`, eventID)
	if err != nil {
		return fmt.Errorf("exam: reseat: %w", err)
	}
	return nil
}

// AnnounceEvent mengumumkan jadwal agenda ke siswa (beranda peserta).
func AnnounceEvent(ctx context.Context, db database.DB, sa StaffActor, eventID string, on bool) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	q := `UPDATE exam_events SET announced_at = coalesce(announced_at, now()) WHERE school_id = $1 AND id = $2`
	if !on {
		q = `UPDATE exam_events SET announced_at = NULL WHERE school_id = $1 AND id = $2`
	}
	tag, err := db.Exec(ctx, q, sa.SchoolID, eventID)
	if err != nil {
		return fmt.Errorf("exam: announce event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ArchiveEvent mengarsipkan agenda; sesi yang belum mulai dibatalkan.
func ArchiveEvent(ctx context.Context, db database.DB, sa StaffActor, eventID string, now time.Time) error {
	if err := requireAgendaWrite(sa); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE exam_events SET archived_at = coalesce(archived_at, now()) WHERE school_id = $1 AND id = $2`, sa.SchoolID, eventID)
		if err != nil {
			return fmt.Errorf("exam: archive event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET cancelled_at = now() WHERE event_id = $1 AND cancelled_at IS NULL AND starts_at > $2`,
			eventID, now); err != nil {
			return fmt.Errorf("exam: cancel event sessions: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam_event.archive", "exam_event", eventID, nil, nil)
	})
}
