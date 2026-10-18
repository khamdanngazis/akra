// Package home menyediakan halaman beranda setelah login, dengan ringkasan
// sesuai peran (T51).
package home

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/alexedwards/scs/v2"

	"akra/internal/announcements"
	"akra/internal/attendance"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/reports"
	"akra/internal/school"
)

// Handler melayani halaman beranda.
type Handler struct {
	db       database.DB
	render   page.Renderer
	sessions *scs.SessionManager
	logger   *slog.Logger
	today    func() time.Time
}

// NewHandler mengembalikan Handler. Harus dipasang di dalam grup
// RequireLogin.
func NewHandler(db database.DB, render page.Renderer, sessions *scs.SessionManager, logger *slog.Logger) *Handler {
	return &Handler{db: db, render: render, sessions: sessions, logger: logger, today: clock.Today}
}

// TreasurerSummary adalah ringkasan bendahara.
type TreasurerSummary struct {
	ArrearsTotal    string // sama dengan total di /keuangan/tunggakan
	ArrearsStudents int
	PaymentsToday   int
	PaidToday       string
}

// TeacherClass adalah kelas wali dan status absensi hari ini.
type TeacherClass struct {
	ID, Name string
	Filled   bool
}

// AdminSummary adalah ringkasan data sekolah (admin/TU, kepala sekolah).
type AdminSummary struct {
	ActiveStudents int
	Teachers       int
	Classes        int // kelas tahun ajaran aktif
	Announcements  int // pengumuman tahun ajaran aktif
}

// KPI adalah satu kartu ringkasan di beranda: satu angka utama, label, dan
// keterangan singkat (AKRA_Brand_Design_Guide.md bagian 5).
type KPI struct {
	Icon, Value, Label, Hint string
	Tone                     string // blue, teal, amber, navy
	Href                     string
}

// RecentPayment adalah pembayaran terbaru untuk beranda bendahara.
type RecentPayment struct {
	InvoiceID, Student, Fees, Period, Amount, Time string
	Voided                                         bool
}

// RecentAnnouncement adalah pengumuman terbaru untuk beranda staf.
type RecentAnnouncement struct {
	Title, Audience, Published string
}

// PrincipalSummary adalah rekap kehadiran minggu ini.
type PrincipalSummary struct {
	Range   string
	Counts  attendance.Counts
	Percent string
	Meter   int // lebar bar kehadiran 0–100; -1 bila belum ada data
}

type view struct {
	User       auth.Identity
	Today      string
	YearName   string
	NoYear     bool
	CanSetYear bool
	Treasurer  *TreasurerSummary
	Teacher    []TeacherClass
	IsTeacher  bool
	Admin      *AdminSummary
	Principal  *PrincipalSummary
	KPIs       []KPI
	Payments   []RecentPayment
	News       []RecentAnnouncement
	Flash      string
	Error      string
}

// ServeHTTP merender beranda. Peran: semua staf; orang tua dialihkan ke
// portal.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	if id.HasRole(auth.RoleOrangTua) {
		http.Redirect(w, r, auth.PortalPath, http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	today := h.today()
	v := view{User: id, Today: clock.FormatDateID(today), Flash: auth.PopFlash(ctx, h.sessions),
		CanSetYear: auth.HasAnyRole(id, auth.RoleAdminTU, auth.RoleKepalaSekolah)}
	year, err := school.GetActiveAcademicYear(ctx, h.db, id.SchoolID)
	switch {
	case errors.Is(err, school.ErrNotFound):
		v.NoYear = true
	case err != nil:
		page.ServerError(h.render, h.logger, w, r, id, "home active year", err)
		return
	default:
		v.YearName = year.Name
	}
	if err := h.fill(ctx, id, year, v.NoYear, today, &v); err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "home summary", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "home", v)
}

func (h *Handler) fill(ctx context.Context, id auth.Identity, year school.AcademicYear, noYear bool, today time.Time, v *view) error {
	if id.HasRole(auth.RoleBendahara) {
		s, err := TreasurerFor(ctx, h.db, id.SchoolID, today)
		if err != nil {
			return err
		}
		v.Treasurer = &s
	}
	if id.HasRole(auth.RoleGuru) {
		v.IsTeacher = true
		classes, err := TeacherClassesFor(ctx, h.db, id.SchoolID, id.UserID, today)
		if err != nil {
			return err
		}
		v.Teacher = classes
	}
	if auth.HasAnyRole(id, auth.RoleAdminTU, auth.RoleKepalaSekolah) {
		s, err := SchoolSummaryFor(ctx, h.db, id.SchoolID)
		if err != nil {
			return err
		}
		v.Admin = &s
	}
	if auth.HasAnyRole(id, auth.RoleKepalaSekolah, auth.RoleAdminTU) && !noYear {
		s, err := PrincipalFor(ctx, h.db, id.SchoolID, year, today)
		if err != nil {
			return err
		}
		v.Principal = s
	}
	if id.HasRole(auth.RoleBendahara) {
		list, err := RecentPaymentsFor(ctx, h.db, id.SchoolID, today, 5)
		if err != nil {
			return err
		}
		v.Payments = list
	}
	news, err := announcements.NewService(h.db).List(ctx, id.SchoolID, 4)
	if err != nil {
		return err
	}
	for _, a := range news {
		aud := "Semua kelas"
		if a.ClassName != "" {
			aud = "Kelas " + a.ClassName
		}
		v.News = append(v.News, RecentAnnouncement{Title: a.Title, Audience: aud, Published: clock.FormatDateID(clock.DateOf(a.PublishedAt))})
	}
	v.KPIs = kpis(v)
	return nil
}

// kpis menyusun kartu ringkasan sesuai data peran yang tersedia; pengguna
// dengan beberapa peran mendapat gabungannya.
func kpis(v *view) []KPI {
	var out []KPI
	if a := v.Admin; a != nil {
		year := "TA " + v.YearName
		if v.NoYear {
			year = "Belum ada tahun ajaran aktif"
		}
		out = append(out,
			KPI{Icon: "users", Value: strconv.Itoa(a.ActiveStudents), Label: "Siswa aktif", Hint: "Semua tingkat", Tone: "blue", Href: "/siswa"},
			KPI{Icon: "teacher", Value: strconv.Itoa(a.Teachers), Label: "Guru", Hint: "Akun guru aktif", Tone: "teal"},
			KPI{Icon: "school", Value: strconv.Itoa(a.Classes), Label: "Kelas", Hint: year, Tone: "navy", Href: "/kelas"},
			KPI{Icon: "megaphone", Value: strconv.Itoa(a.Announcements), Label: "Pengumuman", Hint: "Di tahun ajaran aktif", Tone: "amber", Href: "/pengumuman"},
		)
	}
	if t := v.Treasurer; t != nil {
		out = append(out,
			KPI{Icon: "arrears", Value: t.ArrearsTotal, Label: "Sisa tunggakan", Hint: "Lewat jatuh tempo", Tone: "amber", Href: "/keuangan/tunggakan"},
			KPI{Icon: "users", Value: strconv.Itoa(t.ArrearsStudents), Label: "Siswa menunggak", Hint: "Punya tagihan lewat jatuh tempo", Tone: "navy", Href: "/keuangan/tunggakan"},
			KPI{Icon: "receipt", Value: strconv.Itoa(t.PaymentsToday), Label: "Pembayaran hari ini", Hint: "pembayaran dicatat hari ini", Tone: "blue"},
			KPI{Icon: "wallet", Value: t.PaidToday, Label: "Diterima hari ini", Hint: "Tunai dan transfer", Tone: "teal"},
		)
	}
	if v.IsTeacher {
		pending := 0
		for _, c := range v.Teacher {
			if !c.Filled {
				pending++
			}
		}
		out = append(out,
			KPI{Icon: "school", Value: strconv.Itoa(len(v.Teacher)), Label: "Kelas wali", Hint: "Tahun ajaran aktif", Tone: "navy", Href: "/absensi"},
			KPI{Icon: "attendance", Value: strconv.Itoa(pending), Label: "Absensi belum diisi", Hint: "Hari ini", Tone: "blue", Href: "/absensi"},
		)
	}
	return out
}

// SchoolSummaryFor menghitung jumlah siswa aktif, guru aktif, kelas dan
// pengumuman tahun ajaran aktif.
func SchoolSummaryFor(ctx context.Context, q database.DBTX, schoolID string) (AdminSummary, error) {
	var s AdminSummary
	err := q.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM students WHERE school_id = $1 AND status = 'aktif'),
		(SELECT count(DISTINCT u.id) FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
		  WHERE u.school_id = $1 AND u.is_active AND r.code = 'guru'),
		(SELECT count(*) FROM classes c JOIN academic_years y ON y.school_id = c.school_id AND y.id = c.academic_year_id
		  WHERE c.school_id = $1 AND y.is_active),
		(SELECT count(*) FROM announcements a JOIN academic_years y ON y.school_id = a.school_id AND y.id = a.academic_year_id
		  WHERE a.school_id = $1 AND y.is_active)`, schoolID).Scan(&s.ActiveStudents, &s.Teachers, &s.Classes, &s.Announcements)
	if err != nil {
		return AdminSummary{}, fmt.Errorf("home: school summary: %w", err)
	}
	return s, nil
}

// RecentPaymentsFor mengembalikan pembayaran yang paling baru dicatat.
func RecentPaymentsFor(ctx context.Context, q database.DBTX, schoolID string, today time.Time, limit int) ([]RecentPayment, error) {
	rows, err := q.Query(ctx, `
		SELECT i.id, st.full_name, coalesce(`+billing.FeeNamesSQL+`, ''), i.period, p.amount, p.created_at,
		       EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved')
		FROM payments p
		JOIN invoices i ON i.school_id = p.school_id AND i.id = p.invoice_id
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		WHERE p.school_id = $1
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $2`, schoolID, limit)
	if err != nil {
		return nil, fmt.Errorf("home: recent payments: %w", err)
	}
	defer rows.Close()
	var out []RecentPayment
	for rows.Next() {
		var (
			p      RecentPayment
			period string
			amount int64
			at     time.Time
		)
		if err := rows.Scan(&p.InvoiceID, &p.Student, &p.Fees, &period, &amount, &at, &p.Voided); err != nil {
			return nil, fmt.Errorf("home: recent payments: %w", err)
		}
		p.Period, p.Amount = billing.PeriodLabel(period), money.FormatRupiah(amount)
		p.Time = at.In(clock.Jakarta).Format("15.04")
		if d := clock.DateOf(at); !d.Equal(today) {
			p.Time = clock.FormatDateID(d)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// TreasurerFor menghitung ringkasan bendahara: total tunggakan dari
// billing.ListArrears (sama dengan halaman tunggakan) dan pembayaran aktif
// yang dicatat hari ini (Asia/Jakarta).
func TreasurerFor(ctx context.Context, q database.DBTX, schoolID string, today time.Time) (TreasurerSummary, error) {
	a, err := billing.ListArrears(ctx, q, schoolID, billing.ArrearsFilter{}, today)
	if err != nil {
		return TreasurerSummary{}, err
	}
	var n int
	var sum int64
	err = q.QueryRow(ctx, `SELECT count(*), coalesce(sum(p.amount), 0) FROM payments p
		WHERE p.school_id = $1 AND (p.created_at AT TIME ZONE 'Asia/Jakarta')::date = $2
		  AND NOT EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved')`, schoolID, today).Scan(&n, &sum)
	if err != nil {
		return TreasurerSummary{}, fmt.Errorf("home: payments today: %w", err)
	}
	return TreasurerSummary{ArrearsTotal: money.FormatRupiah(a.TotalRemaining), ArrearsStudents: a.StudentCount, PaymentsToday: n, PaidToday: money.FormatRupiah(sum)}, nil
}

// TeacherClassesFor mengembalikan kelas wali guru di tahun ajaran aktif dan
// apakah absensi hari ini sudah disimpan (ada catatan yang diubah lewat
// form, bukan hanya sesi yang dibuka).
func TeacherClassesFor(ctx context.Context, q database.DBTX, schoolID, userID string, today time.Time) ([]TeacherClass, error) {
	classes, err := attendance.HomeroomClasses(ctx, q, schoolID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]TeacherClass, 0, len(classes))
	for _, c := range classes {
		var filled bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attendance_sessions s
			JOIN attendance_records r ON r.school_id = s.school_id AND r.session_id = s.id
			WHERE s.school_id = $1 AND s.class_id = $2 AND s.session_date = $3 AND r.updated_by IS NOT NULL)`,
			schoolID, c.ID, today).Scan(&filled); err != nil {
			return nil, fmt.Errorf("home: attendance today: %w", err)
		}
		out = append(out, TeacherClass{ID: c.ID, Name: c.Name, Filled: filled})
	}
	return out, nil
}

// PrincipalFor menghitung rekap kehadiran semua kelas minggu ini (Senin
// sampai hari ini, dibatasi tahun ajaran aktif) dengan reports.
// BuildAttendanceReport (angka sama dengan laporan kehadiran). nil jika
// minggu ini di luar tahun ajaran.
func PrincipalFor(ctx context.Context, q database.DBTX, schoolID string, year school.AcademicYear, today time.Time) (*PrincipalSummary, error) {
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	from, to := monday, today
	if from.Before(year.StartDate) {
		from = year.StartDate
	}
	if to.After(year.EndDate) {
		to = year.EndDate
	}
	if from.After(to) {
		return nil, nil
	}
	rep, err := reports.BuildAttendanceReport(ctx, q, schoolID, reports.AttendanceFilter{From: from, To: to})
	if err != nil {
		return nil, err
	}
	return &PrincipalSummary{Range: clock.FormatDateID(from) + " – " + clock.FormatDateID(to), Counts: rep.Total,
		Percent: attendance.FormatPercent(rep.Total.PresentPercent()), Meter: meter(rep.Total.PresentPercent())}, nil
}

// meter mengubah persentase per mil menjadi lebar bar 0–100.
func meter(permille int) int {
	if permille < 0 {
		return -1
	}
	return min(100, (permille+5)/10)
}
