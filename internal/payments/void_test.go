package payments

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

type voidFixture struct {
	*fixture
	voids  *VoidService
	kepala school.Actor
}

func newVoidFixture(t *testing.T) *voidFixture {
	t.Helper()
	f := &voidFixture{fixture: newFixture(t)}
	f.voids = NewVoidService(f.tx)
	f.kepala = school.Actor{SchoolID: f.schoolA}
	if err := f.tx.QueryRow(context.Background(), `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'kepala@uji.id', 'x', 'Kepala') RETURNING id`,
		f.schoolA).Scan(&f.kepala.UserID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *voidFixture) pay(t *testing.T, invoiceID string, amount int64, paidOn time.Time) Payment {
	t.Helper()
	r := cash(invoiceID, amount)
	r.PaidOn = paidOn
	p, err := RecordPayment(context.Background(), f.tx, f.actor, r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *voidFixture) threshold(t *testing.T, v string) {
	t.Helper()
	if err := settings.Set(context.Background(), f.tx, f.schoolA, settings.VoidApprovalThreshold, v); err != nil {
		t.Fatal(err)
	}
}

// try menjalankan fn di savepoint yang di-rollback.
func (f *voidFixture) try(t *testing.T, fn func(*VoidService) error) error {
	t.Helper()
	return f.sp(t, func(q pgx.Tx) error { return fn(NewVoidService(q)) })
}

func TestVoid_Validation(t *testing.T) {
	f := newVoidFixture(t)
	ctx := context.Background()
	today := clock.Today()
	p := f.pay(t, f.invA.ID, 50000, today)
	lastMonth := f.pay(t, f.invA2.ID, 50000, time.Date(today.Year(), today.Month()-1, 28, 0, 0, 0, 0, time.UTC))
	for _, c := range []struct {
		name, payment, reason string
		want                  error
	}{
		{"no reason", p.ID, "", ErrVoidReasonRequired},
		{"blank reason", p.ID, "   \n ", ErrVoidReasonRequired},
		{"reason too long", p.ID, strings.Repeat("a", MaxVoidReasonLen+1), ErrVoidReasonTooLong},
		{"paid last month", lastMonth.ID, "salah input", ErrVoidPeriodClosed},
		{"unknown payment", "00000000-0000-0000-0000-000000000000", "salah input", ErrPaymentNotFound},
		{"invalid id", "x", "salah input", ErrPaymentNotFound},
	} {
		err := f.try(t, func(s *VoidService) error { _, err := s.Request(ctx, f.actor, c.payment, c.reason); return err })
		if !errors.Is(err, c.want) {
			t.Errorf("%s: error = %v, want %v", c.name, err, c.want)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM payment_voids WHERE school_id = $1`, f.schoolA); n != 0 {
		t.Errorf("voids saved = %d", n)
	}
}

func TestVoid_BelowThresholdAppliesImmediately(t *testing.T) {
	f := newVoidFixture(t)
	ctx := context.Background()
	f.threshold(t, "100000")
	p := f.pay(t, f.invA.ID, 100000, clock.Today()) // tepat di ambang: tidak perlu persetujuan
	if s := f.status(t, f.invA.ID); s != billing.StatusPartial {
		t.Fatalf("status = %s", s)
	}
	before, _ := ListByInvoice(ctx, f.tx, f.schoolA, f.invA.ID)

	v, err := f.voids.Request(ctx, f.actor, p.ID, "  salah siswa  ")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != VoidApproved || v.RequiresApproval || v.Reason != "salah siswa" || v.DecidedAt == nil {
		t.Errorf("void = %+v", v)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusUnpaid {
		t.Errorf("status after void = %s, want belum_bayar", s)
	}
	after, _ := ListByInvoice(ctx, f.tx, f.schoolA, f.invA.ID)
	if len(after) != 1 || after[0] != before[0] {
		t.Errorf("payment row changed: %+v -> %+v", before, after)
	}
	// Sisa tagihan kembali penuh: bisa dibayar 150.000 lagi.
	if _, err := RecordPayment(ctx, f.tx, f.actor, cash(f.invA.ID, 150000)); err != nil {
		t.Errorf("paying the full amount after void: %v", err)
	}
	if err := f.try(t, func(s *VoidService) error { _, err := s.Request(ctx, f.actor, p.ID, "lagi"); return err }); !errors.Is(err, ErrAlreadyVoided) {
		t.Errorf("second void: error = %v", err)
	}
}

func TestVoid_AboveThresholdNeedsApproval(t *testing.T) {
	f := newVoidFixture(t)
	ctx := context.Background()
	f.threshold(t, "100000")
	p := f.pay(t, f.invA.ID, 150000, clock.Today())
	v, err := f.voids.Request(ctx, f.actor, p.ID, "transfer ganda")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != VoidPending || !v.RequiresApproval || v.DecidedAt != nil {
		t.Errorf("void = %+v", v)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusPaid {
		t.Errorf("pending void changed status to %s", s)
	}
	if err := f.try(t, func(s *VoidService) error { _, err := s.Request(ctx, f.actor, p.ID, "lagi"); return err }); !errors.Is(err, ErrAlreadyVoided) {
		t.Errorf("void while pending: error = %v", err)
	}
	// Pengaju tidak bisa menyetujui sendiri (mis. punya peran bendahara dan kepala sekolah).
	if err := f.try(t, func(s *VoidService) error { _, err := s.Approve(ctx, f.actor, v.ID); return err }); !errors.Is(err, ErrSelfApproval) {
		t.Errorf("self approval: error = %v", err)
	}
	pending, err := f.voids.ListPending(ctx, f.schoolA)
	if err != nil || len(pending) != 1 || pending[0].Amount != 150000 || pending[0].NIS != "001" || pending[0].RequesterName != "Bendahara" {
		t.Errorf("ListPending = %+v, %v", pending, err)
	}
	if list, _ := f.voids.ListPending(ctx, f.schoolB); len(list) != 0 {
		t.Error("pending voids visible from other school")
	}
	other := school.Actor{SchoolID: f.schoolB, UserID: f.userB}
	if err := f.try(t, func(s *VoidService) error { _, err := s.Approve(ctx, other, v.ID); return err }); !errors.Is(err, ErrVoidNotFound) {
		t.Errorf("approval from other school: error = %v", err)
	}

	got, err := f.voids.Approve(ctx, f.kepala, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != VoidApproved || got.DecidedBy != f.kepala.UserID || got.DecidedAt == nil {
		t.Errorf("approved void = %+v", got)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusUnpaid {
		t.Errorf("status after approval = %s, want belum_bayar", s)
	}
	if err := f.try(t, func(s *VoidService) error { _, err := s.Reject(ctx, f.kepala, v.ID); return err }); !errors.Is(err, ErrVoidNotPending) {
		t.Errorf("deciding twice: error = %v", err)
	}
	for action, want := range map[string]int{ActionVoidRequest: 1, ActionVoidApprove: 1, ActionVoidReject: 0} {
		if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2 AND entity_id = $3`, f.schoolA, action, p.ID); n != want {
			t.Errorf("%s audits = %d, want %d", action, n, want)
		}
	}
}

func TestVoid_RejectKeepsPaymentAndAllowsNewRequest(t *testing.T) {
	f := newVoidFixture(t)
	ctx := context.Background()
	// Ambang belum diatur: semua pembatalan perlu persetujuan.
	p := f.pay(t, f.invA.ID, 1000, clock.Today())
	v, err := f.voids.Request(ctx, f.actor, p.ID, "salah nominal")
	if err != nil || v.Status != VoidPending {
		t.Fatalf("void = %+v, %v (default threshold must require approval)", v, err)
	}
	if v, err = f.voids.Reject(ctx, f.kepala, v.ID); err != nil || v.Status != VoidRejected {
		t.Fatalf("reject = %+v, %v", v, err)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusPartial {
		t.Errorf("status after rejection = %s, want sebagian", s)
	}
	again, err := f.voids.Request(ctx, f.actor, p.ID, "salah nominal, diajukan ulang")
	if err != nil || again.Status != VoidPending {
		t.Errorf("new request after rejection = %+v, %v", again, err)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE action = 'payment.void_reject' AND entity_id = $1`, p.ID); n != 1 {
		t.Errorf("payment.void_reject audits = %d", n)
	}
}

func TestVoid_DatabaseRules(t *testing.T) {
	f := newVoidFixture(t)
	ctx := context.Background()
	f.threshold(t, "0")
	p := f.pay(t, f.invA.ID, 1000, clock.Today())
	v, err := f.voids.Request(ctx, f.actor, p.ID, "uji")
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) error {
		return f.sp(t, func(q pgx.Tx) error { _, err := q.Exec(ctx, sql, args...); return err })
	}
	wantCode := func(name string, err error, code, constraint string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code || (constraint != "" && pgErr.ConstraintName != constraint) {
			t.Errorf("%s: error = %v, want %s %s", name, err, code, constraint)
		}
	}
	wantCode("delete", exec(`DELETE FROM payment_voids WHERE id = $1`, v.ID), "42501", "")
	wantCode("truncate", exec(`TRUNCATE payment_voids`), "42501", "")
	wantCode("change reason", exec(`UPDATE payment_voids SET reason = 'lain' WHERE id = $1`, v.ID), "42501", "")
	wantCode("update without decision", exec(`UPDATE payment_voids SET status = 'pending' WHERE id = $1`, v.ID), "42501", "")
	if _, err := f.voids.Approve(ctx, f.kepala, v.ID); err != nil {
		t.Fatal(err)
	}
	wantCode("change decided void", exec(`UPDATE payment_voids SET status = 'rejected' WHERE id = $1`, v.ID), "42501", "")
	wantCode("delete decided void", exec(`DELETE FROM payment_voids WHERE id = $1`, v.ID), "42501", "")
	wantCode("second active void", exec(`INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status)
		VALUES ($1, $2, 'x', $3, true, 'pending')`, f.schoolA, p.ID, f.actor.UserID), "23505", "payment_voids_payment_active_key")
	wantCode("self approval", exec(`INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status, decided_by, decided_at)
		VALUES ($1, $2, 'x', $3, true, 'rejected', $3, now())`, f.schoolA, p.ID, f.actor.UserID), "23514", "payment_voids_status_check")
	wantCode("empty reason", exec(`INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status)
		VALUES ($1, $2, '  ', $3, true, 'pending')`, f.schoolA, p.ID, f.actor.UserID), "23514", "payment_voids_reason_check")
}
