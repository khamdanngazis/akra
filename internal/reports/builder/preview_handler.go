package builder

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
)

const (
	builderPath = "/laporan/bangun"
	pageBuilder = "reports/builder"
)

// builderRoutes mendaftarkan halaman pembangun laporan (T78). Peran sama
// dengan konfigurasi; akses sumber dan kolom diperiksa ulang di server pada
// setiap pratinjau dan simpan (configFields), bukan hanya di daftar pilihan.
func (h *ConfigHandler) builderRoutes(r chi.Router) {
	r.Route(builderPath, func(r chi.Router) {
		r.Use(auth.RequireRole(configRoles(h.svc.reg)...))
		// Peran: admin_tu, bendahara, kepala_sekolah.
		r.Get("/", h.builder)
		// Peran: admin_tu, bendahara, kepala_sekolah (sumber sesuai peran).
		r.Post("/pratinjau", h.preview)
		// Peran: admin_tu, bendahara, kepala_sekolah; mengubah hanya milik sendiri.
		r.Post("/simpan", h.saveFromBuilder)
	})
}

// Data awal komponen Alpine. Hanya untuk tampilan; server tidak memercayai
// isian yang dikirim balik.
type stateColumn struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Type  ColumnType `json:"type"`
}

type stateSource struct {
	Name    string        `json:"name"`
	Label   string        `json:"label"`
	Columns []stateColumn `json:"columns"`
}

type stateOp struct {
	Value Operator `json:"value"`
	Label string   `json:"label"`
}

type builderState struct {
	Source     string                   `json:"source"`
	Sources    []stateSource            `json:"sources"`
	Columns    []string                 `json:"columns"`
	Filters    []stateFilter            `json:"filters"`
	Sort       []Order                  `json:"sort"`
	Ops        map[ColumnType][]stateOp `json:"ops"`
	MaxColumns int                      `json:"maxColumns"`
	MaxFilters int                      `json:"maxFilters"`
	MaxSort    int                      `json:"maxSort"`
}

// stateFilter menyimpan nilai "in" sebagai teks dipisah koma, sama dengan isian form.
type stateFilter struct {
	Column string   `json:"column"`
	Op     Operator `json:"op"`
	Value  string   `json:"value"`
}

type builderView struct {
	User      auth.Identity
	State     string // JSON builderState untuk data-state
	HasSource bool
	Name      string
	Shared    bool
	// ConfigID diisi jika sedang mengubah konfigurasi milik sendiri.
	ConfigID string
	// CopyOf diisi jika membuka konfigurasi bersama milik orang lain:
	// menyimpan membuat salinan milik sendiri.
	CopyOf  string
	Preview *Preview
	Errors  map[string]string
	Flash   string
	Error   string
}

func (h *ConfigHandler) builderState(id auth.Identity, c Config) builderState {
	st := builderState{Source: c.Source, Columns: nonNil(c.Columns), Sort: nonNil(c.Order),
		Ops: map[ColumnType][]stateOp{}, MaxColumns: MaxColumns, MaxFilters: formFilterRows, MaxSort: formSortRows}
	for t, ops := range opsByType {
		for _, op := range ops {
			st.Ops[t] = append(st.Ops[t], stateOp{op, opLabels[op]})
		}
	}
	for _, s := range h.svc.reg.Sources() {
		if !s.Allowed(id.Roles) {
			continue
		}
		ss := stateSource{Name: s.Name, Label: s.Label}
		for _, col := range s.Columns {
			ss.Columns = append(ss.Columns, stateColumn{col.Key, col.Label, col.Type})
		}
		st.Sources = append(st.Sources, ss)
	}
	if st.Source == "" && len(st.Sources) > 0 {
		st.Source = st.Sources[0].Name
	}
	st.Filters = []stateFilter{}
	for _, f := range c.Filters {
		sf := stateFilter{Column: f.Column, Op: f.Op, Value: f.Value}
		if f.Op == OpIn {
			sf.Value = strings.Join(f.Values, ", ")
		}
		st.Filters = append(st.Filters, sf)
	}
	return st
}

func (h *ConfigHandler) renderBuilder(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, v builderView, c Config) {
	st := h.builderState(id, c)
	b, err := json.Marshal(st)
	if err != nil {
		h.serverError(w, r, "builder state", err)
		return
	}
	v.User, v.State, v.HasSource = id, string(b), len(st.Sources) > 0
	if v.Errors == nil && v.Flash == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	if v.Errors != nil && v.Error == "" {
		v.Error = "Periksa isian yang ditandai."
	}
	h.render.Render(w, r, status, pageBuilder, v)
}

func (h *ConfigHandler) builder(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	v := builderView{}
	var c Config
	if cid := r.URL.Query().Get("konfigurasi"); cid != "" {
		saved, err := h.svc.Get(r.Context(), id, cid)
		if errors.Is(err, ErrConfigNotFound) {
			page.NotFound(h.render, w, r, id, msgConfigGone)
			return
		}
		if err != nil {
			h.serverError(w, r, "get report config", err)
			return
		}
		c, v.Name = saved.Config, saved.Name
		if saved.OwnerID == id.UserID {
			v.ConfigID, v.Shared = saved.ID, saved.Shared
		} else {
			v.CopyOf, v.Name = saved.Name, saved.Name+" (salinan)"
		}
	}
	h.renderBuilder(w, r, id, http.StatusOK, v, c)
}

func (h *ConfigHandler) preview(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	in, ok := h.readForm(w, r, id)
	if !ok {
		return
	}
	v := builderView{Name: in.Name, Shared: in.Shared, ConfigID: r.PostForm.Get("config_id")}
	p, err := h.svc.Preview(r.Context(), id, in.Config)
	var ce *ConfigError
	switch {
	case errors.As(err, &ce):
		v.Errors = ce.Fields
		h.renderBuilder(w, r, id, http.StatusUnprocessableEntity, v, in.Config)
		return
	case err != nil:
		h.serverError(w, r, "report preview", err)
		return
	}
	v.Preview = &p
	h.renderBuilder(w, r, id, http.StatusOK, v, in.Config)
}

// saveFromBuilder menyimpan lewat ConfigService (T77): mengubah konfigurasi
// config_id jika diisi (hanya pemilik), selain itu membuat yang baru.
func (h *ConfigHandler) saveFromBuilder(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	in, ok := h.readForm(w, r, id)
	if !ok {
		return
	}
	configID := strings.TrimSpace(r.PostForm.Get("config_id"))
	var saved SavedConfig
	var err error
	if configID != "" {
		saved, err = h.svc.Update(r.Context(), actorOf(r, id), id, configID, in)
	} else {
		saved, err = h.svc.Create(r.Context(), actorOf(r, id), id, in)
	}
	var ce *ConfigError
	switch {
	case errors.Is(err, ErrConfigNotFound):
		page.NotFound(h.render, w, r, id, msgConfigGone)
		return
	case errors.Is(err, ErrNotOwner):
		page.Forbidden(h.render, w, r, id, msgNotOwner)
		return
	case errors.As(err, &ce):
		h.renderBuilder(w, r, id, http.StatusUnprocessableEntity, builderView{Name: in.Name, Shared: in.Shared, ConfigID: configID, Errors: ce.Fields}, in.Config)
		return
	case err != nil:
		h.serverError(w, r, "save report config", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Konfigurasi "+saved.Name+" disimpan.")
	http.Redirect(w, r, builderPath+"?konfigurasi="+saved.ID, http.StatusSeeOther)
}
