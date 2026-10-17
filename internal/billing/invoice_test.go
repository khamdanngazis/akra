package billing

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

type invoiceFixture struct {
	*fixture
	svc                    *InvoiceService
	year, otherYear        string
	enrA, enrA2, enrB      string // enrA, enrA2: sekolah A; enrB: sekolah B
	spp, kegiatan, seragam FeeType
	gratis                 FeeType
	feeB                   FeeType
}

func newInvoiceFixture(t *testing.T) *invoiceFixture {
	t.Helper()
	f := &invoiceFixture{fixture: newFixture(t)}
	f.svc = NewInvoiceService(f.tx)
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	enroll := func(schoolID, nis string) (string, string) {
		year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, $2, '2026-07-13', '2027-06-25')
			ON CONFLICT (school_id, name) DO UPDATE SET name = excluded.name RETURNING id`, schoolID, "2026/2027")
		class := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`, schoolID, year, "K"+nis)
		student := one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $2) RETURNING id`, schoolID, nis)
		return year, one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-07-13') RETURNING id`,
			schoolID, student, class, year)
	}
	f.year, f.enrA = enroll(f.schoolA, "001")
	_, f.enrA2 = enroll(f.schoolA, "002")
	_, f.enrB = enroll(f.schoolB, "001")
	f.otherYear = one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2025/2026', '2025-07-14', '2026-06-26') RETURNING id`, f.schoolA)

	mk := func(schoolID, name string, amount int64, day int) FeeType {
		ft, err := CreateFeeType(ctx, f.tx, schoolID, FeeTypeInput{Name: name, DefaultAmount: amount, DefaultDueDay: day})
		if err != nil {
			t.Fatal(err)
		}
		return ft
	}
	f.spp = mk(f.schoolA, "SPP", 150000, 10)
	f.kegiatan = mk(f.schoolA, "Kegiatan", 75000, 0)
	f.seragam = mk(f.schoolA, "Seragam", 450000, 0)
	f.gratis = mk(f.schoolA, "Ekskul Gratis", 0, 0)
	f.feeB = mk(f.schoolB, "SPP", 100000, 5)
	if _, err := SetFeeTypeActive(ctx, f.tx, f.schoolA, f.seragam.ID, false); err != nil {
		t.Fatal(err)
	}
	return f
}

func amount(n int64) *int64 { return &n }

// create membuat tagihan di savepoint dan memaksa pemeriksaan constraint
// deferred berjalan sebelum savepoint selesai.
func (f *invoiceFixture) create(t *testing.T, req InvoiceRequest) (Invoice, error) {
	t.Helper()
	var inv Invoice
	err := f.sp(t, func(q pgx.Tx) error {
		var err error
		if inv, err = CreateInvoice(context.Background(), q, f.actor, req); err != nil {
			return err
		}
		return checkDeferred(q)
	})
	return inv, err
}

// checkDeferred memaksa constraint deferred (total tagihan) diperiksa sekarang,
// lalu mengembalikannya ke mode deferred untuk sisa transaksi uji.
func checkDeferred(q pgx.Tx) error {
	ctx := context.Background()
	_, err := q.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	if _, resetErr := q.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); err == nil {
		err = resetErr
	}
	return err
}

// keep membuat tagihan permanen di transaksi uji (bukan savepoint).
func (f *invoiceFixture) keep(t *testing.T, req InvoiceRequest) Invoice {
	t.Helper()
	inv, err := CreateInvoice(context.Background(), f.tx, f.actor, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkDeferred(f.tx); err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestInvoice_TotalIsSumOfItems(t *testing.T) {
	f := newInvoiceFixture(t)
	inv := f.keep(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{
		{FeeTypeID: f.spp.ID},
		{FeeTypeID: f.kegiatan.ID, Amount: amount(80000), Description: "Pramuka"},
	}})
	if inv.TotalAmount != 230000 || len(inv.Items) != 2 || inv.Status != StatusUnpaid {
		t.Errorf("invoice = %+v", inv)
	}
	if !inv.DueDate.Equal(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("due date = %v, want 2026-09-10 (SPP default day 10)", inv.DueDate)
	}
	var stored, sum int64
	_ = f.tx.QueryRow(context.Background(), `SELECT total_amount, (SELECT sum(amount) FROM invoice_items WHERE invoice_id = $1) FROM invoices WHERE id = $1`, inv.ID).Scan(&stored, &sum)
	if stored != 230000 || sum != 230000 {
		t.Errorf("stored total = %d, items sum = %d", stored, sum)
	}
	got, err := f.svc.Get(context.Background(), f.schoolA, inv.ID)
	if err != nil || got.Items[1].Description != "Pramuka" || got.Items[1].FeeTypeName != "Kegiatan" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if _, err := f.svc.Get(context.Background(), f.schoolB, inv.ID); !errors.Is(err, ErrInvoiceNotFound) {
		t.Errorf("Get from other school: %v", err)
	}

	var n int
	_ = f.tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'invoice.create' AND entity_id = $1`, inv.ID).Scan(&n)
	if n != 1 {
		t.Errorf("invoice.create audits = %d", n)
	}
}

func TestInvoice_DuplicateFeeTypeInSamePeriodRejected(t *testing.T) {
	f := newInvoiceFixture(t)
	f.keep(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}})

	// SPP September untuk enrollment yang sama, di tagihan lain.
	if _, err := f.create(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}}); !errors.Is(err, ErrDuplicateItem) {
		t.Errorf("second SPP September: error = %v, want ErrDuplicateItem", err)
	}
	// Jenis tagihan yang sama dua kali dalam satu tagihan.
	if _, err := f.create(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-10", Items: []ItemRequest{{FeeTypeID: f.spp.ID}, {FeeTypeID: f.spp.ID}}}); !errors.Is(err, ErrDuplicateItem) {
		t.Errorf("same fee type twice in one invoice: error = %v", err)
	}
	// Bulan lain dan siswa lain boleh.
	if _, err := f.create(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-10", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}}); err != nil {
		t.Errorf("SPP October: %v", err)
	}
	if _, err := f.create(t, InvoiceRequest{EnrollmentID: f.enrA2, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}}); err != nil {
		t.Errorf("SPP September for another student: %v", err)
	}
}

func TestInvoice_Validation(t *testing.T) {
	f := newInvoiceFixture(t)
	cases := []struct {
		name string
		req  InvoiceRequest
		want error
	}{
		{"negative amount", InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID, Amount: amount(-1)}}}, ErrNegativeAmount},
		{"too large", InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID, Amount: amount(MaxAmount + 1)}}}, ErrAmountTooLarge},
		{"inactive fee type", InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.seragam.ID}}}, ErrFeeTypeInactive},
		{"fee type of another school", InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.feeB.ID}}}, ErrFeeTypeNotFound},
		{"enrollment of another school", InvoiceRequest{EnrollmentID: f.enrB, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}}, ErrEnrollmentNotFound},
		{"bad period", InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-13", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}}, ErrInvalidPeriod},
		{"no items", InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09"}, ErrNoItems},
		{"no due date", InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.kegiatan.ID}}}, ErrDueDateRequired},
	}
	for _, tc := range cases {
		if _, err := f.create(t, tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.want)
		}
	}
	// Jatuh tempo eksplisit untuk jenis tagihan tanpa default.
	inv, err := f.create(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", DueDate: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Items: []ItemRequest{{FeeTypeID: f.kegiatan.ID}}})
	if err != nil || inv.DueDate.Day() != 30 {
		t.Errorf("explicit due date: %v, %v", inv.DueDate, err)
	}
	// Tagihan gratis langsung lunas.
	free, err := f.create(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", DueDate: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Items: []ItemRequest{{FeeTypeID: f.gratis.ID}}})
	if err != nil || free.TotalAmount != 0 || free.Status != StatusPaid {
		t.Errorf("free invoice = %+v, %v", free, err)
	}
}

func TestInvoice_DatabaseGuards(t *testing.T) {
	f := newInvoiceFixture(t)
	ctx := context.Background()
	inv := f.keep(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}})

	exec := func(sql string, args ...any) error {
		return f.sp(t, func(q pgx.Tx) error {
			if _, err := q.Exec(ctx, sql, args...); err != nil {
				return err
			}
			return checkDeferred(q)
		})
	}
	expect := func(name, constraint string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != constraint {
			t.Errorf("%s: error = %v, want violation of %s", name, err, constraint)
		}
	}

	expect("negative item amount", "invoice_items_amount_check",
		exec(`INSERT INTO invoice_items (school_id, invoice_id, enrollment_id, period, fee_type_id, amount) VALUES ($1, $2, $3, '2026-09', $4, -1)`,
			f.schoolA, inv.ID, f.enrA, f.kegiatan.ID))
	expect("status terlambat", "invoices_status_check",
		exec(`UPDATE invoices SET status = 'terlambat' WHERE id = $1`, inv.ID))
	expect("total not matching items", "invoice_total_matches_items",
		exec(`UPDATE invoices SET total_amount = total_amount + 1 WHERE id = $1`, inv.ID))
	expect("item added without updating total", "invoice_total_matches_items",
		exec(`INSERT INTO invoice_items (school_id, invoice_id, enrollment_id, period, fee_type_id, amount) VALUES ($1, $2, $3, '2026-09', $4, 1000)`,
			f.schoolA, inv.ID, f.enrA, f.kegiatan.ID))
	expect("item with another period than its invoice", "invoice_items_invoice_fkey",
		exec(`INSERT INTO invoice_items (school_id, invoice_id, enrollment_id, period, fee_type_id, amount) VALUES ($1, $2, $3, '2026-10', $4, 0)`,
			f.schoolA, inv.ID, f.enrA, f.gratis.ID))
	expect("invoice in another academic year than its enrollment", "invoices_enrollment_fkey",
		exec(`INSERT INTO invoices (school_id, academic_year_id, enrollment_id, period, due_date) VALUES ($1, $2, $3, '2026-09', '2026-09-10')`,
			f.schoolA, f.otherYear, f.enrA))
	expect("item with fee type of another school", "invoice_items_fee_type_fkey",
		exec(`INSERT INTO invoice_items (school_id, invoice_id, enrollment_id, period, fee_type_id, amount) VALUES ($1, $2, $3, '2026-09', $4, 0)`,
			f.schoolA, inv.ID, f.enrA, f.feeB.ID))

	// Item dan total yang diperbarui bersamaan diterima.
	if err := exec(`WITH i AS (INSERT INTO invoice_items (school_id, invoice_id, enrollment_id, period, fee_type_id, amount) VALUES ($1, $2, $3, '2026-09', $4, 1000))
		UPDATE invoices SET total_amount = total_amount + 1000 WHERE id = $2`, f.schoolA, inv.ID, f.enrA, f.kegiatan.ID); err != nil {
		t.Errorf("consistent item + total update rejected: %v", err)
	}
	var terlambat int
	_ = f.tx.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE status NOT IN ('belum_bayar', 'sebagian', 'lunas')`).Scan(&terlambat)
	if terlambat != 0 {
		t.Error("an invoice has a status other than the three stored statuses")
	}
}

func TestInvoice_ChangingDefaultAmountKeepsExistingInvoices(t *testing.T) {
	f := newInvoiceFixture(t)
	ctx := context.Background()
	inv := f.keep(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}})

	if _, err := NewFeeTypeService(f.tx).Update(ctx, f.actor, f.spp.ID, FeeTypeForm{Name: "SPP", DefaultAmount: "200.000", DefaultDueDay: "10"}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Get(ctx, f.schoolA, inv.ID)
	if got.TotalAmount != 150000 || got.Items[0].Amount != 150000 {
		t.Errorf("existing invoice changed after default update: total %d, item %d", got.TotalAmount, got.Items[0].Amount)
	}
	// Tagihan baru memakai nominal baru.
	next := f.keep(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-10", Items: []ItemRequest{{FeeTypeID: f.spp.ID}}})
	if next.TotalAmount != 200000 {
		t.Errorf("new invoice total = %d, want 200000", next.TotalAmount)
	}
}

func TestDueDateFor(t *testing.T) {
	d, err := DueDateFor("2027-02", 28)
	if err != nil || d.Format("2006-01-02") != "2027-02-28" {
		t.Errorf("DueDateFor(2027-02, 28) = %v, %v", d, err)
	}
	if _, err := DueDateFor("2027-02", 29); !errors.Is(err, ErrInvalidDueDay) {
		t.Errorf("day 29: %v", err)
	}
	if _, err := DueDateFor("27-02", 1); !errors.Is(err, ErrInvalidPeriod) {
		t.Errorf("bad period: %v", err)
	}
}

// TestInvoice_MismatchedTotalFailsAtCommit memastikan pemeriksaan total
// berjalan saat COMMIT dalam mode default (deferred), seperti di production.
func TestInvoice_MismatchedTotalFailsAtCommit(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var schoolID, yearID, enrollmentID string
		if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('C', 'test-fee-commit') RETURNING id`).Scan(&schoolID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, 'Y', '2026-07-01', '2027-06-30') RETURNING id`, schoolID).Scan(&yearID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			WITH c AS (INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1A', 1) RETURNING id),
			     s AS (INSERT INTO students (school_id, nis, full_name) VALUES ($1, '1', 'S') RETURNING id)
			INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
			SELECT $1, s.id, c.id, $2, '2026-07-01' FROM s, c RETURNING id`, schoolID, yearID).Scan(&enrollmentID); err != nil {
			return err
		}
		// Total 100.000 tanpa item: tiap perintah lolos, tetapi COMMIT harus gagal.
		_, err := tx.Exec(ctx, `INSERT INTO invoices (school_id, academic_year_id, enrollment_id, period, due_date, total_amount)
			VALUES ($1, $2, $3, '2026-09', '2026-09-10', 100000)`, schoolID, yearID, enrollmentID)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "invoice_total_matches_items" {
		t.Fatalf("commit error = %v, want invoice_total_matches_items", err)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM schools WHERE code = 'test-fee-commit'`).Scan(&n)
	if n != 0 {
		t.Error("failed commit left data behind")
	}
}
