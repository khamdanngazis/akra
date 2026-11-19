package exam

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// controlRoutes mendaftarkan pusat kendali (T144) di grup agenda: baca
// untuk koordinator, admin_tu, dan kepala sekolah; aksi untuk koordinator
// dan admin_tu.
func (h *Handler) controlRoutes(readers, writers chi.Router) {
	readers.Get("/agenda/{id}/kendali", h.controlPage)
	readers.Get("/agenda/{id}/kendali/data", h.controlData)
	writers.Post("/agenda/{id}/kendali/aksi", h.controlAction)
	writers.Post("/agenda/{id}/kendali/pindah", h.controlMove)
	writers.Post("/agenda/{id}/kendali/pengawas", h.controlProctor)
}

func controlPath(eventID string) string { return "/ujian/agenda/" + eventID + "/kendali" }

// controlCard adalah satu kartu ruang untuk tampilan.
type controlCard struct {
	ControlUnit
	When      string
	Remaining string
	Proctor   string // nama pengawas atau "Belum ada pengawas"
}

type controlLive struct {
	Cards     []controlCard
	Counts    ControlCounts
	Health    Health
	UpdatedAt int64
	CanWrite  bool
	EventID   string
}

type controlView struct {
	User         auth.Identity
	Event        Event
	Day          string
	Zone         string
	Live         controlLive
	Recap        DailyRecap
	Participants []ControlParticipant
	Targets      []MoveTarget
	Teachers     []optionItem
	Exams        []optionItem // ujian hari ini (ralat/batalkan soal)
	CanWrite     bool
	Flash        string
	Error        string
	Today        bool
	MaxMessage   int
}

// controlEvent memuat agenda dari URL; menulis 404 bila tidak ada.
func (h *Handler) controlEvent(w http.ResponseWriter, r *http.Request) (auth.Identity, StaffActor, Event, bool) {
	id, sa := h.staff(r)
	ev, err := GetEvent(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Agenda tidak ditemukan.")
		return id, sa, ev, false
	}
	if err != nil {
		h.fail(w, r, "get event", err)
		return id, sa, ev, false
	}
	return id, sa, ev, true
}

// controlLiveData memuat kartu ruang hari ini dan indikator kesehatan.
func (h *Handler) controlLiveData(ctx context.Context, sa StaffActor, ev Event, loc *time.Location, now time.Time) (controlLive, []ControlSession, error) {
	from, to := DayBounds(now, loc)
	sessions, err := LoadControlSessions(ctx, h.db, sa.SchoolID, ev.ID, from, to, now)
	if err != nil {
		return controlLive{}, nil, err
	}
	units := BuildControlUnits(sessions, now)
	live := controlLive{Counts: CountUnits(units), Health: SaveHealth.Snapshot(sa.SchoolID, now), UpdatedAt: now.UnixMilli(),
		CanWrite: sa.CanAny(ActAgenda, Write), EventID: ev.ID}
	for _, u := range units {
		a, b := u.StartsAt.In(loc), u.EndsAt.In(loc)
		c := controlCard{ControlUnit: u, When: a.Format("15.04") + "–" + b.Format("15.04"), Proctor: strings.Join(u.Proctors, ", ")}
		if u.Shift > 1 {
			c.When += " · gel. " + strconv.Itoa(u.Shift)
		}
		if c.Proctor == "" {
			c.Proctor = "Belum ada pengawas"
		}
		if u.Phase == "berlangsung" {
			c.Remaining = clockLeft(u.EndsAt.Sub(now))
		}
		live.Cards = append(live.Cards, c)
	}
	return live, sessions, nil
}

func (h *Handler) controlPage(w http.ResponseWriter, r *http.Request) {
	id, sa, ev, ok := h.controlEvent(w, r)
	if !ok {
		return
	}
	ctx, now, loc := r.Context(), time.Now(), id.Location()
	live, sessions, err := h.controlLiveData(ctx, sa, ev, loc, now)
	if err != nil {
		h.fail(w, r, "control live", err)
		return
	}
	today := now.In(loc)
	v := controlView{User: id, Event: ev, Live: live, CanWrite: live.CanWrite, Flash: auth.PopFlash(ctx, h.sessions),
		Zone: clock.ZoneLabel(loc), Day: dayNames[today.Weekday()] + ", " + clock.FormatDateID(today), MaxMessage: MaxBroadcastLen}
	d := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	v.Today = !d.Before(ev.StartsOn) && !d.After(ev.EndsOn)
	if v.Recap, err = LoadDailyRecap(ctx, h.db, sa.SchoolID, ev.ID, sessions, now); err != nil {
		h.fail(w, r, "daily recap", err)
		return
	}
	for i := range v.Recap.Unsigned {
		t := v.Recap.Unsigned[i].StartsAt.In(loc)
		v.Recap.Unsigned[i].When = dayNames[t.Weekday()] + " " + clock.FormatDateID(t) + " " + t.Format("15.04")
	}
	if v.CanWrite {
		if v.Participants, v.Targets, err = LoadMoveOptions(ctx, h.db, sa.SchoolID, sessions, now); err != nil {
			h.fail(w, r, "move options", err)
			return
		}
		ts, err := rosterTeachers(ctx, h.db, ev)
		if err != nil {
			h.fail(w, r, "roster teachers", err)
			return
		}
		for _, t := range ts {
			v.Teachers = append(v.Teachers, optionItem{Value: t.ID, Label: t.Name})
		}
	}
	seen := map[string]bool{}
	for _, s := range sessions {
		if !seen[s.ExamID] {
			seen[s.ExamID] = true
			v.Exams = append(v.Exams, optionItem{Value: s.ExamID, Label: s.ExamName})
		}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	h.render.Render(w, r, http.StatusOK, "exam/control", v)
}

func (h *Handler) controlData(w http.ResponseWriter, r *http.Request) {
	id, sa, ev, ok := h.controlEvent(w, r)
	if !ok {
		return
	}
	live, _, err := h.controlLiveData(r.Context(), sa, ev, id.Location(), time.Now())
	if err != nil {
		h.fail(w, r, "control live", err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	fr, ok := h.render.(fragmentRenderer)
	if !ok {
		http.Error(w, "fragment renderer unavailable", http.StatusInternalServerError)
		return
	}
	fr.RenderFragment(w, r, http.StatusOK, "exam/control", "control_live", controlView{Event: ev, Live: live, CanWrite: live.CanWrite})
}

// controlMessage menerjemahkan error aksi pusat kendali menjadi pesan.
func controlMessage(err error) string {
	switch {
	case errors.Is(err, ErrBadMessage):
		return "Isi pesan untuk peserta (paling banyak 300 karakter)."
	case errors.Is(err, ErrMoveTarget):
		return "Sesi tujuan harus ujian yang sama, belum berakhir, dan belum ditutup."
	case errors.Is(err, ErrMoveDuplicate):
		return "Peserta ini sudah terdaftar di sesi tujuan."
	case errors.Is(err, ErrBadProctor):
		return "Pilih guru aktif sebagai pengawas pengganti."
	}
	return actionMessage(err)
}

func (h *Handler) controlDone(w http.ResponseWriter, r *http.Request, eventID, msg string, err error) {
	if err != nil {
		m := controlMessage(err)
		if m == "" {
			h.fail(w, r, "control action", err)
			return
		}
		msg = m
	}
	h.flash(r, msg)
	http.Redirect(w, r, controlPath(eventID), http.StatusSeeOther)
}

// controlAction menjalankan aksi lintas ruang untuk kartu yang dipilih
// (atau semua sesi berlangsung): jeda, lanjut, tambah waktu, pesan.
func (h *Handler) controlAction(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	ctx, now := r.Context(), time.Now()
	f := r.PostForm
	var ids []string
	var err error
	if f.Get("target") == "semua" {
		ids, err = RunningEventSessions(ctx, h.db, sa.SchoolID, ev.ID, now)
	} else {
		ids, err = EventSessionIDs(ctx, h.db, sa.SchoolID, ev.ID, f["sid"])
	}
	if err != nil {
		h.fail(w, r, "control targets", err)
		return
	}
	if len(ids) == 0 {
		h.controlDone(w, r, ev.ID, "Pilih ruang dulu (centang kartunya), atau pilih semua sesi yang berlangsung.", nil)
		return
	}
	aksi := f.Get("aksi")
	reason := strings.TrimSpace(f.Get("alasan"))
	var msg string
	switch aksi {
	case "pesan":
		var n int
		n, err = SendBroadcast(ctx, h.db, sa, ids, f.Get("pesan"), now)
		msg = "Pesan dikirim ke " + strconv.Itoa(n) + " sesi. Muncul di layar peserta dalam beberapa detik."
	case "jeda", "lanjut", "waktu":
		mins, _ := strconv.Atoi(f.Get("menit"))
		if aksi == "waktu" && (mins < 1 || mins > 60) {
			err = ErrBadExtra
			break
		}
		total := 0
		for _, sid := range ids {
			var n int
			switch aksi {
			case "jeda":
				n, err = PauseSession(ctx, h.db, sa, sid, now)
			case "lanjut":
				n, err = ResumeSession(ctx, h.db, sa, sid, now)
			case "waktu":
				err = AddSessionTime(ctx, h.db, sa, sid, mins, now)
			}
			if err != nil {
				break
			}
			total += n
			body := map[string]string{"jeda": "Sesi dijeda oleh koordinator", "lanjut": "Sesi dilanjutkan oleh koordinator",
				"waktu": "Waktu ditambah " + strconv.Itoa(mins) + " menit oleh koordinator"}[aksi]
			if reason != "" {
				body += " (" + reason + ")"
			}
			if err = AddNote(ctx, h.db, sa, sid, "", body+"."); err != nil {
				break
			}
		}
		msg = map[string]string{
			"jeda":   strconv.Itoa(len(ids)) + " sesi dijeda. " + strconv.Itoa(total) + " peserta berhenti; layar mereka terkunci.",
			"lanjut": strconv.Itoa(len(ids)) + " sesi dilanjutkan. Lama jeda ditambahkan ke waktu " + strconv.Itoa(total) + " peserta.",
			"waktu":  "Waktu " + strconv.Itoa(len(ids)) + " sesi ditambah " + strconv.Itoa(mins) + " menit.",
		}[aksi]
	default:
		err = ErrNotFound
	}
	h.controlDone(w, r, ev.ID, msg, err)
}

// controlMove memindahkan satu peserta ke sesi lain (komputer rusak).
func (h *Handler) controlMove(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	f := r.PostForm
	res, err := MoveParticipant(r.Context(), h.db, sa, ev.ID, f.Get("peserta"), f.Get("tujuan"), f.Get("alasan"), time.Now())
	msg := res.Name + " dipindah ke " + res.ToRoom + "."
	if res.Running {
		msg += " Jawaban dan sisa waktunya tetap; ia bisa masuk lagi dari perangkat baru dengan kode yang sama."
	}
	h.controlDone(w, r, ev.ID, msg, err)
}

// controlProctor mengganti pengawas kartu ruang.
func (h *Handler) controlProctor(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	f := r.PostForm
	name, err := ReplaceProctor(r.Context(), h.db, sa, ev.ID, f["sid"], f.Get("user_id"), f.Get("alasan"), time.Now())
	h.controlDone(w, r, ev.ID, "Pengawas diganti menjadi "+name+". Ia bisa langsung membuka layar pengawas dari beranda.", err)
}

func examSoalPath(examID string) string { return "/ujian/paket/" + examID + "?tab=soal" }

// examVoidQuestion membatalkan atau memulihkan satu soal ujian yang sedang
// atau sudah berlangsung (pembuat ujian, koordinator, admin_tu).
func (h *Handler) examVoidQuestion(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 8<<10) {
		return
	}
	_, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Write)
	if !ok {
		return
	}
	f := r.PostForm
	void := f.Get("aksi") != "pulihkan"
	n, err := VoidQuestion(r.Context(), h.db, sa, e, chi.URLParam(r, "qid"), f.Get("alasan"), void, f.Get("umumkan") == "1", time.Now())
	back := examSoalPath(e.ID)
	if p := f.Get("paket"); p != "" && len(p) == 1 {
		back += "&paket=" + p
	}
	switch {
	case errors.Is(err, ErrNotLocked):
		h.flash(r, "Soal hanya bisa dibatalkan setelah ujian dimulai (soal sudah dibekukan).")
	case errors.Is(err, ErrBadNote):
		h.flash(r, "Alasan paling banyak 300 karakter.")
	case err != nil:
		h.fail(w, r, "void question", err)
		return
	case !void:
		h.flash(r, "Soal dipulihkan dan kembali dinilai.")
	case n > 0:
		h.flash(r, "Soal dibatalkan dan tidak dinilai. Pemberitahuan dikirim ke "+strconv.Itoa(n)+" sesi yang berlangsung.")
	default:
		h.flash(r, "Soal dibatalkan dan tidak dinilai untuk semua peserta.")
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// examCorrection mengirim pesan ralat ke semua sesi ujian yang sedang
// berlangsung.
func (h *Handler) examCorrection(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 8<<10) {
		return
	}
	_, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Write)
	if !ok {
		return
	}
	ctx, now := r.Context(), time.Now()
	ids, err := RunningExamSessions(ctx, h.db, sa.SchoolID, e.ID, now)
	if err != nil {
		h.fail(w, r, "running exam sessions", err)
		return
	}
	if len(ids) == 0 {
		h.flash(r, "Tidak ada sesi ujian ini yang sedang berlangsung.")
		http.Redirect(w, r, examSoalPath(e.ID), http.StatusSeeOther)
		return
	}
	body := strings.TrimSpace(r.PostForm.Get("pesan"))
	if body == "" {
		body = " " // ditolak SendBroadcast sebagai pesan kosong
	} else {
		body = "Ralat: " + body
	}
	n, err := SendBroadcast(ctx, h.db, sa, ids, body, now)
	switch {
	case errors.Is(err, ErrBadMessage):
		h.flash(r, "Isi ralat (paling banyak 290 karakter).")
	case err != nil:
		h.fail(w, r, "send correction", err)
		return
	default:
		h.flash(r, "Ralat dikirim ke "+strconv.Itoa(n)+" sesi yang berlangsung dan dicatat di berita acara.")
	}
	http.Redirect(w, r, examSoalPath(e.ID), http.StatusSeeOther)
}
