package reports

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/attendance"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/reports/export"
	"akra/internal/school"
)

const (
	attendancePath = "/laporan/kehadiran"
	arrearsPath    = "/laporan/tunggakan"
	pageAttendance = "reports/attendance"
	dateLayout     = "2006-01-02"
)

// Handler melayani halaman dan unduhan laporan.
type Handler struct {
	db     database.DBTX
	render page.Renderer
	logger *slog.Logger
	today  func() time.Time
	// maxRows adalah batas ekspor langsung (MaxSyncRows; diperkecil di uji).
	maxRows int
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DBTX, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, render: render, logger: logger, today: clock.Today, maxRows: MaxSyncRows}
}

// Routes mendaftarkan route laporan.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		// Guru melihat rekap kelasnya lewat /absensi/rekap (T27).
		r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah))
		// Peran: admin_tu, kepala_sekolah.
		r.Get(attendancePath, h.attendance)
		// Peran: admin_tu, kepala_sekolah.
		r.Get(attendancePath+".xlsx", h.attendanceExcel)
		// Peran: admin_tu, kepala_sekolah.
		r.Get(attendancePath+".pdf", h.attendancePDF)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleBendahara, auth.RoleAdminTU, auth.RoleKepalaSekolah))
		// Peran: bendahara, admin_tu, kepala_sekolah. Halaman layarnya di
		// /keuangan/tunggakan (T42).
		r.Get(arrearsPath+".xlsx", h.arrearsExcel)
		// Peran: bendahara, admin_tu, kepala_sekolah.
		r.Get(arrearsPath+".pdf", h.arrearsPDF)
	})
}

type option struct {
	Value, Label string
	Selected     bool
}

type attendanceRowView struct {
	NIS, Name, Class         string
	Hadir, Sakit, Izin, Alpa int
	Percent                  string
}

type attendanceView struct {
	User     auth.Identity
	Classes  []option
	From, To string
	Report   AttendanceReport
	Range    string
	Rows     []attendanceRowView
	Total    attendanceRowView
	Query    string // query string untuk tautan unduhan
	Error    string
	Flash    string
}

// attendanceFilter membaca filter dari query. Tanpa tanggal: tahun ajaran
// kelas (atau tahun ajaran aktif untuk semua kelas) dari awal sampai hari
// ini, sama dengan default rekap kelas T27.
func (h *Handler) attendanceFilter(r *http.Request, schoolID string) (AttendanceFilter, string, error) {
	q := r.URL.Query()
	f := AttendanceFilter{ClassID: q.Get("kelas")}
	var yearStart, yearEnd time.Time
	if f.ClassID != "" {
		cy, err := attendance.GetClassYear(r.Context(), h.db, schoolID, f.ClassID)
		if err != nil {
			return f, "", err
		}
		yearStart, yearEnd = cy.YearStart, cy.YearEnd
	} else {
		y, err := school.GetActiveAcademicYear(r.Context(), h.db, schoolID)
		if err != nil && !errors.Is(err, school.ErrNotFound) {
			return f, "", err
		}
		yearStart, yearEnd = y.StartDate, y.EndDate
	}
	f.To = h.today()
	if !yearEnd.IsZero() && f.To.After(yearEnd) {
		f.To = yearEnd
	}
	f.From = yearStart
	if f.From.IsZero() || f.To.Before(f.From) {
		f.From, f.To = yearStart, yearEnd
	}
	var badDate string
	if raw := q.Get("dari"); raw != "" {
		if d, err := time.Parse(dateLayout, raw); err == nil {
			f.From = d
		} else {
			badDate = "Tanggal tidak valid."
		}
	}
	if raw := q.Get("sampai"); raw != "" {
		if d, err := time.Parse(dateLayout, raw); err == nil {
			f.To = d
		} else {
			badDate = "Tanggal tidak valid."
		}
	}
	return f, badDate, nil
}

func (h *Handler) attendance(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	v := attendanceView{User: id}
	f, badDate, err := h.attendanceFilter(r, id.SchoolID)
	if errors.Is(err, attendance.ErrClassNotFound) {
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "attendance report filter", err)
		return
	}
	v.Error = badDate
	if !f.From.IsZero() {
		v.From, v.To = f.From.Format(dateLayout), f.To.Format(dateLayout)
	}
	if err := h.classOptions(r, id.SchoolID, f.ClassID, &v); err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list classes", err)
		return
	}
	status := http.StatusOK
	if f.From.IsZero() {
		v.Error = "Belum ada tahun ajaran aktif. Pilih kelas atau isi rentang tanggal."
	} else if v.Error == "" {
		rep, err := BuildAttendanceReport(r.Context(), h.db, id.SchoolID, f)
		if msg, ok := rangeMessage(err); ok {
			v.Error, status = msg, http.StatusBadRequest
		} else if err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "attendance report", err)
			return
		} else {
			v.Report = rep
			v.Range = clock.FormatDateID(rep.From) + " – " + clock.FormatDateID(rep.To)
			for _, row := range rep.Rows {
				v.Rows = append(v.Rows, rowView(row.NIS, row.FullName, row.ClassName, row.Counts))
			}
			v.Total = rowView("", "Total", "", rep.Total)
			v.Query = url.Values{"kelas": {f.ClassID}, "dari": {v.From}, "sampai": {v.To}}.Encode()
		}
	}
	h.render.Render(w, r, status, pageAttendance, v)
}

func rowView(nis, name, class string, c attendance.Counts) attendanceRowView {
	return attendanceRowView{NIS: nis, Name: name, Class: class, Hadir: c.Hadir, Sakit: c.Sakit, Izin: c.Izin, Alpa: c.Alpa,
		Percent: attendance.FormatPercent(c.PresentPercent())}
}

func rangeMessage(err error) (string, bool) {
	switch {
	case errors.Is(err, attendance.ErrInvalidRange):
		return "Tanggal awal harus sebelum tanggal akhir.", true
	case errors.Is(err, attendance.ErrRangeOutsideYear):
		return "Rentang laporan maksimal satu tahun ajaran. Pilih tanggal di dalam satu tahun ajaran.", true
	}
	return "", false
}

func (h *Handler) classOptions(r *http.Request, schoolID, selected string, v *attendanceView) error {
	v.Classes = append(v.Classes, option{Value: "", Label: "Semua kelas", Selected: selected == ""})
	rows, err := h.db.Query(r.Context(), `
		SELECT c.id, c.name || ' · ' || y.name FROM classes c JOIN academic_years y ON y.school_id = c.school_id AND y.id = c.academic_year_id
		WHERE c.school_id = $1 ORDER BY y.start_date DESC, c.grade_level, lower(c.name)`, schoolID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var o option
		if err := rows.Scan(&o.Value, &o.Label); err != nil {
			return err
		}
		o.Selected = o.Value == selected
		v.Classes = append(v.Classes, o)
	}
	return rows.Err()
}

// attendanceTable mengubah laporan menjadi tabel ekspor (kolom sama dengan
// layar).
func attendanceTable(rep AttendanceReport, schoolName string, printed time.Time) export.Table {
	class := rep.ClassName
	if class == "" {
		class = "Semua kelas"
	}
	t := export.Table{
		Title: "Rekap Kehadiran", SchoolName: schoolName, PrintedAt: printed,
		Meta: []string{
			"Tahun ajaran: " + rep.YearName,
			"Kelas: " + class,
			"Periode: " + clock.FormatDateID(rep.From) + " – " + clock.FormatDateID(rep.To),
		},
		Columns: []export.Column{
			{Header: "NIS", Width: 24}, {Header: "Nama", Width: 58}, {Header: "Kelas", Width: 20},
			{Header: "Hadir", Width: 15, Numeric: true}, {Header: "Sakit", Width: 15, Numeric: true},
			{Header: "Izin", Width: 15, Numeric: true}, {Header: "Alpa", Width: 15, Numeric: true},
			{Header: "Kehadiran", Width: 24, Numeric: true},
		},
	}
	for _, row := range rep.Rows {
		t.Rows = append(t.Rows, []any{row.NIS, row.FullName, row.ClassName, row.Hadir, row.Sakit, row.Izin, row.Alpa, export.Percent(row.PresentPercent())})
	}
	t.Total = []any{"", "Total", "", rep.Total.Hadir, rep.Total.Sakit, rep.Total.Izin, rep.Total.Alpa, export.Percent(rep.Total.PresentPercent())}
	return t
}

func (h *Handler) attendanceExcel(w http.ResponseWriter, r *http.Request) {
	h.attendanceExport(w, r, "xlsx")
}

func (h *Handler) attendancePDF(w http.ResponseWriter, r *http.Request) {
	h.attendanceExport(w, r, "pdf")
}

func (h *Handler) attendanceExport(w http.ResponseWriter, r *http.Request, format string) {
	id, _ := auth.CurrentUser(r.Context())
	f, badDate, err := h.attendanceFilter(r, id.SchoolID)
	if errors.Is(err, attendance.ErrClassNotFound) {
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "attendance report filter", err)
		return
	}
	if badDate != "" || f.From.IsZero() {
		page.BadRequest(h.render, w, r, id, "Filter tidak valid", "Periksa kelas dan tanggal laporan.")
		return
	}
	rep, err := BuildAttendanceReport(r.Context(), h.db, id.SchoolID, f)
	if msg, ok := rangeMessage(err); ok {
		page.BadRequest(h.render, w, r, id, "Rentang tidak valid", msg)
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "attendance report", err)
		return
	}
	if len(rep.Rows) > h.maxRows {
		page.BadRequest(h.render, w, r, id, "Data terlalu besar", fmt.Sprintf("Laporan berisi lebih dari %d baris. Persempit filter (mis. per kelas).", h.maxRows))
		return
	}
	t := attendanceTable(rep, id.SchoolName, h.today())
	name := fmt.Sprintf("rekap-kehadiran-%s-%s.%s", rep.From.Format(dateLayout), rep.To.Format(dateLayout), format)
	h.writeExport(w, r, id, t, format, "Rekap", name)
}

func (h *Handler) arrearsExcel(w http.ResponseWriter, r *http.Request) { h.arrearsExport(w, r, "xlsx") }

func (h *Handler) arrearsPDF(w http.ResponseWriter, r *http.Request) { h.arrearsExport(w, r, "pdf") }

func (h *Handler) arrearsExport(w http.ResponseWriter, r *http.Request, format string) {
	id, _ := auth.CurrentUser(r.Context())
	f, _, _, badDate := billing.ParseArrearsQuery(r.URL.Query())
	if badDate {
		page.BadRequest(h.render, w, r, id, "Filter tidak valid", "Periksa tanggal jatuh tempo.")
		return
	}
	labels, err := h.arrearsFilterLabels(r, id.SchoolID, f)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "arrears filter labels", err)
		return
	}
	today := h.today()
	t, a, err := ArrearsReport(r.Context(), h.db, id.SchoolID, id.SchoolName, f, labels, today)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "arrears report", err)
		return
	}
	if len(a.Items) > h.maxRows {
		page.BadRequest(h.render, w, r, id, "Data terlalu besar", fmt.Sprintf("Laporan berisi lebih dari %d baris. Persempit filter (mis. per kelas).", h.maxRows))
		return
	}
	h.writeExport(w, r, id, t, format, "Tunggakan", fmt.Sprintf("tunggakan-%s.%s", today.Format(dateLayout), format))
}

// arrearsFilterLabels menjelaskan filter aktif untuk kepala laporan.
func (h *Handler) arrearsFilterLabels(r *http.Request, schoolID string, f billing.ArrearsFilter) ([]string, error) {
	class, fee := "Semua kelas", "Semua jenis"
	if database.IsUUID(f.ClassID) {
		if err := h.db.QueryRow(r.Context(), `SELECT c.name || ' · ' || y.name FROM classes c
			JOIN academic_years y ON y.school_id = c.school_id AND y.id = c.academic_year_id
			WHERE c.school_id = $1 AND c.id = $2`, schoolID, f.ClassID).Scan(&class); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if database.IsUUID(f.FeeTypeID) {
		if err := h.db.QueryRow(r.Context(), `SELECT name FROM fee_types WHERE school_id = $1 AND id = $2`, schoolID, f.FeeTypeID).Scan(&fee); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	labels := []string{"Kelas: " + class, "Jenis tagihan: " + fee}
	if !f.DueFrom.IsZero() || !f.DueTo.IsZero() {
		from, to := "awal", "akhir"
		if !f.DueFrom.IsZero() {
			from = clock.FormatDateID(f.DueFrom)
		}
		if !f.DueTo.IsZero() {
			to = clock.FormatDateID(f.DueTo)
		}
		labels = append(labels, "Jatuh tempo: "+from+" – "+to)
	}
	return labels, nil
}

// writeExport menulis t sebagai unduhan Excel atau PDF.
func (h *Handler) writeExport(w http.ResponseWriter, r *http.Request, id auth.Identity, t export.Table, format, sheet, filename string) {
	var buf bytes.Buffer
	var err error
	contentType := "application/pdf"
	if format == "xlsx" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		err = export.WriteExcel(&buf, t, sheet)
	} else {
		err = export.WritePDF(&buf, t)
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "export report", err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", contentType)
	hdr.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = buf.WriteTo(w)
}
