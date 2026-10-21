package grades

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/academic"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	componentsPath   = "/nilai/komponen"
	pageComponents   = "grades/components"
	maxComponentForm = 16 << 10
	// blankRows adalah jumlah baris kosong untuk komponen baru.
	blankRows      = 3
	msgSubjectGone = "Mata pelajaran tidak ditemukan di sekolah Anda."
	msgNoAccess    = "Komponen nilai mapel ini hanya bisa diatur admin/TU atau guru yang ditugaskan mengajar mapel ini."
)

// ComponentHandler melayani halaman komponen penilaian.
type ComponentHandler struct {
	svc      *ComponentService
	db       database.DBTX
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewComponentHandler mengembalikan ComponentHandler. Route-nya harus berada
// di dalam grup RequireLogin.
func NewComponentHandler(svc *ComponentService, db database.DBTX, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *ComponentHandler {
	return &ComponentHandler{svc: svc, db: db, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route komponen nilai. Peran admin_tu (semua mapel) dan
// guru (hanya mapel yang ditugaskan, selain itu 403).
func (h *ComponentHandler) Routes(r chi.Router) {
	r.Route(componentsPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleGuru))
		// Peran: admin_tu, guru (mapel yang ditugaskan).
		r.Get("/", h.page)
		// Peran: admin_tu, guru (mapel yang ditugaskan).
		r.Post("/{subjectID}", h.save)
	})
}

type rowView struct {
	Index  int
	ID     string
	Name   string
	Weight string
	Active bool
	New    bool
	Errors map[string]string
}

type componentsView struct {
	User     auth.Identity
	NoYear   bool
	YearName string
	Subjects []academic.Subject
	Subject  academic.Subject
	Rows     []rowView
	HasSaved bool // mapel sudah punya komponen tersimpan
	Total    int  // total bobot aktif (dari isian yang tampil)
	Required int
	TotalErr string
	Flash    string
	Error    string
}

func (h *ComponentHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.show(w, r, id, http.StatusOK, r.URL.Query().Get("mapel"), nil, nil)
}

// show menampilkan komponen mapel subjectID (kosong = mapel pertama yang
// boleh diatur). submitted dan ve berisi isian dan error simpan yang gagal.
func (h *ComponentHandler) show(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, subjectID string, submitted []RowInput, ve *ValidationError) {
	v := componentsView{User: id, Required: RequiredTotal}
	if ve == nil {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	} else {
		v.TotalErr = ve.Total
		v.Error = "Komponen belum disimpan. Periksa isian yang ditandai."
		if ve.Total != "" {
			v.Error = ve.Total
		}
	}
	subjects, err := h.svc.Subjects(r.Context(), id)
	if err != nil {
		h.serverError(w, r, "list subjects", err)
		return
	}
	v.Subjects = subjects
	if subjectID == "" && len(subjects) > 0 {
		subjectID = subjects[0].ID
	}
	if subjectID == "" {
		year, err := activeYear(r.Context(), h.db, id.SchoolID)
		v.NoYear = errors.Is(err, ErrNoActiveYear)
		if err != nil && !v.NoYear {
			h.serverError(w, r, "active year", err)
			return
		}
		v.YearName = year.Name
		h.render.Render(w, r, status, pageComponents, v)
		return
	}
	p, err := h.svc.Page(r.Context(), id.SchoolID, subjectID)
	switch {
	case errors.Is(err, ErrNoActiveYear):
		v.NoYear = true
		h.render.Render(w, r, status, pageComponents, v)
		return
	case errors.Is(err, ErrSubjectNotFound):
		page.NotFound(h.render, w, r, id, msgSubjectGone)
		return
	case err != nil:
		h.serverError(w, r, "subject components", err)
		return
	}
	if !h.allowed(w, r, id, p.Subject.ID) {
		return
	}
	v.YearName, v.Subject, v.HasSaved = p.Year.Name, p.Subject, len(p.Components) > 0
	if !containsSubject(v.Subjects, p.Subject.ID) {
		// Mapel nonaktif (dibuka lewat tautan): tetap tampil di pilihan.
		v.Subjects = append(v.Subjects, p.Subject)
	}
	if submitted != nil {
		for i, in := range submitted {
			rv := rowView{Index: i, ID: in.ID, Name: in.Name, Weight: in.Weight, Active: in.Active, New: in.ID == ""}
			if ve != nil {
				rv.Errors = ve.Rows[i]
			}
			v.Rows = append(v.Rows, rv)
		}
	} else {
		for _, c := range p.Components {
			v.Rows = append(v.Rows, rowView{Index: len(v.Rows), ID: c.ID, Name: c.Name, Weight: strconv.Itoa(c.Weight), Active: c.IsActive})
		}
		for range blankRows {
			v.Rows = append(v.Rows, rowView{Index: len(v.Rows), Active: true, New: true})
		}
	}
	for _, row := range v.Rows {
		if n, err := strconv.Atoi(row.Weight); err == nil && row.Active && row.Name != "" {
			v.Total += n
		}
	}
	h.render.Render(w, r, status, pageComponents, v)
}

// allowed memeriksa CanManage; menulis 403 jika ditolak.
func (h *ComponentHandler) allowed(w http.ResponseWriter, r *http.Request, id auth.Identity, subjectID string) bool {
	ok, err := CanManage(r.Context(), h.db, id, subjectID)
	if err != nil {
		h.serverError(w, r, "check subject access", err)
		return false
	}
	if !ok {
		page.Forbidden(h.render, w, r, id, msgNoAccess)
	}
	return ok
}

func (h *ComponentHandler) save(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	subjectID := chi.URLParam(r, "subjectID")
	r.Body = http.MaxBytesReader(w, r.Body, maxComponentForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	// Mapel sekolah lain: 404 lebih dulu, baru hak akses guru (403).
	sub, err := getSubject(r.Context(), h.db, id.SchoolID, subjectID)
	if errors.Is(err, ErrSubjectNotFound) {
		page.NotFound(h.render, w, r, id, msgSubjectGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get subject", err)
		return
	}
	if !h.allowed(w, r, id, sub.ID) {
		return
	}
	n, err := strconv.Atoi(r.PostForm.Get("rows"))
	if err != nil || n < 0 || n > MaxRows {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Jumlah baris komponen tidak valid. Muat ulang halaman lalu coba lagi.")
		return
	}
	rows := make([]RowInput, n)
	for i := range rows {
		k := strconv.Itoa(i)
		rows[i] = RowInput{ID: r.PostForm.Get("id_" + k), Name: r.PostForm.Get("name_" + k), Weight: r.PostForm.Get("weight_" + k),
			Active: r.PostForm.Get("active_"+k) == "on"}
	}
	res, err := h.svc.Save(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, sub.ID, rows)
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		h.show(w, r, id, http.StatusUnprocessableEntity, sub.ID, rows, ve)
		return
	case errors.Is(err, ErrNoActiveYear):
		page.BadRequest(h.render, w, r, id, "Belum ada tahun ajaran aktif", "Aktifkan tahun ajaran lebih dulu.")
		return
	case errors.Is(err, ErrComponentNotFound):
		page.BadRequest(h.render, w, r, id, "Form kedaluwarsa", "Daftar komponen berubah sejak halaman dibuka. Muat ulang halaman lalu coba lagi.")
		return
	case err != nil:
		h.serverError(w, r, "save components", err)
		return
	}
	msg := "Komponen nilai " + sub.Name + " tidak berubah."
	if res.Created+res.Updated > 0 {
		msg = fmt.Sprintf("Komponen nilai %s disimpan: %d baru, %d diubah. Total bobot aktif %d%%.", sub.Name, res.Created, res.Updated, res.Total)
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, componentsPath+"?"+url.Values{"mapel": {sub.ID}}.Encode(), http.StatusSeeOther)
}

func containsSubject(list []academic.Subject, id string) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}

func (h *ComponentHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
