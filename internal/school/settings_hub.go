package school

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
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
	db     database.DBTX
	render Renderer
	logger *slog.Logger
}

// NewSettingsHubHandler mengembalikan SettingsHubHandler. Route-nya harus
// berada di dalam grup RequireLogin. db boleh nil bila halaman Modul &
// Paket tidak dibuka (router uji).
func NewSettingsHubHandler(db database.DBTX, render Renderer, logger *slog.Logger) *SettingsHubHandler {
	return &SettingsHubHandler{db: db, render: render, logger: logger}
}

// Routes mendaftarkan pusat Pengaturan ke r.
func (h *SettingsHubHandler) Routes(r chi.Router) {
	// Peran: admin_tu, kepala_sekolah, bendahara, guru (isi per peran).
	r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleBendahara, auth.RoleGuru)).Get(SettingsPath, h.show)
	// Peran: admin_tu, kepala_sekolah (baca saja; modul diatur tim AKRA).
	r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah)).Get(ModulesPath, h.modules)
}

type settingsHubView struct {
	User     auth.Identity
	Sections []nav.Group
}

func (h *SettingsHubHandler) show(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.render.Render(w, r, http.StatusOK, "school/settings", settingsHubView{User: id, Sections: nav.SettingsFor(id.Roles, id.Duties, module.From(r.Context()).IDs())})
}
