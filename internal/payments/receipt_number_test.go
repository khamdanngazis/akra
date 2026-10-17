package payments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/school"
)

func TestReceiptNumber_SequentialAndNoGapAfterRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	year := clock.Today().Year()
	p1, err := RecordPayment(ctx, f.tx, f.actor, cash(f.invA.ID, 1000))
	if err != nil {
		t.Fatal(err)
	}
	// Pembayaran yang gagal setelah nomor diambil (mis. transaksi form
	// beberapa tagihan dibatalkan): nomornya ikut dikembalikan.
	_ = f.sp(t, func(q pgx.Tx) error {
		p, err := RecordPayment(ctx, q, f.actor, cash(f.invA.ID, 1000))
		if err != nil || p.ReceiptNumber != fmt.Sprintf("KW/%d/000002", year) {
			t.Fatalf("inside rolled back transaction: %q, %v", p.ReceiptNumber, err)
		}
		return errors.New("rollback")
	})
	p2, err := RecordPayment(ctx, f.tx, f.actor, cash(f.invA.ID, 1000))
	if err != nil {
		t.Fatal(err)
	}
	want1, want2 := fmt.Sprintf("KW/%d/000001", year), fmt.Sprintf("KW/%d/000002", year)
	if p1.ReceiptNumber != want1 || p2.ReceiptNumber != want2 {
		t.Errorf("numbers = %s, %s; want %s, %s (no gap after rollback)", p1.ReceiptNumber, p2.ReceiptNumber, want1, want2)
	}
	// Sekolah lain dan tahun lain punya urutan sendiri.
	pB, err := RecordPayment(ctx, f.tx, school.Actor{SchoolID: f.schoolB, UserID: f.userB}, cash(f.invB.ID, 1000))
	if err != nil || pB.ReceiptNumber != want1 {
		t.Errorf("other school: %q, %v; want %s", pB.ReceiptNumber, err, want1)
	}
	if n, err := NextReceiptNumber(ctx, f.tx, f.schoolA, 2031); err != nil || n != "KW/2031/000001" {
		t.Errorf("other year: %q, %v", n, err)
	}
}

// TestReceiptNumber_Concurrent menjalankan transaksi sungguhan secara
// paralel (bukan savepoint) pada sekolah uji khusus yang dihapus lagi.
// Separuh transaksi di-rollback: nomor yang ter-commit harus unik dan
// berurutan tanpa celah.
func TestReceiptNumber_Concurrent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	const workers = 20
	pool, err := database.Connect(ctx, dsn, workers+2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close) // didaftarkan dulu agar berjalan setelah pembersihan data
	var schoolID string
	if err := pool.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('test-kw-par', 'test-kw-par') RETURNING id`).Scan(&schoolID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM receipt_counters WHERE school_id = $1`, `DELETE FROM schools WHERE id = $1`} {
			if _, err := pool.Exec(ctx, sql, schoolID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	var (
		mu        sync.Mutex
		committed []string
		wg        sync.WaitGroup
		start     = make(chan struct{})
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			n, err := NextReceiptNumber(ctx, tx, schoolID, 2026)
			if err != nil {
				t.Error(err)
				return
			}
			if i%2 == 1 {
				return // rollback
			}
			if err := tx.Commit(ctx); err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			committed = append(committed, n)
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()

	sort.Strings(committed)
	if len(committed) != workers/2 {
		t.Fatalf("committed %d numbers, want %d", len(committed), workers/2)
	}
	for i, n := range committed {
		if want := fmt.Sprintf("KW/2026/%06d", i+1); n != want {
			t.Fatalf("committed numbers = %v; want KW/2026/000001..%06d without duplicates or gaps", committed, workers/2)
		}
	}
	var last int64
	if err := pool.QueryRow(ctx, `SELECT last_number FROM receipt_counters WHERE school_id = $1 AND year = 2026`, schoolID).Scan(&last); err != nil || last != workers/2 {
		t.Errorf("counter = %d, %v; want %d", last, err, workers/2)
	}
}
