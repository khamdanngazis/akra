package attendance

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// reportFixture: kelas 1A dengan 10 sesi (1–10 September 2026) dan kelas 1B.
// Ani dan Citra di 1A sepanjang waktu; Budi di 1A sampai 5 September, lalu
// pindah ke 1B mulai 6 September.
type reportFixture struct {
	tx               pgx.Tx
	schoolA          string
	class1A, class1B string
	ani, budi, citra string
	teacher          string
}

func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	pool := connect(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f := &reportFixture{tx: tx}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', 'test-rep-a') RETURNING id`)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25') RETURNING id`, f.schoolA)
	f.teacher = one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'g@a.id', 'x', 'Guru') RETURNING id`, f.schoolA)
	f.class1A = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1A', 1) RETURNING id`, f.schoolA, year)
	f.class1B = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1B', 1) RETURNING id`, f.schoolA, year)
	student := `INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`
	f.ani = one(student, f.schoolA, "001", "Ani")
	f.budi = one(student, f.schoolA, "002", "Budi")
	f.citra = one(student, f.schoolA, "003", "Citra")
	enroll := `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date, end_date) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	one(enroll, f.schoolA, f.ani, f.class1A, year, day("2026-07-13"), nil)
	one(enroll, f.schoolA, f.citra, f.class1A, year, day("2026-07-13"), nil)
	one(enroll, f.schoolA, f.budi, f.class1A, year, day("2026-07-13"), day("2026-09-05"))
	one(enroll, f.schoolA, f.budi, f.class1B, year, day("2026-09-06"), nil)

	// Status per hari (1–10 September), dihitung manual di uji di bawah.
	statuses := map[string]string{
		f.ani:   "HHSHIHAHHH",
		f.budi:  "HAHHS", // hanya 1–5 di 1A
		f.citra: "HHHHHHHHHA",
	}
	code := map[byte]string{'H': "hadir", 'S': "sakit", 'I': "izin", 'A': "alpa"}
	for d := 1; d <= 10; d++ {
		date := time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC)
		for _, class := range []string{f.class1A, f.class1B} {
			if class == f.class1B && d < 6 {
				continue
			}
			s, err := GetOrCreateSession(ctx, tx, f.schoolA, class, "", date, f.teacher)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := EnsureRecords(ctx, tx, f.schoolA, s.ID); err != nil {
				t.Fatal(err)
			}
			if class != f.class1A {
				continue
			}
			for student, seq := range statuses {
				if d > len(seq) {
					continue
				}
				if _, err := tx.Exec(ctx, `UPDATE attendance_records r SET status = $3
					FROM enrollments e WHERE e.id = r.enrollment_id AND r.session_id = $1 AND e.student_id = $2`,
					s.ID, student, code[seq[d-1]]); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// Catatan nyasar: Budi di sesi 1A tanggal 8, padahal sudah pindah. Rekap
	// harus mengabaikannya karena di luar masa enrollment 1A-nya.
	if _, err := tx.Exec(ctx, `
		INSERT INTO attendance_records (school_id, session_id, enrollment_id, status)
		SELECT $1, s.id, e.id, 'alpa'
		FROM attendance_sessions s, enrollments e
		WHERE s.class_id = $2 AND s.session_date = '2026-09-08' AND e.student_id = $3 AND e.class_id = $2`,
		f.schoolA, f.class1A, f.budi); err != nil {
		t.Fatal(err)
	}
	return f
}

func byName(list []StudentSummary) map[string]StudentSummary {
	m := map[string]StudentSummary{}
	for _, s := range list {
		m[s.FullName] = s
	}
	return m
}

func TestReport_ManuallyComputedNumbers(t *testing.T) {
	f := newReportFixture(t)
	ctx := context.Background()
	from, to := day("2026-09-01"), day("2026-09-10")

	students, err := SummaryByStudent(ctx, f.tx, f.schoolA, f.class1A, "", from, to)
	if err != nil {
		t.Fatal(err)
	}
	got := byName(students)
	want := map[string]Counts{
		"Ani":   {Hadir: 7, Sakit: 1, Izin: 1, Alpa: 1}, // HHSHIHAHHH
		"Budi":  {Hadir: 3, Sakit: 1, Izin: 0, Alpa: 1}, // HAHHS (1–5), catatan nyasar tgl 8 diabaikan
		"Citra": {Hadir: 9, Sakit: 0, Izin: 0, Alpa: 1}, // HHHHHHHHHA
	}
	wantPct := map[string]int{"Ani": 700, "Budi": 600, "Citra": 900}
	if len(got) != 3 {
		t.Fatalf("students = %v", got)
	}
	for name, w := range want {
		if got[name].Counts != w {
			t.Errorf("%s = %+v, want %+v", name, got[name].Counts, w)
		}
		if p := got[name].PresentPercent(); p != wantPct[name] {
			t.Errorf("%s percent = %d, want %d", name, p, wantPct[name])
		}
	}
	// Kriteria: total per siswa = jumlah sesi yang tercatat dalam masa enrollment-nya.
	if got["Ani"].Total() != 10 || got["Budi"].Total() != 5 || got["Citra"].Total() != 10 {
		t.Errorf("totals = %d, %d, %d; want 10, 5, 10", got["Ani"].Total(), got["Budi"].Total(), got["Citra"].Total())
	}

	class, err := SummaryByClass(ctx, f.tx, f.schoolA, f.class1A, "", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if class.Sessions != 10 || class.Counts != (Counts{Hadir: 19, Sakit: 2, Izin: 1, Alpa: 3}) || class.PresentPercent() != 760 {
		t.Errorf("class 1A = %+v, percent %d; want 10 sessions, 19/2/1/3, 760", class, class.PresentPercent())
	}

	// Budi di 1B: 6–10 September, semua hadir.
	b, _ := SummaryByStudent(ctx, f.tx, f.schoolA, f.class1B, "", from, to)
	if len(b) != 1 || b[0].FullName != "Budi" || b[0].Counts != (Counts{Hadir: 5}) || b[0].PresentPercent() != 1000 {
		t.Errorf("class 1B = %+v", b)
	}

	// Sebagian rentang: 3–7 September.
	part := byName(must(SummaryByStudent(ctx, f.tx, f.schoolA, f.class1A, "", day("2026-09-03"), day("2026-09-07"))))
	if part["Ani"].Counts != (Counts{Hadir: 2, Sakit: 1, Izin: 1, Alpa: 1}) || part["Budi"].Counts != (Counts{Hadir: 2, Sakit: 1}) || part["Citra"].Counts != (Counts{Hadir: 5}) {
		t.Errorf("3–7 September = %+v", part)
	}

	// Sekolah lain tidak melihat apa pun.
	if list, _ := SummaryByStudent(ctx, f.tx, "00000000-0000-0000-0000-000000000000", f.class1A, "", from, to); len(list) != 0 {
		t.Error("another school sees records")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestReport_RangeLimitedToAcademicYear(t *testing.T) {
	f := newReportFixture(t)
	c, err := GetClassYear(context.Background(), f.tx, f.schoolA, f.class1A)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		from, to string
		want     error
	}{
		{"2026-07-13", "2027-06-25", nil},
		{"2026-09-01", "2026-09-01", nil},
		{"2026-09-10", "2026-09-01", ErrInvalidRange},
		{"2026-07-12", "2026-09-01", ErrRangeOutsideYear},
		{"2026-09-01", "2027-06-26", ErrRangeOutsideYear},
		{"2025-07-01", "2027-06-25", ErrRangeOutsideYear},
	}
	for _, tc := range cases {
		if err := c.CheckRange(day(tc.from), day(tc.to)); !errors.Is(err, tc.want) {
			t.Errorf("%s–%s: error = %v, want %v", tc.from, tc.to, err, tc.want)
		}
	}
}

func TestReportHandler(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()

	// 60 sesi untuk 30 siswa 1A (1800 catatan), status bervariasi.
	if _, err := e.tx.Exec(ctx, `
		WITH s AS (
			INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by)
			SELECT c.school_id, c.academic_year_id, c.id, $3::date - g, $2
			FROM classes c, generate_series(0, 59) g WHERE c.id = $1
			RETURNING id, school_id)
		INSERT INTO attendance_records (school_id, session_id, enrollment_id, status)
		SELECT s.school_id, s.id, e.id,
		       (ARRAY['hadir','hadir','hadir','hadir','hadir','hadir','sakit','izin','alpa','hadir'])[1 + (abs(hashtext(s.id::text || e.id::text)) % 10)]
		FROM s JOIN enrollments e ON e.class_id = $1`, e.class1A, e.guru1, e.today); err != nil {
		t.Fatal(err)
	}
	_, _ = e.tx.Exec(ctx, `ANALYZE attendance_records; ANALYZE attendance_sessions; ANALYZE enrollments`)

	start := time.Now()
	rec := e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1A, e.admin, "admin_tu", nil)
	elapsed := time.Since(start)
	t.Logf("report for 30 students x 60 sessions took %v", elapsed)
	if rec.Code != http.StatusOK || elapsed > time.Second {
		t.Fatalf("admin report: status = %d, took %v", rec.Code, elapsed)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "60 sesi absensi") || strings.Count(body, `<td class="nowrap">`) != 30 {
		t.Errorf("report does not show 60 sessions and 30 students")
	}
	// Total per siswa = 60 sesi.
	students, _ := SummaryByStudent(ctx, e.tx, e.schoolA, e.class1A, "", e.today.AddDate(0, 0, -59), e.today)
	for _, s := range students {
		if s.Total() != 60 {
			t.Fatalf("%s total = %d, want 60", s.FullName, s.Total())
		}
	}

	// Akses.
	cases := []struct {
		name, user, roles string
		want              int
	}{
		{"homeroom teacher", e.guru1, "guru", http.StatusOK},
		{"other teacher", e.guru2, "guru", http.StatusForbidden},
		{"principal", e.admin, "kepala_sekolah", http.StatusOK},
		{"parent", e.admin, "orang_tua", http.StatusForbidden},
	}
	for _, tc := range cases {
		if rec := e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1A, tc.user, tc.roles, nil); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
	// Guru lain tanpa ?kelas hanya melihat kelas perwaliannya (1B), bukan 1A.
	body = e.do(http.MethodGet, "/absensi/rekap", e.guru2, "guru", nil).Body.String()
	if strings.Contains(body, e.class1A) || !strings.Contains(body, e.class1B) {
		t.Error("teacher's class picker shows another teacher's class")
	}

	// Rentang melebihi tahun ajaran ditolak dengan pesan jelas.
	rec = e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1A+"&dari="+e.today.AddDate(-1, 0, 0).Format(dateLayout)+"&sampai="+e.today.Format(dateLayout), e.admin, "admin_tu", nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Rekap dibatasi satu tahun ajaran") {
		t.Errorf("range beyond year: status = %d", rec.Code)
	}
	rec = e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1A+"&dari="+e.today.Format(dateLayout)+"&sampai="+e.today.AddDate(0, 0, -3).Format(dateLayout), e.admin, "admin_tu", nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Tanggal awal harus sebelum") {
		t.Errorf("reversed range: status = %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/absensi/rekap?kelas=00000000-0000-0000-0000-000000000000", e.admin, "admin_tu", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown class: status = %d, want 404", rec.Code)
	}
}
