// Package home menyediakan halaman beranda setelah login, dengan ringkasan
// sesuai peran (T51).
package home

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

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
	db       database.DBTX
	render   page.Renderer
	sessions *scs.SessionManager
	logger   *slog.Logger
	today    func() time.Time
}

// NewHandler mengembalikan Handler. Harus dipasang di dalam grup
// RequireLogin.
func NewHandler(db database.DBTX, render page.Renderer, sessions *scs.SessionManager, logger *slog.Logger) *Handler {
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

// AdminSummary adalah ringkasan admin/TU.
type AdminSummary struct {
	ActiveStudents int
}

// PrincipalSummary adalah rekap kehadiran minggu ini.
type PrincipalSummary struct {
	Range   string
	Counts  attendance.Counts
	Percent string
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
	if id.HasRole(auth.RoleAdminTU) {
		var s AdminSummary
		if err := h.db.QueryRow(ctx, `SELECT count(*) FROM students WHERE school_id = $1 AND status = 'aktif'`, id.SchoolID).Scan(&s.ActiveStudents); err != nil {
			return fmt.Errorf("home: count students: %w", err)
		}
		v.Admin = &s
	}
	if id.HasRole(auth.RoleKepalaSekolah) && !noYear {
		s, err := PrincipalFor(ctx, h.db, id.SchoolID, year, today)
		if err != nil {
			return err
		}
		v.Principal = s
	}
	return nil
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
		Percent: attendance.FormatPercent(rep.Total.PresentPercent())}, nil
}
