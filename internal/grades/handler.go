package grades

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
)

const (
	recapPath       = "/nilai/rekap"
	reportCardPath  = "/nilai/rapor"
	pageCardBlocked = "grades/report_card_blocked"
	pageSummary     = "grades/summary"
	msgNoClassView  = "Ringkasan nilai kelas ini hanya untuk wali kelasnya, admin/TU, dan kepala sekolah. Buka rekap mapel yang Anda ajar."
	msgNoRecap      = "Rekap nilai mapel ini hanya untuk guru pengampunya di kelas ini, wali kelas, admin/TU, dan kepala sekolah."
)

// ReportHandler melayani rekap nilai dan rapor PDF.
type ReportHandler struct {
	db       database.DB
	input    *InputService
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewReportHandler mengembalikan ReportHandler. Route-nya harus berada di
// dalam grup RequireLogin.
func NewReportHandler(db database.DB, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *ReportHandler {
	return &ReportHandler{db: db, input: NewInputService(db), sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route rekap nilai. Semuanya baca saja.
//
//   - admin_tu, kepala_sekolah: semua kelas dan mapel;
//   - guru: rekap mapel yang diajarnya di kelas itu, dan ringkasan serta
//     rekap semua mapel kelas yang ia walikan. Selain itu 403.
func (h *ReportHandler) Routes(r chi.Router) {
	r.Route(recapPath, func(r chi.Router) {
		// Peran: admin_tu, kepala_sekolah (semua), guru (mapel yang diajar; kelas perwalian).
		r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleGuru)).Get("/", h.page)
	})
	// Peran: admin_tu (semua kelas), guru (hanya wali kelas siswa itu).
	r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleGuru)).Get(reportCardPath+"/{enrollmentID}.pdf", h.reportCard)
}

type summaryView struct {
	User     auth.Identity
	NoYear   bool
	Staff    bool            // admin/TU atau kepala sekolah
	Classes  []classes.Class // pilihan kelas untuk staf
	Teaching []Teaching      // halaman awal guru: mapel yang diajar
	Homeroom []classes.Class // halaman awal guru: kelas perwalian
	Class    *classes.Class
	Summary  *ClassReport
	Subject  *SubjectReport
	CanClass bool // boleh membuka ringkasan kelas (tautan kembali)
	// CanPrint: admin/TU atau wali kelas; Cards berisi kesiapan rapor per siswa.
	CanPrint bool
	Cards    []CardStatus
	Semester int
	Flash    string
	Error    string
}

func isStaff(id auth.Identity) bool {
	return id.HasRole(auth.RoleAdminTU) || id.HasRole(auth.RoleKepalaSekolah)
}

func isHomeroom(id auth.Identity, c classes.Class) bool {
	return id.HasRole(auth.RoleGuru) && c.HomeroomTeacherID != "" && c.HomeroomTeacherID == id.UserID
}

func (h *ReportHandler) page(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	v := summaryView{User: id, Staff: isStaff(id)}
	year, err := activeYear(r.Context(), h.db, id.SchoolID)
	if errors.Is(err, ErrNoActiveYear) {
		v.NoYear = true
		h.render.Render(w, r, http.StatusOK, pageSummary, v)
		return
	}
	if err != nil {
		h.serverError(w, r, "active year", err)
		return
	}
	classID := q.Get("kelas")
	if v.Staff {
		if v.Classes, err = classes.ListByYear(r.Context(), h.db, id.SchoolID, year.ID); err != nil {
			h.serverError(w, r, "list classes", err)
			return
		}
		if classID == "" && len(v.Classes) > 0 {
			classID = v.Classes[0].ID
		}
	}
	if classID == "" {
		h.teacherHome(w, r, id, year.ID, v)
		return
	}

	c, err := activeClass(r.Context(), h.db, id.SchoolID, classID)
	if errors.Is(err, ErrClassNotFound) {
		page.NotFound(h.render, w, r, id, msgClassGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get class", err)
		return
	}
	v.Class = &c
	v.CanClass = v.Staff || isHomeroom(id, c)

	subjectID := q.Get("mapel")
	if subjectID == "" {
		if !v.CanClass {
			page.Forbidden(h.render, w, r, id, msgNoClassView)
			return
		}
		rep, err := ClassSummary(r.Context(), h.db, id.SchoolID, c)
		if err != nil {
			h.serverError(w, r, "class summary", err)
			return
		}
		v.Summary = &rep
		if v.CanPrint = canPrint(id, c); v.CanPrint {
			v.Cards, v.Semester = rep.CardStatuses(), DefaultSemester(time.Now())
		}
		h.render.Render(w, r, http.StatusOK, pageSummary, v)
		return
	}

	sub, err := getSubject(r.Context(), h.db, id.SchoolID, subjectID)
	if errors.Is(err, ErrSubjectNotFound) {
		page.NotFound(h.render, w, r, id, msgSubjectGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get subject", err)
		return
	}
	if !v.CanClass {
		ok, err := CanEnterGrades(r.Context(), h.db, id, c.ID, sub.ID)
		if err != nil {
			h.serverError(w, r, "check recap access", err)
			return
		}
		if !ok {
			page.Forbidden(h.render, w, r, id, msgNoRecap)
			return
		}
	}
	rep, err := SubjectSummary(r.Context(), h.db, id.SchoolID, c, sub)
	if err != nil {
		h.serverError(w, r, "subject summary", err)
		return
	}
	v.Subject = &rep
	h.render.Render(w, r, http.StatusOK, pageSummary, v)
}

// teacherHome menampilkan mapel yang diajar guru dan kelas perwaliannya.
func (h *ReportHandler) teacherHome(w http.ResponseWriter, r *http.Request, id auth.Identity, yearID string, v summaryView) {
	if id.HasRole(auth.RoleGuru) {
		var err error
		if v.Teaching, err = h.input.MyTeaching(r.Context(), id.SchoolID, id.UserID); err != nil {
			h.serverError(w, r, "my teaching", err)
			return
		}
		all, err := classes.ListByYear(r.Context(), h.db, id.SchoolID, yearID)
		if err != nil {
			h.serverError(w, r, "list classes", err)
			return
		}
		for _, c := range all {
			if isHomeroom(id, c) {
				v.Homeroom = append(v.Homeroom, c)
			}
		}
	}
	h.render.Render(w, r, http.StatusOK, pageSummary, v)
}

// canPrint: rapor dicetak admin/TU atau wali kelas (T74). Kepala sekolah
// dan guru mapel hanya melihat rekap.
func canPrint(id auth.Identity, c classes.Class) bool {
	return id.HasRole(auth.RoleAdminTU) || isHomeroom(id, c)
}

type blockedView struct {
	User     auth.Identity
	Student  string
	Class    classes.Class
	Subjects []string
	Flash    string
	Error    string
}

// reportCard mengirim rapor PDF satu siswa. Rapor hanya dibuat jika semua
// mapel lengkap; jika tidak, halaman 422 menyebut mapel yang kosong. Setiap
// cetak diaudit report_card.print sebelum PDF dikirim.
func (h *ReportHandler) reportCard(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	e, err := GetCardEnrollment(r.Context(), h.db, id.SchoolID, chi.URLParam(r, "enrollmentID"))
	if errors.Is(err, ErrEnrollmentNotFound) {
		page.NotFound(h.render, w, r, id, "Siswa tidak ditemukan di kelas tahun ajaran aktif sekolah Anda.")
		return
	}
	if err != nil {
		h.serverError(w, r, "get enrollment", err)
		return
	}
	if !canPrint(id, e.Class) {
		page.Forbidden(h.render, w, r, id, "Rapor hanya bisa dicetak oleh wali kelasnya dan admin/TU.")
		return
	}
	semester := DefaultSemester(time.Now())
	if raw := r.URL.Query().Get("semester"); raw != "" {
		if semester, err = strconv.Atoi(raw); err != nil || (semester != 1 && semester != 2) {
			page.BadRequest(h.render, w, r, id, "Semester tidak valid", "Pilih semester 1 (Ganjil) atau 2 (Genap).")
			return
		}
	}
	card, err := BuildReportCard(r.Context(), h.db, id.SchoolID, e, semester, clock.Today())
	var ie *IncompleteError
	switch {
	case errors.As(err, &ie):
		var name string
		_ = h.db.QueryRow(r.Context(), `SELECT full_name FROM students WHERE school_id = $1 AND id = $2`, id.SchoolID, e.StudentID).Scan(&name)
		h.render.Render(w, r, http.StatusUnprocessableEntity, pageCardBlocked, blockedView{User: id, Student: name, Class: e.Class, Subjects: ie.Subjects})
		return
	case errors.Is(err, ErrEnrollmentNotFound):
		page.NotFound(h.render, w, r, id, "Siswa tidak ditemukan di kelas tahun ajaran aktif sekolah Anda.")
		return
	case err != nil:
		h.serverError(w, r, "build report card", err)
		return
	}
	var buf bytes.Buffer
	if err := WriteReportCardPDF(&buf, card, true); err != nil {
		h.serverError(w, r, "render report card", err)
		return
	}
	err = pgx.BeginFunc(r.Context(), h.db, func(tx pgx.Tx) error {
		return audit.Record(r.Context(), tx, audit.Entry{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r),
			Action: ActionReportCardPrint, EntityType: "enrollment", EntityID: e.ID,
			After: map[string]any{"class_id": e.Class.ID, "semester": semester, "subjects": len(card.Subjects)}})
	})
	if err != nil {
		h.serverError(w, r, "audit report card", err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/pdf")
	hdr.Set("Content-Disposition", `attachment; filename="`+ReportCardFilename(card)+`"`)
	hdr.Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

func (h *ReportHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
