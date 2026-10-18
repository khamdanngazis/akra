package home

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/payments"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/platform/render"
	"akra/internal/reports"
	"akra/internal/school"
	"akra/web"
)

type fixture struct {
	tx                                       pgx.Tx
	schoolA, schoolB                         string
	bendahara, guru1A, guru1B, admin, kepala string
	c1A, c1B                                 string
	today                                    time.Time
	h                                        http.Handler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	// Hari ini sebenarnya (pembayaran "hari ini" memakai created_at).
	f := &fixture{tx: tx, today: clock.Today()}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('SD Beranda', 'test-home-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('SD Tanpa Tahun', 'test-home-b') RETURNING id`)
	user := func(email string) string {
		return one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, $2, 'x', $2) RETURNING id`, f.schoolA, email)
	}
	f.bendahara, f.guru1A, f.guru1B, f.admin, f.kepala = user("bendahara@uji.id"), user("guru1a@uji.id"), user("guru1b@uji.id"), user("admin@uji.id"), user("kepala@uji.id")
	start, end := f.today.AddDate(0, -3, 0), f.today.AddDate(0, 6, 0)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', $2, $3, true) RETURNING id`, f.schoolA, start, end)
	f.c1A = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, '1A', 1, $3) RETURNING id`, f.schoolA, year, f.guru1A)
	f.c1B = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, '1B', 1, $3) RETURNING id`, f.schoolA, year, f.guru1B)
	enroll := func(nis, classID, status string) string {
		st := one(`INSERT INTO students (school_id, nis, full_name, status) VALUES ($1, $2, $2, $3) RETURNING id`, f.schoolA, nis, status)
		return one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`, f.schoolA, st, classID, year, start)
	}
	e1, e2 := enroll("001", f.c1A, "aktif"), enroll("002", f.c1A, "aktif")
	enroll("003", f.c1B, "aktif")
	enroll("004", f.c1B, "lulus")

	// Absensi hari ini: 1A disimpan (updated_by), 1B hanya dibuka.
	sA := one(`INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`, f.schoolA, year, f.c1A, f.today, f.guru1A)
	for e, st := range map[string]string{e1: "hadir", e2: "sakit"} {
		if _, err := tx.Exec(ctx, `INSERT INTO attendance_records (school_id, session_id, enrollment_id, status, updated_by) VALUES ($1, $2, $3, $4, $5)`, f.schoolA, sA, e, st, f.guru1A); err != nil {
			t.Fatal(err)
		}
	}
	one(`INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`, f.schoolA, year, f.c1B, f.today, f.guru1B)

	// Tunggakan dan satu pembayaran hari ini.
	actor := school.Actor{SchoolID: f.schoolA, UserID: f.bendahara}
	ft, err := billing.CreateFeeType(ctx, tx, f.schoolA, billing.FeeTypeInput{Name: "SPP", DefaultAmount: 150000})
	if err != nil {
		t.Fatal(err)
	}
	due := f.today.AddDate(0, 0, -20)
	inv, err := billing.CreateInvoice(ctx, tx, actor, billing.InvoiceRequest{EnrollmentID: e1, Period: due.Format("2006-01"), DueDate: due, Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := billing.CreateInvoice(ctx, tx, actor, billing.InvoiceRequest{EnrollmentID: e2, Period: due.Format("2006-01"), DueDate: due, Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := payments.RecordPayment(ctx, tx, actor, payments.Request{InvoiceID: inv.ID, Amount: 40000, PaidOn: f.today, Method: payments.MethodCash}); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	hd := NewHandler(tx, rd, sm, logger)
	hd.today = func() time.Time { return f.today }
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity{UserID: r.Header.Get("X-User"), SchoolID: r.Header.Get("X-School"), Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Uji", SchoolName: "SD Beranda"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	r.Method(http.MethodGet, "/", hd)
	f.h = r
	return f
}

func (f *fixture) home(user, school, roles string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User", user)
	req.Header.Set("X-School", school)
	req.Header.Set("X-Roles", roles)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

// nav mengembalikan isi elemen <nav> header.
func nav(body string) string {
	i, j := strings.Index(body, `<nav id="app-nav"`), strings.Index(body, "</nav>")
	if i < 0 || j < i {
		return ""
	}
	return body[i:j]
}

func TestTreasurerSummaryMatchesArrears(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s, err := TreasurerFor(ctx, f.tx, f.schoolA, f.today)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := billing.ListArrears(ctx, f.tx, f.schoolA, billing.ArrearsFilter{}, f.today)
	if s.ArrearsTotal != money.FormatRupiah(a.TotalRemaining) || a.TotalRemaining != 260000 || s.ArrearsStudents != 2 {
		t.Errorf("summary %+v vs arrears %d", s, a.TotalRemaining)
	}
	if s.PaymentsToday != 1 || s.PaidToday != "Rp40.000" {
		t.Errorf("payments today = %d, %s", s.PaymentsToday, s.PaidToday)
	}
	body := f.home(f.bendahara, f.schoolA, "bendahara").Body.String()
	if !strings.Contains(body, "Rp260.000") || !strings.Contains(body, "pembayaran dicatat hari ini") {
		t.Error("treasurer home page")
	}
	recent, err := RecentPaymentsFor(ctx, f.tx, f.schoolA, f.today, 5)
	if err != nil || len(recent) == 0 || recent[0].Amount != "Rp40.000" || !strings.Contains(body, "Pembayaran terbaru") || !strings.Contains(body, recent[0].Student) {
		t.Errorf("recent payments = %+v, %v", recent, err)
	}
	if other, err := RecentPaymentsFor(ctx, f.tx, f.schoolB, f.today, 5); err != nil || len(other) != 0 {
		t.Errorf("school B recent payments = %+v, %v", other, err)
	}
	if n := nav(body); strings.Contains(n, "/absensi") || !strings.Contains(n, `href="/keuangan/pembayaran/baru"`) || !strings.Contains(n, `href="/" aria-current="page"`) {
		t.Errorf("treasurer menu: %s", n)
	}
}

func TestTeacherHome(t *testing.T) {
	f := newFixture(t)
	body := f.home(f.guru1A, f.schoolA, "guru").Body.String()
	if !strings.Contains(body, "Kelas 1A") || !strings.Contains(body, "Sudah diisi") || strings.Contains(body, "Kelas 1B") {
		t.Error("teacher of 1A: class status")
	}
	if !strings.Contains(f.home(f.guru1B, f.schoolA, "guru").Body.String(), "Belum diisi") {
		t.Error("opened but unsaved attendance must count as not filled")
	}
	n := nav(body)
	for _, p := range []string{"/keuangan", "/siswa", "/laporan", "/kelas"} {
		if strings.Contains(n, p) {
			t.Errorf("teacher menu contains %s", p)
		}
	}
	if !strings.Contains(n, `href="/absensi"`) {
		t.Error("teacher menu misses attendance")
	}
	if body := f.home(f.admin, f.schoolA, "guru").Body.String(); !strings.Contains(body, "belum menjadi wali kelas") {
		t.Error("teacher without homeroom class")
	}
}

func TestAdminAndPrincipalHome(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	y := mustYear(t, f)
	if _, err := f.tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = 'guru'`, f.guru1A); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(ctx, `INSERT INTO announcements (school_id, academic_year_id, title, body, created_by) VALUES ($1, $2, 'Libur semester', 'Isi', $3)`, f.schoolA, y.ID, f.guru1A); err != nil {
		t.Fatal(err)
	}
	body := f.home(f.admin, f.schoolA, "admin_tu").Body.String()
	if !strings.Contains(body, "Libur semester") || !strings.Contains(body, "Pengumuman terbaru") {
		t.Error("admin home misses recent announcements")
	}
	if !strings.Contains(body, `<span class="kpi-value">3</span><span class="kpi-hint">Semua tingkat</span>`) || !strings.Contains(body, "2026/2027") {
		t.Error("admin summary: 3 active students and the active year")
	}
	sum, err := SchoolSummaryFor(ctx, f.tx, f.schoolA)
	if err != nil || sum != (AdminSummary{ActiveStudents: 3, Teachers: 1, Classes: 2, Announcements: 1}) {
		t.Errorf("school summary = %+v, %v", sum, err)
	}
	ps, err := PrincipalFor(ctx, f.tx, f.schoolA, mustYear(t, f), f.today)
	if err != nil || ps == nil {
		t.Fatalf("principal summary = %v, %v", ps, err)
	}
	monday := f.today.AddDate(0, 0, -((int(f.today.Weekday()) + 6) % 7))
	rep, _ := reports.BuildAttendanceReport(ctx, f.tx, f.schoolA, reports.AttendanceFilter{From: monday, To: f.today})
	if ps.Counts != rep.Total || ps.Counts.Hadir != 1 || ps.Counts.Sakit != 1 || ps.Percent != "50,0%" {
		t.Errorf("principal summary %+v vs report %+v", ps, rep.Total)
	}
	if body := f.home(f.kepala, f.schoolA, "kepala_sekolah").Body.String(); !strings.Contains(body, "Kehadiran minggu ini") || !strings.Contains(body, "50,0%") {
		t.Error("principal home page")
	}
}

func mustYear(t *testing.T, f *fixture) school.AcademicYear {
	t.Helper()
	y, err := school.GetActiveAcademicYear(context.Background(), f.tx, f.schoolA)
	if err != nil {
		t.Fatal(err)
	}
	return y
}

func TestHomeWithoutActiveYear(t *testing.T) {
	f := newFixture(t)
	var adminB string
	if err := f.tx.QueryRow(context.Background(), `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'admin@b.id', 'x', 'Admin B') RETURNING id`, f.schoolB).Scan(&adminB); err != nil {
		t.Fatal(err)
	}
	rec := f.home(adminB, f.schoolB, "admin_tu")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Belum ada tahun ajaran aktif") || !strings.Contains(rec.Body.String(), `<a href="/pengaturan/tahun-ajaran">Atur tahun ajaran</a>`) {
		t.Errorf("admin without active year: status = %d", rec.Code)
	}
	if body := f.home(adminB, f.schoolB, "bendahara").Body.String(); !strings.Contains(body, "Hubungi admin/TU") || strings.Contains(body, "Atur tahun ajaran") {
		t.Error("treasurer without active year must not get the settings link")
	}
}

func TestTwoRolesAndParent(t *testing.T) {
	f := newFixture(t)
	body := f.home(f.guru1A, f.schoolA, "guru,bendahara").Body.String()
	if !strings.Contains(body, "Absensi hari ini") || !strings.Contains(body, "Rp260.000") {
		t.Error("two roles must show both summaries")
	}
	n := nav(body)
	if strings.Count(n, `href="/pengumuman"`) != 1 || strings.Count(n, `href="/"`) != 1 || !strings.Contains(n, `href="/absensi"`) || !strings.Contains(n, `href="/keuangan/tagihan-massal"`) {
		t.Errorf("merged menu: %s", n)
	}
	if rec := f.home(f.guru1A, f.schoolA, "orang_tua"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/portal" {
		t.Errorf("parent: status = %d", rec.Code)
	}
}
