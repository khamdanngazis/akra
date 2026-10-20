package schedule

// Uji permintaan bersamaan (T66). Data di-commit karena setiap goroutine
// butuh transaksi sendiri; sekolah uji dihapus di akhir.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/platform/database"
	"akra/internal/school"
)

const concurrencyRuns = 30

func TestConcurrent_ConflictingSlots(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 6)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	// Kode acak: jika pembersihan gagal, run berikutnya tidak bentrok.
	schoolID := one(`INSERT INTO schools (name, code) VALUES ('Uji jadwal konkuren', $1) RETURNING id`, "test-jadwal-"+hex.EncodeToString(suffix))
	t.Cleanup(func() { cleanupSchool(t, pool, schoolID) })
	actor := school.Actor{SchoolID: schoolID, UserID: one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'admin@conc.uji', 'x', 'Admin') RETURNING id`, schoolID)}
	teacher := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru@conc.uji', 'x', 'Guru') RETURNING id`, schoolID)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, schoolID)
	class := `INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`
	classA, classB := one(class, schoolID, year, "1A"), one(class, schoolID, year, "1B")
	subject := `INSERT INTO subjects (school_id, name) VALUES ($1, $2) RETURNING id`
	mtk, ipa := one(subject, schoolID, "Matematika"), one(subject, schoolID, "IPA")
	other := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru2@conc.uji', 'x', 'Guru 2') RETURNING id`, schoolID)
	for _, a := range [][3]string{{classA, mtk, teacher}, {classB, mtk, teacher}, {classA, ipa, other}} {
		if _, err := pool.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
			schoolID, year, a[0], a[1], a[2]); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(pool)

	// Setiap putaran memakai jam berbeda agar tidak bentrok dengan putaran
	// sebelumnya. Skenario: kelas sama (mapel beda) dan guru sama (kelas beda).
	for _, sc := range []struct {
		name   string
		second func(start string) (string, Form)
	}{
		{"same class", func(start string) (string, Form) { return classA, form(ipa, "", start, "") }},
		{"same teacher", func(start string) (string, Form) { return classB, form(mtk, "", start, "") }},
	} {
		day := map[string]string{"same class": "1", "same teacher": "2"}[sc.name]
		for i := range concurrencyRuns {
			start := fmt.Sprintf("%02d:%02d", 6+i/4, (i%4)*15)
			end := fmt.Sprintf("%02d:%02d", 6+i/4, (i%4)*15+10)
			reqs := [2]struct {
				class string
				f     Form
			}{{classA, form(mtk, day, start, end)}}
			reqs[1].class, reqs[1].f = sc.second(start)
			reqs[1].f.Day, reqs[1].f.End = day, end

			var wg sync.WaitGroup
			errs := make([]error, 2)
			for k := range reqs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, errs[k] = svc.Create(ctx, actor, reqs[k].class, reqs[k].f)
				}()
			}
			wg.Wait()
			ok := 0
			for _, err := range errs {
				var ce *ConflictError
				switch {
				case err == nil:
					ok++
				case errors.As(err, &ce), errors.Is(err, ErrOverlap):
				default:
					t.Fatalf("%s run %d: unexpected error %v", sc.name, i, err)
				}
			}
			if ok != 1 {
				t.Fatalf("%s run %d: %d requests succeeded, want exactly 1 (%v)", sc.name, i, ok, errs)
			}
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM schedule_slots WHERE school_id = $1 AND day_of_week = $2`, schoolID, day).Scan(&n); err != nil || n != concurrencyRuns {
			t.Errorf("%s: %d slots stored, want %d (%v)", sc.name, n, concurrencyRuns, err)
		}
	}
}

// cleanupSchool menghapus seluruh data sekolah uji yang sudah di-commit.
func cleanupSchool(t *testing.T, pool *pgxpool.Pool, schoolID string) {
	t.Helper()
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT c.table_name FROM information_schema.columns c
			JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
			WHERE c.table_schema = 'public' AND c.column_name = 'school_id' AND t.table_type = 'BASE TABLE'`)
		if err != nil {
			return err
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id IN (SELECT id FROM users WHERE school_id = $1)`, schoolID); err != nil {
			return err
		}
		for _, table := range tables {
			if _, err := tx.Exec(ctx, `DELETE FROM `+pgx.Identifier{table}.Sanitize()+` WHERE school_id = $1`, schoolID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM schools WHERE id = $1`, schoolID)
		return err
	})
	if err != nil {
		t.Logf("cleanup of test school %s skipped: %v", schoolID, err)
	}
}
