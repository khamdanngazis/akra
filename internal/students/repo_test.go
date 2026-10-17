package students

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

type fixture struct {
	tx               pgx.Tx
	schoolA, schoolB string
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
	for _, s := range []struct {
		dst  *string
		code string
	}{{&f.schoolA, "test-stu-a"}, {&f.schoolB, "test-stu-b"}} {
		if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`, s.code).Scan(s.dst); err != nil {
			t.Fatal(err)
		}
	}
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

func (f *fixture) create(t *testing.T, schoolID string, in StudentInput) error {
	t.Helper()
	return f.try(t, func(q pgx.Tx) error {
		_, err := Create(context.Background(), q, schoolID, in)
		return err
	})
}

func names(list []Student) string {
	var n []string
	for _, s := range list {
		n = append(n, s.FullName)
	}
	return strings.Join(n, ",")
}

func TestStudents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	birth := time.Date(2018, 3, 9, 0, 0, 0, 0, time.UTC)

	s007, err := Create(ctx, f.tx, f.schoolA, StudentInput{NIS: "007", FullName: "Ahmad Fauzi", BirthDate: &birth, Gender: GenderMale})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("NIS keeps leading zeros and defaults are applied", func(t *testing.T) {
		got, err := GetByID(ctx, f.tx, f.schoolA, s007.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.NIS != "007" || got.Status != StatusActive || got.Gender != "L" || got.BirthDate == nil || !got.BirthDate.Equal(birth) {
			t.Errorf("student = %+v", got)
		}
		var raw string
		_ = f.tx.QueryRow(ctx, `SELECT nis FROM students WHERE id = $1`, s007.ID).Scan(&raw)
		if raw != "007" {
			t.Errorf("stored nis = %q, want 007", raw)
		}
	})

	t.Run("duplicate NIS in the same school is rejected", func(t *testing.T) {
		if err := f.create(t, f.schoolA, StudentInput{NIS: "007", FullName: "Orang Lain"}); !errors.Is(err, ErrDuplicateNIS) {
			t.Errorf("error = %v, want ErrDuplicateNIS", err)
		}
		// "7" berbeda dengan "007".
		if err := f.create(t, f.schoolA, StudentInput{NIS: "7", FullName: "Tujuh"}); err != nil {
			t.Errorf("NIS 7 next to 007: error = %v", err)
		}
	})

	t.Run("same NIS in another school is allowed", func(t *testing.T) {
		if _, err := Create(ctx, f.tx, f.schoolB, StudentInput{NIS: "007", FullName: "Ahmad Sekolah B"}); err != nil {
			t.Errorf("error = %v, want nil", err)
		}
	})

	t.Run("gender other than L, P, or empty is rejected", func(t *testing.T) {
		for _, g := range []string{"X", "l", "LP", " "} {
			err := f.create(t, f.schoolA, StudentInput{NIS: "G1", FullName: "Uji", Gender: g})
			if !errors.Is(err, ErrInvalidGender) {
				t.Errorf("gender %q: error = %v, want ErrInvalidGender", g, err)
			}
		}
		if err := f.create(t, f.schoolA, StudentInput{NIS: "G2", FullName: "Uji", Gender: "P"}); err != nil {
			t.Errorf("gender P: error = %v", err)
		}
		if err := f.create(t, f.schoolA, StudentInput{NIS: "G3", FullName: "Uji"}); err != nil {
			t.Errorf("empty gender: error = %v", err)
		}
	})

	t.Run("invalid NIS and status are rejected", func(t *testing.T) {
		for _, nis := range []string{"", " 008", "008 "} {
			if err := f.create(t, f.schoolA, StudentInput{NIS: nis, FullName: "Uji"}); !errors.Is(err, ErrInvalidNIS) {
				t.Errorf("NIS %q: error = %v, want ErrInvalidNIS", nis, err)
			}
		}
		if err := f.create(t, f.schoolA, StudentInput{NIS: "S1", FullName: "Uji", Status: "naik"}); !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("status naik: error = %v, want ErrInvalidStatus", err)
		}
		for _, st := range Statuses {
			if err := f.create(t, f.schoolA, StudentInput{NIS: "S-" + st, FullName: "Uji", Status: st}); err != nil {
				t.Errorf("status %s: error = %v", st, err)
			}
		}
	})

	t.Run("errors do not leak personal data", func(t *testing.T) {
		err := f.create(t, f.schoolA, StudentInput{NIS: "RAHASIA-123", FullName: "Nama Rahasia", Gender: "Q"})
		if err == nil || strings.Contains(err.Error(), "RAHASIA") || strings.Contains(err.Error(), "Nama Rahasia") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		u, err := Update(ctx, f.tx, f.schoolA, s007.ID, StudentInput{NIS: "0070", FullName: "Ahmad Fauzi Rahman", Status: StatusTransferred})
		if err != nil {
			t.Fatal(err)
		}
		if u.NIS != "0070" || u.FullName != "Ahmad Fauzi Rahman" || u.BirthDate != nil || u.Gender != "" || u.Status != StatusTransferred {
			t.Errorf("updated = %+v", u)
		}
		err = f.try(t, func(q pgx.Tx) error {
			_, err := Update(ctx, q, f.schoolB, s007.ID, StudentInput{NIS: "X", FullName: "Diretas"})
			return err
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("update from another school: error = %v, want ErrNotFound", err)
		}
	})

	t.Run("GetByID is scoped to the school", func(t *testing.T) {
		if _, err := GetByID(ctx, f.tx, f.schoolB, s007.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("other school: error = %v", err)
		}
		if _, err := GetByID(ctx, f.tx, f.schoolA, "bukan-uuid"); !errors.Is(err, ErrNotFound) {
			t.Errorf("invalid id: error = %v", err)
		}
	})
}

func TestSearch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, s := range []StudentInput{
		{NIS: "1001", FullName: "Budi Santoso"},
		{NIS: "1002", FullName: "Siti Aminah"},
		{NIS: "2001", FullName: "budiman Hakim"},
		{NIS: "3001", FullName: "Rina 100% Ceria"},
		{NIS: "3002", FullName: "Dewi_Lestari"},
	} {
		if _, err := Create(ctx, f.tx, f.schoolA, s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Create(ctx, f.tx, f.schoolB, StudentInput{NIS: "1001", FullName: "Budi Sekolah Lain"}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"budi":  "Budi Santoso,budiman Hakim",
		"BUDI":  "Budi Santoso,budiman Hakim",
		" ami ": "Siti Aminah",
		"100":   "Budi Santoso,Rina 100% Ceria,Siti Aminah", // awalan NIS 100x, dan nama mengandung "100"
		"20":    "budiman Hakim",
		"%":     "Rina 100% Ceria",
		"_":     "Dewi_Lestari",
		"zzz":   "",
		"":      "Budi Santoso,budiman Hakim,Dewi_Lestari,Rina 100% Ceria,Siti Aminah",
	}
	for query, want := range cases {
		got, err := Search(ctx, f.tx, f.schoolA, query, 0)
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		if names(got) != want {
			t.Errorf("Search(%q) = %q, want %q", query, names(got), want)
		}
		for _, s := range got {
			if s.SchoolID != f.schoolA {
				t.Errorf("Search(%q) returned a student of another school", query)
			}
		}
	}

	if got, _ := Search(ctx, f.tx, f.schoolB, "budi", 0); names(got) != "Budi Sekolah Lain" {
		t.Errorf("school B search = %q", names(got))
	}
	if got, _ := Search(ctx, f.tx, f.schoolA, "", 2); len(got) != 2 {
		t.Errorf("limit 2 returned %d rows", len(got))
	}
}
