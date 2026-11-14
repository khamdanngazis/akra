package dapodik

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
	"akra/internal/platform/pii"
	"akra/internal/platform/storage"
	"akra/internal/school"
	"akra/internal/students"
)

const (
	basePath       = "/dapodik"
	msgStudentGone = "Siswa yang Anda cari tidak ada atau bukan milik sekolah Anda."
	// ActionEntered dicatat saat siswa ditandai sudah dimasukkan ke Dapodik.
	ActionEntered = "dapodik.entered"
)

// Handler melayani halaman bantu Dapodik.
type Handler struct {
	db       database.DB
	keys     *pii.Keyring
	store    storage.Store
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DB, keys *pii.Keyring, store storage.Store, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, keys: keys, store: store, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route Dapodik ke r.
func (h *Handler) Routes(r chi.Router) {
	r.Route(basePath, func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah))
			// Peran: admin_tu, kepala_sekolah. Daftar siswa dan status Dapodik.
			r.Get("/", h.index)
			// Peran: admin_tu, kepala_sekolah. F-PD terisi (T99), memuat NIK.
			r.Get("/siswa/{id}/fpd.pdf", h.studentFPD)
			// Peran: admin_tu, kepala_sekolah.
			r.Get("/kelas/{classID}/fpd.pdf", h.classFPD)
			// Peran: admin_tu, kepala_sekolah. F-PD kosong.
			r.Get("/fpd-kosong.pdf", h.blankFPD)
			// Peran: admin_tu, kepala_sekolah. Ekspor Daftar PD (T100), memuat NIK; diaudit.
			r.Get("/ekspor-pd.xlsx", h.exportPD)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU))
			// Peran: admin_tu. Lembar salin memuat NIK utuh (T98).
			r.Get("/siswa/{id}", h.copySheet)
			// Peran: admin_tu.
			r.Post("/siswa/{id}/tandai", h.markEntered)
			// Peran: admin_tu. Impor ekspor Daftar PD Dapodik (T101).
			r.Get("/impor", h.importForm)
			// Peran: admin_tu.
			r.Post("/impor", h.importPreview)
			// Peran: admin_tu.
			r.Post("/impor/terapkan", h.importApply)
		})
	})
}

// load mengumpulkan data satu siswa untuk lembar salin, F-PD, dan ekspor.
func load(ctx context.Context, db database.DB, keys *pii.Keyring, schoolID, studentID string) (Input, error) {
	st, err := students.GetByID(ctx, db, schoolID, studentID)
	if err != nil {
		return Input{}, err
	}
	prof, err := students.NewProfileService(db, keys).Get(ctx, schoolID, st.ID)
	if err != nil {
		return Input{}, err
	}
	list, err := students.ListByStudent(ctx, db, schoolID, st.ID)
	if err != nil {
		return Input{}, err
	}
	in := Input{Student: st, Profile: prof, Parents: map[string]Parent{}}
	details := students.NewGuardianDetailService(db, keys)
	for _, role := range students.ParentRoles {
		g, ok := students.FindParent(list, role)
		if !ok {
			continue
		}
		d, err := details.Get(ctx, schoolID, g.ID)
		if err != nil {
			return Input{}, err
		}
		in.Parents[role] = Parent{Name: g.FullName, Details: d}
	}
	return in, nil
}

type indexRow struct {
	ID, NIS, FullName, ClassName string
	Percent                      int
	EnteredAt                    string
}

type indexView struct {
	User    auth.Identity
	CanEdit bool
	Classes []option
	ClassID string
	Status  string
	Rows    []indexRow
	Flash   string
	Error   string
}

type option struct {
	Value, Label string
	Selected     bool
}

// index menampilkan siswa aktif beserta kelengkapan dan tanda sudah masuk
// Dapodik. Peran: admin_tu, kepala_sekolah.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	q := r.URL.Query()
	view := indexView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Status: q.Get("status"), Flash: auth.PopFlash(ctx, h.sessions)}
	year, err := school.GetActiveAcademicYear(ctx, h.db, id.SchoolID)
	yearID := ""
	if err == nil {
		yearID = year.ID
		list, err := classes.ListByYear(ctx, h.db, id.SchoolID, year.ID)
		if err != nil {
			h.serverError(w, r, "list classes", err)
			return
		}
		for _, c := range list {
			if c.ID == q.Get("kelas") {
				view.ClassID = c.ID
			}
			view.Classes = append(view.Classes, option{Value: c.ID, Label: c.Name, Selected: c.ID == q.Get("kelas")})
		}
	} else if !errors.Is(err, school.ErrNotFound) {
		h.serverError(w, r, "active year", err)
		return
	}
	rows, err := h.db.Query(ctx, `
		SELECT s.id, s.nis, s.full_name, coalesce(c.name, ''), `+students.ReadinessPercentSQL()+`, p.dapodik_entered_at
		FROM students s
		LEFT JOIN student_profiles p ON p.school_id = s.school_id AND p.student_id = s.id
		LEFT JOIN enrollments e ON e.school_id = s.school_id AND e.student_id = s.id AND e.academic_year_id = $2::uuid AND e.end_date IS NULL
		LEFT JOIN classes c ON c.id = e.class_id
		WHERE s.school_id = $1 AND s.status = 'aktif'
		  AND ($3::uuid IS NULL OR e.class_id = $3::uuid)
		  AND (CASE $4 WHEN 'belum' THEN p.dapodik_entered_at IS NULL WHEN 'sudah' THEN p.dapodik_entered_at IS NOT NULL ELSE true END)
		ORDER BY c.name NULLS LAST, lower(s.full_name) LIMIT 1000`,
		id.SchoolID, nilIfEmpty(yearID), nilIfEmpty(view.ClassID), view.Status)
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x indexRow
		var entered *time.Time
		if err := rows.Scan(&x.ID, &x.NIS, &x.FullName, &x.ClassName, &x.Percent, &entered); err != nil {
			h.serverError(w, r, "scan students", err)
			return
		}
		if entered != nil {
			x.EnteredAt = clock.FormatDateID(entered.In(clock.Jakarta))
		}
		view.Rows = append(view.Rows, x)
	}
	if err := rows.Err(); err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "dapodik/index", view)
}

type sheetView struct {
	User      auth.Identity
	Student   students.Student
	ClassName string
	Sections  []SheetSection
	Readiness students.Readiness
	EnteredAt string
	NextID    string
	NextName  string
	Version   string
	Flash     string
	Error     string
}

// copySheet menampilkan lembar salin satu siswa. Peran: admin_tu.
func (h *Handler) copySheet(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	in, err := load(ctx, h.db, h.keys, id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, students.ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgStudentGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "load student", err)
		return
	}
	readiness, err := students.GetReadiness(ctx, h.db, id.SchoolID, in.Student.ID)
	if err != nil {
		h.serverError(w, r, "readiness", err)
		return
	}
	view := sheetView{User: id, Student: in.Student, Sections: Sheet(in), Readiness: readiness, Version: LayoutVersion,
		Flash: auth.PopFlash(ctx, h.sessions)}
	var entered *time.Time
	var classID *string
	err = h.db.QueryRow(ctx, `
		SELECT p.dapodik_entered_at, e.class_id::text, coalesce(c.name, '')
		FROM students s
		LEFT JOIN student_profiles p ON p.student_id = s.id
		LEFT JOIN academic_years y ON y.school_id = s.school_id AND y.is_active
		LEFT JOIN enrollments e ON e.student_id = s.id AND e.academic_year_id = y.id AND e.end_date IS NULL
		LEFT JOIN classes c ON c.id = e.class_id
		WHERE s.school_id = $1 AND s.id = $2`, id.SchoolID, in.Student.ID).Scan(&entered, &classID, &view.ClassName)
	if err != nil {
		h.serverError(w, r, "student status", err)
		return
	}
	if entered != nil {
		view.EnteredAt = clock.FormatDateID(entered.In(clock.Jakarta))
	}
	if classID != nil {
		// Siswa berikutnya di kelas yang sama (urut nama), agar operator bisa
		// bekerja per kelas.
		err := h.db.QueryRow(ctx, `
			SELECT s.id, s.full_name FROM enrollments e JOIN students s ON s.id = e.student_id
			WHERE e.school_id = $1 AND e.class_id = $2 AND e.end_date IS NULL AND s.status = 'aktif'
			  AND (lower(s.full_name), s.id::text) > (lower($3), $4)
			ORDER BY lower(s.full_name), s.id LIMIT 1`, id.SchoolID, *classID, in.Student.FullName, in.Student.ID).Scan(&view.NextID, &view.NextName)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			h.serverError(w, r, "next student", err)
			return
		}
	}
	h.render.Render(w, r, http.StatusOK, "dapodik/sheet", view)
}

// markEntered menandai (atau membatalkan tanda) siswa sudah dimasukkan ke
// Dapodik. Peran: admin_tu.
func (h *Handler) markEntered(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	studentID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	entered := r.PostForm.Get("entered") != "0"
	actor := school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
	err := pgx.BeginFunc(r.Context(), h.db, func(tx pgx.Tx) error {
		if _, err := students.GetByID(r.Context(), tx, id.SchoolID, studentID); err != nil {
			return err
		}
		var at, by any
		if entered {
			at, by = time.Now(), actor.UserID
		}
		// Siswa tanpa data rinci mendapat baris kosong agar tanda bisa disimpan.
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO student_profiles (student_id, school_id, dapodik_entered_at, dapodik_entered_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (student_id) DO UPDATE SET dapodik_entered_at = EXCLUDED.dapodik_entered_at, dapodik_entered_by = EXCLUDED.dapodik_entered_by`,
			studentID, id.SchoolID, at, by); err != nil {
			return fmt.Errorf("dapodik: mark entered: %w", err)
		}
		return audit.Record(r.Context(), tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionEntered, EntityType: "student", EntityID: studentID, After: map[string]any{"entered": entered}})
	})
	switch {
	case err == nil:
		msg := "Ditandai sudah masuk Dapodik."
		if !entered {
			msg = "Tanda sudah masuk Dapodik dibatalkan."
		}
		auth.SetFlash(r.Context(), h.sessions, msg)
		next := basePath + "/siswa/" + studentID
		if n := r.PostForm.Get("next"); n != "" && database.IsUUID(n) {
			next = basePath + "/siswa/" + n
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	case errors.Is(err, students.ErrNotFound):
		page.NotFound(h.render, w, r, id, msgStudentGone)
	default:
		h.serverError(w, r, "mark entered", err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func nilIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
