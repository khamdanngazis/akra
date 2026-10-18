package portal

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/billing"
	"akra/internal/payments"
	"akra/internal/platform/clock"
	"akra/internal/platform/storage"
	"akra/internal/school"
)

type billingData struct {
	sept, oct, aug billing.Invoice
	voided         payments.Payment
}

// billingData membuat tagihan dan pembayaran Ani lewat service sungguhan.
func (f *fixture) billingData(t *testing.T) billingData {
	t.Helper()
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	bendahara := school.Actor{SchoolID: f.schoolA, UserID: one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'bendahara@uji.id', 'x', 'Pak Bendahara') RETURNING id`, f.schoolA)}
	kepala := school.Actor{SchoolID: f.schoolA, UserID: one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'kepala@uji.id', 'x', 'Bu Kepala') RETURNING id`, f.schoolA)}
	ft, err := billing.CreateFeeType(ctx, f.tx, f.schoolA, billing.FeeTypeInput{Name: "SPP", DefaultAmount: 150000})
	if err != nil {
		t.Fatal(err)
	}
	enr := one(`SELECT e.id FROM enrollments e JOIN academic_years y ON y.id = e.academic_year_id WHERE e.student_id = $1 AND y.is_active`, f.ani)
	invoice := func(period, due string, amount int64) billing.Invoice {
		t.Helper()
		d, _ := time.Parse("2006-01-02", due)
		inv, err := billing.CreateInvoice(ctx, f.tx, bendahara, billing.InvoiceRequest{EnrollmentID: enr, Period: period, DueDate: d, Items: []billing.ItemRequest{{FeeTypeID: ft.ID, Amount: &amount}}})
		if err != nil {
			t.Fatal(err)
		}
		return inv
	}
	pay := func(inv billing.Invoice, amount int64, method payments.Method) payments.Payment {
		t.Helper()
		r := payments.Request{InvoiceID: inv.ID, Amount: amount, PaidOn: clock.Today(), Method: method, Note: "catatan internal bendahara"}
		if method == payments.MethodTransfer {
			r.BankAccount = "BCA 999 rahasia"
		}
		p, err := payments.RecordPayment(ctx, f.tx, bendahara, r)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	var d billingData
	d.sept = invoice("2026-09", "2026-09-10", 150000)
	d.oct = invoice("2026-10", "2099-10-25", 100000) // belum jatuh tempo
	d.aug = invoice("2026-08", "2026-08-10", 150000)
	pay(d.sept, 50000, payments.MethodCash)
	d.voided = pay(d.sept, 30000, payments.MethodTransfer)
	pending := pay(d.sept, 20000, payments.MethodCash)
	pay(d.aug, 150000, payments.MethodTransfer)

	voids := payments.NewVoidService(f.tx)
	v, err := voids.Request(ctx, bendahara, d.voided.ID, "transfer ganda")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := voids.Approve(ctx, kepala, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := voids.Request(ctx, bendahara, pending.ID, "menunggu keputusan"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payments.NewProofService(f.tx, store).Upload(ctx, bendahara, d.voided.ID, bytes.NewReader([]byte("%PDF-1.4\nbukti\n"))); err != nil {
		t.Fatal(err)
	}
	// Tagihan keluarga lain.
	enrCitra := one(`SELECT id FROM enrollments WHERE student_id = $1`, f.citra)
	amt := int64(99000)
	if _, err := billing.CreateInvoice(ctx, f.tx, bendahara, billing.InvoiceRequest{EnrollmentID: enrCitra, Period: "2025-09", DueDate: time.Date(2025, 9, 10, 0, 0, 0, 0, time.UTC), Items: []billing.ItemRequest{{FeeTypeID: ft.ID, Amount: &amt}}}); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGetChildBilling(t *testing.T) {
	f := newFixture(t)
	d := f.billingData(t)
	ctx := context.Background()
	today := clock.Today()
	b, err := NewService(f.tx).GetChildBilling(ctx, f.schoolA, f.userSari, f.ani, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Invoices) != 3 || b.Invoices[0].ID != d.oct.ID {
		t.Fatalf("invoices = %+v", b.Invoices)
	}
	byID := map[string]ChildInvoice{}
	for _, inv := range b.Invoices {
		byID[inv.ID] = inv
	}
	sept, oct, aug := byID[d.sept.ID], byID[d.oct.ID], byID[d.aug.ID]
	// 50.000 + 20.000 (pembatalan menunggu tetap aktif); 30.000 dibatalkan.
	if sept.Paid != 70000 || sept.Remaining != 80000 || sept.Status != billing.StatusPartial || !sept.Overdue {
		t.Errorf("September = %+v", sept)
	}
	if oct.Overdue || oct.Remaining != 100000 {
		t.Errorf("October (not due) = %+v", oct)
	}
	if aug.Overdue || aug.Status != billing.StatusPaid || aug.Remaining != 0 {
		t.Errorf("August (paid) = %+v", aug)
	}
	if b.TotalRemaining != 180000 || b.OverdueRemaining != 80000 {
		t.Errorf("summary = %d / %d", b.TotalRemaining, b.OverdueRemaining)
	}
	// Sama dengan daftar tunggakan (T42) untuk siswa ini.
	arrears, err := billing.ListArrears(ctx, f.tx, f.schoolA, billing.ArrearsFilter{}, today)
	if err != nil {
		t.Fatal(err)
	}
	var fromArrears int64
	for _, a := range arrears.Items {
		if a.StudentID == f.ani {
			fromArrears += a.Remaining
			if inv := byID[a.InvoiceID]; inv.Remaining != a.Remaining || inv.Paid != a.Paid || inv.Total != a.Total {
				t.Errorf("invoice %s: portal %+v vs arrears %+v", a.InvoiceID, inv, a)
			}
		}
	}
	if fromArrears != b.OverdueRemaining {
		t.Errorf("arrears for Ani = %d, portal overdue = %d", fromArrears, b.OverdueRemaining)
	}

	if len(b.Payments) != 4 {
		t.Fatalf("payments = %d, want 4 (voided one included)", len(b.Payments))
	}
	voided := 0
	for _, p := range b.Payments {
		if p.Voided {
			voided++
			if p.ID != d.voided.ID || p.VoidReason != "transfer ganda" {
				t.Errorf("voided payment = %+v", p)
			}
		}
		if p.ReceiptNumber == "" {
			t.Errorf("payment without receipt number: %+v", p)
		}
	}
	if voided != 1 {
		t.Errorf("voided payments = %d, want 1 (pending void shown as a normal payment)", voided)
	}

	for name, st := range map[string]string{"other family": f.citra, "other school": f.dewi, "unknown": "00000000-0000-0000-0000-000000000000"} {
		if _, err := NewService(f.tx).GetChildBilling(ctx, f.schoolA, f.userSari, st, today); err != ErrNotFound {
			t.Errorf("%s: error = %v, want ErrNotFound", name, err)
		}
	}
}

func TestChildBillingPage(t *testing.T) {
	f := newFixture(t)
	f.billingData(t)
	rec := f.getPath("/portal/siswa/"+f.ani+"/tagihan", f.userSari, f.schoolA, "orang_tua")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{"Rp180.000", "Rp80.000", "sisa Rp80.000", "Rp150.000", "Dibatalkan", "transfer ganda", "No. KW/"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if n := strings.Count(body, ">Terlambat<"); n != 1 {
		t.Errorf("Terlambat labels = %d, want 1 (only the overdue unpaid invoice)", n)
	}
	for _, leak := range []string{"bukti", "/keuangan/", "Pak Bendahara", "catatan internal", "BCA 999", "menunggu keputusan", "Menunggu"} {
		if strings.Contains(body, leak) {
			t.Errorf("page leaks %q", leak)
		}
	}
	if strings.Count(body, "<form") != 1 {
		t.Error("billing page must be read-only")
	}
	for name, st := range map[string]string{"other family": f.citra, "other school": f.dewi} {
		if rec := f.getPath("/portal/siswa/"+st+"/tagihan", f.userSari, f.schoolA, "orang_tua"); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, rec.Code)
		}
	}
	if rec := f.getPath("/portal/siswa/"+f.ani+"/tagihan", f.userSari, f.schoolA, "bendahara"); rec.Code != http.StatusForbidden {
		t.Errorf("staff: status = %d, want 403", rec.Code)
	}
	if !strings.Contains(f.get(f.userSari, f.schoolA, "orang_tua").Body.String(), `href="/portal/siswa/`+f.ani+`/tagihan"`) {
		t.Error("children list must link to the billing page")
	}
}
