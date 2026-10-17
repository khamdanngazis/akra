package payments

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/school"
)

type fixture struct {
	tx               pgx.Tx
	svc              *Service
	actor            school.Actor
	schoolA, schoolB string
	userB            string
	invA, invA2      billing.Invoice // sekolah A, masing-masing Rp150.000
	free             billing.Invoice // sekolah A, Rp0
	invB             billing.Invoice // sekolah B
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
	f := &fixture{tx: tx, svc: NewService(tx)}
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.schoolA = one(`INSERT INTO schools (name, code) VALUES ('test-pay-a', 'test-pay-a') RETURNING id`)
	f.schoolB = one(`INSERT INTO schools (name, code) VALUES ('test-pay-b', 'test-pay-b') RETURNING id`)
	f.actor = school.Actor{SchoolID: f.schoolA, IP: netip.MustParseAddr("192.0.2.7"),
		UserID: one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'bendahara@uji.id', 'x', 'Bendahara') RETURNING id`, f.schoolA)}
	f.userB = one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'bendahara@uji.id', 'x', 'Bendahara') RETURNING id`, f.schoolB)

	invoice := func(actor school.Actor, nis string, amount int64) billing.Invoice {
		t.Helper()
		year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25')
			ON CONFLICT (school_id, name) DO UPDATE SET name = excluded.name RETURNING id`, actor.SchoolID)
		class := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 1) RETURNING id`, actor.SchoolID, year, "K"+nis)
		student := one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $2) RETURNING id`, actor.SchoolID, nis)
		enr := one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, '2026-07-13') RETURNING id`,
			actor.SchoolID, student, class, year)
		ft, err := billing.CreateFeeType(ctx, tx, actor.SchoolID, billing.FeeTypeInput{Name: "Biaya " + nis, DefaultAmount: amount})
		if err != nil {
			t.Fatal(err)
		}
		inv, err := billing.CreateInvoice(ctx, tx, actor, billing.InvoiceRequest{EnrollmentID: enr, Period: "2026-09",
			DueDate: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		return inv
	}
	f.invA = invoice(f.actor, "001", 150000)
	f.invA2 = invoice(f.actor, "002", 150000)
	f.free = invoice(f.actor, "003", 0)
	f.invB = invoice(school.Actor{SchoolID: f.schoolB, UserID: f.userB}, "001", 100000)
	return f
}

// sp menjalankan fn di savepoint yang selalu di-rollback.
func (f *fixture) sp(t *testing.T, fn func(pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	s, err := f.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Rollback(ctx) }()
	return fn(s)
}

func cash(invoiceID string, amount int64) Request {
	return Request{InvoiceID: invoiceID, Amount: amount, PaidOn: clock.Today(), Method: MethodCash}
}

func (f *fixture) status(t *testing.T, invoiceID string) billing.Status {
	t.Helper()
	inv, err := billing.GetInvoice(context.Background(), f.tx, f.schoolA, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	return inv.Status
}

func (f *fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.tx.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPayments_DatabaseConstraints(t *testing.T) {
	f := newFixture(t)
	rawReceipt := func(schoolID, invoiceID string, amount int64, method string, bank any, recordedBy string, receipt any) error {
		return f.sp(t, func(q pgx.Tx) error {
			_, err := q.Exec(context.Background(), `INSERT INTO payments (school_id, invoice_id, amount, paid_on, method, bank_account, recorded_by, receipt_number)
				VALUES ($1, $2, $3, '2026-09-01', $4, $5, $6, $7)`, schoolID, invoiceID, amount, method, bank, recordedBy, receipt)
			return err
		})
	}
	raw := func(schoolID, invoiceID string, amount int64, method string, bank any, recordedBy string) error {
		return rawReceipt(schoolID, invoiceID, amount, method, bank, recordedBy, "KW/2026/999999")
	}
	a, u := f.schoolA, f.actor.UserID
	for _, c := range []struct {
		name       string
		err        error
		constraint string
	}{
		{"amount 0", raw(a, f.invA.ID, 0, "tunai", nil, u), "payments_amount_check"},
		{"amount -1", raw(a, f.invA.ID, -1, "tunai", nil, u), "payments_amount_check"},
		{"unknown method", raw(a, f.invA.ID, 1, "kartu", "BCA 123", u), "payments_method_check"},
		{"transfer without bank account", raw(a, f.invA.ID, 1, "transfer", nil, u), "payments_bank_account_check"},
		{"transfer with blank bank account", raw(a, f.invA.ID, 1, "transfer", "  ", u), "payments_bank_account_check"},
		{"invoice of other school", raw(a, f.invB.ID, 1, "tunai", nil, u), "payments_invoice_fkey"},
		{"recorder of other school", raw(a, f.invA.ID, 1, "tunai", nil, f.userB), "payments_recorded_by_fkey"},
		{"bad receipt number", rawReceipt(a, f.invA.ID, 1, "tunai", nil, u, "KW-1"), "payments_receipt_number_check"},
	} {
		var pgErr *pgconn.PgError
		if !errors.As(c.err, &pgErr) || pgErr.ConstraintName != c.constraint {
			t.Errorf("%s: error = %v, want %s", c.name, c.err, c.constraint)
		}
	}
	if err := raw(a, f.invA.ID, 1, "transfer", "BCA 123", u); err != nil {
		t.Errorf("valid transfer: %v", err)
	}
	var pgErr *pgconn.PgError
	if err := rawReceipt(a, f.invA.ID, 1, "tunai", nil, u, nil); !errors.As(err, &pgErr) || pgErr.Code != "23502" {
		t.Errorf("payment without receipt number: error = %v, want not-null violation", err)
	}
}

func TestPayments_AppendOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := RecordPayment(ctx, f.tx, f.actor, cash(f.invA.ID, 50000))
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`UPDATE payments SET amount = 1 WHERE id = $1`,
		`UPDATE payments SET note = 'ubah' WHERE id = $1`,
		`DELETE FROM payments WHERE id = $1`,
		`TRUNCATE payments CASCADE`,
	} {
		err := f.sp(t, func(q pgx.Tx) error {
			args := []any{p.ID}
			if strings.HasPrefix(sql, "TRUNCATE") {
				args = nil
			}
			_, err := q.Exec(ctx, sql, args...)
			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" || !strings.Contains(pgErr.Message, "append-only") {
			t.Errorf("%s: error = %v, want append-only rejection", sql, err)
		}
	}
	list, err := ListByInvoice(ctx, f.tx, f.schoolA, f.invA.ID)
	if err != nil || len(list) != 1 || list[0].Amount != 50000 || list[0] != p {
		t.Errorf("payment changed: %+v, %v (was %+v)", list, err, p)
	}
}

func TestRecordPayment_Validation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.tx.Exec(ctx, `UPDATE invoices SET voided_at = now() WHERE id = $1`, f.invA2.ID); err != nil {
		t.Fatal(err)
	}
	transfer := func(bank string) Request {
		r := cash(f.invA.ID, 1000)
		r.Method, r.BankAccount = MethodTransfer, bank
		return r
	}
	with := func(fn func(*Request)) Request {
		r := cash(f.invA.ID, 1000)
		fn(&r)
		return r
	}
	for _, c := range []struct {
		name string
		req  Request
		want error
	}{
		{"amount 0", cash(f.invA.ID, 0), ErrInvalidAmount},
		{"negative amount", cash(f.invA.ID, -5000), ErrInvalidAmount},
		{"amount too large", cash(f.invA.ID, billing.MaxAmount+1), billing.ErrAmountTooLarge},
		{"over remaining", cash(f.invA.ID, 150001), billing.ErrOverpayment},
		{"free invoice", cash(f.free.ID, 1), billing.ErrOverpayment},
		{"unknown method", with(func(r *Request) { r.Method = "kartu" }), ErrInvalidMethod},
		{"transfer without bank account", transfer(""), ErrBankAccountRequired},
		{"transfer with blank bank account", transfer("   "), ErrBankAccountRequired},
		{"no paid on", with(func(r *Request) { r.PaidOn = time.Time{} }), ErrPaidOnRequired},
		{"paid on tomorrow", with(func(r *Request) { r.PaidOn = clock.Today().AddDate(0, 0, 1) }), ErrPaidOnFuture},
		{"note too long", with(func(r *Request) { r.Note = strings.Repeat("é", MaxNoteLen+1) }), ErrTextTooLong},
		{"invoice of other school", cash(f.invB.ID, 1000), billing.ErrInvoiceNotFound},
		{"invalid invoice id", cash("bukan-uuid", 1000), billing.ErrInvoiceNotFound},
		{"voided invoice", cash(f.invA2.ID, 1000), ErrInvoiceVoided},
	} {
		err := f.sp(t, func(q pgx.Tx) error {
			_, err := RecordPayment(ctx, q, f.actor, c.req)
			return err
		})
		if !errors.Is(err, c.want) {
			t.Errorf("%s: error = %v, want %v", c.name, err, c.want)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM payments WHERE school_id = $1`, f.schoolA); n != 0 {
		t.Errorf("payments saved = %d, want 0", n)
	}
	if _, err := RecordPayment(ctx, f.tx, f.actor, with(func(r *Request) { r.Note = strings.Repeat("é", MaxNoteLen) })); err != nil {
		t.Errorf("note at max length: %v", err)
	}
}

func TestRecordPayment_StatusAndRemaining(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	r := cash(f.invA.ID, 50000)
	r.BankAccount, r.Reference, r.Note = "BCA 123", "REF-1", "  dibayar ibu  "
	p1, err := f.svc.Record(ctx, f.actor, r)
	if err != nil {
		t.Fatal(err)
	}
	if p1.BankAccount != "" || p1.Reference != "" || p1.Note != "dibayar ibu" || p1.RecordedBy != f.actor.UserID || p1.ReceiptNumber == "" {
		t.Errorf("cash payment = %+v", p1)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusPartial {
		t.Errorf("after partial payment: status = %s, want sebagian", s)
	}

	if _, err := f.svc.Record(ctx, f.actor, cash(f.invA.ID, 100001)); !errors.Is(err, billing.ErrOverpayment) {
		t.Errorf("over remaining: error = %v", err)
	}
	tr := cash(f.invA.ID, 100000)
	tr.Method, tr.BankAccount, tr.Reference = MethodTransfer, " BRI 456 ", "TRX-9"
	tr.PaidOn = time.Date(2026, 9, 5, 15, 0, 0, 0, time.UTC) // 22.00 WIB, tetap 5 September
	p2, err := f.svc.Record(ctx, f.actor, tr)
	if err != nil {
		t.Fatal(err)
	}
	if p2.BankAccount != "BRI 456" || p2.Reference != "TRX-9" || !p2.PaidOn.Equal(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("transfer payment = %+v", p2)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusPaid {
		t.Errorf("after full payment: status = %s, want lunas", s)
	}
	if _, err := f.svc.Record(ctx, f.actor, cash(f.invA.ID, 1)); !errors.Is(err, billing.ErrOverpayment) {
		t.Errorf("paid invoice: error = %v", err)
	}

	list, err := f.svc.ListByInvoice(ctx, f.schoolA, f.invA.ID)
	if err != nil || len(list) != 2 || list[0].ID != p2.ID || list[1].ID != p1.ID {
		t.Errorf("ListByInvoice = %+v, %v", list, err)
	}
	if list, _ := f.svc.ListByInvoice(ctx, f.schoolB, f.invA.ID); len(list) != 0 {
		t.Errorf("ListByInvoice from other school = %d rows", len(list))
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE action = 'payment.create' AND school_id = $1
		AND after->>'invoice_status' IS NOT NULL AND NOT after ? 'note'`, f.schoolA); n != 2 {
		t.Errorf("payment.create audits = %d, want 2", n)
	}
}

// Pembayaran beberapa tagihan dalam satu transaksi: jika satu gagal, yang
// lain ikut batal (dasar form T35).
func TestRecordPayment_FailureRollsBackWholeTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	err := f.sp(t, func(q pgx.Tx) error {
		if _, err := RecordPayment(ctx, q, f.actor, cash(f.invA.ID, 150000)); err != nil {
			t.Fatal(err)
		}
		_, err := RecordPayment(ctx, q, f.actor, cash(f.invA2.ID, 150001))
		return err
	})
	if !errors.Is(err, billing.ErrOverpayment) {
		t.Fatalf("error = %v", err)
	}
	if n := f.count(t, `SELECT count(*) FROM payments WHERE school_id = $1`, f.schoolA); n != 0 {
		t.Errorf("payments saved = %d, want 0", n)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusUnpaid {
		t.Errorf("status = %s, want belum_bayar", s)
	}
}
