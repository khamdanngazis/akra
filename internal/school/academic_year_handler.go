package school

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

const (
	academicYearsPath   = "/pengaturan/tahun-ajaran"
	maxAcademicYearForm = 16 << 10
)

// Renderer merender halaman dari web/templates.
type Renderer = page.Renderer

// AcademicYearHandler melayani halaman pengaturan tahun ajaran.
type AcademicYearHandler struct {
	svc      *AcademicYearService
	sessions *scs.SessionManager
	render   Renderer
	logger   *slog.Logger
}

// NewAcademicYearHandler mengembalikan AcademicYearHandler. Route-nya harus
// berada di dalam grup RequireLogin.
func NewAcademicYearHandler(svc *AcademicYearService, sessions *scs.SessionManager, render Renderer, logger *slog.Logger) *AcademicYearHandler {
	return &AcademicYearHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route tahun ajaran ke r.
func (h *AcademicYearHandler) Routes(r chi.Router) {
	r.Route(academicYearsPath, func(r chi.Router) {
		// Peran: admin_tu, kepala_sekolah (hanya baca).
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah)).Get("/", h.list)

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
			// Peran: admin_tu.
			r.Post("/{id}/aktifkan", h.activate)
		})
	})
}

type academicYearRow struct {
	ID        string
	Name      string
	StartDate string
	EndDate   string
	IsActive  bool
}

type academicYearListView struct {
	User    auth.Identity
	Years   []academicYearRow
	CanEdit bool
	Flash   string
	Error   string
}

type academicYearFormView struct {
	User   auth.Identity
	IsNew  bool
	ID     string
	Form   AcademicYearForm
	Errors map[string]string
	Error  string
	Flash  string
}

func (h *AcademicYearHandler) list(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	years, err := h.svc.List(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list academic years", err)
		return
	}
	view := academicYearListView{
		User:    id,
		CanEdit: id.HasRole(auth.RoleAdminTU),
		Flash:   auth.PopFlash(r.Context(), h.sessions),
	}
	for _, y := range years {
		view.Years = append(view.Years, academicYearRow{
			ID: y.ID, Name: y.Name, IsActive: y.IsActive,
			StartDate: clock.FormatDateID(y.StartDate), EndDate: clock.FormatDateID(y.EndDate),
		})
	}
	h.render.Render(w, r, http.StatusOK, "school/academic_years", view)
}

func (h *AcademicYearHandler) newForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.render.Render(w, r, http.StatusOK, "school/academic_year_form", academicYearFormView{User: id, IsNew: true})
}

func (h *AcademicYearHandler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	form, ok := h.parseForm(w, r)
	if !ok {
		return
	}
	y, err := h.svc.Create(r.Context(), actorFrom(r, id), form)
	if h.formError(w, r, err, academicYearFormView{User: id, IsNew: true, Form: form}) {
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Tahun ajaran "+y.Name+" berhasil dibuat.")
	http.Redirect(w, r, academicYearsPath, http.StatusSeeOther)
}

func (h *AcademicYearHandler) editForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	y, err := h.svc.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		h.notFound(w, r, id)
		return
	}
	if err != nil {
		h.serverError(w, r, "get academic year", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "school/academic_year_form", academicYearFormView{
		User: id, ID: y.ID,
		Form: AcademicYearForm{Name: y.Name, StartDate: y.StartDate.Format(dateLayout), EndDate: y.EndDate.Format(dateLayout)},
	})
}

func (h *AcademicYearHandler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	yearID := chi.URLParam(r, "id")
	form, ok := h.parseForm(w, r)
	if !ok {
		return
	}
	y, err := h.svc.Update(r.Context(), actorFrom(r, id), yearID, form)
	if h.formError(w, r, err, academicYearFormView{User: id, ID: yearID, Form: form}) {
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Tahun ajaran "+y.Name+" berhasil diubah.")
	http.Redirect(w, r, academicYearsPath, http.StatusSeeOther)
}

func (h *AcademicYearHandler) activate(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	y, changed, err := h.svc.Activate(r.Context(), actorFrom(r, id), chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		h.notFound(w, r, id)
		return
	}
	if err != nil {
		h.serverError(w, r, "activate academic year", err)
		return
	}
	msg := "Tahun ajaran " + y.Name + " sekarang aktif."
	if !changed {
		msg = "Tahun ajaran " + y.Name + " sudah aktif."
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, academicYearsPath, http.StatusSeeOther)
}

func (h *AcademicYearHandler) parseForm(w http.ResponseWriter, r *http.Request) (AcademicYearForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAcademicYearForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return AcademicYearForm{}, false
	}
	return AcademicYearForm{
		Name:      r.PostForm.Get("name"),
		StartDate: r.PostForm.Get("start_date"),
		EndDate:   r.PostForm.Get("end_date"),
	}, true
}

// formError menangani error dari Create/Update. Mengembalikan true jika
// respons sudah ditulis.
func (h *AcademicYearHandler) formError(w http.ResponseWriter, r *http.Request, err error, view academicYearFormView) bool {
	if err == nil {
		return false
	}
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		view.Errors = ve.Fields
		h.render.Render(w, r, http.StatusBadRequest, "school/academic_year_form", view)
	case errors.Is(err, ErrNotFound):
		h.notFound(w, r, view.User)
	default:
		h.serverError(w, r, "save academic year", err)
	}
	return true
}

func (h *AcademicYearHandler) notFound(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	page.NotFound(h.render, w, r, id, "Tahun ajaran yang Anda cari tidak ada atau bukan milik sekolah Anda.")
}

func (h *AcademicYearHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func actorFrom(r *http.Request, id auth.Identity) Actor {
	return Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
