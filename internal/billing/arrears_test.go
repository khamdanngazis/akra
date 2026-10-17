package billing

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type arrearsFixture struct {
	*recalcFixture
	classA, classA2 string
	inv1, inv3      Invoice
	today           time.Time
}

func date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func newArrearsFixture(t *testing.T) *arrearsFixture {
	t.Helper()
	f := &arrearsFixture{recalcFixture: newRecalcFixture(t), today: date("2026-10-15")}
	ctx := context.Background()
	_ = f.tx.QueryRow(ctx, `SELECT class_id FROM enrollments WHERE id = $1`, f.enrA).Scan(&f.classA)
	_ = f.tx.QueryRow(ctx, `SELECT class_id FROM enrollments WHERE id = $1`, f.enrA2).Scan(&f.classA2)

	mk := func(enr, period, due string, fee FeeType, amount int64) Invoice {
		t.Helper()
		return f.keep(t, InvoiceRequest{EnrollmentID: enr, Period: period, DueDate: date(due), Items: []ItemRequest{{FeeTypeID: fee.ID, Amount: &amount}}})
	}
	payOn := func(inv Invoice, amount int64) string {
		t.Helper()
		f.inv = inv
		return f.pay(t, amount)
	}
	recalc := func(inv Invoice) {
		t.Helper()
		if _, err := RecalculateInvoice(ctx, f.tx, inv.SchoolID, inv.ID); err != nil {
			t.Fatal(err)
		}
	}

	// recalcFixture sudah membuat tagihan Rp100.000 SPP 2026-09 untuk enrA
	// (jatuh tempo default 10 September): pakai sebagai inv1, belum dibayar.
	f.inv1 = f.recalcFixture.inv
	// Jatuh tempo hari ini: belum terlambat.
	mk(f.enrA, "2026-10", "2026-10-15", f.kegiatan, 75000)
	// enrA2: Rp150.000, dibayar 50.000 + 30.000 (pembatalan menunggu) +
	// 20.000 (pembatalan disetujui) -> pembayaran aktif 80.000, sisa 70.000.
	f.inv3 = mk(f.enrA2, "2026-09", "2026-09-10", f.spp, 150000)
	payOn(f.inv3, 50000)
	f.void(t, payOn(f.inv3, 30000), "pending")
	f.void(t, payOn(f.inv3, 20000), "approved")
	recalc(f.inv3)
	// Lunas: tidak muncul.
	paid := mk(f.enrA2, "2026-08", "2026-08-10", f.spp, 150000)
	payOn(paid, 150000)
	recalc(paid)
	// Tagihan dibatalkan: tidak muncul.
	voided := mk(f.enrA, "2026-08", "2026-08-20", f.kegiatan, 75000)
	if _, err := f.tx.Exec(ctx, `UPDATE invoices SET voided_at = now() WHERE id = $1`, voided.ID); err != nil {
		t.Fatal(err)
	}
	// Sekolah lain: tidak muncul.
	b := f.actor
	b.SchoolID = f.schoolB
	b.UserID = ""
	if _, err := CreateInvoice(ctx, f.tx, b, InvoiceRequest{EnrollmentID: f.enrB, Period: "2026-09", Items: []ItemRequest{{FeeTypeID: f.feeB.ID}}}); err != nil {
		t.Fatal(err)
	}
	if err := checkDeferred(f.tx); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *arrearsFixture) list(t *testing.T, flt ArrearsFilter) Arrears {
	t.Helper()
	a, err := ListArrears(context.Background(), f.tx, f.schoolA, flt, f.today)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func ids(a Arrears) string {
	s := ""
	for _, it := range a.Items {
		s += it.InvoiceID[:8] + ","
	}
	return s
}

func TestArrears_ListAndAmounts(t *testing.T) {
	f := newArrearsFixture(t)
	a := f.list(t, ArrearsFilter{})
	want := f.inv1.ID[:8] + "," + f.inv3.ID[:8] + ","
	if ids(a) != want {
		t.Fatalf("arrears = %s, want %s (class order, no paid, due today, voided, or other school)", ids(a), want)
	}
	first, second := a.Items[0], a.Items[1]
	if first.Remaining != 100000 || first.Paid != 0 || first.DaysOverdue != 35 || first.NIS != "001" || first.FeeNames != "SPP" {
		t.Errorf("first = %+v", first)
	}
	if second.Total != 150000 || second.Paid != 80000 || second.Remaining != 70000 {
		t.Errorf("second = %+v; want paid 80.000 (pending void still active, approved void excluded), remaining 70.000", second)
	}
	if a.TotalRemaining != 170000 || a.StudentCount != 2 {
		t.Errorf("summary = %d, %d students", a.TotalRemaining, a.StudentCount)
	}
	// Satu hari kemudian, tagihan jatuh tempo 15 Oktober ikut menunggak.
	f.today = date("2026-10-16")
	if a := f.list(t, ArrearsFilter{}); len(a.Items) != 3 || a.TotalRemaining != 245000 || a.StudentCount != 2 {
		t.Errorf("next day: %d items, %d, %d students", len(a.Items), a.TotalRemaining, a.StudentCount)
	}
}

func TestArrears_Filters(t *testing.T) {
	f := newArrearsFixture(t)
	for _, c := range []struct {
		name string
		flt  ArrearsFilter
		want string
	}{
		{"class A2", ArrearsFilter{ClassID: f.classA2}, f.inv3.ID[:8] + ","},
		{"class A", ArrearsFilter{ClassID: f.classA}, f.inv1.ID[:8] + ","},
		{"fee type SPP", ArrearsFilter{FeeTypeID: f.spp.ID}, f.inv1.ID[:8] + "," + f.inv3.ID[:8] + ","},
		{"fee type Kegiatan", ArrearsFilter{FeeTypeID: f.kegiatan.ID}, ""},
		{"due in September", ArrearsFilter{DueFrom: date("2026-09-01"), DueTo: date("2026-09-30")}, f.inv1.ID[:8] + "," + f.inv3.ID[:8] + ","},
		{"due until August", ArrearsFilter{DueTo: date("2026-08-31")}, ""},
		{"due from 11 September", ArrearsFilter{DueFrom: date("2026-09-11")}, ""},
		{"class of other school", ArrearsFilter{ClassID: fmt.Sprint("00000000-0000-0000-0000-000000000000")}, ""},
		{"invalid class id", ArrearsFilter{ClassID: "1A"}, ""},
	} {
		if got := ids(f.list(t, c.flt)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if a, _ := ListArrears(context.Background(), f.tx, f.schoolB, ArrearsFilter{}, f.today); len(a.Items) != 1 {
		t.Errorf("school B sees %d arrears, want only its own 1", len(a.Items))
	}
}
