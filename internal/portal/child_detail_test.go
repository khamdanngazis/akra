package portal

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/attendance"
)

// attendanceData mengisi profil dan absensi contoh, lalu mengembalikan id
// kelas 1A.
func (f *fixture) attendanceData(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	exec(`UPDATE students SET birth_date = '2019-05-02', gender = 'P' WHERE id = $1`, f.ani)
	teacher := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru@uji.id', 'x', 'Bu Guru Wati') RETURNING id`, f.schoolA)
	class := func(name string) (id, year string) {
		if err := f.tx.QueryRow(ctx, `SELECT id, academic_year_id FROM classes WHERE school_id = $1 AND name = $2`, f.schoolA, name).Scan(&id, &year); err != nil {
			t.Fatal(err)
		}
		return id, year
	}
	c1A, year := class("1A")
	c2B, _ := class("2B")
	c6A, oldYear := class("6A")
	exec(`UPDATE classes SET homeroom_teacher_id = $2 WHERE id = $1`, c1A, teacher)
	enrollment := func(student, classID string) string {
		return one(`SELECT id FROM enrollments WHERE student_id = $1 AND class_id = $2`, student, classID)
	}
	session := func(classID, yearID, date string) string {
		return one(`INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			f.schoolA, yearID, classID, date, teacher)
	}
	record := func(sessionID, enrollmentID, status string) {
		exec(`INSERT INTO attendance_records (school_id, session_id, enrollment_id, status) VALUES ($1, $2, $3, $4)`, f.schoolA, sessionID, enrollmentID, status)
	}
	aniNow, budi1A, budi2B := enrollment(f.ani, c1A), enrollment(f.budi, c1A), enrollment(f.budi, c2B)
	s1, s2, s3 := session(c1A, year, "2026-07-20"), session(c1A, year, "2026-07-21"), session(c1A, year, "2026-07-22")
	s4 := session(c1A, year, "2026-08-05")
	record(s1, aniNow, "hadir")
	record(s2, aniNow, "sakit")
	record(s3, aniNow, "hadir")
	record(s4, aniNow, "alpa")
	// Budi pindah dari 1A ke 2B per 1 Agustus.
	record(s1, budi1A, "hadir")
	record(s2, budi1A, "izin")
	record(session(c2B, year, "2026-08-10"), budi2B, "hadir")
	// Tahun lalu: tidak ikut rekap tahun aktif.
	record(session(c6A, oldYear, "2025-09-01"), enrollment(f.ani, c6A), "alpa")
	return c1A
}

func TestGetChildDetail(t *testing.T) {
	f := newFixture(t)
	c1A := f.attendanceData(t)
	ctx := context.Background()
	svc := NewService(f.tx)
	today := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)

	d, err := svc.GetChildDetail(ctx, f.schoolA, f.userSari, f.ani, today)
	if err != nil {
		t.Fatal(err)
	}
	if d.FullName != "Ani Lestari" || d.NIS != "001" || d.Gender != "P" || d.BirthDate == nil || d.BirthDate.Format("2006-01-02") != "2019-05-02" ||
		d.ClassName != "1A" || d.Homeroom != "Bu Guru Wati" || d.YearName != "2026/2027" {
		t.Errorf("detail = %+v", d)
	}
	if !d.HasRecap || d.RecapFrom.Format("2006-01-02") != "2026-07-13" || !d.RecapTo.Equal(today) {
		t.Errorf("recap range = %v – %v", d.RecapFrom, d.RecapTo)
	}
	want := attendance.Counts{Hadir: 2, Sakit: 1, Alpa: 1}
	if d.Attendance != want {
		t.Errorf("attendance = %+v, want %+v (last year's record excluded)", d.Attendance, want)
	}
	// Sama dengan rekap kelas T27 untuk siswa yang sama.
	rows, err := attendance.SummaryByStudent(ctx, f.tx, f.schoolA, c1A, d.RecapFrom, d.RecapTo)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.StudentID == f.ani && r.Counts != d.Attendance {
			t.Errorf("portal %+v differs from class recap %+v", d.Attendance, r.Counts)
		}
	}

	// Anak yang pindah kelas: rekap dari kedua kelas.
	b, err := svc.GetChildDetail(ctx, f.schoolA, f.userSari, f.budi, today)
	if err != nil || b.ClassName != "2B" || b.Attendance != (attendance.Counts{Hadir: 2, Izin: 1}) || b.Homeroom != "" {
		t.Errorf("Budi = %+v, %v", b, err)
	}

	for name, c := range map[string]struct{ school, user, student string }{
		"child of another family":   {f.schoolA, f.userSari, f.citra},
		"child of another school":   {f.schoolA, f.userSari, f.dewi},
		"unknown id":                {f.schoolA, f.userSari, "00000000-0000-0000-0000-000000000000"},
		"invalid id":                {f.schoolA, f.userSari, "001"},
		"guardian without children": {f.schoolA, f.none, f.ani},
		"mismatched school":         {f.schoolB, f.userSari, f.ani},
	} {
		if _, err := svc.GetChildDetail(ctx, c.school, c.user, c.student, today); err != ErrNotFound {
			t.Errorf("%s: error = %v, want ErrNotFound", name, err)
		}
	}
}

func TestChildDetailPage(t *testing.T) {
	f := newFixture(t)
	f.attendanceData(t)
	rec := f.getPath("/portal/siswa/"+f.ani, f.userSari, f.schoolA, "orang_tua")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"Ani Lestari", "NIS 001", "2 Mei 2019", "Perempuan", "1A · 2026/2027", "Bu Guru Wati", `href="/portal"`, "50,0%"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	// Hanya form keluar di header: tidak ada form atau tombol ubah data.
	if strings.Count(body, "<form") != 1 || strings.Contains(body, "Ubah") || strings.Contains(body, "Sari") {
		t.Error("detail page must be read-only and must not show other guardians")
	}
	if list := f.get(f.userSari, f.schoolA, "orang_tua").Body.String(); !strings.Contains(list, `href="/portal/siswa/`+f.ani+`"`) || strings.Contains(list, "badge") {
		t.Error("children list must link to the detail page and must not show the internal student status")
	}

	// Semua kasus "bukan anak saya" sama persis: 404 dengan isi yang sama.
	notFound := f.getPath("/portal/siswa/00000000-0000-0000-0000-000000000000", f.userSari, f.schoolA, "orang_tua")
	for name, path := range map[string]string{"other family": "/portal/siswa/" + f.citra, "other school": "/portal/siswa/" + f.dewi, "invalid": "/portal/siswa/abc"} {
		rec := f.getPath(path, f.userSari, f.schoolA, "orang_tua")
		if rec.Code != http.StatusNotFound || rec.Body.String() != notFound.Body.String() {
			t.Errorf("%s: status = %d, body differs from unknown id: %v", name, rec.Code, rec.Body.String() != notFound.Body.String())
		}
	}
	for _, roles := range []string{"admin_tu", "bendahara", "guru"} {
		if rec := f.getPath("/portal/siswa/"+f.ani, f.userSari, f.schoolA, roles); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", roles, rec.Code)
		}
	}
}
