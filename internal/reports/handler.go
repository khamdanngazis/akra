package reports

import (
	"context"
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
	"akra/internal/platform/storage"
	"akra/internal/reports/builder"
	"akra/internal/reports/export"
	"akra/internal/school"
)

const (
	attendancePath = "/laporan/kehadiran"
	jobsPath       = "/laporan/jobs"
	arrearsPath    = "/laporan/tunggakan"
	pageAttendance = "reports/attendance"
	dateLayout     = "2006-01-02"
)

// Handler melayani halaman dan unduhan laporan.
type Handler struct {
	db     database.DB
	jobs   JobInserter   // nil: ekspor besar ditolak (tanpa job)
	store  storage.Store // file hasil job
	render page.Renderer
	logger *slog.Logger
	today  func() time.Time
	// maxRows adalah batas ekspor langsung (MaxSyncRows; diperkecil di uji).
	maxRows int
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DB, jobs JobInserter, store storage.Store, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, jobs: jobs, store: store, render: render, logger: logger, today: clock.Today, maxRows: MaxSyncRows}
}

// Routes mendaftarkan route laporan.
func (h *Handler) Routes(r chi.Router) {
	// Peran: admin_tu, bendahara, kepala_sekolah. Daftar laporan sesuai peran.
	r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleBendahara, auth.RoleKepalaSekolah)).Get("/laporan", h.hub)
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
		// Peran: pemilik job, atau kepala_sekolah (semua job sekolah).
		r.Get(jobsPath+"/{id}", h.jobStatus)
		// Peran: pemilik job, atau kepala_sekolah. Redirect ke URL 5 menit.
		r.Get(jobsPath+"/{id}/unduh", h.jobDownload)
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
	today := h.today()
	t, rows, err := buildTable(r.Context(), h.db, id.SchoolID, id.SchoolName, TypeAttendance, jobParamsFromAttendance(f, today), false)
	if msg, ok := rangeMessage(err); ok {
		page.BadRequest(h.render, w, r, id, "Rentang tidak valid", msg)
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "attendance report", err)
		return
	}
	h.deliver(w, r, id, t, rows, TypeAttendance, format, jobParamsFromAttendance(f, today))
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
	today := h.today()
	t, rows, err := buildTable(r.Context(), h.db, id.SchoolID, id.SchoolName, TypeArrears, jobParamsFromArrears(f, today), false)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "arrears report", err)
		return
	}
	h.deliver(w, r, id, t, rows, TypeArrears, format, jobParamsFromArrears(f, today))
}

// deliver mengirim file langsung jika baris laporan tidak melebihi maxRows;
// selain itu membuat job ekspor di server dan mengalihkan ke halaman
// statusnya (T49). Datanya sudah dihitung dengan fungsi yang sama dengan
// job, jadi tidak ada COUNT terpisah yang bisa berbeda.
func (h *Handler) deliver(w http.ResponseWriter, r *http.Request, id auth.Identity, t export.Table, rows int, reportType, format string, p JobParams) {
	if rows <= h.maxRows {
		data, err := renderTable(t, reportType, format)
		if err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "export report", err)
			return
		}
		h.sendFile(w, data, format, exportFilename(ReportJob{Type: reportType, Format: format, Params: p}, t.PrintedAt))
		return
	}
	if h.jobs == nil {
		page.BadRequest(h.render, w, r, id, "Data terlalu besar", fmt.Sprintf("Laporan berisi lebih dari %d baris. Persempit filter (mis. per kelas).", h.maxRows))
		return
	}
	jobID, err := enqueueExport(r.Context(), h.db, h.jobs, school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, reportType, format, p, rows)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "enqueue report export", err)
		return
	}
	http.Redirect(w, r, jobsPath+"/"+jobID, http.StatusSeeOther)
}

// arrearsFilterLabels menjelaskan filter aktif untuk kepala laporan.
func arrearsFilterLabels(ctx context.Context, q database.DBTX, schoolID string, f billing.ArrearsFilter) ([]string, error) {
	class, fee := "Semua kelas", "Semua jenis"
	if database.IsUUID(f.ClassID) {
		if err := q.QueryRow(ctx, `SELECT c.name || ' · ' || y.name FROM classes c
			JOIN academic_years y ON y.school_id = c.school_id AND y.id = c.academic_year_id
			WHERE c.school_id = $1 AND c.id = $2`, schoolID, f.ClassID).Scan(&class); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if database.IsUUID(f.FeeTypeID) {
		if err := q.QueryRow(ctx, `SELECT name FROM fee_types WHERE school_id = $1 AND id = $2`, schoolID, f.FeeTypeID).Scan(&fee); err != nil && !errors.Is(err, pgx.ErrNoRows) {
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

// sendFile mengirim file laporan sebagai unduhan.
func (h *Handler) sendFile(w http.ResponseWriter, data []byte, format, filename string) {
	contentType := "application/pdf"
	if format == "xlsx" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	hdr := w.Header()
	hdr.Set("Content-Type", contentType)
	hdr.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// ExportCustom mengirim laporan kustom (T79) sebagai xlsx atau pdf, langsung
// jika tidak lebih dari MaxSyncRows baris, selain itu sebagai job (T49)
// dengan salinan konfigurasi di params. Hak akses (pemilik, berbagi, peran
// sumber) sudah diperiksa builder.ConfigHandler sebelum memanggil ini.
func (h *Handler) ExportCustom(w http.ResponseWriter, r *http.Request, title string, cfg builder.Config, format string) {
	id, _ := auth.CurrentUser(r.Context())
	p := JobParams{Title: title, Config: &cfg, AsOf: h.today().Format(dateLayout)}
	t, rows, err := buildTable(r.Context(), h.db, id.SchoolID, id.SchoolName, TypeCustom, p, false)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "custom report", err)
		return
	}
	h.deliver(w, r, id, t, rows, TypeCustom, format, p)
}
