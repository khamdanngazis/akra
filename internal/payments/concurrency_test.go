package payments

// Uji transaksi bersamaan (T60). Berbeda dengan uji lain, data di sini
// di-commit karena setiap goroutine butuh transaksi sendiri; sekolah uji
// dihapus di akhir (lihat cleanupSchool).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

// concurrencyRuns adalah jumlah pengulangan setiap skenario dalam satu
// uji, agar kondisi balapan sempat muncul.
const concurrencyRuns = 50

type concFixture struct {
	pool       *pgxpool.Pool
	schoolID   string
	treasurer  school.Actor
	enrollment string
	feeType    string
	invoices   int // penghitung periode tagihan
}

func newConcFixture(t *testing.T, conns int32) *concFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, conns)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &concFixture{pool: pool}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	// Kode acak: jika pembersihan gagal, run berikutnya tidak bentrok.
	f.schoolID = one(`INSERT INTO schools (name, code) VALUES ('Uji konkuren', $1) RETURNING id`, "test-conc-"+hex.EncodeToString(suffix))
	t.Cleanup(func() { cleanupSchool(t, pool, f.schoolID) })
	user := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'bendahara@conc.uji', 'x', 'Bendahara') RETURNING id`, f.schoolID)
	f.treasurer = school.Actor{SchoolID: f.schoolID, UserID: user}
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true) RETURNING id`, f.schoolID)
	class := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1A', 1) RETURNING id`, f.schoolID, year)
	student := one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, '001', 'Ani') RETURNING id`, f.schoolID)
	f.enrollment = one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-07-13') RETURNING id`,
		f.schoolID, student, class, year)
	ft, err := billing.CreateFeeType(ctx, pool, f.schoolID, billing.FeeTypeInput{Name: "SPP", DefaultAmount: 300000, DefaultDueDay: 10})
	if err != nil {
		t.Fatal(err)
	}
	f.feeType = ft.ID
	return f
}

// cleanupSchool menghapus semua data sekolah uji. payments, payment_voids,
// dan audit_logs append-only (dijaga trigger), jadi trigger dan foreign key
// dimatikan dengan session_replication_role (butuh superuser, seperti di CI
// dan Postgres lokal). Jika tidak bisa, data dibiarkan dengan kode acak.
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

// newInvoice membuat tagihan baru (periode berbeda setiap kali) sebesar
// 300.000.
func (f *concFixture) newInvoice(t *testing.T) string {
	t.Helper()
	f.invoices++
	period := fmt.Sprintf("%04d-%02d", 2000+f.invoices/12, f.invoices%12+1)
	var id string
	err := pgx.BeginFunc(context.Background(), f.pool, func(tx pgx.Tx) error {
		inv, err := billing.CreateInvoice(context.Background(), tx, f.treasurer,
			billing.InvoiceRequest{EnrollmentID: f.enrollment, Period: period, Items: []billing.ItemRequest{{FeeTypeID: f.feeType}}})
		id = inv.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *concFixture) record(ctx context.Context, invoiceID string, amount int64) (Payment, error) {
	var p Payment
	err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		var err error
		p, err = RecordPayment(ctx, tx, f.treasurer, Request{InvoiceID: invoiceID, Amount: amount, PaidOn: clock.Today(), Method: MethodCash})
		return err
	})
	return p, err
}

// invoiceState mengembalikan status tersimpan dan jumlah pembayaran aktif
// (tidak dibatalkan) sebuah tagihan.
func (f *concFixture) invoiceState(t *testing.T, invoiceID string) (billing.Status, int64) {
	t.Helper()
	var (
		status billing.Status
		paid   int64
	)
	err := f.pool.QueryRow(context.Background(), `
		SELECT i.status, coalesce((SELECT sum(p.amount) FROM payments p WHERE p.invoice_id = i.id
			AND NOT EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved')), 0)
		FROM invoices i WHERE i.school_id = $1 AND i.id = $2`, f.schoolID, invoiceID).Scan(&status, &paid)
	if err != nil {
		t.Fatal(err)
	}
	return status, paid
}

// runTogether menjalankan fns bersamaan, dilepas pada saat yang sama.
func runTogether(fns ...func()) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn()
		}()
	}
	close(start)
	wg.Wait()
}

// Dua pembayaran bersamaan yang totalnya melebihi sisa tagihan: tepat satu
// berhasil, yang lain ditolak sebagai kelebihan bayar.
func TestConcurrency_TwoPaymentsOverRemaining(t *testing.T) {
	f := newConcFixture(t, 6)
	ctx := context.Background()
	for run := range concurrencyRuns {
		inv := f.newInvoice(t)
		var errs [2]error
		runTogether(
			func() { _, errs[0] = f.record(ctx, inv, 200000) },
			func() { _, errs[1] = f.record(ctx, inv, 200000) },
		)
		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, billing.ErrOverpayment):
				t.Fatalf("run %d: unexpected error: %v", run, err)
			}
		}
		if ok != 1 {
			t.Fatalf("run %d: %d payments succeeded, want exactly 1 (errors %v)", run, ok, errs)
		}
		status, paid := f.invoiceState(t, inv)
		if paid != 200000 || status != billing.StatusPartial {
			t.Fatalf("run %d: paid %d, status %s", run, paid, status)
		}
	}
}

// Pembayaran pelunasan dan pembatalan pembayaran sebelumnya bersamaan: apa
// pun urutannya, status akhir tagihan sesuai jumlah pembayaran aktif dan
// tidak pernah melebihi total.
func TestConcurrency_PaymentAndVoid(t *testing.T) {
	f := newConcFixture(t, 6)
	ctx := context.Background()
	// Ambang tinggi: pembatalan langsung berlaku tanpa persetujuan.
	if err := settings.Set(ctx, f.pool, f.schoolID, settings.VoidApprovalThreshold, "1000000"); err != nil {
		t.Fatal(err)
	}
	voids := NewVoidService(f.pool)
	outcomes := map[string]int{}
	for run := range concurrencyRuns {
		inv := f.newInvoice(t)
		first, err := f.record(ctx, inv, 100000)
		if err != nil {
			t.Fatal(err)
		}
		// Sisa 200.000. Pembayaran 250.000 hanya muat jika pembatalan
		// 100.000 berlaku lebih dulu.
		var payErr, voidErr error
		runTogether(
			func() { _, payErr = f.record(ctx, inv, 250000) },
			func() { _, voidErr = voids.Request(ctx, f.treasurer, first.ID, "salah catat") },
		)
		if voidErr != nil {
			t.Fatalf("run %d: void: %v", run, voidErr)
		}
		status, paid := f.invoiceState(t, inv)
		switch {
		case payErr == nil:
			// Pembatalan dulu, lalu pembayaran 250.000 masuk.
			outcomes["void first"]++
			if paid != 250000 || status != billing.StatusPartial {
				t.Fatalf("run %d (void first): paid %d, status %s", run, paid, status)
			}
		case errors.Is(payErr, billing.ErrOverpayment):
			// Pembayaran dulu dan ditolak; lalu pembatalan berlaku.
			outcomes["payment first"]++
			if paid != 0 || status != billing.StatusUnpaid {
				t.Fatalf("run %d (payment first): paid %d, status %s", run, paid, status)
			}
		default:
			t.Fatalf("run %d: payment: %v", run, payErr)
		}
		if want, err := billing.ComputeStatus(300000, paid); err != nil || status != want {
			t.Fatalf("run %d: stored status %s, computed %s (%v)", run, status, want, err)
		}
	}
	t.Logf("outcomes: %v", outcomes)
}

// 20 pembayaran paralel (tagihan berbeda) mendapat 20 nomor kuitansi
// berbeda yang berurutan tanpa celah.
func TestConcurrency_ParallelReceiptNumbers(t *testing.T) {
	const workers = 20
	f := newConcFixture(t, workers+2)
	ctx := context.Background()
	invoices := make([]string, workers)
	for i := range invoices {
		invoices[i] = f.newInvoice(t)
	}
	var (
		mu       sync.Mutex
		receipts []string
		fns      []func()
	)
	for _, inv := range invoices {
		fns = append(fns, func() {
			p, err := f.record(ctx, inv, 100000)
			if err != nil {
				t.Errorf("payment: %v", err)
				return
			}
			mu.Lock()
			receipts = append(receipts, p.ReceiptNumber)
			mu.Unlock()
		})
	}
	runTogether(fns...)
	if len(receipts) != workers {
		t.Fatalf("%d receipts", len(receipts))
	}
	slices.Sort(receipts)
	year := clock.Today().Year()
	for i, r := range receipts {
		if want := fmt.Sprintf("KW/%04d/%06d", year, i+1); r != want {
			t.Fatalf("receipts = %v; position %d is %s, want %s (duplicate or gap)", receipts, i, r, want)
		}
	}
}
