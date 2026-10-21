package grades

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	inputPath    = "/nilai/input"
	pageInput    = "grades/input"
	maxInputForm = 64 << 10
	scorePrefix  = "g_"
	msgClassGone = "Kelas tidak ditemukan di tahun ajaran aktif sekolah Anda."
	msgCompGone  = "Komponen nilai tidak ditemukan untuk mapel dan tahun ajaran ini."
	msgNoInput   = "Nilai mapel ini hanya bisa diisi oleh guru yang ditugaskan mengajar mapel tersebut di kelas ini."
)

// InputHandler melayani halaman input nilai guru.
type InputHandler struct {
	svc      *InputService
	db       database.DBTX
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewInputHandler mengembalikan InputHandler. Route-nya harus berada di dalam
// grup RequireLogin.
func NewInputHandler(svc *InputService, db database.DBTX, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *InputHandler {
	return &InputHandler{svc: svc, db: db, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route input nilai. Hanya peran guru, dan hanya untuk
// kelas–mapel yang ditugaskan kepadanya (selain itu 403).
func (h *InputHandler) Routes(r chi.Router) {
	r.Route(inputPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleGuru))
		// Peran: guru (kelas–mapel yang ditugaskan).
		r.Get("/", h.page)
		// Peran: guru (kelas–mapel yang ditugaskan).
		r.Post("/{classID}/{componentID}", h.save)
	})
}

type tabView struct {
	Label, URL string
	Active     bool
}

type inputRow struct {
	No           int
	EnrollmentID string
	FullName     string
	NIS          string
	Value        string
	Moved        bool
	Error        string
}

type inputView struct {
	User     auth.Identity
	Teaching []Teaching // halaman awal tanpa kelas/mapel
	Page     *InputPage
	Tabs     []tabView
	Rows     []inputRow
	Empty    int
	Inactive bool
	SetupURL string // halaman komponen mapel, jika belum ada komponen aktif
	Flash    string
	Error    string
}

func inputURL(classID, subjectID, componentID string) string {
	v := url.Values{"kelas": {classID}, "mapel": {subjectID}}
	if componentID != "" {
		v.Set("komponen", componentID)
	}
	return inputPath + "?" + v.Encode()
}

func (h *InputHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	classID, subjectID := q.Get("kelas"), q.Get("mapel")
	if classID == "" || subjectID == "" {
		list, err := h.svc.MyTeaching(r.Context(), id.SchoolID, id.UserID)
		if err != nil {
			h.serverError(w, r, "my teaching", err)
			return
		}
		h.render.Render(w, r, http.StatusOK, pageInput, inputView{User: id, Teaching: list, Flash: auth.PopFlash(r.Context(), h.sessions)})
		return
	}
	h.show(w, r, id, http.StatusOK, classID, subjectID, q.Get("komponen"), nil, nil)
}

// show menampilkan tabel input. submitted dan bad berisi isian dan error
// simpan yang gagal.
func (h *InputHandler) show(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, classID, subjectID, componentID string, submitted, bad map[string]string) {
	// Kelas dan mapel sekolah lain lebih dulu 404, baru hak akses (403).
	if _, err := activeClass(r.Context(), h.db, id.SchoolID, classID); err != nil {
		h.lookupError(w, r, id, err)
		return
	}
	if _, err := getSubject(r.Context(), h.db, id.SchoolID, subjectID); err != nil {
		h.lookupError(w, r, id, err)
		return
	}
	if !h.allowed(w, r, id, classID, subjectID) {
		return
	}
	p, err := h.svc.Page(r.Context(), id.SchoolID, classID, subjectID, componentID)
	if err != nil {
		h.lookupError(w, r, id, err)
		return
	}
	v := inputView{User: id, Page: &p, Inactive: p.Component.ID != "" && !p.Component.IsActive}
	if bad == nil {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	} else {
		v.Error = fmt.Sprintf("Nilai belum disimpan: %d isian tidak valid. Periksa sel yang ditandai.", len(bad))
	}
	if p.Component.ID == "" {
		v.SetupURL = componentsPath + "?" + url.Values{"mapel": {p.Subject.ID}}.Encode()
		h.render.Render(w, r, status, pageInput, v)
		return
	}
	for _, c := range p.Components {
		v.Tabs = append(v.Tabs, tabView{Label: fmt.Sprintf("%s · %d%%", c.Name, c.Weight), URL: inputURL(p.Class.ID, p.Subject.ID, c.ID), Active: c.ID == p.Component.ID})
	}
	for i, sr := range p.Rows {
		row := inputRow{No: i + 1, EnrollmentID: sr.EnrollmentID, FullName: sr.FullName, NIS: sr.NIS, Moved: !sr.Active}
		if sr.Grade != nil && sr.Grade.Score != nil {
			row.Value = sr.Grade.Score.String()
		}
		if val, ok := submitted[sr.EnrollmentID]; ok {
			row.Value, row.Error = strings.TrimSpace(val), bad[sr.EnrollmentID]
		}
		if row.Value == "" && !row.Moved {
			v.Empty++
		}
		v.Rows = append(v.Rows, row)
	}
	h.render.Render(w, r, status, pageInput, v)
}

func (h *InputHandler) lookupError(w http.ResponseWriter, r *http.Request, id auth.Identity, err error) {
	switch {
	case errors.Is(err, ErrClassNotFound):
		page.NotFound(h.render, w, r, id, msgClassGone)
	case errors.Is(err, ErrSubjectNotFound):
		page.NotFound(h.render, w, r, id, msgSubjectGone)
	case errors.Is(err, ErrComponentNotFound):
		page.NotFound(h.render, w, r, id, msgCompGone)
	default:
		h.serverError(w, r, "grade input", err)
	}
}

func (h *InputHandler) allowed(w http.ResponseWriter, r *http.Request, id auth.Identity, classID, subjectID string) bool {
	ok, err := CanEnterGrades(r.Context(), h.db, id, classID, subjectID)
	if err != nil {
		h.serverError(w, r, "check grade access", err)
		return false
	}
	if !ok {
		page.Forbidden(h.render, w, r, id, msgNoInput)
	}
	return ok
}

func (h *InputHandler) save(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxInputForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian nilai terlalu besar atau rusak. Muat ulang halaman lalu coba lagi.")
		return
	}
	t, err := ResolveComponent(r.Context(), h.db, id.SchoolID, chi.URLParam(r, "classID"), chi.URLParam(r, "componentID"))
	if err != nil {
		h.lookupError(w, r, id, err)
		return
	}
	if !h.allowed(w, r, id, t.Class.ID, t.Subject.ID) {
		return
	}
	scores := map[string]string{}
	for key, values := range r.PostForm {
		if enrollmentID, found := strings.CutPrefix(key, scorePrefix); found && len(values) > 0 {
			scores[enrollmentID] = values[len(values)-1]
		}
	}
	res, err := h.svc.Save(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, t, scores)
	var se *ScoreError
	switch {
	case errors.As(err, &se):
		h.show(w, r, id, http.StatusUnprocessableEntity, t.Class.ID, t.Subject.ID, t.Component.ID, scores, se.Fields)
		return
	case errors.Is(err, ErrComponentInactive):
		page.BadRequest(h.render, w, r, id, "Komponen nonaktif", "Komponen "+t.Component.Name+" sudah dinonaktifkan, nilainya tidak bisa diubah.")
		return
	case errors.Is(err, ErrUnknownEnrollment):
		page.BadRequest(h.render, w, r, id, "Data tidak valid", "Isian nilai tidak cocok dengan daftar siswa. Muat ulang halaman lalu coba lagi.")
		return
	case err != nil:
		h.serverError(w, r, "save grades", err)
		return
	}
	msg := fmt.Sprintf("Nilai %s %s kelas %s disimpan: %d berubah, %d sudah dinilai", t.Component.Name, t.Subject.Name, t.Class.Name, res.Changed, res.Graded)
	if res.Empty > 0 {
		msg += fmt.Sprintf(", %d belum dinilai", res.Empty)
	}
	auth.SetFlash(r.Context(), h.sessions, msg+".")
	http.Redirect(w, r, inputURL(t.Class.ID, t.Subject.ID, t.Component.ID), http.StatusSeeOther)
}

func (h *InputHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
