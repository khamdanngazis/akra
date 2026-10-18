package announcements

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	basePath  = "/pengumuman"
	pageIndex = "announcements/index"
	pageForm  = "announcements/form"
	listLimit = 100
)

// Handler melayani halaman pengumuman staf.
type Handler struct {
	svc      *Service
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route pengumuman.
func (h *Handler) Routes(r chi.Router) {
	writers := auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah)
	// Peran: admin_tu, kepala_sekolah, bendahara, guru (baca).
	r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleBendahara, auth.RoleGuru)).Get(basePath, h.index)
	// Peran: admin_tu, kepala_sekolah.
	r.With(writers).Get(basePath+"/baru", h.newForm)
	// Peran: admin_tu, kepala_sekolah.
	r.With(writers).Post(basePath, h.create)
}

type item struct {
	Title, Body, Audience, Published, Author string
}

type indexView struct {
	User     auth.Identity
	Items    []item
	CanWrite bool
	Flash    string
	Error    string
}

type option struct {
	Value, Label string
	Selected     bool
}

type formView struct {
	User     auth.Identity
	YearName string
	Classes  []option
	Form     Form
	Errors   map[string]string
	Error    string
	Flash    string
}

func audience(a Announcement) string {
	if a.ClassName == "" {
		return "Semua kelas · " + a.YearName
	}
	return "Kelas " + a.ClassName + " · " + a.YearName
}

// FormatPublished memformat waktu terbit, mis. "18 Oktober 2026 09.30 WIB".
func FormatPublished(a Announcement) string {
	t := a.PublishedAt.In(clock.Jakarta)
	return clock.FormatDateID(t) + " " + t.Format("15.04") + " WIB"
}

// Audience menjelaskan sasaran pengumuman, mis. "Kelas 1A · 2026/2027".
func Audience(a Announcement) string { return audience(a) }

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	list, err := h.svc.List(r.Context(), id.SchoolID, listLimit)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list announcements", err)
		return
	}
	v := indexView{User: id, CanWrite: auth.HasAnyRole(id, auth.RoleAdminTU, auth.RoleKepalaSekolah), Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, a := range list {
		v.Items = append(v.Items, item{Title: a.Title, Body: a.Body, Audience: audience(a), Published: FormatPublished(a), Author: a.AuthorName})
	}
	h.render.Render(w, r, http.StatusOK, pageIndex, v)
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderForm(w, r, id, http.StatusOK, Form{}, nil)
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, f Form, errs map[string]string) {
	y, classes, err := h.svc.ActiveYear(r.Context(), id.SchoolID)
	v := formView{User: id, Form: f, Errors: errs}
	switch {
	case errors.Is(err, ErrNoActiveYear):
		v.Error = "Belum ada tahun ajaran aktif. Aktifkan tahun ajaran sebelum membuat pengumuman."
	case err != nil:
		page.ServerError(h.render, h.logger, w, r, id, "announcement form", err)
		return
	}
	v.YearName = y.Name
	v.Classes = append(v.Classes, option{Value: "", Label: "Semua kelas", Selected: f.ClassID == ""})
	for _, c := range classes {
		v.Classes = append(v.Classes, option{Value: c.ID, Label: c.Name, Selected: c.ID == f.ClassID})
	}
	h.render.Render(w, r, status, pageForm, v)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	f := Form{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), ClassID: r.PostForm.Get("class_id")}
	_, err := h.svc.Create(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, f)
	var ve *ValidationError
	switch {
	case err == nil:
	case errors.As(err, &ve):
		h.renderForm(w, r, id, http.StatusBadRequest, f, ve.Fields)
		return
	case errors.Is(err, ErrClassNotFound):
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan di tahun ajaran aktif.")
		return
	case errors.Is(err, ErrNoActiveYear):
		h.renderForm(w, r, id, http.StatusBadRequest, f, nil)
		return
	default:
		page.ServerError(h.render, h.logger, w, r, id, "create announcement", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Pengumuman diterbitkan.")
	http.Redirect(w, r, basePath, http.StatusSeeOther)
}
