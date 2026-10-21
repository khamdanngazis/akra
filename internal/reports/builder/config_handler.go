package builder

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	configsPath    = "/laporan/konfigurasi"
	pageConfigs    = "reports/configs"
	maxConfigForm  = 16 << 10
	formFilterRows = MaxFilters
	formSortRows   = 2
	msgConfigGone  = "Konfigurasi laporan tidak ditemukan."
	msgNotOwner    = "Konfigurasi ini milik pengguna lain. Hanya pemiliknya yang bisa mengubah atau menghapusnya."
)

// ConfigHandler melayani halaman konfigurasi laporan tersimpan.
type ConfigHandler struct {
	svc      *ConfigService
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewConfigHandler mengembalikan ConfigHandler. Route-nya harus berada di
// dalam grup RequireLogin.
func NewConfigHandler(svc *ConfigService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *ConfigHandler {
	return &ConfigHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// configRoles adalah peran yang punya minimal satu sumber laporan.
func configRoles(reg *Registry) []string {
	var out []string
	for _, s := range reg.Sources() {
		for _, r := range s.Roles {
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Routes mendaftarkan route konfigurasi laporan. Peran yang punya sumber
// laporan (admin_tu, bendahara, kepala_sekolah); lainnya 403.
func (h *ConfigHandler) Routes(r chi.Router) {
	r.Route(configsPath, func(r chi.Router) {
		r.Use(auth.RequireRole(configRoles(h.svc.reg)...))
		// Peran: admin_tu, bendahara, kepala_sekolah.
		r.Get("/", h.page)
		// Peran: admin_tu, bendahara, kepala_sekolah (sumber sesuai peran).
		r.Post("/", h.create)
		// Peran: pemilik konfigurasi (selain itu 403).
		r.Post("/{id}", h.update)
		// Peran: pemilik konfigurasi (selain itu 403).
		r.Post("/{id}/hapus", h.delete)
	})
	h.builderRoutes(r)
}

type sourceView struct {
	Source
	Ops map[ColumnType][]Operator
}

type configRow struct {
	SavedConfig
	SourceLabel string
	Mine        bool
}

type filterRow struct {
	Index int
	Filter
	Raw string // nilai apa adanya; untuk "in" dipisah koma
}

type configsView struct {
	User    auth.Identity
	Configs []configRow
	Sources []sourceView
	// Form berisi isian form tambah/ubah. EditID kosong untuk konfigurasi baru.
	EditID  string
	Form    ConfigInput
	Filters []filterRow
	Sorts   []Order
	Errors  map[string]string
	Flash   string
	Error   string
}

var opsByType = map[ColumnType][]Operator{
	TypeText:  {OpEq, OpNe, OpContains, OpIn},
	TypeInt:   {OpEq, OpNe, OpGte, OpLte, OpIn},
	TypeMoney: {OpEq, OpNe, OpGte, OpLte, OpIn},
	TypeDate:  {OpEq, OpNe, OpGte, OpLte, OpIn},
}

func (h *ConfigHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	editID := r.URL.Query().Get("ubah")
	in := ConfigInput{}
	if editID != "" {
		c, err := h.svc.Get(r.Context(), id, editID)
		if errors.Is(err, ErrConfigNotFound) {
			page.NotFound(h.render, w, r, id, msgConfigGone)
			return
		}
		if err != nil {
			h.serverError(w, r, "get report config", err)
			return
		}
		if c.OwnerID != id.UserID {
			page.Forbidden(h.render, w, r, id, msgNotOwner)
			return
		}
		in = ConfigInput{Name: c.Name, Config: c.Config, Shared: c.Shared}
	}
	h.show(w, r, id, http.StatusOK, editID, in, nil)
}

func (h *ConfigHandler) show(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, editID string, in ConfigInput, errs map[string]string) {
	list, err := h.svc.Visible(r.Context(), id)
	if err != nil {
		h.serverError(w, r, "list report configs", err)
		return
	}
	v := configsView{User: id, EditID: editID, Form: in, Errors: errs}
	if errs == nil {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	} else {
		v.Error = "Konfigurasi belum disimpan. Periksa isian yang ditandai."
	}
	for _, s := range h.svc.reg.Sources() {
		if s.Allowed(id.Roles) {
			v.Sources = append(v.Sources, sourceView{Source: s, Ops: opsByType})
		}
	}
	if v.Form.Config.Source == "" && len(v.Sources) > 0 {
		v.Form.Config.Source = v.Sources[0].Name
	}
	for _, c := range list {
		label := c.Config.Source
		if src, ok := h.svc.reg.Source(c.Config.Source); ok {
			label = src.Label
		}
		v.Configs = append(v.Configs, configRow{SavedConfig: c, SourceLabel: label, Mine: c.OwnerID == id.UserID})
	}
	for i := range formFilterRows {
		row := filterRow{Index: i}
		if i < len(in.Config.Filters) {
			row.Filter = in.Config.Filters[i]
			row.Raw = row.Value
			if row.Op == OpIn {
				row.Raw = strings.Join(row.Values, ", ")
			}
		}
		v.Filters = append(v.Filters, row)
	}
	for i := range formSortRows {
		o := Order{}
		if i < len(in.Config.Order) {
			o = in.Config.Order[i]
		}
		v.Sorts = append(v.Sorts, o)
	}
	h.render.Render(w, r, status, pageConfigs, v)
}

// parseConfigForm membaca form. Isian kolom, filter, dan urutan memakai
// awalan nama sumber (columns_<sumber>, f_<sumber>_col_<i>, ...) sehingga
// form tetap benar tanpa JavaScript; hanya isian sumber terpilih yang dibaca.
func parseConfigForm(r *http.Request) ConfigInput {
	p := r.PostForm
	src := strings.TrimSpace(p.Get("source"))
	in := ConfigInput{Name: p.Get("name"), Shared: p.Get("shared") == "on", Config: Config{Source: src, Columns: p["columns_"+src]}}
	for i := range formFilterRows {
		k := "f_" + src + "_"
		col := strings.TrimSpace(p.Get(k + "col_" + strconv.Itoa(i)))
		if col == "" {
			continue
		}
		f := Filter{Column: col, Op: Operator(p.Get(k + "op_" + strconv.Itoa(i)))}
		raw := p.Get(k + "val_" + strconv.Itoa(i))
		if f.Op == OpIn {
			for _, part := range strings.Split(raw, ",") {
				if part = strings.TrimSpace(part); part != "" {
					f.Values = append(f.Values, part)
				}
			}
		} else {
			f.Value = strings.TrimSpace(raw)
		}
		in.Config.Filters = append(in.Config.Filters, f)
	}
	for i := range formSortRows {
		col := strings.TrimSpace(p.Get("s_" + src + "_col_" + strconv.Itoa(i)))
		if col != "" {
			in.Config.Order = append(in.Config.Order, Order{Column: col, Desc: p.Get("s_"+src+"_desc_"+strconv.Itoa(i)) == "on"})
		}
	}
	return in
}

func (h *ConfigHandler) readForm(w http.ResponseWriter, r *http.Request, id auth.Identity) (ConfigInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return ConfigInput{}, false
	}
	return parseConfigForm(r), true
}

func actorOf(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}

func (h *ConfigHandler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	in, ok := h.readForm(w, r, id)
	if !ok {
		return
	}
	saved, err := h.svc.Create(r.Context(), actorOf(r, id), id, in)
	var ce *ConfigError
	switch {
	case errors.As(err, &ce):
		h.show(w, r, id, http.StatusUnprocessableEntity, "", in, ce.Fields)
		return
	case err != nil:
		h.serverError(w, r, "create report config", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Konfigurasi "+saved.Name+" disimpan.")
	http.Redirect(w, r, configsPath, http.StatusSeeOther)
}

func (h *ConfigHandler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	configID := chi.URLParam(r, "id")
	in, ok := h.readForm(w, r, id)
	if !ok {
		return
	}
	saved, err := h.svc.Update(r.Context(), actorOf(r, id), id, configID, in)
	var ce *ConfigError
	switch {
	case errors.Is(err, ErrConfigNotFound):
		page.NotFound(h.render, w, r, id, msgConfigGone)
		return
	case errors.Is(err, ErrNotOwner):
		page.Forbidden(h.render, w, r, id, msgNotOwner)
		return
	case errors.As(err, &ce):
		h.show(w, r, id, http.StatusUnprocessableEntity, configID, in, ce.Fields)
		return
	case err != nil:
		h.serverError(w, r, "update report config", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Konfigurasi "+saved.Name+" disimpan.")
	http.Redirect(w, r, configsPath, http.StatusSeeOther)
}

func (h *ConfigHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	before, err := h.svc.Delete(r.Context(), actorOf(r, id), chi.URLParam(r, "id"))
	switch {
	case errors.Is(err, ErrConfigNotFound):
		page.NotFound(h.render, w, r, id, msgConfigGone)
		return
	case errors.Is(err, ErrNotOwner):
		page.Forbidden(h.render, w, r, id, msgNotOwner)
		return
	case err != nil:
		h.serverError(w, r, "delete report config", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Konfigurasi "+before.Name+" dihapus.")
	http.Redirect(w, r, configsPath, http.StatusSeeOther)
}

func (h *ConfigHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

// HasColumn melaporkan apakah kolom key dipilih di form.
func (v configsView) HasColumn(key string) bool { return slices.Contains(v.Form.Config.Columns, key) }

var opLabels = map[Operator]string{OpEq: "sama dengan", OpNe: "bukan", OpGte: "≥ (minimal)", OpLte: "≤ (maksimal)", OpContains: "memuat", OpIn: "salah satu dari"}

// OpLabel adalah nama operator untuk pengguna.
func (configsView) OpLabel(op Operator) string { return opLabels[op] }

// AllOps adalah semua operator, urut tampil. Kecocokan dengan tipe kolom
// diperiksa server (Compile).
func (configsView) AllOps() []Operator { return []Operator{OpEq, OpNe, OpContains, OpIn, OpGte, OpLte} }
