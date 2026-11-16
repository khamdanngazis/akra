package school

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

const (
	profilePath    = "/pengaturan/sekolah"
	maxProfileForm = 4 << 10
)

// ProfileHandler melayani halaman Pengaturan → Profil sekolah (T126).
type ProfileHandler struct {
	svc      *ProfileService
	sessions *scs.SessionManager
	render   Renderer
	logger   *slog.Logger
}

// NewProfileHandler mengembalikan ProfileHandler. Route-nya harus berada
// di dalam grup RequireLogin.
func NewProfileHandler(svc *ProfileService, sessions *scs.SessionManager, render Renderer, logger *slog.Logger) *ProfileHandler {
	return &ProfileHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route profil sekolah ke r.
func (h *ProfileHandler) Routes(r chi.Router) {
	r.Route(profilePath, func(r chi.Router) {
		// Peran: admin_tu, kepala_sekolah (hanya baca).
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah)).Get("/", h.show)
		// Peran: admin_tu.
		r.With(auth.RequireRole(auth.RoleAdminTU)).Post("/", h.save)
	})
}

type profileView struct {
	User    auth.Identity
	Profile Profile
	Zones   []clock.Zone
	CanEdit bool
	Now     string // jam sekarang di zona sekolah, untuk memastikan pilihan
	Flash   string
	Error   string
}

func (h *ProfileHandler) show(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	p, err := h.svc.Get(r.Context(), id.SchoolID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "school profile", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "school/profile", h.view(r, id, p, ""))
}

func (h *ProfileHandler) view(r *http.Request, id auth.Identity, p Profile, errMsg string) profileView {
	v := profileView{User: id, Profile: p, Zones: clock.Zones, CanEdit: id.HasRole(auth.RoleAdminTU), Error: errMsg}
	v.Now = clock.FormatDateTimeID(time.Now(), clock.Location(p.Timezone))
	if errMsg == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	return v
}

func (h *ProfileHandler) save(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxProfileForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	tz := r.PostForm.Get("timezone")
	changed, err := h.svc.SetTimezone(r.Context(), Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, tz)
	if errors.Is(err, ErrInvalidTimezone) {
		p, gerr := h.svc.Get(r.Context(), id.SchoolID)
		if gerr != nil {
			page.ServerError(h.render, h.logger, w, r, id, "school profile", gerr)
			return
		}
		h.render.Render(w, r, http.StatusUnprocessableEntity, "school/profile", h.view(r, id, p, "Pilih salah satu zona waktu: WIB, WITA, atau WIT."))
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "set school timezone", err)
		return
	}
	msg := "Zona waktu tidak berubah."
	if changed {
		auth.SetSessionTimezone(r.Context(), h.sessions, tz)
		msg = "Zona waktu sekolah disimpan. Pengguna lain melihat jam baru setelah login ulang."
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, profilePath, http.StatusSeeOther)
}
