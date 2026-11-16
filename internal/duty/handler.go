package duty

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	dutiesPath  = "/pengaturan/tugas"
	maxDutyForm = 4 << 10
)

// Handler melayani Pengaturan → Tugas tambahan (T125).
type Handler struct {
	db       database.DB
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DB, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route tugas tambahan.
func (h *Handler) Routes(r chi.Router) {
	r.Route(dutiesPath, func(r chi.Router) {
		// Peran: admin_tu, kepala_sekolah (hanya baca).
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah)).Get("/", h.list)
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU))
			// Peran: admin_tu.
			r.Post("/", h.assign)
			// Peran: admin_tu.
			r.Post("/{id}/hapus", h.remove)
		})
	})
}

type dutyOption struct{ Value, Label, Hint string }

type listView struct {
	User        auth.Identity
	CanEdit     bool
	Years       []school.AcademicYear
	Year        school.AcademicYear
	NoYear      bool
	Assignments []Assignment
	Teachers    []classes.Teacher
	Duties      []dutyOption
	Flash       string
	Error       string
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	h.show(w, r, http.StatusOK, "")
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	v := listView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Error: errMsg,
		Duties: []dutyOption{{ExamCoordinator, Labels[ExamCoordinator], Hints[ExamCoordinator]}}}
	if errMsg == "" {
		v.Flash = auth.PopFlash(ctx, h.sessions)
	}
	years, err := school.ListAcademicYears(ctx, h.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list academic years", err)
		return
	}
	v.Years = years
	yearID := r.URL.Query().Get("tahun")
	if yearID == "" {
		yearID = r.PostFormValue("academic_year_id")
	}
	for _, y := range years {
		if (yearID == "" && y.IsActive) || y.ID == yearID {
			v.Year = y
		}
	}
	if v.Year.ID == "" {
		if yearID != "" {
			page.NotFound(h.render, w, r, id, "Tahun ajaran tidak ditemukan.")
			return
		}
		v.NoYear = true
		h.render.Render(w, r, status, "settings/duties", v)
		return
	}
	if v.Assignments, err = List(ctx, h.db, id.SchoolID, v.Year.ID); err != nil {
		h.serverError(w, r, "list duties", err)
		return
	}
	if v.CanEdit {
		if v.Teachers, err = classes.ListTeachers(ctx, h.db, id.SchoolID); err != nil {
			h.serverError(w, r, "list teachers", err)
			return
		}
	}
	h.render.Render(w, r, status, "settings/duties", v)
}

func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxDutyForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	yearID := r.PostForm.Get("academic_year_id")
	_, err := Assign(r.Context(), h.db, h.actor(r, id), yearID, r.PostForm.Get("user_id"), r.PostForm.Get("duty"))
	switch {
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, "Tahun ajaran tidak ditemukan.")
		return
	case errors.Is(err, ErrNotTeacher):
		h.show(w, r, http.StatusUnprocessableEntity, "Pilih guru aktif. Tugas koordinator ujian hanya untuk akun berperan guru.")
		return
	case errors.Is(err, ErrInvalidDuty):
		h.show(w, r, http.StatusUnprocessableEntity, "Pilih jenis tugas.")
		return
	case errors.Is(err, ErrAlreadyExists):
		h.show(w, r, http.StatusUnprocessableEntity, "Guru itu sudah punya tugas ini di tahun ajaran yang sama.")
		return
	case err != nil:
		h.serverError(w, r, "assign duty", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Tugas disimpan. Menu baru muncul setelah guru tersebut login ulang; haknya langsung berlaku.")
	http.Redirect(w, r, dutiesPath+"?tahun="+url.QueryEscape(yearID), http.StatusSeeOther)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxDutyForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	err := Remove(r.Context(), h.db, h.actor(r, id), chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Tugas tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "remove duty", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Tugas dicabut.")
	back := dutiesPath
	if y := r.PostForm.Get("academic_year_id"); database.IsUUID(y) {
		back += "?tahun=" + y
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) actor(r *http.Request, id auth.Identity) Actor {
	return Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
