package school

import (
	"net/http"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/module"
	"akra/internal/platform/page"
)

// ModulesPath adalah halaman Pengaturan → Modul & Paket (T167): modul yang
// dipakai sekolah, baca saja. Modul diatur tim implementator AKRA.
const ModulesPath = "/pengaturan/modul"

type moduleRow struct {
	module.Info
	// Since: tanggal modul diaktifkan (atau dicatat); kosong untuk bawaan.
	Since string
}

type modulesView struct {
	User     auth.Identity
	Package  string
	Active   []moduleRow
	Inactive []moduleRow
}

func (h *SettingsHubHandler) modules(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	var pkg string
	if err := h.db.QueryRow(ctx, `SELECT package FROM schools WHERE id = $1`, id.SchoolID).Scan(&pkg); err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "school package", err)
		return
	}
	rows, err := module.Rows(ctx, h.db, id.SchoolID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "school modules", err)
		return
	}
	set := module.From(ctx)
	v := modulesView{User: id, Package: module.PackageLabels[pkg]}
	if v.Package == "" && pkg != "" {
		v.Package = pkg
	}
	for _, m := range module.Catalog {
		row := moduleRow{Info: m}
		if st, ok := rows[m.ID]; ok {
			row.Since = clock.FormatDateID(st.ChangedAt.In(id.Location()))
		}
		if set.Has(m.ID) {
			v.Active = append(v.Active, row)
		} else {
			v.Inactive = append(v.Inactive, row)
		}
	}
	h.render.Render(w, r, http.StatusOK, "school/modules", v)
}
