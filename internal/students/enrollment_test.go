package students

import (
	"context"
	"errors"
	"net/netip"
	"os"
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

type placeFixture struct {
	tx                     pgx.Tx
	schoolA, schoolB       string
	yearA, yearA2, yearB   string
	class1A, class1B, cl2A string // kelas tahun yearA
	classNext              string // kelas tahun yearA2
	classB                 string
	student, studentB      string
}

func newPlaceFixture(t *testing.T, q database.DBTX, suffix string) *placeFixture {
	t.Helper()
	ctx := context.Background()
	f := &placeFixture{}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := q.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	school := `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`
	year := `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, $2, $3, $4) RETURNING id`
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, $4) RETURNING id`
	student := `INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`
	f.schoolA = one(school, "test-enr-a"+suffix)
	f.schoolB = one(school, "test-enr-b"+suffix)
	f.yearA = one(year, f.schoolA, "2026/2027", "2026-07-13", "2027-06-25")
	f.yearA2 = one(year, f.schoolA, "2027/2028", "2027-07-12", "2028-06-23")
	f.yearB = one(year, f.schoolB, "2026/2027", "2026-07-13", "2027-06-25")
	f.class1A = one(class, f.schoolA, f.yearA, "1A", 1)
	f.class1B = one(class, f.schoolA, f.yearA, "1B", 1)
	f.cl2A = one(class, f.schoolA, f.yearA, "2A", 2)
	f.classNext = one(class, f.schoolA, f.yearA2, "2A", 2)
	f.classB = one(class, f.schoolB, f.yearB, "1A", 1)
	f.student = one(student, f.schoolA, "001", "Siswa A")
	f.studentB = one(student, f.schoolB, "001", "Siswa B")
	return f
}

func txFixture(t *testing.T) (*pgxpool.Pool, pgx.Tx, *placeFixture) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	f := newPlaceFixture(t, tx, "")
	f.tx = tx
	return pool, tx, f
}

// sp menjalankan fn di savepoint yang selalu di-rollback.
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

func history(t *testing.T, q database.DBTX, f *placeFixture) []Enrollment {
	t.Helper()
	list, err := ListEnrollments(context.Background(), q, f.schoolA, f.student)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestEnrollments_DatabaseRejectsTwoActive(t *testing.T) {
	_, tx, f := txFixture(t)
	ctx := context.Background()
	insert := `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-07-13')`
	if _, err := tx.Exec(ctx, insert, f.schoolA, f.student, f.class1A, f.yearA); err != nil {
		t.Fatal(err)
	}
	err := sp(t, tx, func(q pgx.Tx) error {
		_, err := q.Exec(ctx, insert, f.schoolA, f.student, f.class1B, f.yearA)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "enrollments_one_active") {
		t.Errorf("second active enrollment: error = %v, want unique violation", err)
	}
	// Tahun ajaran lain boleh punya enrollment aktif sendiri.
	if _, err := tx.Exec(ctx, insert, f.schoolA, f.student, f.classNext, f.yearA2); err != nil {
		t.Errorf("active enrollment in another year: %v", err)
	}
}

func TestPlace_MoveKeepsHistory(t *testing.T) {
	_, tx, f := txFixture(t)
	ctx := context.Background()

	first, err := Place(ctx, tx, f.schoolA, f.student, f.class1A, f.yearA, day("2026-07-13"))
	if err != nil || !first.Changed || first.Closed != nil {
		t.Fatalf("initial place: %+v, %v", first, err)
	}

	moved, err := Place(ctx, tx, f.schoolA, f.student, f.class1B, f.yearA, day("2026-09-01"))
	if err != nil {
		t.Fatal(err)
	}
	if moved.Closed == nil || moved.Closed.ID != first.Enrollment.ID || !moved.Closed.EndDate.Equal(day("2026-08-31")) {
		t.Errorf("closed = %+v, want first enrollment ending 2026-08-31", moved.Closed)
	}
	if moved.Enrollment.ClassID != f.class1B || !moved.Enrollment.StartDate.Equal(day("2026-09-01")) || !moved.Enrollment.Active() {
		t.Errorf("new enrollment = %+v", moved.Enrollment)
	}

	h := history(t, tx, f)
	if len(h) != 2 || h[0].ClassName != "1B" || !h[0].Active() || h[1].ClassName != "1A" || h[1].Active() {
		t.Errorf("history = %+v", h)
	}
	// Kelas enrollment lama tidak diubah.
	if h[1].ClassID != f.class1A || !h[1].StartDate.Equal(day("2026-07-13")) {
		t.Errorf("old enrollment was modified: %+v", h[1])
	}
}

func TestPlace_Rules(t *testing.T) {
	_, tx, f := txFixture(t)
	ctx := context.Background()
	if _, err := Place(ctx, tx, f.schoolA, f.student, f.class1A, f.yearA, day("2026-08-01")); err != nil {
		t.Fatal(err)
	}

	t.Run("same class is a no-op", func(t *testing.T) {
		res, err := Place(ctx, tx, f.schoolA, f.student, f.class1A, f.yearA, day("2026-09-01"))
		if err != nil || res.Changed || len(history(t, tx, f)) != 1 {
			t.Errorf("res = %+v, err = %v, history = %d", res, err, len(history(t, tx, f)))
		}
	})

	t.Run("date before the current start is rejected", func(t *testing.T) {
		err := sp(t, tx, func(q pgx.Tx) error {
			_, err := Place(ctx, q, f.schoolA, f.student, f.class1B, f.yearA, day("2026-07-20"))
			return err
		})
		if !errors.Is(err, ErrInvalidPlacementDate) {
			t.Errorf("error = %v, want ErrInvalidPlacementDate", err)
		}
	})

	t.Run("date after the academic year is rejected", func(t *testing.T) {
		err := sp(t, tx, func(q pgx.Tx) error {
			_, err := Place(ctx, q, f.schoolA, f.student, f.class1B, f.yearA, day("2027-07-01"))
			return err
		})
		if !errors.Is(err, ErrInvalidPlacementDate) {
			t.Errorf("error = %v, want ErrInvalidPlacementDate", err)
		}
	})

	t.Run("same-day correction keeps an empty history row", func(t *testing.T) {
		err := sp(t, tx, func(q pgx.Tx) error {
			res, err := Place(ctx, q, f.schoolA, f.student, f.class1B, f.yearA, day("2026-08-01"))
			if err != nil {
				return err
			}
			if !res.Closed.EndDate.Equal(day("2026-07-31")) {
				t.Errorf("closed end = %v, want 2026-07-31 (empty period)", res.Closed.EndDate)
			}
			if h := history(t, q, f); len(h) != 2 {
				t.Errorf("history = %d rows, want 2", len(h))
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("date before the academic year starts is moved to its start", func(t *testing.T) {
		res, err := Place(ctx, tx, f.schoolA, f.student, f.classNext, f.yearA2, day("2027-06-01"))
		if err != nil || !res.Enrollment.StartDate.Equal(day("2027-07-12")) {
			t.Errorf("res = %+v, err = %v; want start 2027-07-12", res.Enrollment, err)
		}
	})
}

func TestPlace_FailedStepLeavesNothingHalfDone(t *testing.T) {
	_, tx, f := txFixture(t)
	ctx := context.Background()
	if _, err := Place(ctx, tx, f.schoolA, f.student, f.class1A, f.yearA, day("2026-07-13")); err != nil {
		t.Fatal(err)
	}

	// Trigger sementara (ikut di-rollback bersama transaksi uji) yang
	// menggagalkan INSERT enrollment baru, setelah enrollment lama ditutup.
	if _, err := tx.Exec(ctx, `
		CREATE FUNCTION pg_temp.fail_enrollment_insert() RETURNS trigger LANGUAGE plpgsql AS
		$$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
		CREATE TRIGGER fail_enrollment_insert BEFORE INSERT ON enrollments
		FOR EACH ROW WHEN (NEW.class_id = '`+f.class1B+`') EXECUTE FUNCTION pg_temp.fail_enrollment_insert();`); err != nil {
		t.Fatal(err)
	}

	// Seperti service: Place dalam transaksi (savepoint) sendiri.
	err := pgx.BeginFunc(ctx, tx, func(inner pgx.Tx) error {
		_, err := Place(ctx, inner, f.schoolA, f.student, f.class1B, f.yearA, day("2026-09-01"))
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "P0001") {
		t.Fatalf("error = %v, want injected failure", err)
	}

	h := history(t, tx, f)
	if len(h) != 1 || h[0].ClassID != f.class1A || !h[0].Active() {
		t.Errorf("after failed move: %+v; want only the original enrollment, still active", h)
	}

	// Kegagalan setelah Place berhasil (mis. audit gagal) juga membatalkan semuanya.
	err = pgx.BeginFunc(ctx, tx, func(inner pgx.Tx) error {
		if _, err := Place(ctx, inner, f.schoolA, f.student, f.cl2A, f.yearA, day("2026-09-01")); err != nil {
			return err
		}
		return errors.New("later step failed")
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if h := history(t, tx, f); len(h) != 1 || !h[0].Active() || h[0].ClassID != f.class1A {
		t.Errorf("after rolled-back move: %+v", h)
	}
}

func TestPlace_OtherSchool(t *testing.T) {
	_, tx, f := txFixture(t)
	ctx := context.Background()

	cases := []struct {
		name                   string
		student, class, yearID string
		want                   error
	}{
		{"class of another school", f.student, f.classB, f.yearB, ErrClassNotFound},
		{"class of another school, own year", f.student, f.classB, f.yearA, ErrClassNotFound},
		{"class of another year", f.student, f.classNext, f.yearA, ErrClassNotFound},
		{"student of another school", f.studentB, f.class1A, f.yearA, ErrNotFound},
		{"invalid ids", "x", "y", "z", ErrNotFound},
	}
	for _, tc := range cases {
		err := sp(t, tx, func(q pgx.Tx) error {
			_, err := Place(ctx, q, f.schoolA, tc.student, tc.class, tc.yearID, day("2026-08-01"))
			return err
		})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.want)
		}
	}

	// Database menolak walau repository dilewati.
	err := sp(t, tx, func(q pgx.Tx) error {
		_, err := q.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-08-01')`,
			f.schoolA, f.student, f.classB, f.yearB)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "enrollments_class_fkey") {
		t.Errorf("raw cross-school insert: error = %v, want composite foreign key violation", err)
	}
	err = sp(t, tx, func(q pgx.Tx) error {
		_, err := q.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-08-01')`,
			f.schoolA, f.student, f.classNext, f.yearA)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "enrollments_class_fkey") {
		t.Errorf("raw insert with class of another year: error = %v, want composite foreign key violation", err)
	}
}

func TestEnrollmentService_PlaceAudits(t *testing.T) {
	_, tx, f := txFixture(t)
	ctx := context.Background()
	svc := NewEnrollmentService(tx)
	svc.today = func() time.Time { return day("2026-08-03") }
	actor := school.Actor{SchoolID: f.schoolA, UserID: "44444444-4444-4444-4444-444444444444", IP: netip.MustParseAddr("192.0.2.9")}

	if _, err := svc.Place(ctx, actor, f.student, f.class1A, nil); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Place(ctx, actor, f.student, f.class1B, ptr(day("2026-10-01")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Place(ctx, actor, f.student, f.class1B, nil); err != nil { // tanpa perubahan
		t.Fatal(err)
	}
	if !res.Enrollment.StartDate.Equal(day("2026-10-01")) {
		t.Errorf("start = %v", res.Enrollment.StartDate)
	}

	var n int
	var firstStart, closedEnd string
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'student.place'`, f.schoolA).Scan(&n)
	_ = tx.QueryRow(ctx, `SELECT after->>'start_date' FROM audit_logs WHERE school_id = $1 AND action = 'student.place' ORDER BY id LIMIT 1`, f.schoolA).Scan(&firstStart)
	_ = tx.QueryRow(ctx, `SELECT before->>'end_date' FROM audit_logs WHERE school_id = $1 AND action = 'student.place' ORDER BY id DESC LIMIT 1`, f.schoolA).Scan(&closedEnd)
	if n != 2 || firstStart != "2026-08-03" || closedEnd != "2026-09-30" {
		t.Errorf("audit rows = %d, first start = %s, closed end = %s", n, firstStart, closedEnd)
	}

	if _, err := svc.Place(ctx, actor, f.student, f.classB, nil); !errors.Is(err, ErrClassNotFound) {
		t.Errorf("service with other school's class: error = %v", err)
	}
}

// TestPlace_Concurrent memindahkan siswa yang sama ke dua kelas berbeda
// secara bersamaan. Keduanya harus berhasil bergantian, menyisakan tepat
// satu enrollment aktif.
func TestPlace_Concurrent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Didaftarkan lebih dulu agar berjalan setelah cleanup data (urutan LIFO).
	t.Cleanup(pool.Close)
	f := newPlaceFixture(t, pool, "-c")
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM enrollments WHERE school_id IN ($1, $2)`,
			`DELETE FROM students WHERE school_id IN ($1, $2)`,
			`DELETE FROM classes WHERE school_id IN ($1, $2)`,
			`DELETE FROM academic_years WHERE school_id IN ($1, $2)`,
			`DELETE FROM schools WHERE id IN ($1, $2)`,
		} {
			if _, err := pool.Exec(ctx, sql, f.schoolA, f.schoolB); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := Place(ctx, tx, f.schoolA, f.student, f.class1A, f.yearA, day("2026-07-13"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 30; round++ {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, class := range []string{f.class1B, f.cl2A} {
			wg.Add(1)
			go func(i int, class string) {
				defer wg.Done()
				errs[i] = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
					_, err := Place(ctx, tx, f.schoolA, f.student, class, f.yearA, day("2026-07-13").AddDate(0, 0, round+1))
					return err
				})
			}(i, class)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		var active int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE student_id = $1 AND end_date IS NULL`, f.student).Scan(&active)
		if active != 1 {
			t.Fatalf("round %d: %d active enrollments", round, active)
		}
	}
}

func ptr[T any](v T) *T { return &v }
