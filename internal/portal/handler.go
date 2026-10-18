// Package portal menyediakan halaman untuk orang tua: daftar anak (T43),
// detail siswa (T44), tagihan dan riwayat (T45), kuitansi (T46).
package portal

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/attendance"
	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

const (
	pageChildren    = "portal/children"
	pageChildDetail = "portal/child_detail"
)

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
		// Peran: orang_tua (hanya anak sendiri; selain itu 404).
		r.Get("/siswa/{id}", h.childDetail)
	})
}

var relationshipLabels = map[string]string{"ayah": "Ayah", "ibu": "Ibu", "wali": "Wali", "lainnya": "Keluarga"}

// childCard sengaja tanpa status siswa (status internal sekolah).
type childCard struct {
	ID, Name, NIS, Relationship, ClassName, YearName string
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
			ID: c.ID, Name: c.FullName, NIS: c.NIS, Relationship: relationshipLabels[c.Relationship],
			ClassName: c.ClassName, YearName: c.YearName,
		})
	}
	h.render.Render(w, r, http.StatusOK, pageChildren, v)
}

var genderLabels = map[string]string{"L": "Laki-laki", "P": "Perempuan"}

type childDetailView struct {
	User      auth.Identity
	Child     ChildDetail
	BirthDate string
	Gender    string
	Range     string
	Percent   string
	Flash     string
	Error     string
}

func (h *Handler) childDetail(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	d, err := h.svc.GetChildDetail(r.Context(), id.SchoolID, id.UserID, chi.URLParam(r, "id"), clock.Today())
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Data siswa tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "child detail", err)
		return
	}
	v := childDetailView{User: id, Child: d, Gender: genderLabels[d.Gender], Percent: attendance.FormatPercent(d.Attendance.PresentPercent())}
	if d.BirthDate != nil {
		v.BirthDate = clock.FormatDateID(*d.BirthDate)
	}
	if d.HasRecap {
		v.Range = clock.FormatDateID(d.RecapFrom) + " – " + clock.FormatDateID(d.RecapTo)
	}
	h.render.Render(w, r, http.StatusOK, pageChildDetail, v)
}
