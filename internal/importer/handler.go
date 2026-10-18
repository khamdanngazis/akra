package importer

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
)

const (
	studentPath      = "/impor/siswa"
	templateFilename = "template-impor-siswa.xlsx"
)

// Handler melayani halaman impor.
type Handler struct {
	render page.Renderer
	logger *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{render: render, logger: logger}
}

// Routes mendaftarkan route impor.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU))
		// Peran: admin_tu.
		r.Get(studentPath, h.index)
		// Peran: admin_tu.
		r.Get(studentPath+"/template.xlsx", h.template)
	})
}

type indexView struct {
	User    auth.Identity
	Columns []Column
	Flash   string
	Error   string
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.render.Render(w, r, http.StatusOK, "importer/index", indexView{User: id, Columns: StudentColumns})
}

func (h *Handler) template(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	data, err := StudentTemplate()
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "student import template", err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	hdr.Set("Content-Disposition", `attachment; filename="`+templateFilename+`"`)
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
