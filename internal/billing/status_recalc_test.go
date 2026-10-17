package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestActivePaid(t *testing.T) {
	p := func(id string, amount int64) PaymentLine { return PaymentLine{ID: id, Amount: amount} }
	for _, c := range []struct {
		name     string
		payments []PaymentLine
		voids    map[string]string
		want     int64
	}{
		{"no payments", nil, nil, 0},
		{"two payments, no voids", []PaymentLine{p("a", 50000), p("b", 50000)}, nil, 100000},
		{"one approved void", []PaymentLine{p("a", 50000), p("b", 50000)}, map[string]string{"a": "approved"}, 50000},
		{"pending void does not count", []PaymentLine{p("a", 100000)}, map[string]string{"a": "pending"}, 100000},
		{"rejected void does not count", []PaymentLine{p("a", 100000)}, map[string]string{"a": "rejected"}, 100000},
		{"two voids on different payments", []PaymentLine{p("a", 30000), p("b", 30000), p("c", 40000)},
			map[string]string{"a": "approved", "c": "approved"}, 30000},
		{"void for unknown payment ignored", []PaymentLine{p("a", 10000)}, map[string]string{"x": "approved"}, 10000},
	} {
		if got := ActivePaid(c.payments, c.voids); got != c.want {
			t.Errorf("%s: ActivePaid = %d, want %d", c.name, got, c.want)
		}
	}
}

// recalcFixture membuat tagihan Rp100.000 dan menyediakan helper untuk
// pembayaran dan pembatalan langsung lewat SQL (paket billing tidak boleh
// mengimpor paket payments).
type recalcFixture struct {
	*invoiceFixture
	inv    Invoice
	kepala string
	n      int // nomor kuitansi terakhir
}

func newRecalcFixture(t *testing.T) *recalcFixture {
	t.Helper()
	f := &recalcFixture{invoiceFixture: newInvoiceFixture(t)}
	f.inv = f.keep(t, InvoiceRequest{EnrollmentID: f.enrA, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.spp.ID, Amount: amount(100000)}}})
	if err := f.tx.QueryRow(context.Background(), `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'kepala@uji.id', 'x', 'Kepala') RETURNING id`,
		f.schoolA).Scan(&f.kepala); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *recalcFixture) pay(t *testing.T, amount int64) string {
	t.Helper()
	var id string
	f.n++
	if err := f.tx.QueryRow(context.Background(), `INSERT INTO payments (school_id, invoice_id, amount, paid_on, method, recorded_by, receipt_number)
		VALUES ($1, $2, $3, current_date, 'tunai', $4, $5) RETURNING id`, f.schoolA, f.inv.ID, amount, f.actor.UserID, fmt.Sprintf("KW/2026/%06d", f.n)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// void mencatat pembatalan dengan status pending, approved, atau rejected.
func (f *recalcFixture) void(t *testing.T, paymentID, status string) {
	t.Helper()
	sql := `INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status, decided_by, decided_at)
		VALUES ($1, $2, 'uji', $3, true, $4, $5, now())`
	args := []any{f.schoolA, paymentID, f.actor.UserID, status, f.kepala}
	if status == "pending" {
		sql = `INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status)
			VALUES ($1, $2, 'uji', $3, true, 'pending')`
		args = args[:3]
	}
	if _, err := f.tx.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *recalcFixture) recalc(t *testing.T) Status {
	t.Helper()
	s, err := RecalculateInvoice(context.Background(), f.tx, f.schoolA, f.inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored Status
	_ = f.tx.QueryRow(context.Background(), `SELECT status FROM invoices WHERE id = $1`, f.inv.ID).Scan(&stored)
	if stored != s {
		t.Fatalf("returned status %s, stored %s", s, stored)
	}
	return s
}

func TestRecalculate_TwoPaymentsOneVoided(t *testing.T) {
	f := newRecalcFixture(t)
	a := f.pay(t, 50000)
	f.pay(t, 50000)
	if s := f.recalc(t); s != StatusPaid {
		t.Fatalf("two payments of 50.000: %s, want lunas", s)
	}
	f.void(t, a, "approved")
	if s := f.recalc(t); s != StatusPartial {
		t.Errorf("after voiding one: %s, want sebagian", s)
	}
}

func TestRecalculate_FullPaymentVoided(t *testing.T) {
	f := newRecalcFixture(t)
	a := f.pay(t, 100000)
	if s := f.recalc(t); s != StatusPaid {
		t.Fatalf("full payment: %s", s)
	}
	f.void(t, a, "approved")
	if s := f.recalc(t); s != StatusUnpaid {
		t.Errorf("after void: %s, want belum_bayar", s)
	}
}

func TestRecalculate_PendingAndRejectedVoidsKeepStatus(t *testing.T) {
	f := newRecalcFixture(t)
	a := f.pay(t, 100000)
	f.void(t, a, "rejected")
	f.void(t, a, "pending")
	if s := f.recalc(t); s != StatusPaid {
		t.Errorf("pending void: %s, want lunas", s)
	}
}

func TestRecalculate_TwoVoidsOnDifferentPayments(t *testing.T) {
	f := newRecalcFixture(t)
	a, b := f.pay(t, 30000), f.pay(t, 30000)
	c := f.pay(t, 40000)
	f.void(t, a, "approved")
	f.void(t, c, "approved")
	if s := f.recalc(t); s != StatusPartial {
		t.Errorf("30.000 active of 100.000: %s, want sebagian", s)
	}
	f.void(t, b, "approved")
	if s := f.recalc(t); s != StatusUnpaid {
		t.Errorf("all voided: %s, want belum_bayar", s)
	}
}

func TestRecalculate_RollsBackWithTrigger(t *testing.T) {
	f := newRecalcFixture(t)
	a := f.pay(t, 100000)
	f.recalc(t)
	// Aksi pemicu gagal setelah hitung ulang: status ikut kembali.
	err := f.sp(t, func(q pgx.Tx) error {
		if _, err := q.Exec(context.Background(), `INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status, decided_at)
			VALUES ($1, $2, 'uji', $3, false, 'approved', now())`, f.schoolA, a, f.actor.UserID); err != nil {
			return err
		}
		if s, err := RecalculateInvoice(context.Background(), q, f.schoolA, f.inv.ID); err != nil || s != StatusUnpaid {
			t.Fatalf("inside transaction: %s, %v", s, err)
		}
		return errors.New("trigger action failed")
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if s := f.recalc(t); s != StatusPaid {
		t.Errorf("after rollback: %s, want lunas", s)
	}
}

func TestRecalculate_OtherSchoolAndUnknown(t *testing.T) {
	f := newRecalcFixture(t)
	for _, c := range []struct{ school, id string }{
		{f.schoolB, f.inv.ID}, {f.schoolA, "00000000-0000-0000-0000-000000000000"}, {f.schoolA, "x"},
	} {
		if _, err := RecalculateInvoice(context.Background(), f.tx, c.school, c.id); !errors.Is(err, ErrInvoiceNotFound) {
			t.Errorf("%s/%s: error = %v", c.school, c.id, err)
		}
	}
}
