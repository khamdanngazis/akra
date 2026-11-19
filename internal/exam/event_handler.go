package exam

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// eventRoutes mendaftarkan route agenda PTS/PAS (T134), dibuka di tahap R3.
func (h *Handler) eventRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(RequireModule(h.db, h.render, h.logger, R3))
		// Peran: admin_tu, koordinator ujian; kepala_sekolah membaca.
		r.Use(RequireAccess(h.db, h.render, h.logger, ActAgenda, Read))
		r.Get("/agenda", h.events)
		r.Get("/agenda/{id}", h.eventDetail)
		for _, tab := range []string{"jadwal", "ruang", "pengawas"} {
			r.Get("/agenda/{id}/"+tab, h.eventDetail)
		}
		r.Get("/ruang", h.rooms)
		readers := r
		r.Group(func(r chi.Router) {
			r.Use(RequireAccess(h.db, h.render, h.logger, ActAgenda, Write))
			h.eventDocsRoutes(readers, r)
			h.controlRoutes(readers, r)
			r.Get("/agenda/baru", h.eventNew)
			r.Post("/agenda/", h.eventCreate)
			r.Post("/agenda/{id}", h.eventUpdate)
			r.Post("/agenda/{id}/jam", h.eventAddSlot)
			r.Post("/agenda/{id}/jam/{slot}/hapus", h.eventRemoveSlot)
			r.Post("/agenda/{id}/sel", h.eventSetCell)
			r.Post("/agenda/{id}/sel/{cell}/ujian", h.eventCellExam)
			r.Post("/agenda/{id}/ruang", h.eventSetRooms)
			r.Post("/agenda/{id}/bagi", h.eventAllocate)
			r.Post("/agenda/{id}/pindah", h.eventMove)
			r.Post("/agenda/{id}/pengawas", h.eventAutoProctors)
			r.Post("/agenda/{id}/pengawas/tukar", h.eventSetProctor)
			r.Post("/agenda/{id}/terapkan", h.eventApply)
			r.Post("/agenda/{id}/umumkan", h.eventAnnounce)
			r.Post("/agenda/{id}/arsip", h.eventArchive)
			r.Post("/ruang", h.roomSave)
			r.Post("/ruang/{id}", h.roomSave)
			r.Post("/ruang/{id}/arsip", h.roomArchive)
		})
	})
}

func eventPath(id string) string { return "/ujian/agenda/" + id }

type eventsView struct {
	User    auth.Identity
	Events  []Event
	CanEdit bool
	Rooms   int
	Flash   string
	Error   string
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	evs, err := ListEvents(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "list events", err)
		return
	}
	rooms, err := ListRooms(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "list rooms", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/events", eventsView{User: id, Events: evs, CanEdit: sa.CanAny(ActAgenda, Write),
		Rooms: len(rooms), Flash: auth.PopFlash(r.Context(), h.sessions)})
}

type eventFormView struct {
	User   auth.Identity
	IsNew  bool
	Event  Event
	Form   EventForm
	Errors map[string]string
	Grades []optionItem
	Flash  string
	Error  string
}

func gradeOptions(selected []string) []optionItem {
	var out []optionItem
	for g := 1; g <= 12; g++ {
		v := strconv.Itoa(g)
		out = append(out, optionItem{Value: v, Label: "Kelas " + v, Selected: containsStr(selected, v)})
	}
	return out
}

func (h *Handler) eventNew(w http.ResponseWriter, r *http.Request) {
	id, _ := h.staff(r)
	today := clock.TodayIn(id.Location())
	f := EventForm{Purpose: string(PurposeFinal), StartsOn: today.AddDate(0, 0, 14).Format("2006-01-02"),
		EndsOn: today.AddDate(0, 0, 18).Format("2006-01-02"), NumberFormat: "{tahun}-{tingkat}-{urut:3}", ShiftGap: "15", MaxShifts: "3"}
	h.render.Render(w, r, http.StatusOK, "exam/event_form", eventFormView{User: id, IsNew: true, Form: f, Grades: gradeOptions(nil), Errors: map[string]string{}})
}

func eventFormFrom(r *http.Request) EventForm {
	pf := r.PostForm
	return EventForm{Name: pf.Get("name"), Purpose: pf.Get("purpose"), StartsOn: pf.Get("starts_on"), EndsOn: pf.Get("ends_on"),
		Grades: pf["grades"], NumberFormat: pf.Get("number_format"), ShiftGap: pf.Get("shift_gap"), MaxShifts: pf.Get("max_shifts")}
}

func (h *Handler) eventCreate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 32<<10) {
		return
	}
	id, sa := h.staff(r)
	f := eventFormFrom(r)
	ev, err := CreateEvent(r.Context(), h.db, sa, f)
	var fe *FieldError
	if errors.As(err, &fe) {
		h.render.Render(w, r, http.StatusUnprocessableEntity, "exam/event_form", eventFormView{User: id, IsNew: true, Form: f, Errors: fe.Fields,
			Grades: gradeOptions(f.Grades)})
		return
	}
	if err != nil {
		h.eventFail(w, r, "", "create event", err)
		return
	}
	h.flash(r, "Agenda dibuat. Isi jadwal mapel per hari lebih dulu.")
	http.Redirect(w, r, eventPath(ev.ID)+"/jadwal", http.StatusSeeOther)
}

// eventFail menerjemahkan error agenda; error input ditampilkan sebagai
// flash di halaman back.
func (h *Handler) eventFail(w http.ResponseWriter, r *http.Request, back, op string, err error) {
	var fe *FieldError
	var oc *OverCapacityError
	id, _ := auth.CurrentUser(r.Context())
	switch {
	case errors.Is(err, ErrForbidden):
		page.Forbidden(h.render, w, r, id, "Hanya admin/TU dan koordinator ujian yang bisa mengubah agenda.")
		return
	case errors.As(err, &oc):
		h.flash(r, fmt.Sprintf("Tidak dipindah: %s gelombang %d akan berisi %d peserta, melebihi kapasitas %d.", oc.Room, oc.Shift, oc.Want, oc.Capacity))
	case errors.Is(err, ErrSlotUsed):
		h.flash(r, "Jam ini sudah punya sesi ujian. Batalkan sesinya dulu.")
	case errors.As(err, &fe) && back != "":
		var msgs []string
		for _, m := range fe.Fields {
			msgs = append(msgs, m)
		}
		sort.Strings(msgs)
		h.flash(r, strings.Join(msgs, " "))
	default:
		h.fail(w, r, op, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// eventWrite memuat agenda untuk aksi tulis.
func (h *Handler) eventWrite(w http.ResponseWriter, r *http.Request) (StaffActor, Event, bool) {
	if !h.parseForm(w, r, 64<<10) {
		return StaffActor{}, Event{}, false
	}
	id, sa := h.staff(r)
	ev, err := GetEvent(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Agenda tidak ditemukan.")
		return sa, Event{}, false
	}
	if err != nil {
		h.fail(w, r, "get event", err)
		return sa, Event{}, false
	}
	return sa, ev, true
}

func (h *Handler) eventUpdate(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	if err := UpdateEvent(r.Context(), h.db, sa, ev.ID, eventFormFrom(r)); err != nil {
		h.eventFail(w, r, eventPath(ev.ID), "update event", err)
		return
	}
	h.flash(r, "Agenda disimpan.")
	http.Redirect(w, r, eventPath(ev.ID), http.StatusSeeOther)
}

// gridCell adalah satu sel di tabel jadwal.
type gridCell struct {
	Grade  int
	SlotID string
	Cell   *Cell
}

type gridRow struct {
	Slot  Slot
	Cells []gridCell
	First bool // baris pertama di harinya
}

// boardUnit adalah satu kotak ruang+gelombang di papan pembagian.
type boardUnit struct {
	Room     Room
	Shift    int
	Count    int
	Over     bool
	Chips    []boardChip
	Proctors int
}

type boardChip struct {
	ClassID, ClassName string
	Count              int
	Value              string // class|room|shift
}

type rosterRow struct {
	Unit     RosterUnit
	When     string
	Room     string
	Subjects string
	UserID   string
	Issues   []string
}

type eventDetailView struct {
	User      auth.Identity
	IsNew     bool
	Errors    map[string]string
	Event     Event
	Tab       string
	CanEdit   bool
	Form      EventForm
	Grades    []optionItem
	Summary   EventSummary
	Days      []optionItem
	Grid      []gridRow
	Subjects  []subjectOption
	Teachers  []optionItem
	Rooms     []Room
	EventRoom map[string]bool
	Board     []boardUnit
	Unplaced  []boardChip
	Targets   []optionItem
	Roster    []rosterRow
	Classes   []optionItem
	Plans     []RoomPlan
	Issued    int
	CardsOpen bool
	OpensAt   string
	Flash     string
	Error     string
	Zone      string
}

func (h *Handler) eventDetail(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	ctx := r.Context()
	ev, err := GetEvent(ctx, h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Agenda tidak ditemukan.")
		return
	}
	if err != nil {
		h.fail(w, r, "get event", err)
		return
	}
	tab := "ringkasan"
	for _, t := range []string{"jadwal", "ruang", "pengawas", "dokumen"} {
		if strings.HasSuffix(r.URL.Path, "/"+t) {
			tab = t
		}
	}
	loc := id.Location()
	v := eventDetailView{User: id, Event: ev, Tab: tab, CanEdit: sa.CanAny(ActAgenda, Write), Form: EventFormOf(ev),
		Flash: auth.PopFlash(ctx, h.sessions), Zone: clock.ZoneLabel(loc), EventRoom: map[string]bool{}}
	v.Grades = gradeOptions(v.Form.Grades)
	p, err := LoadEventPlan(ctx, h.db, ev)
	if err != nil {
		h.fail(w, r, "load event plan", err)
		return
	}
	if v.Summary, err = Summarize(ctx, h.db, p); err != nil {
		h.fail(w, r, "summarize event", err)
		return
	}
	roomName := map[string]Room{}
	for _, rm := range p.Rooms {
		roomName[rm.ID] = rm
		v.EventRoom[rm.ID] = true
	}
	teacherName := map[string]string{}
	for _, t := range p.Teachers {
		teacherName[t.ID] = t.Name
	}
	switch tab {
	case "jadwal":
		for d := ev.StartsOn; !d.After(ev.EndsOn); d = d.AddDate(0, 0, 1) {
			v.Days = append(v.Days, optionItem{Value: d.Format("2006-01-02"), Label: dayNames[d.Weekday()] + ", " + clock.FormatDateID(d)})
		}
		prev := ""
		for _, s := range p.Slots {
			row := gridRow{Slot: s, First: s.Day.Format("2006-01-02") != prev}
			prev = s.Day.Format("2006-01-02")
			for _, g := range ev.Grades {
				gc := gridCell{Grade: g, SlotID: s.ID}
				if c, ok := s.Cells[g]; ok {
					cc := c
					gc.Cell = &cc
				}
				row.Cells = append(row.Cells, gc)
			}
			v.Grid = append(v.Grid, row)
		}
		if v.Subjects, err = h.readableSubjects(r, sa, ""); err != nil {
			h.fail(w, r, "list subjects", err)
			return
		}
		for _, t := range p.Teachers {
			v.Teachers = append(v.Teachers, optionItem{Value: t.ID, Label: t.Name})
		}
	case "ruang":
		if v.Rooms, err = ListRooms(ctx, h.db, sa.SchoolID); err != nil {
			h.fail(w, r, "list rooms", err)
			return
		}
		type key struct {
			room  string
			shift int
		}
		chips := map[key]map[string]*boardChip{}
		counts := map[key]int{}
		unplaced := map[string]*boardChip{}
		var classOrder []string
		seenClass := map[string]bool{}
		for _, pt := range p.Participants {
			if !seenClass[pt.ClassID] {
				seenClass[pt.ClassID] = true
				classOrder = append(classOrder, pt.ClassID)
			}
			if pt.RoomID == "" {
				if unplaced[pt.ClassID] == nil {
					unplaced[pt.ClassID] = &boardChip{ClassID: pt.ClassID, ClassName: pt.ClassName, Value: pt.ClassID + "||0"}
				}
				unplaced[pt.ClassID].Count++
				continue
			}
			k := key{pt.RoomID, pt.Shift}
			counts[k]++
			if chips[k] == nil {
				chips[k] = map[string]*boardChip{}
			}
			if chips[k][pt.ClassID] == nil {
				chips[k][pt.ClassID] = &boardChip{ClassID: pt.ClassID, ClassName: pt.ClassName, Value: fmt.Sprintf("%s|%s|%d", pt.ClassID, pt.RoomID, pt.Shift)}
			}
			chips[k][pt.ClassID].Count++
		}
		for _, rm := range p.Rooms {
			maxShift := 1
			if rm.Kind == RoomLab {
				maxShift = ev.MaxShifts
			}
			for sh := 1; sh <= maxShift; sh++ {
				k := key{rm.ID, sh}
				if sh > 1 && counts[k] == 0 && counts[key{rm.ID, sh - 1}] < rm.Capacity {
					break
				}
				u := boardUnit{Room: rm, Shift: sh, Count: counts[k], Over: counts[k] > rm.Capacity}
				for _, cid := range classOrder {
					if c := chips[k][cid]; c != nil {
						u.Chips = append(u.Chips, *c)
					}
				}
				v.Board = append(v.Board, u)
				v.Targets = append(v.Targets, optionItem{Value: fmt.Sprintf("%s|%d", rm.ID, sh),
					Label: fmt.Sprintf("%s · gelombang %d (%d/%d)", rm.Name, sh, counts[k], rm.Capacity)})
			}
		}
		for _, cid := range classOrder {
			if c := unplaced[cid]; c != nil {
				v.Unplaced = append(v.Unplaced, *c)
			}
		}
	case "dokumen":
		seen := map[string]bool{}
		for _, pt := range p.Participants {
			if !seen[pt.ClassID] {
				seen[pt.ClassID] = true
				v.Classes = append(v.Classes, optionItem{Value: pt.ClassID, Label: pt.ClassName})
			}
		}
		v.Plans, _ = LoadRoomPlans(ctx, h.db, ev, "")
		var first *time.Time
		if err := h.db.QueryRow(ctx, `SELECT min(starts_at) FROM exam_sessions WHERE event_id = $1 AND cancelled_at IS NULL`, ev.ID).Scan(&first); err != nil {
			h.fail(w, r, "event first session", err)
			return
		}
		if first != nil {
			v.CardsOpen = first.Sub(time.Now()) <= CardLead
			v.OpensAt = clock.FormatDateTimeID(first.Add(-CardLead), loc)
		}
		_ = h.db.QueryRow(ctx, `SELECT count(DISTINCT c.student_id) FROM student_credentials c JOIN exam_sessions x ON x.id = c.exam_session_id
			WHERE x.event_id = $1 AND c.revoked_at IS NULL`, ev.ID).Scan(&v.Issued)
	case "pengawas":
		issues := map[string][]string{}
		for _, is := range p.Issues {
			switch is.Kind {
			case IssueOwnSubject:
				issues[is.UnitKey] = append(issues[is.UnitKey], "mengampu mapel yang diujikan")
			case IssueClash:
				issues[is.UnitKey] = append(issues[is.UnitKey], "bentrok dengan tugas lain")
			}
		}
		subjName := map[string]string{}
		for _, s := range p.Slots {
			for _, c := range s.Cells {
				subjName[c.SubjectID] = c.SubjectName
			}
		}
		units := append([]RosterUnit(nil), p.Units...)
		sort.SliceStable(units, func(i, j int) bool {
			if !units[i].Start.Equal(units[j].Start) {
				return units[i].Start.Before(units[j].Start)
			}
			return roomName[units[i].RoomID].Name < roomName[units[j].RoomID].Name
		})
		for _, u := range units {
			var names []string
			for _, s := range u.Subjects {
				names = append(names, subjName[s])
			}
			a, b := u.Start.In(loc), u.End.In(loc)
			row := rosterRow{Unit: u, Room: roomName[u.RoomID].Name, Subjects: strings.Join(names, ", "), UserID: p.Assign[u.Key],
				When: dayNames[a.Weekday()] + " " + clock.FormatDateID(a) + " · " + a.Format("15.04") + "–" + b.Format("15.04"), Issues: issues[u.Key]}
			if u.Shift > 1 {
				row.Room += " · gel. " + strconv.Itoa(u.Shift)
			}
			v.Roster = append(v.Roster, row)
		}
		for _, t := range p.Teachers {
			v.Teachers = append(v.Teachers, optionItem{Value: t.ID, Label: t.Name})
		}
	}
	h.render.Render(w, r, http.StatusOK, "exam/event_detail", v)
}

func (h *Handler) eventAddSlot(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/jadwal"
	f := SlotForm{Day: r.PostForm.Get("day"), Start: r.PostForm.Get("start"), End: r.PostForm.Get("end")}
	if err := AddSlot(r.Context(), h.db, sa, ev.ID, f); err != nil {
		h.eventFail(w, r, back, "add slot", err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventRemoveSlot(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/jadwal"
	if err := RemoveSlot(r.Context(), h.db, sa, ev.ID, chi.URLParam(r, "slot")); err != nil {
		h.eventFail(w, r, back, "remove slot", err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventSetCell(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/jadwal"
	g, _ := strconv.Atoi(r.PostForm.Get("grade"))
	if err := SetCell(r.Context(), h.db, sa, ev.ID, r.PostForm.Get("slot_id"), g, r.PostForm.Get("subject_id")); err != nil {
		h.eventFail(w, r, back, "set cell", err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventCellExam(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	e, err := CreateCellExam(r.Context(), h.db, sa, ev.ID, chi.URLParam(r, "cell"), r.PostForm.Get("owner_id"), time.Now())
	if err != nil {
		h.eventFail(w, r, eventPath(ev.ID)+"/jadwal", "create cell exam", err)
		return
	}
	h.flash(r, "Ujian dibuat. Guru penyusun bisa mengisi soalnya dari menu Ujian.")
	http.Redirect(w, r, eventPath(ev.ID)+"/jadwal#sel-"+chi.URLParam(r, "cell")+"-"+e.ID[:8], http.StatusSeeOther)
}

func (h *Handler) eventSetRooms(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/ruang"
	if err := SetEventRooms(r.Context(), h.db, sa, ev.ID, r.PostForm["room_id"]); err != nil {
		h.eventFail(w, r, back, "set event rooms", err)
		return
	}
	h.flash(r, "Ruang agenda disimpan.")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventAllocate(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/ruang"
	a, err := AllocateEvent(r.Context(), h.db, sa, ev.ID)
	if err != nil {
		h.eventFail(w, r, back, "allocate event", err)
		return
	}
	msg := fmt.Sprintf("%d peserta dibagi ke ruang dan gelombang.", len(a.Places))
	if len(a.Split) > 0 {
		msg += fmt.Sprintf(" %d kelas dipecah karena tidak muat di satu ruang.", len(a.Split))
	}
	if len(a.Unplaced) > 0 {
		msg += fmt.Sprintf(" %d peserta belum mendapat ruang: tambah ruang atau gelombang lab.", len(a.Unplaced))
	}
	h.flash(r, msg)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventMove(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/ruang"
	m := MoveRequest{StudentID: r.PostForm.Get("student_id")}
	if from := strings.Split(r.PostForm.Get("from"), "|"); len(from) == 3 {
		m.ClassID, m.FromRoom = from[0], from[1]
		m.FromShift, _ = strconv.Atoi(from[2])
	}
	if to := strings.Split(r.PostForm.Get("to"), "|"); len(to) == 2 {
		m.ToRoom = to[0]
		m.ToShift, _ = strconv.Atoi(to[1])
	}
	n, err := MoveParticipants(r.Context(), h.db, sa, ev.ID, m)
	if errors.Is(err, ErrNotFound) {
		h.flash(r, "Pilih kelas asal dan ruang tujuan.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if err != nil {
		h.eventFail(w, r, back, "move participants", err)
		return
	}
	h.flash(r, fmt.Sprintf("%d peserta dipindah.", n))
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventAutoProctors(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/pengawas"
	ro, err := AutoAssignProctors(r.Context(), h.db, sa, ev.ID, r.PostForm.Get("ulang") == "")
	if err != nil {
		h.eventFail(w, r, back, "auto proctors", err)
		return
	}
	msg := fmt.Sprintf("Pengawas dibagi ke %d tugas.", len(ro.Assign))
	if len(ro.Unfilled) > 0 {
		msg += fmt.Sprintf(" %d tugas belum mendapat pengawas: tidak ada guru yang bebas dan tidak mengampu mapelnya.", len(ro.Unfilled))
	}
	h.flash(r, msg)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventSetProctor(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	back := eventPath(ev.ID) + "/pengawas"
	if err := SetProctor(r.Context(), h.db, sa, ev.ID, r.PostForm.Get("unit"), r.PostForm.Get("user_id")); err != nil {
		h.eventFail(w, r, back, "set proctor", err)
		return
	}
	h.flash(r, "Pengawas disimpan.")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) eventApply(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	res, err := ApplyEventSessions(r.Context(), h.db, sa, ev.ID, time.Now())
	if err != nil {
		h.eventFail(w, r, eventPath(ev.ID), "apply event", err)
		return
	}
	h.flash(r, fmt.Sprintf("Sesi diperbarui: %d dibuat, %d diubah, %d dibatalkan.", res.Created, res.Updated, res.Cancelled))
	http.Redirect(w, r, eventPath(ev.ID), http.StatusSeeOther)
}

func (h *Handler) eventAnnounce(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	on := r.PostForm.Get("on") != "0"
	if err := AnnounceEvent(r.Context(), h.db, sa, ev.ID, on); err != nil {
		h.eventFail(w, r, eventPath(ev.ID), "announce event", err)
		return
	}
	if on {
		h.flash(r, "Jadwal diumumkan. Siswa melihatnya di beranda peserta.")
	} else {
		h.flash(r, "Pengumuman jadwal ditarik.")
	}
	http.Redirect(w, r, eventPath(ev.ID), http.StatusSeeOther)
}

func (h *Handler) eventArchive(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	if err := ArchiveEvent(r.Context(), h.db, sa, ev.ID, time.Now()); err != nil {
		h.eventFail(w, r, eventPath(ev.ID), "archive event", err)
		return
	}
	h.flash(r, "Agenda diarsipkan.")
	http.Redirect(w, r, "/ujian/agenda", http.StatusSeeOther)
}

type roomsView struct {
	User    auth.Identity
	Rooms   []Room
	CanEdit bool
	Form    RoomForm
	Errors  map[string]string
	Flash   string
	Error   string
}

func (h *Handler) rooms(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	h.renderRooms(w, r, http.StatusOK, roomsView{User: id, CanEdit: sa.CanAny(ActAgenda, Write), Form: RoomForm{Kind: RoomClass, Capacity: "36"}})
}

func (h *Handler) renderRooms(w http.ResponseWriter, r *http.Request, status int, v roomsView) {
	_, sa := h.staff(r)
	rooms, err := ListRooms(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "list rooms", err)
		return
	}
	v.Rooms = rooms
	if v.Flash == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	if v.Errors == nil {
		v.Errors = map[string]string{}
	}
	h.render.Render(w, r, status, "exam/rooms", v)
}

func (h *Handler) roomSave(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 16<<10) {
		return
	}
	id, sa := h.staff(r)
	f := RoomForm{Name: r.PostForm.Get("name"), Kind: r.PostForm.Get("kind"), Capacity: r.PostForm.Get("capacity")}
	_, err := SaveRoom(r.Context(), h.db, sa, chi.URLParam(r, "id"), f)
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderRooms(w, r, http.StatusUnprocessableEntity, roomsView{User: id, CanEdit: true, Form: f, Errors: fe.Fields,
			Error: "Ruang belum tersimpan. Periksa isian yang ditandai."})
		return
	}
	if err != nil {
		h.eventFail(w, r, "/ujian/ruang", "save room", err)
		return
	}
	h.flash(r, "Ruang disimpan.")
	http.Redirect(w, r, "/ujian/ruang", http.StatusSeeOther)
}

func (h *Handler) roomArchive(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 4<<10) {
		return
	}
	_, sa := h.staff(r)
	if err := ArchiveRoom(r.Context(), h.db, sa, chi.URLParam(r, "id")); err != nil {
		h.eventFail(w, r, "/ujian/ruang", "archive room", err)
		return
	}
	h.flash(r, "Ruang diarsipkan.")
	http.Redirect(w, r, "/ujian/ruang", http.StatusSeeOther)
}
