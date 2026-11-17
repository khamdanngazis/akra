package exam

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/auth"
)

// seedGrade membuat n kelas tingkat grade dengan size siswa aktif masing-
// masing, dan mengembalikan id kelas.
func (e *bankEnv) seedGrade(t *testing.T, grade, n, size int) []string {
	t.Helper()
	ctx := context.Background()
	var ids []string
	for c := 0; c < n; c++ {
		var cid string
		name := fmt.Sprintf("%d%c-uji", grade, 'A'+c)
		if err := e.tx.QueryRow(ctx, `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, $4) RETURNING id`,
			e.a.ID, e.a.YearID, name, grade).Scan(&cid); err != nil {
			t.Fatal(err)
		}
		if _, err := e.tx.Exec(ctx, `
			WITH s AS (INSERT INTO students (school_id, nis, full_name)
			           SELECT $1, $2 || '-' || g, 'Siswa ' || $2 || ' ' || lpad(g::text, 2, '0') FROM generate_series(1, $3) g RETURNING id)
			INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
			SELECT $1, s.id, $4, $5, '2026-07-13' FROM s`, e.a.ID, name, size, cid, e.a.YearID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, cid)
	}
	return ids
}

func TestEventFlow(t *testing.T) {
	e := newExamEnv(t)
	ctx := context.Background()
	admin := e.staff(e.a.Admin.ID, "admin_tu")
	classes := e.seedGrade(t, 7, 3, 30)
	// Guru mengampu Matematika di kelas 7A (tidak boleh mengawasinya).
	if _, err := e.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
		e.a.ID, e.a.YearID, classes[0], e.a.SubjectID, e.a.Teacher.ID); err != nil {
		t.Fatal(err)
	}
	// Dua guru lain tanpa mapel Matematika, calon pengawas.
	for _, n := range []string{"guru3", "guru4"} {
		if _, err := e.tx.Exec(ctx, `WITH u AS (INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, $2, 'x', $3, true) RETURNING id)
			INSERT INTO user_roles (user_id, role_id) SELECT u.id, roles.id FROM u, roles WHERE roles.code = 'guru'`, e.a.ID, n+"@a.uji", "Guru "+n); err != nil {
			t.Fatal(err)
		}
	}
	// Guru biasa tidak boleh mengubah agenda.
	if _, err := SaveRoom(ctx, e.tx, e.owner, "", RoomForm{Name: "X", Kind: RoomLab, Capacity: "10"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("teacher saved room: %v", err)
	}
	lab, err := SaveRoom(ctx, e.tx, admin, "", RoomForm{Name: "Lab Komputer", Kind: RoomLab, Capacity: "40"})
	if err != nil {
		t.Fatal(err)
	}
	room, err := SaveRoom(ctx, e.tx, admin, "", RoomForm{Name: "Ruang 7", Kind: RoomClass, Capacity: "32"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRoom(ctx, e.tx, admin, "", RoomForm{Name: "lab komputer", Kind: RoomLab, Capacity: "10"}); err == nil {
		t.Error("duplicate room name accepted")
	}
	ev, err := CreateEvent(ctx, e.tx, admin, EventForm{Name: "PAS Ganjil", Purpose: "pas", StartsOn: "2027-03-15", EndsOn: "2027-03-19", Grades: []string{"7"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetEventRooms(ctx, e.tx, admin, ev.ID, []string{lab, room}); err != nil {
		t.Fatal(err)
	}
	if err := AddSlot(ctx, e.tx, admin, ev.ID, SlotForm{Day: "2027-03-15", Start: "07:30", End: "09:00"}); err != nil {
		t.Fatal(err)
	}
	if err := AddSlot(ctx, e.tx, admin, ev.ID, SlotForm{Day: "2027-03-20", Start: "07:30", End: "09:00"}); err == nil {
		t.Error("slot outside event dates accepted")
	}
	slots, _ := LoadSchedule(ctx, e.tx, ev)
	if err := SetCell(ctx, e.tx, admin, ev.ID, slots[0].ID, 7, e.a.SubjectID); err != nil {
		t.Fatal(err)
	}
	// 90 siswa, lab 40 + ruang 32: lab dibagi dua gelombang.
	a, err := AllocateEvent(ctx, e.tx, admin, ev.ID)
	if err != nil || len(a.Unplaced) != 0 || len(a.Places) != 90 {
		t.Fatalf("allocate = %d placed, %d unplaced, %v", len(a.Places), len(a.Unplaced), err)
	}
	parts, _ := ListEventParticipants(ctx, e.tx, ev)
	shift2 := 0
	numbers := map[string]bool{}
	for _, p := range parts {
		if p.Shift == 2 {
			shift2++
			if p.RoomID != lab {
				t.Errorf("classroom in shift 2")
			}
		}
		if numbers[p.Number] || !strings.HasPrefix(p.Number, "2027-07-") {
			t.Errorf("number %q", p.Number)
		}
		numbers[p.Number] = true
	}
	if shift2 == 0 {
		t.Error("no second lab shift")
	}
	// Pindah kelas ke ruang yang tidak muat: ditolak, tidak ada yang pindah.
	var fromRoom string
	var fromShift int
	for _, p := range parts {
		if p.ClassID == classes[1] {
			fromRoom, fromShift = p.RoomID, p.Shift
			break
		}
	}
	target := room
	if fromRoom == room {
		target = lab
	}
	_, err = MoveParticipants(ctx, e.tx, admin, ev.ID, MoveRequest{ClassID: classes[1], FromRoom: fromRoom, FromShift: fromShift, ToRoom: target, ToShift: 1})
	var oc *OverCapacityError
	if !errors.As(err, &oc) {
		t.Fatalf("over capacity move: %v", err)
	}
	// Satu siswa ke gelombang 3 lab (kosong): boleh.
	if n, err := MoveParticipants(ctx, e.tx, admin, ev.ID, MoveRequest{StudentID: parts[0].StudentID, ToRoom: lab, ToShift: 3}); err != nil || n != 1 {
		t.Fatalf("single move = %d, %v", n, err)
	}
	if _, err := MoveParticipants(ctx, e.tx, admin, ev.ID, MoveRequest{StudentID: parts[0].StudentID, ToRoom: room, ToShift: 2}); err == nil {
		t.Error("classroom shift 2 accepted")
	}
	// Ujian sel: pemegang otomatis guru pengampu.
	slots, _ = LoadSchedule(ctx, e.tx, ev)
	cell := slots[0].Cells[7]
	if cell.Status != CellNoExam {
		t.Errorf("cell status = %s", cell.Status)
	}
	ex, err := CreateCellExam(ctx, e.tx, admin, ev.ID, cell.ID, "", now)
	if err != nil || ex.OwnerID != e.a.Teacher.ID || ex.Purpose != PurposeFinal || ex.GradeLevel != 7 || ex.DurationMinutes != 90 {
		t.Fatalf("cell exam = %+v, %v", ex, err)
	}
	sec := e.firstSection(t, ex)
	if _, err := AddQuestions(ctx, e.tx, e.owner, ex.ID, sec, []string{e.qs[0].ID, e.qs[3].ID}, now); err != nil {
		t.Fatal(err)
	}
	slots, _ = LoadSchedule(ctx, e.tx, ev)
	if slots[0].Cells[7].Status != CellDrafting {
		t.Errorf("cell after questions = %s", slots[0].Cells[7].Status)
	}
	// Pengawas: guru Matematika tidak mengawasi slot Matematika.
	ro, err := AutoAssignProctors(ctx, e.tx, admin, ev.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	for k, tid := range ro.Assign {
		if tid == e.a.Teacher.ID {
			t.Errorf("own-subject proctor at %s", k)
		}
	}
	p, _ := LoadEventPlan(ctx, e.tx, ev)
	if len(p.Units) != 4 { // lab g1, lab g2, lab g3, ruang
		t.Errorf("units = %d", len(p.Units))
	}
	// Tukar manual ke guru Matematika: dilaporkan.
	if err := SetProctor(ctx, e.tx, admin, ev.ID, p.Units[0].Key, e.a.Teacher.ID); err != nil {
		t.Fatal(err)
	}
	p, _ = LoadEventPlan(ctx, e.tx, ev)
	s, _ := Summarize(ctx, e.tx, p)
	if s.OwnSubject != 1 || s.Ready() {
		t.Errorf("summary = %+v", s)
	}
	// Terapkan ke sesi: satu sesi per ruang+gelombang dengan peserta.
	res, err := ApplyEventSessions(ctx, e.tx, admin, ev.ID, now)
	if err != nil || res.Created != len(p.Units) {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	sessions, _ := ListSessions(ctx, e.tx, e.a.ID, ex.ID)
	total := 0
	for _, se := range sessions {
		total += se.Participants
		if len(se.ProctorIDs) == 0 {
			t.Errorf("session %s without proctor", se.Label)
		}
	}
	if total != 90 {
		t.Errorf("session participants = %d", total)
	}
	// Gelombang 2 mulai setelah gelombang 1 + jeda 15 menit.
	for _, se := range sessions {
		if strings.Contains(se.Label, "Gelombang 2") {
			if got := se.StartsAt.In(time.FixedZone("WIB", 7*3600)).Format("15:04"); got != "09:15" {
				t.Errorf("shift 2 starts %s", got)
			}
		}
	}
	// Menerapkan ulang tidak menggandakan sesi.
	res, err = ApplyEventSessions(ctx, e.tx, admin, ev.ID, now)
	if err != nil || res.Created != 0 || res.Updated != len(p.Units) {
		t.Errorf("reapply = %+v, %v", res, err)
	}
	// Pengawas mendapat hak sesi dari agenda.
	act, _ := LoadActor(ctx, e.tx, authID(e.a.ID, e.a.Teacher.ID, "guru"))
	// Satu sesi dari fixture ditambah satu sesi agenda.
	if len(act.Proctors) != 2 {
		t.Errorf("teacher proctor assignments = %d", len(act.Proctors))
	}
	// Diumumkan: jadwal tampil untuk siswa.
	if err := AnnounceEvent(ctx, e.tx, admin, ev.ID, true); err != nil {
		t.Fatal(err)
	}
	items, err := StudentSchedule(ctx, e.tx, e.a.ID, parts[5].StudentID, now)
	if err != nil || len(items) != 1 || items[0].Number != parts[5].Number {
		t.Errorf("student schedule = %+v, %v", items, err)
	}
	// Halaman agenda: admin dan kepala sekolah; guru biasa 403.
	for _, tab := range []string{"", "/jadwal", "/ruang", "/pengawas"} {
		if rec := e.getAs("/ujian/agenda/"+ev.ID+tab, e.a.Principal.ID, "kepala_sekolah"); rec.Code != http.StatusOK {
			t.Errorf("principal %s = %d", tab, rec.Code)
		}
		if rec := e.getAs("/ujian/agenda/"+ev.ID+tab, e.a.Admin.ID, "admin_tu"); rec.Code != http.StatusOK {
			t.Errorf("admin %s = %d", tab, rec.Code)
		}
	}
	if rec := e.getAs("/ujian/agenda/"+ev.ID, e.teacher2, "guru"); rec.Code != http.StatusForbidden {
		t.Errorf("teacher agenda = %d", rec.Code)
	}
	if body := e.getAs("/ujian/agenda/"+ev.ID+"/pengawas", e.a.Admin.ID, "admin_tu").Body.String(); !strings.Contains(body, "mengampu mapel yang diujikan") {
		t.Error("own-subject issue not shown")
	}
	// Jam yang sudah punya sesi tidak bisa dihapus.
	if err := RemoveSlot(ctx, e.tx, admin, ev.ID, slots[0].ID); !errors.Is(err, ErrSlotUsed) {
		t.Errorf("remove used slot: %v", err)
	}
}

func authID(schoolID, userID, role string) auth.Identity {
	return auth.Identity{UserID: userID, SchoolID: schoolID, Roles: []string{role}}
}
