package grades

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
)

const (
	recapPath      = "/nilai/rekap"
	pageSummary    = "grades/summary"
	msgNoClassView = "Ringkasan nilai kelas ini hanya untuk wali kelasnya, admin/TU, dan kepala sekolah. Buka rekap mapel yang Anda ajar."
	msgNoRecap     = "Rekap nilai mapel ini hanya untuk guru pengampunya di kelas ini, wali kelas, admin/TU, dan kepala sekolah."
)

// ReportHandler melayani rekap nilai.
type ReportHandler struct {
	db       database.DBTX
	input    *InputService
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewReportHandler mengembalikan ReportHandler. Route-nya harus berada di
// dalam grup RequireLogin.
func NewReportHandler(db database.DB, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *ReportHandler {
	return &ReportHandler{db: db, input: NewInputService(db), sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route rekap nilai. Semuanya baca saja.
//
//   - admin_tu, kepala_sekolah: semua kelas dan mapel;
//   - guru: rekap mapel yang diajarnya di kelas itu, dan ringkasan serta
//     rekap semua mapel kelas yang ia walikan. Selain itu 403.
func (h *ReportHandler) Routes(r chi.Router) {
	r.Route(recapPath, func(r chi.Router) {
		// Peran: admin_tu, kepala_sekolah (semua), guru (mapel yang diajar; kelas perwalian).
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleGuru)).Get("/", h.page)
	})
}

type summaryView struct {
	User     auth.Identity
	NoYear   bool
	Staff    bool            // admin/TU atau kepala sekolah
	Classes  []classes.Class // pilihan kelas untuk staf
	Teaching []Teaching      // halaman awal guru: mapel yang diajar
	Homeroom []classes.Class // halaman awal guru: kelas perwalian
	Class    *classes.Class
	Summary  *ClassReport
	Subject  *SubjectReport
	CanClass bool // boleh membuka ringkasan kelas (tautan kembali)
	Flash    string
	Error    string
}

func isStaff(id auth.Identity) bool {
	return id.HasRole(auth.RoleAdminTU) || id.HasRole(auth.RoleKepalaSekolah)
}

func isHomeroom(id auth.Identity, c classes.Class) bool {
	return id.HasRole(auth.RoleGuru) && c.HomeroomTeacherID != "" && c.HomeroomTeacherID == id.UserID
}

func (h *ReportHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	v := summaryView{User: id, Staff: isStaff(id)}
	year, err := activeYear(r.Context(), h.db, id.SchoolID)
	if errors.Is(err, ErrNoActiveYear) {
		v.NoYear = true
		h.render.Render(w, r, http.StatusOK, pageSummary, v)
		return
	}
	if err != nil {
		h.serverError(w, r, "active year", err)
		return
	}
	classID := q.Get("kelas")
	if v.Staff {
		if v.Classes, err = classes.ListByYear(r.Context(), h.db, id.SchoolID, year.ID); err != nil {
			h.serverError(w, r, "list classes", err)
			return
		}
		if classID == "" && len(v.Classes) > 0 {
			classID = v.Classes[0].ID
		}
	}
	if classID == "" {
		h.teacherHome(w, r, id, year.ID, v)
		return
	}

	c, err := activeClass(r.Context(), h.db, id.SchoolID, classID)
	if errors.Is(err, ErrClassNotFound) {
		page.NotFound(h.render, w, r, id, msgClassGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get class", err)
		return
	}
	v.Class = &c
	v.CanClass = v.Staff || isHomeroom(id, c)

	subjectID := q.Get("mapel")
	if subjectID == "" {
		if !v.CanClass {
			page.Forbidden(h.render, w, r, id, msgNoClassView)
			return
		}
		rep, err := ClassSummary(r.Context(), h.db, id.SchoolID, c)
		if err != nil {
			h.serverError(w, r, "class summary", err)
			return
		}
		v.Summary = &rep
		h.render.Render(w, r, http.StatusOK, pageSummary, v)
		return
	}

	sub, err := getSubject(r.Context(), h.db, id.SchoolID, subjectID)
	if errors.Is(err, ErrSubjectNotFound) {
		page.NotFound(h.render, w, r, id, msgSubjectGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get subject", err)
		return
	}
	if !v.CanClass {
		ok, err := CanEnterGrades(r.Context(), h.db, id, c.ID, sub.ID)
		if err != nil {
			h.serverError(w, r, "check recap access", err)
			return
		}
		if !ok {
			page.Forbidden(h.render, w, r, id, msgNoRecap)
			return
		}
	}
	rep, err := SubjectSummary(r.Context(), h.db, id.SchoolID, c, sub)
	if err != nil {
		h.serverError(w, r, "subject summary", err)
		return
	}
	v.Subject = &rep
	h.render.Render(w, r, http.StatusOK, pageSummary, v)
}

// teacherHome menampilkan mapel yang diajar guru dan kelas perwaliannya.
func (h *ReportHandler) teacherHome(w http.ResponseWriter, r *http.Request, id auth.Identity, yearID string, v summaryView) {
	if id.HasRole(auth.RoleGuru) {
		var err error
		if v.Teaching, err = h.input.MyTeaching(r.Context(), id.SchoolID, id.UserID); err != nil {
			h.serverError(w, r, "my teaching", err)
			return
		}
		all, err := classes.ListByYear(r.Context(), h.db, id.SchoolID, yearID)
		if err != nil {
			h.serverError(w, r, "list classes", err)
			return
		}
		for _, c := range all {
			if isHomeroom(id, c) {
				v.Homeroom = append(v.Homeroom, c)
			}
		}
	}
	h.render.Render(w, r, http.StatusOK, pageSummary, v)
}

func (h *ReportHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
