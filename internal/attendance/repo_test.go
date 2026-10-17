package attendance

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/platform/database"
	"akra/internal/school"
)

func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

type fixture struct {
	schoolA, schoolB  string
	year              string
	class1A, class1B  string
	classB            string
	teacher           string
	tetap, pindah, br string // siswa: tetap di 1A, pindah 1A->1B per 1 Okt, baru masuk 1B per 1 Agt
}

func newFixture(t *testing.T, q database.DBTX, suffix string) *fixture {
	t.Helper()
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := q.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f := &fixture{}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('A', $1) RETURNING id`, "test-att-a"+suffix)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('B', $1) RETURNING id`, "test-att-b"+suffix)
	f.year = one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25') RETURNING id`, f.schoolA)
	yearB := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25') RETURNING id`, f.schoolB)
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`
	f.class1A = one(class, f.schoolA, f.year, "1A")
	f.class1B = one(class, f.schoolA, f.year, "1B")
	f.classB = one(class, f.schoolB, yearB, "1A")
	f.teacher = one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru@a.id', 'x', 'Guru') RETURNING id`, f.schoolA)

	student := `INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`
	enroll := `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date, end_date) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	f.tetap = one(student, f.schoolA, "001", "Ani Tetap")
	f.pindah = one(student, f.schoolA, "002", "Budi Pindah")
	f.br = one(student, f.schoolA, "003", "Citra Baru")
	one(enroll, f.schoolA, f.tetap, f.class1A, f.year, day("2026-07-13"), nil)
	one(enroll, f.schoolA, f.pindah, f.class1A, f.year, day("2026-07-13"), day("2026-09-30"))
	one(enroll, f.schoolA, f.pindah, f.class1B, f.year, day("2026-10-01"), nil)
	one(enroll, f.schoolA, f.br, f.class1B, f.year, day("2026-08-01"), nil)
	return f
}

func connect(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := database.Connect(context.Background(), dsn, 6)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func txFixture(t *testing.T) (pgx.Tx, *fixture) {
	t.Helper()
	pool := connect(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx, newFixture(t, tx, "")
}

func sp(t *testing.T, tx pgx.Tx, fn func(pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	s, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Rollback(ctx) }()
	return fn(s)
}

func names(records []Record) string {
	var n []string
	for _, r := range records {
		n = append(n, r.FullName)
	}
	sort.Strings(n)
	return strings.Join(n, ",")
}

func TestSessions_DatabaseConstraints(t *testing.T) {
	tx, f := txFixture(t)
	ctx := context.Background()
	s, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, day("2026-09-15"), f.teacher)
	if err != nil {
		t.Fatal(err)
	}

	err = sp(t, tx, func(q pgx.Tx) error {
		_, err := q.Exec(ctx, `INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, '2026-09-15', $4)`,
			f.schoolA, f.year, f.class1A, f.teacher)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "attendance_sessions_class_date_key") {
		t.Errorf("second session for the same class and date: error = %v", err)
	}

	if _, err := EnsureRecords(ctx, tx, f.schoolA, s.ID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"terlambat", "HADIR", ""} {
		err := sp(t, tx, func(q pgx.Tx) error {
			_, err := q.Exec(ctx, `UPDATE attendance_records SET status = $2 WHERE session_id = $1`, s.ID, status)
			return mapError("update", err)
		})
		if !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("status %q: error = %v, want ErrInvalidStatus", status, err)
		}
	}
	for _, status := range Statuses {
		if _, err := tx.Exec(ctx, `UPDATE attendance_records SET status = $2 WHERE session_id = $1`, s.ID, status); err != nil {
			t.Errorf("status %q rejected: %v", status, err)
		}
	}
}

func TestEnsureRecords_FollowsEnrollmentDates(t *testing.T) {
	tx, f := txFixture(t)
	ctx := context.Background()

	cases := []struct {
		class, date, want string
	}{
		{f.class1A, "2026-09-15", "Ani Tetap,Budi Pindah"}, // sebelum Budi pindah
		{f.class1A, "2026-09-30", "Ani Tetap,Budi Pindah"}, // hari terakhir Budi di 1A
		{f.class1A, "2026-10-01", "Ani Tetap"},             // Budi sudah di 1B
		{f.class1B, "2026-09-30", "Citra Baru"},            // Budi belum masuk 1B
		{f.class1B, "2026-10-01", "Budi Pindah,Citra Baru"},
		{f.class1B, "2026-07-20", ""}, // Citra baru masuk 1 Agustus
	}
	for _, tc := range cases {
		var records []Record
		err := sp(t, tx, func(q pgx.Tx) error {
			s, err := GetOrCreateSession(ctx, q, f.schoolA, tc.class, day(tc.date), f.teacher)
			if err != nil {
				return err
			}
			if _, err := EnsureRecords(ctx, q, f.schoolA, s.ID); err != nil {
				return err
			}
			records, err = ListRecords(ctx, q, f.schoolA, s.ID)
			return err
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.date, err)
		}
		if got := names(records); got != tc.want {
			t.Errorf("class %s on %s = %q, want %q", map[string]string{f.class1A: "1A", f.class1B: "1B"}[tc.class], tc.date, got, tc.want)
		}
		for _, r := range records {
			if r.Status != StatusPresent {
				t.Errorf("default status = %q, want hadir", r.Status)
			}
		}
	}
}

func TestEnsureRecords_KeepsExistingRecords(t *testing.T) {
	tx, f := txFixture(t)
	ctx := context.Background()
	s, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, day("2026-09-15"), f.teacher)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := EnsureRecords(ctx, tx, f.schoolA, s.ID); err != nil || n != 2 {
		t.Fatalf("first EnsureRecords = %d, %v", n, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE attendance_records r SET status = 'sakit'
		FROM enrollments e WHERE e.id = r.enrollment_id AND r.session_id = $1 AND e.student_id = $2`, s.ID, f.tetap); err != nil {
		t.Fatal(err)
	}
	// Siswa baru masuk 1A setelah sesi dibuat (dengan tanggal mulai sebelum sesi).
	var late string
	if err := tx.QueryRow(ctx, `WITH st AS (INSERT INTO students (school_id, nis, full_name) VALUES ($1, '004', 'Dedi Susulan') RETURNING id)
		INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) SELECT $1, st.id, $2, $3, '2026-09-01' FROM st RETURNING student_id`,
		f.schoolA, f.class1A, f.year).Scan(&late); err != nil {
		t.Fatal(err)
	}

	if n, err := EnsureRecords(ctx, tx, f.schoolA, s.ID); err != nil || n != 1 {
		t.Errorf("second EnsureRecords added %d, %v; want 1 (only the new student)", n, err)
	}
	records, _ := ListRecords(ctx, tx, f.schoolA, s.ID)
	got := map[string]string{}
	for _, r := range records {
		got[r.FullName] = r.Status
	}
	if got["Ani Tetap"] != StatusSick || got["Dedi Susulan"] != StatusPresent || len(got) != 3 {
		t.Errorf("records after second EnsureRecords = %v", got)
	}
}

func TestGetOrCreateSession_Rules(t *testing.T) {
	tx, f := txFixture(t)
	ctx := context.Background()

	a, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, day("2026-09-15"), f.teacher)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, day("2026-09-15"), f.teacher)
	if err != nil || a.ID != b.ID || a.ClassName != "1A" || a.CreatedBy != f.teacher {
		t.Errorf("second call returned %+v, %v; want the same session", b, err)
	}

	for name, tc := range map[string]struct {
		school, class, date string
		want                error
	}{
		"class of another school": {f.schoolA, f.classB, "2026-09-15", ErrClassNotFound},
		"invalid class id":        {f.schoolA, "bukan-uuid", "2026-09-15", ErrClassNotFound},
		"before academic year":    {f.schoolA, f.class1A, "2026-07-12", ErrDateOutsideYear},
		"after academic year":     {f.schoolA, f.class1A, "2027-06-26", ErrDateOutsideYear},
	} {
		err := sp(t, tx, func(q pgx.Tx) error {
			_, err := GetOrCreateSession(ctx, q, tc.school, tc.class, day(tc.date), f.teacher)
			return err
		})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", name, err, tc.want)
		}
	}
	if _, err := GetSession(ctx, tx, f.schoolB, a.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession from another school: error = %v", err)
	}
	if list, err := ListRecords(ctx, tx, f.schoolB, a.ID); err != nil || len(list) != 0 {
		t.Errorf("ListRecords from another school: %d records, %v", len(list), err)
	}
}

func TestService_OpenSession(t *testing.T) {
	tx, f := txFixture(t)
	svc := NewService(tx)
	actor := school.Actor{SchoolID: f.schoolA, UserID: f.teacher, IP: netip.MustParseAddr("192.0.2.1")}
	s, records, err := svc.OpenSession(context.Background(), actor, f.class1B, day("2026-10-05"))
	if err != nil {
		t.Fatal(err)
	}
	if s.ClassName != "1B" || names(records) != "Budi Pindah,Citra Baru" {
		t.Errorf("session = %+v, records = %s", s, names(records))
	}
}

// TestGetOrCreateSession_Concurrent menjalankan dua permintaan bersamaan
// untuk kelas dan tanggal yang sama; hasilnya harus satu sesi.
func TestGetOrCreateSession_Concurrent(t *testing.T) {
	pool := connect(t)
	ctx := context.Background()
	f := newFixture(t, pool, "-c")
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM attendance_records WHERE school_id IN ($1, $2)`,
			`DELETE FROM attendance_sessions WHERE school_id IN ($1, $2)`,
			`DELETE FROM enrollments WHERE school_id IN ($1, $2)`,
			`DELETE FROM students WHERE school_id IN ($1, $2)`,
			`DELETE FROM classes WHERE school_id IN ($1, $2)`,
			`DELETE FROM academic_years WHERE school_id IN ($1, $2)`,
			`DELETE FROM users WHERE school_id IN ($1, $2)`,
			`DELETE FROM schools WHERE id IN ($1, $2)`,
		} {
			if _, err := pool.Exec(ctx, sql, f.schoolA, f.schoolB); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	svc := NewService(pool)
	actor := school.Actor{SchoolID: f.schoolA, UserID: f.teacher}

	for i := 0; i < 20; i++ {
		date := day("2026-08-03").AddDate(0, 0, i)
		var wg sync.WaitGroup
		ids := make([]string, 2)
		errs := make([]error, 2)
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				s, _, err := svc.OpenSession(ctx, actor, f.class1A, date)
				ids[g], errs[g] = s.ID, err
			}(g)
		}
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("%s: errors %v, %v", date.Format("2006-01-02"), errs[0], errs[1])
		}
		if ids[0] != ids[1] {
			t.Fatalf("%s: two different sessions %s and %s", date.Format("2006-01-02"), ids[0], ids[1])
		}
		var sessions, records int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM attendance_sessions WHERE class_id = $1 AND session_date = $2`, f.class1A, date).Scan(&sessions)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM attendance_records WHERE session_id = $1`, ids[0]).Scan(&records)
		if sessions != 1 || records != 2 {
			t.Fatalf("%s: %d sessions, %d records; want 1 and 2", date.Format("2006-01-02"), sessions, records)
		}
	}
}
