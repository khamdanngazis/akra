package school

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/platform/database"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := database.Connect(context.Background(), dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func yearInput(name string) AcademicYearInput {
	start := date(name[:4] + "-07-01")
	return AcademicYearInput{Name: name, StartDate: start, EndDate: start.AddDate(1, 0, -1)}
}

func createSchool(t *testing.T, q database.DBTX, code string) string {
	t.Helper()
	var id string
	if err := q.QueryRow(context.Background(), `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`, code).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// activeID mengembalikan ID tahun aktif sekolah, atau "" jika tidak ada.
func activeID(t *testing.T, q database.DBTX, schoolID string) string {
	t.Helper()
	y, err := GetActiveAcademicYear(context.Background(), q, schoolID)
	if errors.Is(err, ErrNotFound) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return y.ID
}

// savepoint menjalankan fn di savepoint yang selalu di-rollback, agar error
// database di fn tidak membatalkan transaksi luar.
func savepoint(t *testing.T, tx pgx.Tx, fn func(sp pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sp.Rollback(ctx) }()
	return fn(sp)
}

func TestAcademicYears(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	schoolA := createSchool(t, tx, "test-ay-a")
	schoolB := createSchool(t, tx, "test-ay-b")

	y1, err := CreateAcademicYear(ctx, tx, schoolA, yearInput("2025/2026"))
	if err != nil {
		t.Fatal(err)
	}
	y2, err := CreateAcademicYear(ctx, tx, schoolA, yearInput("2026/2027"))
	if err != nil {
		t.Fatal(err)
	}
	yB, err := CreateAcademicYear(ctx, tx, schoolB, yearInput("2026/2027"))
	if err != nil {
		t.Fatalf("same name in another school: error = %v, want nil", err)
	}

	t.Run("new years are inactive", func(t *testing.T) {
		for _, y := range []AcademicYear{y1, y2, yB} {
			if y.IsActive {
				t.Errorf("%s is active after create", y.Name)
			}
		}
		if got := activeID(t, tx, schoolA); got != "" {
			t.Errorf("active year = %q, want none", got)
		}
	})

	t.Run("database rejects two active years in one school", func(t *testing.T) {
		err := savepoint(t, tx, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `UPDATE academic_years SET is_active = true WHERE school_id = $1`, schoolA)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "academic_years_one_active") {
			t.Errorf("error = %v, want unique violation on academic_years_one_active", err)
		}
	})

	t.Run("SetActive switches the active year", func(t *testing.T) {
		if err := SetActive(ctx, tx, schoolA, y1.ID); err != nil {
			t.Fatal(err)
		}
		if got := activeID(t, tx, schoolA); got != y1.ID {
			t.Fatalf("active = %q, want y1", got)
		}
		if err := SetActive(ctx, tx, schoolA, y2.ID); err != nil {
			t.Fatal(err)
		}
		if got := activeID(t, tx, schoolA); got != y2.ID {
			t.Errorf("active = %q, want y2", got)
		}
		first, _ := GetAcademicYear(ctx, tx, schoolA, y1.ID)
		if first.IsActive {
			t.Error("y1 is still active after activating y2")
		}
		// Idempoten.
		if err := SetActive(ctx, tx, schoolA, y2.ID); err != nil || activeID(t, tx, schoolA) != y2.ID {
			t.Errorf("activating the active year again: err = %v", err)
		}
	})

	t.Run("schools are independent", func(t *testing.T) {
		if err := SetActive(ctx, tx, schoolB, yB.ID); err != nil {
			t.Fatal(err)
		}
		if got := activeID(t, tx, schoolA); got != y2.ID {
			t.Errorf("school A active = %q after activating in school B, want y2", got)
		}
		if got := activeID(t, tx, schoolB); got != yB.ID {
			t.Errorf("school B active = %q, want yB", got)
		}
	})

	t.Run("cannot activate another school's year", func(t *testing.T) {
		err := SetActive(ctx, tx, schoolA, yB.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
		if activeID(t, tx, schoolA) != y2.ID || activeID(t, tx, schoolB) != yB.ID {
			t.Error("active years changed after a rejected SetActive")
		}
		if err := SetActive(ctx, tx, schoolA, "bukan-uuid"); !errors.Is(err, ErrNotFound) {
			t.Errorf("invalid id: error = %v, want ErrNotFound", err)
		}
	})

	t.Run("dates must be in order", func(t *testing.T) {
		for _, in := range []AcademicYearInput{
			{Name: "terbalik", StartDate: date("2027-06-30"), EndDate: date("2026-07-01")},
			{Name: "sama", StartDate: date("2026-07-01"), EndDate: date("2026-07-01")},
		} {
			err := savepoint(t, tx, func(sp pgx.Tx) error {
				_, err := CreateAcademicYear(ctx, sp, schoolA, in)
				return err
			})
			if !errors.Is(err, ErrInvalidDates) {
				t.Errorf("%s: error = %v, want ErrInvalidDates", in.Name, err)
			}
		}
	})

	t.Run("duplicate name in the same school", func(t *testing.T) {
		err := savepoint(t, tx, func(sp pgx.Tx) error {
			_, err := CreateAcademicYear(ctx, sp, schoolA, yearInput("2025/2026"))
			return err
		})
		if !errors.Is(err, ErrDuplicateName) {
			t.Errorf("error = %v, want ErrDuplicateName", err)
		}
	})

	t.Run("get and update are scoped to the school", func(t *testing.T) {
		err := savepoint(t, tx, func(sp pgx.Tx) error {
			_, err := GetAcademicYear(ctx, sp, schoolA, yB.ID)
			return err
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("get other school's year: error = %v, want ErrNotFound", err)
		}
		err = savepoint(t, tx, func(sp pgx.Tx) error {
			_, err := GetAcademicYear(ctx, sp, schoolA, "bukan-uuid")
			return err
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("get invalid id: error = %v, want ErrNotFound", err)
		}
		err = savepoint(t, tx, func(sp pgx.Tx) error {
			_, err := UpdateAcademicYear(ctx, sp, schoolA, yB.ID, yearInput("2030/2031"))
			return err
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("update other school's year: error = %v, want ErrNotFound", err)
		}

		updated, err := UpdateAcademicYear(ctx, tx, schoolA, y1.ID, AcademicYearInput{Name: "2025/2026 (revisi)", StartDate: date("2025-07-14"), EndDate: date("2026-06-20")})
		if err != nil {
			t.Fatal(err)
		}
		if updated.Name != "2025/2026 (revisi)" || !updated.StartDate.Equal(date("2025-07-14")) || updated.IsActive {
			t.Errorf("updated = %+v", updated)
		}
	})

	t.Run("list is newest first", func(t *testing.T) {
		years, err := ListAcademicYears(ctx, tx, schoolA)
		if err != nil {
			t.Fatal(err)
		}
		if len(years) != 2 || years[0].ID != y2.ID || years[1].ID != y1.ID {
			t.Errorf("list = %+v", years)
		}
	})
}

// TestSetActive_Concurrent menjalankan dua aktivasi bersamaan di sekolah
// yang sama. Keduanya harus berhasil berurutan dan menyisakan tepat satu
// tahun aktif.
func TestSetActive_Concurrent(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	schoolID := createSchool(t, pool, "test-ay-concurrent")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM academic_years WHERE school_id = $1`, schoolID)
		_, _ = pool.Exec(ctx, `DELETE FROM schools WHERE id = $1`, schoolID)
	})
	var ids []string
	for _, name := range []string{"2024/2025", "2025/2026", "2026/2027"} {
		y, err := CreateAcademicYear(ctx, pool, schoolID, yearInput(name))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, y.ID)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return SetActive(ctx, tx, schoolID, ids[0]) }); err != nil {
		t.Fatal(err)
	}

	for round := 0; round < 50; round++ {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, id := range ids[1:] {
			wg.Add(1)
			go func(i int, id string) {
				defer wg.Done()
				errs[i] = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return SetActive(ctx, tx, schoolID, id) })
			}(i, id)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: concurrent SetActive error = %v", round, err)
			}
		}
		var active int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM academic_years WHERE school_id = $1 AND is_active`, schoolID).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active != 1 {
			t.Fatalf("round %d: %d active years, want 1", round, active)
		}
	}
}
