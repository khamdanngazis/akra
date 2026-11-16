package duty

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/testutil"
)

func seed(t *testing.T) (pgx.Tx, *testutil.School, *testutil.School) {
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
	a, b := testutil.SeedTwoSchools(t, tx, "$argon2id$v=19$m=1024,t=1,p=1$c2FsdA$aGFzaA")
	return tx, a, b
}

func auditCount(t *testing.T, tx pgx.Tx, schoolID, action string) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, schoolID, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAssignPerYear(t *testing.T) {
	tx, a, b := seed(t)
	ctx := context.Background()
	actor := Actor{SchoolID: a.ID, UserID: a.Admin.ID}

	has := func() bool {
		ok, err := HasActive(ctx, tx, a.ID, a.Teacher.ID, ExamCoordinator)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if has() {
		t.Fatal("teacher is coordinator before assignment")
	}
	asg, err := Assign(ctx, tx, actor, a.YearID, a.Teacher.ID, ExamCoordinator)
	if err != nil {
		t.Fatal(err)
	}
	if !has() {
		t.Error("teacher is not coordinator after assignment")
	}
	if n := auditCount(t, tx, a.ID, ActionAssign); n != 1 {
		t.Errorf("assign audit = %d", n)
	}
	if _, err := Assign(ctx, tx, actor, a.YearID, a.Teacher.ID, ExamCoordinator); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("duplicate: %v", err)
	}
	list, err := List(ctx, tx, a.ID, a.YearID)
	if err != nil || len(list) != 2 { // fixture: kepala sekolah + guru
		t.Errorf("List = %d, %v", len(list), err)
	}

	// Tahun ajaran baru diaktifkan: tugas tahun lalu tidak berlaku.
	var newYear string
	if err := tx.QueryRow(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2027/2028', '2027-07-12', '2028-06-24') RETURNING id`, a.ID).Scan(&newYear); err != nil {
		t.Fatal(err)
	}
	// Dua langkah: indeks unik satu-tahun-aktif dicek per baris.
	if _, err := tx.Exec(ctx, `UPDATE academic_years SET is_active = false WHERE school_id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE academic_years SET is_active = true WHERE id = $1`, newYear); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Error("coordinator duty inherited by the new academic year")
	}
	if list, _ := List(ctx, tx, a.ID, newYear); len(list) != 0 {
		t.Errorf("new year has %d duties", len(list))
	}

	// Cabut di tahun lama.
	if err := Remove(ctx, tx, actor, asg.ID); err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, tx, a.ID, ActionRemove); n != 1 {
		t.Errorf("remove audit = %d", n)
	}
	if err := Remove(ctx, tx, actor, asg.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove twice: %v", err)
	}

	// Sekolah lain: tahun, guru, dan tugas milik B ditolak.
	if _, err := Assign(ctx, tx, actor, b.YearID, a.Teacher.ID, ExamCoordinator); !errors.Is(err, ErrNotFound) {
		t.Errorf("other school's year: %v", err)
	}
	if _, err := Assign(ctx, tx, actor, a.YearID, b.Teacher.ID, ExamCoordinator); !errors.Is(err, ErrNotTeacher) {
		t.Errorf("other school's teacher: %v", err)
	}
	if err := Remove(ctx, tx, actor, b.DutyID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove other school's duty: %v", err)
	}
}

func TestAssignValidation(t *testing.T) {
	tx, a, _ := seed(t)
	ctx := context.Background()
	actor := Actor{SchoolID: a.ID, UserID: a.Admin.ID}
	if _, err := Assign(ctx, tx, actor, a.YearID, a.Principal.ID, ExamCoordinator); !errors.Is(err, ErrNotTeacher) {
		t.Errorf("principal: %v, want ErrNotTeacher", err)
	}
	if _, err := Assign(ctx, tx, actor, a.YearID, a.Teacher.ID, "kepala_lab"); !errors.Is(err, ErrInvalidDuty) {
		t.Errorf("unknown duty: %v", err)
	}
	if _, err := Assign(ctx, tx, actor, "bukan-uuid", a.Teacher.ID, ExamCoordinator); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad year id: %v", err)
	}
	if err := Remove(ctx, tx, actor, "bukan-uuid"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad duty id: %v", err)
	}
}
