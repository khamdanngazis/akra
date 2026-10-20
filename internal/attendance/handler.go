package attendance

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	attendancePath    = "/absensi"
	dateLayout        = "2006-01-02"
	maxAttendForm     = 64 << 10
	statusFieldPrefix = "s_"
	msgNoAccess       = "Absensi kelas ini hanya bisa diisi oleh wali kelasnya."
	msgNoSubject      = "Absensi mapel ini hanya bisa diisi oleh guru pengampunya di kelas ini."
)

// Handler melayani form absensi guru.
type Handler struct {
	svc      *Service
	db       database.DBTX
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
	today    func() time.Time
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, db database.DBTX, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, db: db, sessions: sessions, render: render, logger: logger, today: clock.Today}
}

// Routes mendaftarkan route absensi. Form hanya untuk peran guru, dan di
// dalamnya hanya wali kelas yang boleh membuka kelasnya (CanEditClass).
// Rekap untuk admin_tu, kepala_sekolah, dan guru untuk kelas perwaliannya.
func (h *Handler) Routes(r chi.Router) {
	r.Route(attendancePath, func(r chi.Router) {
		teacher := auth.RequireRole(auth.RoleGuru)
		// Peran: guru (wali kelas dari kelas yang dibuka).
		r.With(teacher).Get("/", h.form)
		// Peran: guru (wali kelas dari kelas sesi).
		r.With(teacher).Post("/{sessionID}", h.save)
		// Peran: admin_tu, kepala_sekolah (semua kelas), guru (kelas perwaliannya).
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleGuru)).Get("/rekap", h.report)
	})
	// Peran: admin_tu.
	r.With(auth.RequireRole(auth.RoleAdminTU)).Post(settingsPath, h.saveSettings)
}

type classesView struct {
	User    auth.Identity
	Classes []TeacherClass
	Flash   string
	Error   string
}

// modeOption adalah pilihan jenis absensi di form: harian atau satu mapel.
type modeOption struct {
	Label, URL string
	Active     bool
}

type formView struct {
	User      auth.Identity
	Session   Session
	Modes     []modeOption // kosong jika absensi per mapel nonaktif
	Records   []Record
	Summary   Summary
	Statuses  []statusOption
	Date      string // YYYY-MM-DD
	DateLabel string
	Today     string
	IsToday   bool
	PrevURL   string
	NextURL   string // kosong jika besok sudah masa depan
	ReportURL string
	Flash     string
	Error     string
}

type statusOption struct {
	Value, Label, Short string
}

var statusOptions = []statusOption{
	{StatusPresent, "Hadir", "H"}, {StatusSick, "Sakit", "S"}, {StatusExcused, "Izin", "I"}, {StatusAbsent, "Alpa", "A"},
}

func (h *Handler) form(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	classID := q.Get("kelas")
	today := h.today()

	if classID == "" {
		classes, err := TeacherClasses(r.Context(), h.db, id.SchoolID, id.UserID)
		if err != nil {
			h.serverError(w, r, "list homeroom classes", err)
			return
		}
		if len(classes) == 1 {
			http.Redirect(w, r, formURL(classes[0].ID, "", today), http.StatusSeeOther)
			return
		}
		h.render.Render(w, r, http.StatusOK, "attendance/index", classesView{User: id, Classes: classes, Flash: auth.PopFlash(r.Context(), h.sessions)})
		return
	}

	date := today
	if raw := q.Get("tanggal"); raw != "" {
		parsed, err := time.Parse(dateLayout, raw)
		if err != nil {
			page.BadRequest(h.render, w, r, id, "Tanggal tidak valid", "Gunakan format tanggal YYYY-MM-DD.")
			return
		}
		date = parsed
	}
	if err := CheckDate(date, today); err != nil {
		page.BadRequest(h.render, w, r, id, "Tanggal belum tiba", "Absensi hanya bisa diisi untuk hari ini atau tanggal yang sudah lewat.")
		return
	}

	perSubject, err := PerSubjectEnabled(r.Context(), h.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "attendance settings", err)
		return
	}
	subjectID := q.Get("mapel")
	if subjectID != "" && !perSubject {
		page.BadRequest(h.render, w, r, id, "Absensi per mapel nonaktif", "Sekolah belum mengaktifkan absensi per mapel. Isi absensi harian.")
		return
	}
	if !h.canEdit(w, r, id, classID, subjectID) {
		return
	}

	session, records, err := h.svc.OpenSession(r.Context(), actorFrom(r, id), classID, subjectID, date)
	switch {
	case errors.Is(err, ErrDateOutsideYear):
		page.BadRequest(h.render, w, r, id, "Di luar tahun ajaran", "Tanggal tersebut di luar tahun ajaran kelas ini.")
		return
	case errors.Is(err, ErrClassNotFound):
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan.")
		return
	case errors.Is(err, ErrSubjectNotFound):
		page.NotFound(h.render, w, r, id, "Mata pelajaran tidak ditemukan.")
		return
	case err != nil:
		h.serverError(w, r, "open attendance session", err)
		return
	}

	view := formView{
		User: id, Session: session, Records: records, Summary: Summarize(records), Statuses: statusOptions,
		Date: date.Format(dateLayout), DateLabel: clock.FormatDateID(date), Today: today.Format(dateLayout),
		IsToday: date.Equal(today), PrevURL: formURL(classID, subjectID, date.AddDate(0, 0, -1)), ReportURL: reportURL(classID, subjectID),
		Flash: auth.PopFlash(r.Context(), h.sessions),
	}
	if next := date.AddDate(0, 0, 1); !next.After(today) {
		view.NextURL = formURL(classID, subjectID, next)
	}
	if perSubject {
		if view.Modes, err = h.modes(r, id, classID, subjectID, date); err != nil {
			h.serverError(w, r, "attendance modes", err)
			return
		}
	}
	h.render.Render(w, r, http.StatusOK, "attendance/form", view)
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxAttendForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Data tidak valid", "Isian absensi terlalu besar atau rusak.")
		return
	}

	session, err := h.svc.Session(r.Context(), id.SchoolID, chi.URLParam(r, "sessionID"))
	if errors.Is(err, ErrSessionNotFound) {
		page.NotFound(h.render, w, r, id, "Sesi absensi tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "get session", err)
		return
	}
	if !h.canEdit(w, r, id, session.ClassID, session.SubjectID) {
		return
	}
	if err := CheckDate(session.SessionDate, h.today()); err != nil {
		page.BadRequest(h.render, w, r, id, "Tanggal belum tiba", "Absensi hanya bisa diisi untuk hari ini atau tanggal yang sudah lewat.")
		return
	}

	statuses := map[string]string{}
	for key, values := range r.PostForm {
		if enrollmentID, found := strings.CutPrefix(key, statusFieldPrefix); found && len(values) > 0 {
			statuses[enrollmentID] = values[len(values)-1]
		}
	}
	_, sum, err := h.svc.SaveStatuses(r.Context(), actorFrom(r, id), session.ID, statuses)
	switch {
	case errors.Is(err, ErrUnknownEnrollment), errors.Is(err, ErrInvalidStatus):
		page.BadRequest(h.render, w, r, id, "Data tidak valid", "Isian absensi tidak cocok dengan daftar siswa. Muat ulang halaman lalu coba lagi.")
		return
	case err != nil:
		h.serverError(w, r, "save attendance", err)
		return
	}
	what := "Absensi"
	if session.SubjectName != "" {
		what = "Absensi " + session.SubjectName
	}
	auth.SetFlash(r.Context(), h.sessions, fmt.Sprintf("%s %s tersimpan: %d hadir, %d sakit, %d izin, %d alpa.",
		what, clock.FormatDateID(session.SessionDate), sum.Hadir, sum.Sakit, sum.Izin, sum.Alpa))
	http.Redirect(w, r, formURL(session.ClassID, session.SubjectID, session.SessionDate), http.StatusSeeOther)
}

// canEdit memeriksa hak guru atas absensi harian (subjectID kosong: wali
// kelas atau guru yang mengajar di kelas itu) atau absensi mapel subjectID
// (hanya guru pengampu mapel itu di kelas itu). Menulis 403 jika ditolak.
func (h *Handler) canEdit(w http.ResponseWriter, r *http.Request, id auth.Identity, classID, subjectID string) bool {
	var ok bool
	var err error
	msg := msgNoAccess
	if subjectID == "" {
		ok, err = CanEditClass(r.Context(), h.db, id, classID)
	} else {
		ok, err = CanEditSubject(r.Context(), h.db, id, classID, subjectID)
		msg = msgNoSubject
	}
	if err != nil {
		h.serverError(w, r, "check class access", err)
		return false
	}
	if !ok {
		page.Forbidden(h.render, w, r, id, msg)
	}
	return ok
}

// modes menyusun pilihan jenis absensi: harian lalu mapel yang diajar guru
// di kelas ini.
func (h *Handler) modes(r *http.Request, id auth.Identity, classID, subjectID string, date time.Time) ([]modeOption, error) {
	subjects, err := TeacherSubjects(r.Context(), h.db, id.SchoolID, id.UserID, classID)
	if err != nil {
		return nil, err
	}
	out := []modeOption{{Label: "Harian", URL: formURL(classID, "", date), Active: subjectID == ""}}
	for _, s := range subjects {
		out = append(out, modeOption{Label: s.Name, URL: formURL(classID, s.ID, date), Active: s.ID == subjectID})
	}
	return out, nil
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func formURL(classID, subjectID string, date time.Time) string {
	v := url.Values{"kelas": {classID}, "tanggal": {date.Format(dateLayout)}}
	if subjectID != "" {
		v.Set("mapel", subjectID)
	}
	return attendancePath + "?" + v.Encode()
}

func actorFrom(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
