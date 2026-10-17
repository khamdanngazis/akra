package classes

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	classesPath     = "/kelas"
	maxClassForm    = 16 << 10
	pageIndex       = "classes/index"
	pageForm        = "classes/form"
	msgClassMissing = "Kelas yang Anda cari tidak ada atau bukan milik sekolah Anda."
	msgYearMissing  = "Tahun ajaran yang Anda pilih tidak ada atau bukan milik sekolah Anda."
)

// Handler melayani halaman kelas.
type Handler struct {
	svc      *Service
	db       database.DBTX // untuk membaca daftar tahun ajaran
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, db database.DBTX, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, db: db, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route kelas ke r.
func (h *Handler) Routes(r chi.Router) {
	r.Route(classesPath, func(r chi.Router) {
		// Peran: admin_tu, kepala_sekolah (hanya baca).
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah)).Get("/", h.index)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU))
			// Peran: admin_tu.
			r.Get("/baru", h.newForm)
			// Peran: admin_tu.
			r.Post("/", h.create)
			// Peran: admin_tu.
			r.Get("/{id}/ubah", h.editForm)
			// Peran: admin_tu.
			r.Post("/{id}", h.update)
		})
	})
}

type yearOption struct {
	ID       string
	Name     string
	IsActive bool
	Selected bool
}

type teacherOption struct {
	ID       string
	Label    string
	Selected bool
}

type indexView struct {
	User     auth.Identity
	Years    []yearOption
	Year     *school.AcademicYear // nil jika sekolah belum punya tahun ajaran
	Classes  []Class
	CanEdit  bool
	Flash    string
	Error    string
	NewClass string // URL tambah kelas untuk tahun terpilih
}

type formView struct {
	User     auth.Identity
	IsNew    bool
	ID       string
	Form     Form
	Years    []yearOption // hanya saat membuat
	YearName string       // hanya saat mengubah
	Teachers []teacherOption
	Errors   map[string]string
	Error    string
	Flash    string
	BackURL  string
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	years, err := school.ListAcademicYears(r.Context(), h.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list academic years", err)
		return
	}
	view := indexView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Flash: auth.PopFlash(r.Context(), h.sessions)}

	selected, ok := pickYear(years, r.URL.Query().Get("tahun"))
	if !ok {
		page.NotFound(h.render, w, r, id, msgYearMissing)
		return
	}
	if selected != nil {
		view.Year = selected
		view.Years = yearOptions(years, selected.ID)
		view.NewClass = classesPath + "/baru?tahun=" + url.QueryEscape(selected.ID)
		if view.Classes, err = h.svc.List(r.Context(), id.SchoolID, selected.ID); err != nil {
			h.serverError(w, r, "list classes", err)
			return
		}
	}
	h.render.Render(w, r, http.StatusOK, pageIndex, view)
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	view, ok := h.newFormView(w, r, id, Form{AcademicYearID: r.URL.Query().Get("tahun")})
	if !ok {
		return
	}
	h.render.Render(w, r, http.StatusOK, pageForm, view)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := parseForm(w, r)
	if !ok {
		return
	}
	c, err := h.svc.Create(r.Context(), actorFrom(r, id), f)
	if err != nil {
		view, ok := h.newFormView(w, r, id, f)
		if ok {
			h.formError(w, r, err, view)
		}
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Kelas "+c.Name+" berhasil dibuat.")
	http.Redirect(w, r, yearURL(c.AcademicYearID), http.StatusSeeOther)
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	c, err := h.svc.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgClassMissing)
		return
	}
	if err != nil {
		h.serverError(w, r, "get class", err)
		return
	}
	view, ok := h.editFormView(w, r, id, c, Form{
		Name: c.Name, GradeLevel: strconv.Itoa(c.GradeLevel), HomeroomTeacherID: c.HomeroomTeacherID,
	})
	if !ok {
		return
	}
	h.render.Render(w, r, http.StatusOK, pageForm, view)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := parseForm(w, r)
	if !ok {
		return
	}
	c, err := h.svc.Update(r.Context(), actorFrom(r, id), chi.URLParam(r, "id"), f)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			page.NotFound(h.render, w, r, id, msgClassMissing)
			return
		}
		current, getErr := h.svc.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
		if getErr != nil {
			h.serverError(w, r, "get class", getErr)
			return
		}
		if view, ok := h.editFormView(w, r, id, current, f); ok {
			h.formError(w, r, err, view)
		}
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Kelas "+c.Name+" berhasil diubah.")
	http.Redirect(w, r, yearURL(c.AcademicYearID), http.StatusSeeOther)
}

func (h *Handler) newFormView(w http.ResponseWriter, r *http.Request, id auth.Identity, f Form) (formView, bool) {
	years, err := school.ListAcademicYears(r.Context(), h.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list academic years", err)
		return formView{}, false
	}
	selected, ok := pickYear(years, f.AcademicYearID)
	if !ok {
		selected, _ = pickYear(years, "")
	}
	if selected != nil {
		f.AcademicYearID = selected.ID
	}
	teachers, ok := h.teacherOptions(w, r, id, f.HomeroomTeacherID, "", "")
	if !ok {
		return formView{}, false
	}
	return formView{
		User: id, IsNew: true, Form: f, Years: yearOptions(years, f.AcademicYearID),
		Teachers: teachers, BackURL: yearURL(f.AcademicYearID),
	}, true
}

func (h *Handler) editFormView(w http.ResponseWriter, r *http.Request, id auth.Identity, c Class, f Form) (formView, bool) {
	year, err := school.GetAcademicYear(r.Context(), h.db, id.SchoolID, c.AcademicYearID)
	if err != nil {
		h.serverError(w, r, "get academic year", err)
		return formView{}, false
	}
	teachers, ok := h.teacherOptions(w, r, id, f.HomeroomTeacherID, c.HomeroomTeacherID, c.HomeroomTeacherName)
	if !ok {
		return formView{}, false
	}
	return formView{
		User: id, ID: c.ID, Form: f, YearName: year.Name, Teachers: teachers, BackURL: yearURL(c.AcademicYearID),
	}, true
}

// teacherOptions menyusun pilihan wali kelas dari guru aktif. Wali kelas
// saat ini yang sudah tidak aktif tetap ditampilkan (bertanda nonaktif) agar
// menyimpan form tanpa menggantinya tidak diam-diam menghapusnya.
func (h *Handler) teacherOptions(w http.ResponseWriter, r *http.Request, id auth.Identity, selectedID, currentID, currentName string) ([]teacherOption, bool) {
	teachers, err := h.svc.ListTeachers(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list teachers", err)
		return nil, false
	}
	opts := make([]teacherOption, 0, len(teachers)+1)
	currentListed := currentID == ""
	for _, t := range teachers {
		opts = append(opts, teacherOption{ID: t.ID, Label: t.FullName, Selected: t.ID == selectedID})
		if t.ID == currentID {
			currentListed = true
		}
	}
	if !currentListed {
		opts = append(opts, teacherOption{ID: currentID, Label: currentName + " (nonaktif)", Selected: currentID == selectedID})
	}
	return opts, true
}

func (h *Handler) formError(w http.ResponseWriter, r *http.Request, err error, view formView) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		view.Errors = ve.Fields
		h.render.Render(w, r, http.StatusBadRequest, pageForm, view)
		return
	}
	h.serverError(w, r, "save class", err)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func parseForm(w http.ResponseWriter, r *http.Request) (Form, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxClassForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return Form{}, false
	}
	return Form{
		AcademicYearID:    r.PostForm.Get("academic_year_id"),
		Name:              r.PostForm.Get("name"),
		GradeLevel:        r.PostForm.Get("grade_level"),
		HomeroomTeacherID: r.PostForm.Get("homeroom_teacher_id"),
	}, true
}

// pickYear memilih tahun ajaran dari years. param kosong berarti tahun aktif,
// atau tahun terbaru jika belum ada yang aktif. ok bernilai false jika param
// diisi tetapi tidak ada di years (milik sekolah lain atau tidak valid).
func pickYear(years []school.AcademicYear, param string) (*school.AcademicYear, bool) {
	if param != "" {
		for i := range years {
			if years[i].ID == param {
				return &years[i], true
			}
		}
		return nil, false
	}
	for i := range years {
		if years[i].IsActive {
			return &years[i], true
		}
	}
	if len(years) > 0 {
		return &years[0], true
	}
	return nil, true
}

func yearOptions(years []school.AcademicYear, selectedID string) []yearOption {
	opts := make([]yearOption, len(years))
	for i, y := range years {
		opts[i] = yearOption{ID: y.ID, Name: y.Name, IsActive: y.IsActive, Selected: y.ID == selectedID}
	}
	return opts
}

func yearURL(yearID string) string {
	if yearID == "" {
		return classesPath
	}
	return classesPath + "?tahun=" + url.QueryEscape(yearID)
}

func actorFrom(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
