// Package portal menyediakan halaman untuk orang tua. Isi portal (anak,
// absensi, tagihan) dikerjakan di T43–T46.
package portal

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
)

// Handler melayani route /portal.
type Handler struct {
	sessions *scs.SessionManager
	render   page.Renderer
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(sessions *scs.SessionManager, render page.Renderer) *Handler {
	return &Handler{sessions: sessions, render: render}
}

// Routes mendaftarkan route portal. Hanya akun orang tua yang boleh masuk;
// akun staf mendapat 403.
func (h *Handler) Routes(r chi.Router) {
	r.Route(auth.PortalPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleOrangTua))
		// Peran: orang_tua.
		r.Get("/", h.index)
	})
}

type indexView struct {
	User  auth.Identity
	Flash string
	Error string
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.render.Render(w, r, http.StatusOK, "portal/index", indexView{User: id, Flash: auth.PopFlash(r.Context(), h.sessions)})
}
