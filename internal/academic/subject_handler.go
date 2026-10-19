package academic

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	subjectsPath    = "/pengaturan/mapel"
	maxSubjectForm  = 16 << 10
	msgSubjectGone  = "Mata pelajaran yang Anda cari tidak ada atau bukan milik sekolah Anda."
	pageSubjectList = "academic/subjects"
)

// SubjectHandler melayani halaman mata pelajaran.
type SubjectHandler struct {
	svc      *SubjectService
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewSubjectHandler mengembalikan SubjectHandler. Route-nya harus berada di
// dalam grup RequireLogin.
func NewSubjectHandler(svc *SubjectService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *SubjectHandler {
	return &SubjectHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route mata pelajaran ke r. Semuanya hanya untuk
// admin_tu; guru dan peran lain mendapat 403.
func (h *SubjectHandler) Routes(r chi.Router) {
	r.Route(subjectsPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU))
		// Peran: admin_tu.
		r.Get("/", h.list)
		// Peran: admin_tu.
		r.Post("/", h.create)
		// Peran: admin_tu.
		r.Post("/{id}", h.update)
	})
}

type subjectRow struct {
	ID       string
	Name     string
	Code     string
	IsActive bool
	// Form berisi isian form ubah: nilai tersimpan, atau isian terakhir jika
	// penyimpanan gagal. Open membuka form ubah saat halaman tampil.
	Form   SubjectForm
	Errors map[string]string
	Open   bool
}

type subjectsView struct {
	User      auth.Identity
	Rows      []subjectRow
	New       SubjectForm
	NewErrors map[string]string
	Flash     string
	Error     string
}

// showOpts berisi keadaan form yang gagal disimpan, untuk ditampilkan lagi.
type showOpts struct {
	newForm    SubjectForm
	newErrors  map[string]string
	editID     string
	editForm   SubjectForm
	editErrors map[string]string
}

func (h *SubjectHandler) list(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.show(w, r, http.StatusOK, id, showOpts{})
}

func (h *SubjectHandler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := parseSubjectForm(w, r)
	if !ok {
		return
	}
	sub, err := h.svc.Create(r.Context(), actorFrom(r, id), f)
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		h.show(w, r, http.StatusBadRequest, id, showOpts{newForm: f, newErrors: ve.Fields})
		return
	case err != nil:
		h.serverError(w, r, "create subject", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Mata pelajaran "+sub.Name+" berhasil dibuat.")
	http.Redirect(w, r, subjectsPath, http.StatusSeeOther)
}

func (h *SubjectHandler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	subjectID := chi.URLParam(r, "id")
	f, ok := parseSubjectForm(w, r)
	if !ok {
		return
	}
	sub, err := h.svc.Update(r.Context(), actorFrom(r, id), subjectID, f)
	var ve *ValidationError
	switch {
	case errors.Is(err, ErrSubjectNotFound):
		page.NotFound(h.render, w, r, id, msgSubjectGone)
		return
	case errors.As(err, &ve):
		h.show(w, r, http.StatusBadRequest, id, showOpts{editID: subjectID, editForm: f, editErrors: ve.Fields})
		return
	case err != nil:
		h.serverError(w, r, "update subject", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Mata pelajaran "+sub.Name+" berhasil diubah.")
	http.Redirect(w, r, subjectsPath, http.StatusSeeOther)
}

// show menampilkan daftar mapel sekolah pengguna. opts membawa isian form
// yang gagal disimpan.
func (h *SubjectHandler) show(w http.ResponseWriter, r *http.Request, status int, id auth.Identity, opts showOpts) {
	list, err := h.svc.ListAll(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list subjects", err)
		return
	}
	view := subjectsView{
		User:      id,
		New:       opts.newForm,
		NewErrors: opts.newErrors,
		Flash:     auth.PopFlash(r.Context(), h.sessions),
	}
	for _, s := range list {
		row := subjectRow{ID: s.ID, Name: s.Name, Code: s.Code, IsActive: s.IsActive,
			Form: SubjectForm{Name: s.Name, Code: s.Code, Status: StatusActive}}
		if !s.IsActive {
			row.Form.Status = StatusInactive
		}
		if s.ID == opts.editID {
			row.Form, row.Errors, row.Open = opts.editForm, opts.editErrors, true
		}
		view.Rows = append(view.Rows, row)
	}
	h.render.Render(w, r, status, pageSubjectList, view)
}

func parseSubjectForm(w http.ResponseWriter, r *http.Request) (SubjectForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSubjectForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return SubjectForm{}, false
	}
	return SubjectForm{
		Name:   r.PostForm.Get("name"),
		Code:   r.PostForm.Get("code"),
		Status: r.PostForm.Get("status"),
	}, true
}

func (h *SubjectHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func actorFrom(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
