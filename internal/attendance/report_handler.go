package attendance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

type reportOption struct {
	ID, Label string
	Selected  bool
}

type reportRow struct {
	NIS, FullName string
	Counts
	Percent string
}

type reportView struct {
	User      auth.Identity
	Classes   []reportOption
	Class     *ClassYear
	From, To  string // YYYY-MM-DD
	Range     string // label rentang
	YearRange string
	// Subjects berisi pilihan jenis rekap (harian lalu mapel) jika sekolah
	// memakai absensi per mapel; kosong jika tidak.
	Subjects    []reportOption
	SubjectName string // kosong untuk rekap harian
	Summary     ClassSummary
	Percent     string
	Rows        []reportRow
	Error       string
	Flash       string
}

// canViewReport: admin_tu dan kepala_sekolah boleh melihat rekap semua kelas
// sekolahnya; guru hanya kelas yang ia walikan.
func (h *Handler) canViewReport(ctx context.Context, id auth.Identity, classID string) (bool, error) {
	if id.HasRole(auth.RoleAdminTU) || id.HasRole(auth.RoleKepalaSekolah) {
		return true, nil
	}
	return CanEditClass(ctx, h.db, id, classID)
}

// reportClasses menyusun pilihan kelas: semua kelas tahun aktif untuk
// admin/kepala sekolah, kelas perwalian untuk guru.
func (h *Handler) reportClasses(ctx context.Context, id auth.Identity) ([]TeacherClass, error) {
	if !id.HasRole(auth.RoleAdminTU) && !id.HasRole(auth.RoleKepalaSekolah) {
		return TeacherClasses(ctx, h.db, id.SchoolID, id.UserID)
	}
	rows, err := h.db.Query(ctx, `
		SELECT c.id, c.name, y.name
		FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.school_id = c.school_id
		WHERE c.school_id = $1 AND y.is_active
		ORDER BY c.grade_level, lower(c.name)`, id.SchoolID)
	if err != nil {
		return nil, fmt.Errorf("attendance: list report classes: %w", err)
	}
	defer rows.Close()
	var list []TeacherClass
	for rows.Next() {
		var c TeacherClass
		if err := rows.Scan(&c.ID, &c.Name, &c.YearName); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	classes, err := h.reportClasses(r.Context(), id)
	if err != nil {
		h.serverError(w, r, "list report classes", err)
		return
	}
	classID := q.Get("kelas")
	if classID == "" && len(classes) > 0 {
		classID = classes[0].ID
	}
	view := reportView{User: id}
	for _, c := range classes {
		view.Classes = append(view.Classes, reportOption{ID: c.ID, Label: c.Name + " · " + c.YearName, Selected: c.ID == classID})
	}
	if classID == "" {
		h.render.Render(w, r, http.StatusOK, "attendance/rekap", view)
		return
	}

	ok, err := h.canViewReport(r.Context(), id, classID)
	if err != nil {
		h.serverError(w, r, "check report access", err)
		return
	}
	if !ok {
		page.Forbidden(h.render, w, r, id, "Rekap kelas ini hanya bisa dibuka oleh wali kelasnya, admin, atau kepala sekolah.")
		return
	}
	class, err := GetClassYear(r.Context(), h.db, id.SchoolID, classID)
	if errors.Is(err, ErrClassNotFound) {
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "get class year", err)
		return
	}
	view.Class = &class
	view.YearRange = clock.FormatDateID(class.YearStart) + " – " + clock.FormatDateID(class.YearEnd)
	if !containsOption(view.Classes, classID) {
		// Kelas tahun ajaran lain (dibuka lewat tautan): tetap tampilkan di pilihan.
		view.Classes = append(view.Classes, reportOption{ID: class.ClassID, Label: class.ClassName + " · " + class.YearName, Selected: true})
	}

	// Default: awal tahun ajaran sampai hari ini (atau akhir tahun ajaran).
	to := minDate(h.today(), class.YearEnd)
	if to.Before(class.YearStart) {
		to = class.YearEnd
	}
	from := class.YearStart
	status := http.StatusOK
	if raw := q.Get("dari"); raw != "" {
		if from, err = time.Parse(dateLayout, raw); err != nil {
			view.Error, status = "Tanggal awal tidak valid.", http.StatusBadRequest
		}
	}
	if raw := q.Get("sampai"); raw != "" && view.Error == "" {
		if to, err = time.Parse(dateLayout, raw); err != nil {
			view.Error, status = "Tanggal akhir tidak valid.", http.StatusBadRequest
		}
	}
	view.From, view.To = from.Format(dateLayout), to.Format(dateLayout)
	if view.Error == "" {
		switch err := class.CheckRange(from, to); {
		case errors.Is(err, ErrInvalidRange):
			view.Error, status = "Tanggal awal harus sebelum atau sama dengan tanggal akhir.", http.StatusBadRequest
		case errors.Is(err, ErrRangeOutsideYear):
			view.Error = fmt.Sprintf("Rekap dibatasi satu tahun ajaran. Pilih tanggal di dalam tahun ajaran %s (%s).", class.YearName, view.YearRange)
			status = http.StatusBadRequest
		}
	}
	if view.Error != "" {
		h.render.Render(w, r, status, "attendance/rekap", view)
		return
	}

	subjectID, err := h.reportSubjects(r.Context(), &view, id.SchoolID, classID, q.Get("mapel"))
	if err != nil {
		h.serverError(w, r, "report subjects", err)
		return
	}
	students, err := SummaryByStudent(r.Context(), h.db, id.SchoolID, classID, subjectID, from, to)
	if err != nil {
		h.serverError(w, r, "summary by student", err)
		return
	}
	if view.Summary, err = SummaryByClass(r.Context(), h.db, id.SchoolID, classID, subjectID, from, to); err != nil {
		h.serverError(w, r, "summary by class", err)
		return
	}
	view.Percent = FormatPercent(view.Summary.PresentPercent())
	view.Range = clock.FormatDateID(from) + " – " + clock.FormatDateID(to)
	for _, s := range students {
		view.Rows = append(view.Rows, reportRow{NIS: s.NIS, FullName: s.FullName, Counts: s.Counts, Percent: FormatPercent(s.PresentPercent())})
	}
	h.render.Render(w, r, http.StatusOK, "attendance/rekap", view)
}

// reportSubjects mengisi pilihan jenis rekap untuk kelas classID dan
// mengembalikan mapel yang dipakai. Pilihan tampil jika absensi per mapel
// aktif atau kelas sudah punya sesi mapel. Mapel yang bukan pilihan kelas
// ini (mis. tersisa setelah ganti kelas, atau milik sekolah lain) diganti
// rekap harian.
func (h *Handler) reportSubjects(ctx context.Context, view *reportView, schoolID, classID, requested string) (string, error) {
	enabled, err := PerSubjectEnabled(ctx, h.db, schoolID)
	if err != nil {
		return "", err
	}
	if !enabled {
		if enabled, err = HasSubjectSessions(ctx, h.db, schoolID, classID); err != nil || !enabled {
			return "", err
		}
	}
	subjects, err := ClassSubjects(ctx, h.db, schoolID, classID)
	if err != nil {
		return "", err
	}
	chosen := ""
	for _, s := range subjects {
		if s.ID == requested {
			chosen, view.SubjectName = s.ID, s.Name
		}
	}
	view.Subjects = append(view.Subjects, reportOption{ID: "", Label: "Harian", Selected: chosen == ""})
	for _, s := range subjects {
		view.Subjects = append(view.Subjects, reportOption{ID: s.ID, Label: s.Name, Selected: s.ID == chosen})
	}
	return chosen, nil
}

// FormatPercent memformat persepuluhan persen sebagai "92,5%", atau "–"
// jika belum ada catatan.
func FormatPercent(tenths int) string {
	if tenths < 0 {
		return "–"
	}
	return strconv.Itoa(tenths/10) + "," + strconv.Itoa(tenths%10) + "%"
}

func minDate(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func containsOption(opts []reportOption, id string) bool {
	for _, o := range opts {
		if o.ID == id {
			return true
		}
	}
	return false
}

// reportURL mengembalikan tautan rekap kelas classID, untuk mapel subjectID
// jika tidak kosong.
func reportURL(classID, subjectID string) string {
	v := url.Values{"kelas": {classID}}
	if subjectID != "" {
		v.Set("mapel", subjectID)
	}
	return attendancePath + "/rekap?" + v.Encode()
}
