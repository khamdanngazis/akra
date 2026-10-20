package schedule

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	schedulePath = "/jadwal"
	maxSlotForm  = 8 << 10
	msgClassGone = "Kelas tidak ditemukan di tahun ajaran aktif sekolah Anda."
	msgSlotGone  = "Slot jadwal tidak ditemukan."
	msgSubjGone  = "Mata pelajaran tidak ditemukan di sekolah Anda."
)

// Handler melayani halaman jadwal.
type Handler struct {
	svc      *Service
	db       database.DBTX
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, db database.DBTX, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, db: db, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route jadwal. Melihat jadwal kelas: admin_tu dan
// kepala_sekolah. Mengubah: admin_tu. Jadwal mengajar sendiri: guru.
func (h *Handler) Routes(r chi.Router) {
	r.Route(schedulePath, func(r chi.Router) {
		view := auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah)
		write := auth.RequireRole(auth.RoleAdminTU)
		// Peran: admin_tu, kepala_sekolah.
		r.With(view).Get("/", h.class)
		// Peran: guru (hanya jadwal mengajarnya sendiri).
		r.With(auth.RequireRole(auth.RoleGuru)).Get("/saya", h.mine)
		// Peran: admin_tu.
		r.With(write).Post("/kelas/{classID}", h.create)
		// Peran: admin_tu.
		r.With(write).Get("/slot/{id}/ubah", h.edit)
		// Peran: admin_tu.
		r.With(write).Post("/slot/{id}", h.update)
		// Peran: admin_tu.
		r.With(write).Post("/slot/{id}/hapus", h.delete)
	})
}

type dayOption struct {
	Value int
	Name  string
}

type classView struct {
	User     auth.Identity
	CanEdit  bool
	NoYear   bool
	YearName string
	Classes  []classes.Class
	Page     ClassPage
	Week     WeekView
	DayOpts  []dayOption
	Form     Form
	Errors   map[string]string
	Flash    string
	Error    string
}

func dayOptions() []dayOption {
	var out []dayOption
	for _, d := range Days() {
		out = append(out, dayOption{d, DayName(d)})
	}
	return out
}

func (h *Handler) class(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.showClass(w, r, id, http.StatusOK, r.URL.Query().Get("kelas"), Form{Day: "1"}, nil, "")
}

// showClass menampilkan jadwal kelas classID (kosong = kelas pertama tahun
// ajaran aktif) dengan form tambah slot berisi f.
func (h *Handler) showClass(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, classID string, f Form, errs map[string]string, errMsg string) {
	v := classView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), DayOpts: dayOptions(), Form: f, Errors: errs, Error: errMsg}
	if errMsg == "" && errs == nil {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	year, err := school.GetActiveAcademicYear(r.Context(), h.db, id.SchoolID)
	if errors.Is(err, school.ErrNotFound) {
		v.NoYear = true
		h.render.Render(w, r, status, "schedule/class", v)
		return
	}
	if err != nil {
		h.serverError(w, r, "active year", err)
		return
	}
	v.YearName = year.Name
	if v.Classes, err = classes.ListByYear(r.Context(), h.db, id.SchoolID, year.ID); err != nil {
		h.serverError(w, r, "list classes", err)
		return
	}
	if classID == "" && len(v.Classes) > 0 {
		classID = v.Classes[0].ID
	}
	if classID != "" {
		v.Page, err = h.svc.Class(r.Context(), id.SchoolID, classID)
		if errors.Is(err, ErrClassNotFound) {
			page.NotFound(h.render, w, r, id, msgClassGone)
			return
		}
		if err != nil {
			h.serverError(w, r, "class schedule", err)
			return
		}
	}
	v.Week = WeekView{Days: GroupByDay(v.Page.Slots), Today: TodayTab(time.Now()), ShowTeacher: true, CanEdit: v.CanEdit}
	h.render.Render(w, r, status, "schedule/class", v)
}

type mineView struct {
	User  auth.Identity
	Week  WeekView
	Flash string
	Error string
}

// mine menampilkan jadwal mengajar guru yang login di tahun ajaran aktif.
// ID guru selalu dari sesi, jadi guru tidak bisa melihat jadwal guru lain.
func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	days, err := TeacherWeek(r.Context(), h.db, id.SchoolID, id.UserID)
	if err != nil {
		h.serverError(w, r, "teacher schedule", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "schedule/week", mineView{User: id, Week: WeekView{Days: days, Today: TodayTab(time.Now()), ShowClass: true},
		Flash: auth.PopFlash(r.Context(), h.sessions)})
}

func parseForm(w http.ResponseWriter, r *http.Request, id auth.Identity, rd page.Renderer) (Form, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSlotForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(rd, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return Form{}, false
	}
	p := r.PostForm
	return Form{SubjectID: p.Get("subject_id"), Day: p.Get("day"), Start: p.Get("start"), End: p.Get("end")}, true
}

// formErrors memetakan error service ke pesan form; ok false jika error lain.
func formErrors(err error) (fields map[string]string, msg string, ok bool) {
	var ve *ValidationError
	var ce *ConflictError
	switch {
	case errors.As(err, &ve):
		return ve.Fields, "Slot belum disimpan. Periksa isian yang ditandai.", true
	case errors.As(err, &ce):
		return map[string]string{"start": ce.Message()}, ce.Message(), true
	case errors.Is(err, ErrOverlap):
		m := "Slot bentrok dengan jadwal lain yang baru saja disimpan. Muat ulang halaman lalu coba lagi."
		return map[string]string{"start": m}, m, true
	}
	return nil, "", false
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	classID := chi.URLParam(r, "classID")
	f, ok := parseForm(w, r, id, h.render)
	if !ok {
		return
	}
	slot, err := h.svc.Create(r.Context(), actor(r, id), classID, f)
	switch {
	case errors.Is(err, ErrClassNotFound):
		page.NotFound(h.render, w, r, id, msgClassGone)
		return
	case errors.Is(err, ErrSubjectNotFound):
		page.NotFound(h.render, w, r, id, msgSubjGone)
		return
	}
	if fields, msg, ok := formErrors(err); ok {
		h.showClass(w, r, id, http.StatusUnprocessableEntity, classID, f, fields, msg)
		return
	}
	if err != nil {
		h.serverError(w, r, "create slot", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Slot "+slot.SubjectName+" "+DayName(slot.Day)+" "+slot.Start.String()+"–"+slot.End.String()+" ditambahkan.")
	http.Redirect(w, r, schedulePath+"?kelas="+url.QueryEscape(classID), http.StatusSeeOther)
}

type editView struct {
	User    auth.Identity
	Slot    Slot
	Options []ClassOption
	DayOpts []dayOption
	Form    Form
	Errors  map[string]string
	Error   string
	Flash   string
}

func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	slot, err := h.svc.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrSlotNotFound) {
		page.NotFound(h.render, w, r, id, msgSlotGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get slot", err)
		return
	}
	h.showEdit(w, r, id, http.StatusOK, slot, FormOf(slot), nil, "")
}

func (h *Handler) showEdit(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, slot Slot, f Form, errs map[string]string, msg string) {
	cp, err := h.svc.Class(r.Context(), id.SchoolID, slot.ClassID)
	if errors.Is(err, ErrClassNotFound) {
		page.NotFound(h.render, w, r, id, msgClassGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "class schedule", err)
		return
	}
	h.render.Render(w, r, status, "schedule/form", editView{User: id, Slot: slot, Options: cp.Options, DayOpts: dayOptions(), Form: f, Errors: errs, Error: msg})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	slotID := chi.URLParam(r, "id")
	f, ok := parseForm(w, r, id, h.render)
	if !ok {
		return
	}
	slot, err := h.svc.Update(r.Context(), actor(r, id), slotID, f)
	switch {
	case errors.Is(err, ErrSlotNotFound):
		page.NotFound(h.render, w, r, id, msgSlotGone)
		return
	case errors.Is(err, ErrClassNotFound):
		page.NotFound(h.render, w, r, id, msgClassGone)
		return
	case errors.Is(err, ErrSubjectNotFound):
		page.NotFound(h.render, w, r, id, msgSubjGone)
		return
	}
	if fields, msg, ok := formErrors(err); ok {
		before, gerr := h.svc.Get(r.Context(), id.SchoolID, slotID)
		if gerr != nil {
			h.serverError(w, r, "get slot", gerr)
			return
		}
		h.showEdit(w, r, id, http.StatusUnprocessableEntity, before, f, fields, msg)
		return
	}
	if err != nil {
		h.serverError(w, r, "update slot", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Slot "+slot.SubjectName+" disimpan.")
	http.Redirect(w, r, schedulePath+"?kelas="+url.QueryEscape(slot.ClassID), http.StatusSeeOther)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	slot, err := h.svc.Delete(r.Context(), actor(r, id), chi.URLParam(r, "id"))
	if errors.Is(err, ErrSlotNotFound) {
		page.NotFound(h.render, w, r, id, msgSlotGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "delete slot", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Slot "+slot.SubjectName+" "+DayName(slot.Day)+" "+slot.Start.String()+"–"+slot.End.String()+" dihapus.")
	http.Redirect(w, r, schedulePath+"?kelas="+url.QueryEscape(slot.ClassID), http.StatusSeeOther)
}

func actor(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
