package academic

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	assignmentsPath   = "/pengaturan/penugasan"
	assignmentField   = "t_"
	maxAssignmentForm = 64 << 10
	msgClassGone      = "Kelas tidak ditemukan di tahun ajaran aktif sekolah Anda."
)

// AssignmentHandler melayani halaman penugasan guru mapel.
type AssignmentHandler struct {
	svc      *AssignmentService
	db       database.DBTX
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewAssignmentHandler mengembalikan AssignmentHandler. Route-nya harus
// berada di dalam grup RequireLogin.
func NewAssignmentHandler(svc *AssignmentService, db database.DBTX, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *AssignmentHandler {
	return &AssignmentHandler{svc: svc, db: db, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route penugasan. Melihat: admin_tu (semua kelas) dan
// guru (penugasannya sendiri). Mengubah: admin_tu.
func (h *AssignmentHandler) Routes(r chi.Router) {
	r.Route(assignmentsPath, func(r chi.Router) {
		// Peran: admin_tu, guru.
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleGuru)).Get("/", h.page)
		// Peran: admin_tu.
		r.With(auth.RequireRole(auth.RoleAdminTU)).Post("/{classID}", h.save)
	})
}

type assignmentsView struct {
	User auth.Identity
	// Admin/TU.
	Admin    bool
	NoYear   bool
	YearName string
	Classes  []classes.Class
	Plan     ClassPlan
	Values   map[string]string // ID mapel -> ID guru yang dipilih
	Errors   map[string]string
	// Guru.
	Mine  []Assignment
	Flash string
	Error string
}

func (h *AssignmentHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	if !id.HasRole(auth.RoleAdminTU) {
		mine, err := h.svc.Mine(r.Context(), id.SchoolID, id.UserID)
		if err != nil {
			h.serverError(w, r, "list my assignments", err)
			return
		}
		h.render.Render(w, r, http.StatusOK, "academic/assignments", assignmentsView{User: id, Mine: mine, Flash: auth.PopFlash(r.Context(), h.sessions)})
		return
	}
	h.adminPage(w, r, id, http.StatusOK, r.URL.Query().Get("kelas"), nil, nil, "")
}

// adminPage menampilkan form penugasan kelas classID (kosong = kelas pertama
// tahun ajaran aktif). values dan errs membawa isian yang gagal disimpan.
func (h *AssignmentHandler) adminPage(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, classID string, values, errs map[string]string, errMsg string) {
	v := assignmentsView{User: id, Admin: true, Errors: errs, Error: errMsg}
	if errMsg == "" && errs == nil {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	year, err := school.GetActiveAcademicYear(r.Context(), h.db, id.SchoolID)
	if errors.Is(err, school.ErrNotFound) {
		v.NoYear = true
		h.render.Render(w, r, status, "academic/assignments", v)
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
		v.Plan, err = h.svc.Plan(r.Context(), id.SchoolID, classID)
		if errors.Is(err, ErrClassNotFound) {
			page.NotFound(h.render, w, r, id, msgClassGone)
			return
		}
		if err != nil {
			h.serverError(w, r, "assignment plan", err)
			return
		}
	}
	v.Values = values
	if v.Values == nil {
		v.Values = map[string]string{}
		for _, row := range v.Plan.Rows {
			v.Values[row.Subject.ID] = row.TeacherID
		}
	}
	h.render.Render(w, r, status, "academic/assignments", v)
}

func (h *AssignmentHandler) save(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	classID := chi.URLParam(r, "classID")
	r.Body = http.MaxBytesReader(w, r.Body, maxAssignmentForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	teachers := map[string]string{}
	for key, vals := range r.PostForm {
		if subjectID, ok := strings.CutPrefix(key, assignmentField); ok && len(vals) > 0 {
			teachers[subjectID] = vals[len(vals)-1]
		}
	}
	res, err := h.svc.Save(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, classID, teachers)
	var ve *ValidationError
	switch {
	case errors.Is(err, ErrClassNotFound):
		page.NotFound(h.render, w, r, id, msgClassGone)
		return
	case errors.Is(err, ErrSubjectNotFound):
		page.NotFound(h.render, w, r, id, "Mata pelajaran tidak ditemukan di sekolah Anda.")
		return
	case errors.As(err, &ve):
		msg := "Penugasan belum disimpan. Periksa baris yang ditandai."
		if m, ok := ve.Fields[""]; ok {
			msg = m
		}
		h.adminPage(w, r, id, http.StatusBadRequest, classID, teachers, ve.Fields, msg)
		return
	case err != nil:
		h.serverError(w, r, "save assignments", err)
		return
	}
	msg := "Tidak ada perubahan penugasan."
	if res.Assigned+res.Unassigned > 0 {
		msg = fmt.Sprintf("Penugasan disimpan: %d guru ditetapkan, %d dilepas.", res.Assigned, res.Unassigned)
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, assignmentsPath+"?kelas="+url.QueryEscape(classID), http.StatusSeeOther)
}

func (h *AssignmentHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
