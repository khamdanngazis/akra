// Package portal menyediakan halaman untuk orang tua: daftar anak (T43),
// detail siswa (T44), tagihan dan riwayat (T45), kuitansi (T46).
package portal

import (
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
)

const pageChildren = "portal/children"

// Handler melayani route /portal.
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

// Routes mendaftarkan route portal. Hanya akun orang tua yang boleh masuk;
// akun staf mendapat 403.
func (h *Handler) Routes(r chi.Router) {
	r.Route(auth.PortalPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleOrangTua))
		// Peran: orang_tua.
		r.Get("/", h.children)
	})
}

var relationshipLabels = map[string]string{"ayah": "Ayah", "ibu": "Ibu", "wali": "Wali", "lainnya": "Keluarga"}

type childCard struct {
	ID, Name, NIS, Status, Relationship, ClassName, YearName string
}

type childrenView struct {
	User     auth.Identity
	Children []childCard
	Single   bool
	Flash    string
	Error    string
}

func (h *Handler) children(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	list, err := h.svc.ListChildren(r.Context(), id.SchoolID, id.UserID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list children", err)
		return
	}
	v := childrenView{User: id, Single: len(list) == 1, Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, c := range list {
		v.Children = append(v.Children, childCard{
			ID: c.ID, Name: c.FullName, NIS: c.NIS, Status: c.Status, Relationship: relationshipLabels[c.Relationship],
			ClassName: c.ClassName, YearName: c.YearName,
		})
	}
	h.render.Render(w, r, http.StatusOK, pageChildren, v)
}
