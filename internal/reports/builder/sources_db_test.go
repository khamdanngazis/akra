package builder

// Uji integrasi: setiap sumber dijalankan ke database sungguhan, sehingga
// nama tabel/kolom yang salah di sources.go langsung ketahuan.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/testutil"
)

func seededTx(t *testing.T) (pgx.Tx, *testutil.School, *testutil.School) {
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
	hash, err := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}).HashPassword(testutil.Password)
	if err != nil {
		t.Fatal(err)
	}
	a, b := testutil.SeedTwoSchools(t, tx, hash)
	return tx, a, b
}

// run menjalankan cfg untuk schoolID dan mengembalikan baris sebagai map
// kunci kolom -> nilai.
func run(t *testing.T, tx pgx.Tx, schoolID string, cfg Config) []map[string]any {
	t.Helper()
	q, err := Compile(Default, schoolID, cfg, Options{})
	if err != nil {
		t.Fatalf("%s: compile: %v", cfg.Source, err)
	}
	rows, err := tx.Query(context.Background(), q.SQL, q.Args...)
	if err != nil {
		t.Fatalf("%s: query: %v\n%s", cfg.Source, err, q.SQL)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("%s: scan: %v", cfg.Source, err)
		}
		row := make(map[string]any, len(vals))
		for i, c := range q.Columns {
			row[c.Key] = vals[i]
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: rows: %v", cfg.Source, err)
	}
	return out
}

func allKeys(s Source) []string {
	keys := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		keys[i] = c.Key
	}
	return keys
}

// TestSources_RunAgainstSchema: setiap sumber dengan semua kolomnya berjalan
// di skema sungguhan, berisi data sekolah sendiri saja.
func TestSources_RunAgainstSchema(t *testing.T) {
	tx, a, b := seededTx(t)
	for _, s := range Default.Sources() {
		rows := run(t, tx, a.ID, Config{Source: s.Name, Columns: allKeys(s)})
		if len(rows) == 0 {
			t.Errorf("%s: no rows for the seeded school", s.Name)
		}
		for _, r := range rows {
			name, _ := r["student_name"].(string)
			if !strings.Contains(name, a.Marker) || strings.Contains(name, b.Marker) {
				t.Errorf("%s: row from another school: %q", s.Name, name)
			}
		}
	}
}

// TestSources_FiltersBindAgainstSchema menjalankan satu filter per kolom
// dengan operator yang sesuai tipenya, agar tipe parameter cocok dengan
// ekspresi kolom di database.
func TestSources_FiltersBindAgainstSchema(t *testing.T) {
	tx, a, _ := seededTx(t)
	for _, s := range Default.Sources() {
		for _, c := range s.Columns {
			var filters []Filter
			switch c.Type {
			case TypeText:
				filters = []Filter{{Column: c.Key, Op: OpContains, Value: "a"}, {Column: c.Key, Op: OpIn, Values: []string{"x", "y"}}, {Column: c.Key, Op: OpNe, Value: "x"}}
			case TypeInt, TypeMoney:
				filters = []Filter{{Column: c.Key, Op: OpGte, Value: "0"}, {Column: c.Key, Op: OpIn, Values: []string{"1", "2"}}}
			case TypeDate:
				filters = []Filter{{Column: c.Key, Op: OpLte, Value: "2099-12-31"}, {Column: c.Key, Op: OpIn, Values: []string{"2026-09-10"}}}
			}
			for _, f := range filters {
				run(t, tx, a.ID, Config{Source: s.Name, Columns: []string{c.Key}, Filters: []Filter{f}, Order: []Order{{Column: c.Key, Desc: true}}})
			}
		}
	}
}

// TestSources_ArrearsMatchesBilling: sumber tunggakan sama dengan daftar
// tunggakan /keuangan/tunggakan (billing.ListArrears).
func TestSources_ArrearsMatchesBilling(t *testing.T) {
	tx, a, _ := seededTx(t)
	want, err := billing.ListArrears(context.Background(), tx, a.ID, billing.ArrearsFilter{}, clock.Today())
	if err != nil {
		t.Fatal(err)
	}
	rows := run(t, tx, a.ID, Config{Source: "arrears", Columns: []string{"nis", "fee_types", "total", "paid", "remaining", "days_overdue"}})
	if len(rows) != len(want.Items) || len(rows) == 0 {
		t.Fatalf("rows = %d, billing = %d", len(rows), len(want.Items))
	}
	var total int64
	byNIS := map[string]billing.Arrear{}
	for _, it := range want.Items {
		byNIS[it.NIS] = it
	}
	for _, r := range rows {
		it := byNIS[r["nis"].(string)]
		if r["paid"] != it.Paid || r["remaining"] != it.Remaining || r["fee_types"] != it.FeeNames || int(r["days_overdue"].(int32)) != it.DaysOverdue {
			t.Errorf("row %v, billing %+v", r, it)
		}
		total += r["remaining"].(int64)
	}
	if total != want.TotalRemaining {
		t.Errorf("total remaining %d, billing %d", total, want.TotalRemaining)
	}
}

// TestSources_InjectionIsPlainText: nilai berisi SQL dikirim sebagai
// parameter; tabel tetap ada dan tidak ada baris yang cocok.
func TestSources_InjectionIsPlainText(t *testing.T) {
	tx, a, _ := seededTx(t)
	for _, f := range []Filter{
		{Column: "student_name", Op: OpEq, Value: "'; DROP TABLE students; --"},
		{Column: "student_name", Op: OpContains, Value: "' OR '1'='1"},
		{Column: "student_name", Op: OpIn, Values: []string{"x') OR true --"}},
	} {
		if rows := run(t, tx, a.ID, Config{Source: "payments", Columns: []string{"nis"}, Filters: []Filter{f}}); len(rows) != 0 {
			t.Errorf("%+v matched %d rows", f, len(rows))
		}
	}
	var n int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM students WHERE school_id = $1`, a.ID).Scan(&n); err != nil || n == 0 {
		t.Fatalf("students table after injection attempt: %v, %d rows", err, n)
	}
}
