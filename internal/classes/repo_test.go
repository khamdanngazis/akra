package classes

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

type fixture struct {
	tx                     pgx.Tx
	schoolA, schoolB       string
	yearA1, yearA2, yearB1 string
	teacherA, teacherB     string
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
	school := `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`
	year := `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, $2, $3, $4) RETURNING id`
	user := `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, $2, 'x', $3) RETURNING id`
	f.schoolA = one(school, "test-cls-a")
	f.schoolB = one(school, "test-cls-b")
	f.yearA1 = one(year, f.schoolA, "2025/2026", "2025-07-01", "2026-06-30")
	f.yearA2 = one(year, f.schoolA, "2026/2027", "2026-07-01", "2027-06-30")
	f.yearB1 = one(year, f.schoolB, "2025/2026", "2025-07-01", "2026-06-30")
	f.teacherA = one(user, f.schoolA, "guru@a.id", "Bu Ani")
	f.teacherB = one(user, f.schoolB, "guru@b.id", "Pak Budi")
	return f
}

// try menjalankan fn di savepoint yang selalu di-rollback.
func (f *fixture) try(t *testing.T, fn func(q pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	sp, err := f.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sp.Rollback(ctx) }()
	return fn(sp)
}

func (f *fixture) create(t *testing.T, schoolID string, in ClassInput) error {
	t.Helper()
	return f.try(t, func(q pgx.Tx) error {
		_, err := Create(context.Background(), q, schoolID, in)
		return err
	})
}

func TestClasses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	c1A, err := Create(ctx, f.tx, f.schoolA, ClassInput{AcademicYearID: f.yearA1, Name: "1A", GradeLevel: 1, HomeroomTeacherID: f.teacherA})
	if err != nil {
		t.Fatalf("create 1A: %v", err)
	}
	if c1A.SchoolID != f.schoolA || c1A.AcademicYearID != f.yearA1 || c1A.GradeLevel != 1 ||
		c1A.HomeroomTeacherID != f.teacherA || c1A.HomeroomTeacherName != "Bu Ani" {
		t.Errorf("created class = %+v", c1A)
	}

	t.Run("duplicate name in the same year is rejected", func(t *testing.T) {
		for _, name := range []string{"1A", "1a"} {
			err := f.create(t, f.schoolA, ClassInput{AcademicYearID: f.yearA1, Name: name, GradeLevel: 1})
			if !errors.Is(err, ErrDuplicateName) {
				t.Errorf("%s: error = %v, want ErrDuplicateName", name, err)
			}
		}
	})

	t.Run("same name in another year is allowed", func(t *testing.T) {
		if _, err := Create(ctx, f.tx, f.schoolA, ClassInput{AcademicYearID: f.yearA2, Name: "1A", GradeLevel: 1}); err != nil {
			t.Errorf("error = %v, want nil", err)
		}
	})

	t.Run("grade level outside 1-12 is rejected by the database", func(t *testing.T) {
		for _, g := range []int{0, 13, -1} {
			err := f.create(t, f.schoolA, ClassInput{AcademicYearID: f.yearA1, Name: "X", GradeLevel: g})
			if !errors.Is(err, ErrInvalidGradeLevel) {
				t.Errorf("grade %d: error = %v, want ErrInvalidGradeLevel", g, err)
			}
		}
		err := f.try(t, func(q pgx.Tx) error {
			_, err := q.Exec(ctx, `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, 'Y', 13)`, f.schoolA, f.yearA1)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "classes_grade_level_check") {
			t.Errorf("raw insert grade 13: error = %v, want check violation", err)
		}
	})

	t.Run("academic year of another school is rejected", func(t *testing.T) {
		err := f.create(t, f.schoolA, ClassInput{AcademicYearID: f.yearB1, Name: "2A", GradeLevel: 2})
		if !errors.Is(err, ErrAcademicYearNotFound) {
			t.Errorf("repository: error = %v, want ErrAcademicYearNotFound", err)
		}
		for _, bad := range []string{"", "bukan-uuid", "00000000-0000-0000-0000-000000000000"} {
			if err := f.create(t, f.schoolA, ClassInput{AcademicYearID: bad, Name: "2A", GradeLevel: 2}); !errors.Is(err, ErrAcademicYearNotFound) {
				t.Errorf("year %q: error = %v, want ErrAcademicYearNotFound", bad, err)
			}
		}
		// Database juga menolak walau repository dilewati.
		err = f.try(t, func(q pgx.Tx) error {
			_, err := q.Exec(ctx, `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '2A', 2)`, f.schoolA, f.yearB1)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "classes_academic_year_fkey") {
			t.Errorf("raw insert: error = %v, want composite foreign key violation", err)
		}
	})

	t.Run("homeroom teacher of another school is rejected", func(t *testing.T) {
		err := f.create(t, f.schoolA, ClassInput{AcademicYearID: f.yearA1, Name: "3A", GradeLevel: 3, HomeroomTeacherID: f.teacherB})
		if !errors.Is(err, ErrTeacherNotFound) {
			t.Errorf("error = %v, want ErrTeacherNotFound", err)
		}
		if err := f.create(t, f.schoolA, ClassInput{AcademicYearID: f.yearA1, Name: "3A", GradeLevel: 3, HomeroomTeacherID: "bukan-uuid"}); !errors.Is(err, ErrTeacherNotFound) {
			t.Errorf("invalid teacher id: error = %v, want ErrTeacherNotFound", err)
		}
	})

	t.Run("ListByYear only returns the caller's classes", func(t *testing.T) {
		if _, err := Create(ctx, f.tx, f.schoolA, ClassInput{AcademicYearID: f.yearA1, Name: "2B", GradeLevel: 2}); err != nil {
			t.Fatal(err)
		}
		if _, err := Create(ctx, f.tx, f.schoolA, ClassInput{AcademicYearID: f.yearA1, Name: "1b", GradeLevel: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := Create(ctx, f.tx, f.schoolB, ClassInput{AcademicYearID: f.yearB1, Name: "1A", GradeLevel: 1}); err != nil {
			t.Fatal(err)
		}

		list, err := ListByYear(ctx, f.tx, f.schoolA, f.yearA1)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, c := range list {
			names = append(names, c.Name)
			if c.SchoolID != f.schoolA {
				t.Errorf("class %s belongs to another school", c.Name)
			}
		}
		if got := strings.Join(names, ","); got != "1A,1b,2B" {
			t.Errorf("names = %s, want 1A,1b,2B (by grade, then name)", got)
		}

		// Sekolah B memakai ID tahun ajaran sekolah A: hasil kosong.
		if list, err := ListByYear(ctx, f.tx, f.schoolB, f.yearA1); err != nil || len(list) != 0 {
			t.Errorf("school B listing school A's year: %d classes, err %v; want none", len(list), err)
		}
		if list, err := ListByYear(ctx, f.tx, f.schoolA, "bukan-uuid"); err != nil || len(list) != 0 {
			t.Errorf("invalid year id: %d classes, err %v", len(list), err)
		}
	})

	t.Run("GetByID is scoped to the school", func(t *testing.T) {
		got, err := GetByID(ctx, f.tx, f.schoolA, c1A.ID)
		if err != nil || got.Name != "1A" {
			t.Errorf("own class: %+v, %v", got, err)
		}
		if _, err := GetByID(ctx, f.tx, f.schoolB, c1A.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("other school: error = %v, want ErrNotFound", err)
		}
		if _, err := GetByID(ctx, f.tx, f.schoolA, "bukan-uuid"); !errors.Is(err, ErrNotFound) {
			t.Errorf("invalid id: error = %v, want ErrNotFound", err)
		}
	})
}
