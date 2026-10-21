package importer

import (
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	mappingPath  = "/impor/mapping"
	pageMappings = "importer/mapping"
	// maxHeaderCols membatasi jumlah judul kolom file contoh yang dibawa ke
	// langkah kedua form.
	maxHeaderCols = 200
)

// MappingHandler melayani halaman mapping impor (T81).
type MappingHandler struct {
	svc      *MappingService
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewMappingHandler mengembalikan MappingHandler. Route-nya harus berada di
// dalam grup RequireLogin.
func NewMappingHandler(svc *MappingService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *MappingHandler {
	return &MappingHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route mapping impor. Hanya admin_tu.
func (h *MappingHandler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU))
		// Peran: admin_tu.
		r.Get(mappingPath, h.page)
		// Peran: admin_tu. Langkah 1 (step=contoh): baca header file contoh;
		// langkah 2 (step=simpan): simpan mapping.
		r.Post(mappingPath, h.post)
	})
}

type mappingRow struct {
	Mapping
	TargetLabel string
	Pairs       []string // "NIS ← Nomor Induk"
}

type columnChoice struct {
	Index    int
	Column   Column
	Selected string
	Error    string
}

type mappingView struct {
	User     auth.Identity
	Mappings []mappingRow
	Targets  []Target
	// Langkah 2: header file contoh dan pilihan per kolom baku.
	Step2    bool
	Name     string
	Target   Target
	FileName string
	Header   []string
	Choices  []columnChoice
	Errors   map[string]string
	Flash    string
	Error    string
}

func (h *MappingHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.show(w, r, id, http.StatusOK, mappingView{Flash: auth.PopFlash(r.Context(), h.sessions)})
}

func (h *MappingHandler) show(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, v mappingView) {
	list, err := h.svc.List(r.Context(), id.SchoolID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list import mappings", err)
		return
	}
	v.User, v.Targets = id, Targets
	for _, m := range list {
		row := mappingRow{Mapping: m, TargetLabel: m.Target}
		if t, ok := TargetByKey(m.Target); ok {
			row.TargetLabel = t.Label
			for _, c := range t.Columns {
				if src := m.Columns[c.Header]; src != "" {
					row.Pairs = append(row.Pairs, c.Header+" ← "+src)
				}
			}
		}
		v.Mappings = append(v.Mappings, row)
	}
	h.render.Render(w, r, status, pageMappings, v)
}

// suggest memilih judul file yang sama dengan judul kolom baku (tanpa
// membedakan huruf besar dan spasi) sebagai pilihan awal.
func suggest(c Column, header []string) string {
	for _, hd := range header {
		if headerKey(hd) == headerKey(c.Header) {
			return hd
		}
	}
	return ""
}

func (h *MappingHandler) post(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(1 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var mbe *http.MaxBytesError
		msg := "Unggahan tidak bisa dibaca. Coba lagi."
		if errors.As(err, &mbe) {
			msg = parseErrors[ErrFileTooLarge]
		}
		h.show(w, r, id, http.StatusBadRequest, mappingView{Error: msg})
		return
	}
	name := r.FormValue("name")
	t, ok := TargetByKey(r.FormValue("target"))
	if !ok {
		h.show(w, r, id, http.StatusUnprocessableEntity, mappingView{Name: name, Errors: map[string]string{"target": "Pilih jenis data dari daftar."}, Error: "Pilih jenis data."})
		return
	}
	if r.FormValue("step") == "simpan" {
		h.save(w, r, id, name, t)
		return
	}

	// Langkah 1: baca header file contoh.
	file, hdr, err := r.FormFile("file")
	switch {
	case errors.Is(err, http.ErrMissingFile):
		err = ErrFileEmpty
	case err == nil:
		defer file.Close()
		if !strings.EqualFold(path.Ext(strings.TrimSpace(hdr.Filename)), ".xlsx") {
			err = ErrNotXLSX
		}
	}
	var header []string
	if err == nil {
		header, err = ReadHeader(file)
	}
	if err != nil {
		msg := parseMessage(err)
		if msg == "" {
			page.ServerError(h.render, h.logger, w, r, id, "read mapping sample", err)
			return
		}
		h.show(w, r, id, http.StatusUnprocessableEntity, mappingView{Name: name, Error: msg})
		return
	}
	if len(header) > maxHeaderCols {
		header = header[:maxHeaderCols]
	}
	v := mappingView{Step2: true, Name: name, Target: t, FileName: cleanFileName(hdr.Filename), Header: nonEmpty(header)}
	for i, c := range t.Columns {
		v.Choices = append(v.Choices, columnChoice{Index: i, Column: c, Selected: suggest(c, v.Header)})
	}
	h.show(w, r, id, http.StatusOK, v)
}

// save adalah langkah 2: pilihan per kolom baku (map_<i>) divalidasi
// terhadap header file contoh (field header berulang) lalu disimpan.
func (h *MappingHandler) save(w http.ResponseWriter, r *http.Request, id auth.Identity, name string, t Target) {
	header := nonEmpty(r.Form["header"])
	if len(header) > maxHeaderCols {
		header = header[:maxHeaderCols]
	}
	m := ColumnMap{}
	v := mappingView{Step2: true, Name: name, Target: t, FileName: r.FormValue("file_name"), Header: header}
	for i, c := range t.Columns {
		sel := strings.TrimSpace(r.FormValue("map_" + strconv.Itoa(i)))
		m[c.Header] = sel
		v.Choices = append(v.Choices, columnChoice{Index: i, Column: c, Selected: sel})
	}
	_, err := h.svc.Create(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, name, t.Key, m, header)
	var me *MappingError
	switch {
	case errors.As(err, &me):
		v.Errors = me.Fields
		for i := range v.Choices {
			v.Choices[i].Error = me.Fields[v.Choices[i].Column.Header]
		}
		v.Error = "Mapping belum disimpan. Periksa isian yang ditandai."
		h.show(w, r, id, http.StatusUnprocessableEntity, v)
		return
	case err != nil:
		page.ServerError(h.render, h.logger, w, r, id, "save import mapping", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Mapping "+strings.Join(strings.Fields(name), " ")+" disimpan.")
	http.Redirect(w, r, mappingPath, http.StatusSeeOther)
}

func nonEmpty(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = clean(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
