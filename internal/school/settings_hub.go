package school

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/module"
	"akra/internal/platform/nav"
)

// SettingsPath adalah pusat Pengaturan (T156): satu pintu ke semua halaman
// pengaturan sekolah dan modul.
const SettingsPath = "/pengaturan"

// SettingsHubHandler melayani pusat Pengaturan. Isinya mengikuti
// nav.SettingsItems: hanya halaman yang boleh dibuka peran pengguna, dan
// hanya modul yang aktif untuk sekolah.
type SettingsHubHandler struct {
	render Renderer
}

// NewSettingsHubHandler mengembalikan SettingsHubHandler. Route-nya harus
// berada di dalam grup RequireLogin.
func NewSettingsHubHandler(render Renderer) *SettingsHubHandler {
	return &SettingsHubHandler{render: render}
}

// Routes mendaftarkan pusat Pengaturan ke r.
func (h *SettingsHubHandler) Routes(r chi.Router) {
	// Peran: admin_tu, kepala_sekolah, bendahara, guru (isi per peran).
	r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleBendahara, auth.RoleGuru)).Get(SettingsPath, h.show)
}

type settingsHubView struct {
	User     auth.Identity
	Sections []nav.Group
}

func (h *SettingsHubHandler) show(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.render.Render(w, r, http.StatusOK, "school/settings", settingsHubView{User: id, Sections: nav.SettingsFor(id.Roles, id.Duties, module.From(r.Context()).IDs())})
}
