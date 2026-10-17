package payments

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/storage"
)

// TestFinanceAudit memastikan setiap aksi keuangan (T40) menghasilkan tepat
// satu baris audit di transaksi yang sama, dan aksi yang gagal tidak
// menghasilkan baris apa pun.
func TestFinanceAudit(t *testing.T) {
	f := newVoidFixture(t)
	ctx := context.Background()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fees := billing.NewFeeTypeService(f.tx)
	proofs := NewProofService(f.tx, local)

	total := func() int {
		return f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1`, f.schoolA)
	}
	// once menjalankan aksi yang harus berhasil dan menulis tepat satu baris
	// audit dengan aksi want.
	once := func(want string, fn func() error) {
		t.Helper()
		before := total()
		byAction := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, want)
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if got := total() - before; got != 1 {
			t.Errorf("%s: %d audit rows written, want 1", want, got)
		}
		if got := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, f.schoolA, want) - byAction; got != 1 {
			t.Errorf("%s: %d rows with this action, want 1", want, got)
		}
	}
	// none menjalankan aksi yang harus gagal (di savepoint) tanpa baris audit.
	none := func(name string, fn func(q pgx.Tx) error) {
		t.Helper()
		before := total()
		if err := f.sp(t, fn); err == nil {
			t.Errorf("%s: expected failure", name)
		}
		if got := total() - before; got != 0 {
			t.Errorf("%s: failed action wrote %d audit rows", name, got)
		}
	}
	seen := map[string]bool{}
	track := func(action string) string { seen[action] = true; return action }

	var ft billing.FeeType
	once(track(audit.ActionFeeTypeCreate), func() (err error) {
		ft, err = fees.Create(ctx, f.actor, billing.FeeTypeForm{Name: "Uang Buku", DefaultAmount: "200.000"})
		return err
	})
	once(track(audit.ActionFeeTypeUpdate), func() error {
		_, err := fees.Update(ctx, f.actor, ft.ID, billing.FeeTypeForm{Name: "Uang Buku", DefaultAmount: "250.000"})
		return err
	})
	once(track(audit.ActionFeeTypeDeactivate), func() error { _, _, err := fees.SetActive(ctx, f.actor, ft.ID, false); return err })
	once(track(audit.ActionFeeTypeActivate), func() error { _, _, err := fees.SetActive(ctx, f.actor, ft.ID, true); return err })
	none("fee type without name", func(q pgx.Tx) error {
		_, err := billing.NewFeeTypeService(q).Create(ctx, f.actor, billing.FeeTypeForm{Name: "", DefaultAmount: "1"})
		return err
	})

	var inv billing.Invoice
	once(track(audit.ActionInvoiceCreate), func() (err error) {
		inv, err = billing.NewInvoiceService(f.tx).Create(ctx, f.actor, billing.InvoiceRequest{
			EnrollmentID: f.invA.EnrollmentID, Period: "2026-11", DueDate: clock.Today(), Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
		return err
	})
	none("duplicate invoice item", func(q pgx.Tx) error {
		_, err := billing.NewInvoiceService(q).Create(ctx, f.actor, billing.InvoiceRequest{
			EnrollmentID: f.invA.EnrollmentID, Period: "2026-11", DueDate: clock.Today(), Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
		return err
	})

	if _, err := f.tx.Exec(ctx, `UPDATE academic_years SET is_active = true WHERE school_id = $1`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	once(track(audit.ActionBulkCreate), func() error {
		_, err := billing.NewBulkService(f.tx, nil).Run(ctx, f.actor, billing.BulkForm{FeeTypeID: ft.ID, Period: "2026-12", DueDate: "2026-12-10"})
		return err
	})

	var p Payment
	once(track(audit.ActionPaymentCreate), func() (err error) {
		r := cash(inv.ID, 250000)
		r.Method, r.BankAccount = MethodTransfer, "BCA 1"
		p, err = f.svc.Record(ctx, f.actor, r)
		return err
	})
	none("overpayment", func(q pgx.Tx) error { _, err := RecordPayment(ctx, q, f.actor, cash(inv.ID, 1)); return err })

	var proof Proof
	once(track(audit.ActionProofUpload), func() (err error) {
		proof, err = proofs.Upload(ctx, f.actor, p.ID, bytes.NewReader(pdfFile))
		return err
	})
	none("duplicate proof", func(q pgx.Tx) error {
		_, err := NewProofService(q, local).Upload(ctx, f.actor, p.ID, bytes.NewReader(pdfFile))
		return err
	})
	once(track(audit.ActionProofView), func() error { _, err := proofs.OpenURL(ctx, f.actor, proof.ID); return err })

	var v Void
	once(track(audit.ActionVoidRequest), func() (err error) { v, err = f.voids.Request(ctx, f.actor, p.ID, "transfer ganda"); return err })
	none("void without reason", func(q pgx.Tx) error { _, err := NewVoidService(q).Request(ctx, f.actor, p.ID, " "); return err })
	none("self approval", func(q pgx.Tx) error { _, err := NewVoidService(q).Approve(ctx, f.actor, v.ID); return err })
	once(track(audit.ActionVoidReject), func() error { _, err := f.voids.Reject(ctx, f.kepala, v.ID); return err })
	once(audit.ActionVoidRequest, func() (err error) { v, err = f.voids.Request(ctx, f.actor, p.ID, "transfer ganda"); return err })
	once(track(audit.ActionVoidApprove), func() error { _, err := f.voids.Approve(ctx, f.kepala, v.ID); return err })

	once(track(audit.ActionSettingsUpdate), func() error { _, err := UpdateFinanceSettings(ctx, f.tx, f.kepala, "500.000"); return err })
	none("invalid threshold", func(q pgx.Tx) error { _, err := UpdateFinanceSettings(ctx, q, f.kepala, "abc"); return err })

	for _, a := range audit.FinanceActions {
		if !seen[a] {
			t.Errorf("finance action %s is not covered", a)
		}
	}

	// before/after tidak berisi rahasia atau alamat.
	rows, err := f.tx.Query(ctx, `SELECT action, coalesce(before::text, ''), coalesce(after::text, '') FROM audit_logs WHERE school_id = $1 AND action = ANY($2)`,
		f.schoolA, audit.FinanceActions)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var action, before, after string
		if err := rows.Scan(&action, &before, &after); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"password", "token", "secret", "address", "alamat", "storage_key", "note"} {
			if strings.Contains(strings.ToLower(before+after), `"`+bad) {
				t.Errorf("%s audit contains %q: %s %s", action, bad, before, after)
			}
		}
	}
}
