package reports

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"akra/internal/attendance"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/render"
	"akra/internal/reports/export"
	"akra/web"
)

type fixture struct {
	tx               pgx.Tx
	schoolA, schoolB string
	c1A, c1B, oldC   string
	h                *Handler
	router           http.Handler
}

func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
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
	f := &fixture{tx: tx}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('SD Laporan', 'test-rep-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('SD Lain', 'test-rep-b') RETURNING id`)
	teacher := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru@uji.id', 'x', 'Guru') RETURNING id`, f.schoolA)
	oldYear := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2025/2026', '2025-07-14', '2026-06-26') RETURNING id`, f.schoolA)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolA)
	class := func(y, name string, grade int) string {
		return one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, $4) RETURNING id`, f.schoolA, y, name, grade)
	}
	f.c1B, f.c1A, f.oldC = class(year, "1B", 1), class(year, "1A", 1), class(oldYear, "6A", 6)
	enroll := func(nis, name, classID, yearID, start string) string {
		st := one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`, f.schoolA, nis, name)
		return one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			f.schoolA, st, classID, yearID, start)
	}
	eAni, eBudi := enroll("001", "Ani", f.c1A, year, "2026-07-13"), enroll("002", "Budi", f.c1A, year, "2026-07-13")
	eCici := enroll("003", "Cici", f.c1B, year, "2026-07-13")
	eLama := enroll("004", "Lama", f.oldC, oldYear, "2025-07-14")
	session := func(classID, yearID, date string) string {
		return one(`INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			f.schoolA, yearID, classID, date, teacher)
	}
	record := func(s, e, status string) {
		if _, err := tx.Exec(ctx, `INSERT INTO attendance_records (school_id, session_id, enrollment_id, status) VALUES ($1, $2, $3, $4)`, f.schoolA, s, e, status); err != nil {
			t.Fatal(err)
		}
	}
	s1, s2, s3 := session(f.c1A, year, "2026-08-03"), session(f.c1A, year, "2026-08-04"), session(f.c1A, year, "2026-09-01")
	record(s1, eAni, "hadir")
	record(s2, eAni, "sakit")
	record(s3, eAni, "hadir")
	record(s1, eBudi, "alpa")
	record(s2, eBudi, "hadir")
	record(s3, eBudi, "izin")
	record(session(f.c1B, year, "2026-08-03"), eCici, "hadir")
	record(session(f.oldC, oldYear, "2025-09-01"), eLama, "hadir")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	f.h = NewHandler(tx, nil, nil, rd, logger)
	f.h.today = func() time.Time { return day("2026-10-18") }
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := teacher
			if u := r.Header.Get("X-User"); u != "" {
				user = u
			}
			id := auth.Identity{UserID: user, SchoolID: f.schoolA, Roles: strings.Split(r.Header.Get("X-Roles"), ","), FullName: "Uji", SchoolName: "SD Laporan"}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithIdentity(r.Context(), id)))
		})
	})
	f.h.Routes(r)
	f.router = r
	return f
}

func (f *fixture) get(path, roles string) *httptest.ResponseRecorder {
	return f.getAs("", path, roles)
}

func (f *fixture) getAs(user, path, roles string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Roles", roles)
	req.Header.Set("X-User", user)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestBuildAttendanceReport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	from, to := day("2026-07-13"), day("2026-10-18")

	rep, err := BuildAttendanceReport(ctx, f.tx, f.schoolA, AttendanceFilter{ClassID: f.c1A, From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := attendance.SummaryByStudent(ctx, f.tx, f.schoolA, f.c1A, from, to)
	if len(rep.Rows) != len(want) || rep.ClassName != "1A" || rep.YearName != "2026/2027" {
		t.Fatalf("report = %+v", rep)
	}
	for i, w := range want {
		if rep.Rows[i].NIS != w.NIS || rep.Rows[i].Counts != w.Counts {
			t.Errorf("row %d = %+v, class recap = %+v", i, rep.Rows[i], w)
		}
	}
	if rep.Total != (attendance.Counts{Hadir: 3, Sakit: 1, Izin: 1, Alpa: 1}) {
		t.Errorf("total = %+v", rep.Total)
	}

	all, err := BuildAttendanceReport(ctx, f.tx, f.schoolA, AttendanceFilter{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range all.Rows {
		order = append(order, r.ClassName+"/"+r.FullName)
	}
	if strings.Join(order, ",") != "1A/Ani,1A/Budi,1B/Cici" || all.Total.Hadir != 4 {
		t.Errorf("all classes = %v, total %+v (last year's class excluded)", order, all.Total)
	}

	for name, c := range map[string]struct {
		flt  AttendanceFilter
		want error
	}{
		"class beyond its year":    {AttendanceFilter{ClassID: f.c1A, From: from, To: day("2027-07-01")}, attendance.ErrRangeOutsideYear},
		"all classes across years": {AttendanceFilter{From: day("2026-01-05"), To: to}, attendance.ErrRangeOutsideYear},
		"reversed":                 {AttendanceFilter{From: to, To: from}, attendance.ErrInvalidRange},
		"class of other school":    {AttendanceFilter{ClassID: "00000000-0000-0000-0000-000000000000", From: from, To: to}, attendance.ErrClassNotFound},
	} {
		if _, err := BuildAttendanceReport(ctx, f.tx, f.schoolA, c.flt); !errors.Is(err, c.want) {
			t.Errorf("%s: error = %v, want %v", name, err, c.want)
		}
	}
	if rep, _ := BuildAttendanceReport(ctx, f.tx, f.schoolB, AttendanceFilter{From: from, To: to}); len(rep.Rows) != 0 {
		t.Error("other school sees rows")
	}
}

func TestAttendanceReportPages(t *testing.T) {
	f := newFixture(t)
	for _, roles := range []string{"guru", "bendahara", "orang_tua"} {
		for _, p := range []string{"/laporan/kehadiran", "/laporan/kehadiran.xlsx", "/laporan/kehadiran.pdf"} {
			if rec := f.get(p, roles); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d, want 403", roles, p, rec.Code)
			}
		}
	}
	q := "?kelas=" + f.c1A + "&dari=2026-07-13&sampai=2026-10-18"
	screen := f.get("/laporan/kehadiran"+q, "kepala_sekolah")
	body := screen.Body.String()
	if screen.Code != http.StatusOK {
		t.Fatalf("screen: %d", screen.Code)
	}
	// Ani 2/3 = 66,7%; Budi 1/3 = 33,3%; total 3/6 = 50,0%.
	for _, want := range []string{"66,7%", "33,3%", "50,0%", `href="/laporan/kehadiran.xlsx?`, `href="/laporan/kehadiran.pdf?`} {
		if !strings.Contains(body, want) {
			t.Errorf("screen missing %q", want)
		}
	}
	if rec := f.get("/laporan/kehadiran?dari=2026-01-01&sampai=2026-10-18", "admin_tu"); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "maksimal satu tahun ajaran") {
		t.Errorf("range across years: status = %d", rec.Code)
	}
	if rec := f.get("/laporan/kehadiran.xlsx?dari=2026-01-01&sampai=2026-10-18", "admin_tu"); rec.Code != http.StatusBadRequest {
		t.Errorf("export across years: status = %d, want 400", rec.Code)
	}

	// Excel: angka sama dengan layar.
	rec := f.get("/laporan/kehadiran.xlsx"+q, "admin_tu")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "rekap-kehadiran-2026-07-13-2026-10-18.xlsx") {
		t.Fatalf("xlsx: %d %v", rec.Code, rec.Header())
	}
	checkXML(t, rec.Body.Bytes())
	x, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := x.GetRows("Rekap")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, r := range rows {
		got = append(got, strings.Join(r, "|"))
	}
	want := []string{
		"NIS|Nama|Kelas|Hadir|Sakit|Izin|Alpa|Kehadiran",
		"001|Ani|1A|2|1|0|0|66.7%",
		"002|Budi|1A|1|0|1|1|33.3%",
		"|Total||3|1|1|1|50.0%",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("xlsx rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if raw, _ := x.GetCellValue("Rekap", "H2", excelize.Options{RawCellValue: true}); raw != "0.667" {
		t.Errorf("percent stored as %q, want the number 0.667", raw)
	}
	if style, _ := x.GetCellStyle("Rekap", "A1"); style == 0 {
		t.Error("header is not styled (bold)")
	}
	if info, _ := x.GetRows("Info"); len(info) < 4 || info[0][0] != "Rekap Kehadiran" || !strings.Contains(strings.Join(info[len(info)-1], ""), "18 Oktober 2026") {
		t.Errorf("info sheet = %v", info)
	}

	// PDF.
	rec = f.get("/laporan/kehadiran.pdf"+q, "kepala_sekolah")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("pdf: %d %v", rec.Code, rec.Header())
	}
	rep, _ := BuildAttendanceReport(context.Background(), f.tx, f.schoolA, AttendanceFilter{ClassID: f.c1A, From: day("2026-07-13"), To: day("2026-10-18")})
	tbl := attendanceTable(rep, "SD Laporan", day("2026-10-18"))
	tbl.Uncompressed = true
	var pdf bytes.Buffer
	if err := export.WritePDF(&pdf, tbl); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Rekap Kehadiran", "SD Laporan", "Periode: 13 Juli 2026", "18 Oktober 2026", "Kelas: 1A", "66,7%", "50,0%", "Total"} {
		if !strings.Contains(pdf.String(), want) {
			t.Errorf("pdf missing %q", want)
		}
	}

	f.h.maxRows = 1
	if rec := f.get("/laporan/kehadiran.xlsx"+q, "admin_tu"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "lebih dari 1 baris") {
		t.Errorf("too many rows: status = %d", rec.Code)
	}
}

// checkXML memastikan setiap bagian XML di file .xlsx valid (file rusak
// memicu peringatan "perbaikan" di Excel).
func checkXML(t *testing.T, data []byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, zf := range zr.File {
		if !strings.HasSuffix(zf.Name, ".xml") && !strings.HasSuffix(zf.Name, ".rels") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		dec := xml.NewDecoder(rc)
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Errorf("%s: invalid XML: %v", zf.Name, err)
				break
			}
		}
		rc.Close()
	}
}

func mustQuery(q string) url.Values {
	v, err := url.ParseQuery(strings.TrimPrefix(q, "?"))
	if err != nil {
		panic(err)
	}
	return v
}

func writePDF(w io.Writer, t export.Table) error { return export.WritePDF(w, t) }
