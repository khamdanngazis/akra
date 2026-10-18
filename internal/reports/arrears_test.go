package reports

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"akra/internal/billing"
	"akra/internal/payments"
	"akra/internal/school"
)

// arrearsData membuat tagihan contoh: Ani (1A) SPP September dibayar
// sebagian, Budi (1A) belum bayar, Cici (1B) Kegiatan Agustus belum bayar
// dan SPP Oktober jatuh tempo hari cetak (belum terlambat).
func (f *fixture) arrearsData(t *testing.T) (spp, kegiatan billing.FeeType) {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := f.tx.QueryRow(ctx, `SELECT id FROM users WHERE school_id = $1 LIMIT 1`, f.schoolA).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	actor := school.Actor{SchoolID: f.schoolA, UserID: userID}
	var err error
	if spp, err = billing.CreateFeeType(ctx, f.tx, f.schoolA, billing.FeeTypeInput{Name: "SPP", DefaultAmount: 150000}); err != nil {
		t.Fatal(err)
	}
	if kegiatan, err = billing.CreateFeeType(ctx, f.tx, f.schoolA, billing.FeeTypeInput{Name: "Kegiatan", DefaultAmount: 75000}); err != nil {
		t.Fatal(err)
	}
	enr := func(nis string) string {
		var id string
		if err := f.tx.QueryRow(ctx, `SELECT e.id FROM enrollments e JOIN students s ON s.id = e.student_id WHERE s.school_id = $1 AND s.nis = $2`, f.schoolA, nis).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	inv := func(nis, period, due string, ft billing.FeeType) billing.Invoice {
		t.Helper()
		i, err := billing.CreateInvoice(ctx, f.tx, actor, billing.InvoiceRequest{EnrollmentID: enr(nis), Period: period, DueDate: day(due), Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	ani := inv("001", "2026-09", "2026-09-10", spp)
	inv("002", "2026-09", "2026-09-10", spp)
	inv("003", "2026-08", "2026-08-20", kegiatan)
	inv("003", "2026-10", "2026-10-18", spp)
	if _, err := payments.RecordPayment(ctx, f.tx, actor, payments.Request{InvoiceID: ani.ID, Amount: 50000, PaidOn: day("2026-09-12"), Method: payments.MethodCash}); err != nil {
		t.Fatal(err)
	}
	return spp, kegiatan
}

func TestArrearsReportFiles(t *testing.T) {
	f := newFixture(t)
	spp, _ := f.arrearsData(t)
	ctx := context.Background()
	today := day("2026-10-18")

	for _, roles := range []string{"guru", "orang_tua"} {
		for _, p := range []string{"/laporan/tunggakan.xlsx", "/laporan/tunggakan.pdf"} {
			if rec := f.get(p, roles); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d, want 403", roles, p, rec.Code)
			}
		}
	}
	for _, roles := range []string{"bendahara", "admin_tu", "kepala_sekolah"} {
		if rec := f.get("/laporan/tunggakan.xlsx", roles); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", roles, rec.Code)
		}
	}

	for name, q := range map[string]string{"all": "", "class 1A": "?kelas=" + f.c1A, "SPP until September": "?jenis=" + spp.ID + "&sampai=2026-09-30"} {
		flt, _, _, _ := billing.ParseArrearsQuery(mustQuery(q))
		want, err := billing.ListArrears(ctx, f.tx, f.schoolA, flt, today)
		if err != nil {
			t.Fatal(err)
		}
		rec := f.get("/laporan/tunggakan.xlsx"+q, "bendahara")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "tunggakan-2026-10-18.xlsx") {
			t.Fatalf("%s: %d %v", name, rec.Code, rec.Header())
		}
		checkXML(t, rec.Body.Bytes())
		x, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := x.GetRows("Tunggakan")
		if len(rows) != len(want.Items)+2 {
			t.Errorf("%s: %d data rows, want %d invoices from ListArrears", name, len(rows)-2, len(want.Items))
			continue
		}
		raw := func(sheet, cell string) string {
			v, _ := x.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
			return v
		}
		last := len(rows)
		if got := raw("Tunggakan", fmt.Sprintf("I%d", last)); got != fmt.Sprint(want.TotalRemaining) {
			t.Errorf("%s: total sisa in file = %s, ListArrears = %d", name, got, want.TotalRemaining)
		}
		for i, it := range want.Items {
			r := i + 2
			if raw("Tunggakan", fmt.Sprintf("A%d", r)) != it.NIS || raw("Tunggakan", fmt.Sprintf("I%d", r)) != fmt.Sprint(it.Remaining) ||
				raw("Tunggakan", fmt.Sprintf("J%d", r)) != fmt.Sprint(int(today.Sub(it.DueDate).Hours()/24)) {
				t.Errorf("%s: row %d = %v, want %+v", name, r, rows[i+1], it)
			}
		}
		if raw("Ringkasan", "B1") != fmt.Sprint(want.TotalRemaining) || raw("Ringkasan", "B2") != fmt.Sprint(want.StudentCount) || raw("Ringkasan", "B3") != fmt.Sprint(len(want.Items)) {
			t.Errorf("%s: summary sheet = %s / %s / %s", name, raw("Ringkasan", "B1"), raw("Ringkasan", "B2"), raw("Ringkasan", "B3"))
		}
	}

	all, _ := billing.ListArrears(ctx, f.tx, f.schoolA, billing.ArrearsFilter{}, today)
	if len(all.Items) != 3 || all.TotalRemaining != 100000+150000+75000 {
		t.Fatalf("fixture arrears = %d items, %d", len(all.Items), all.TotalRemaining)
	}
	// Hari terlambat dari jatuh tempo ke tanggal cetak: 10 September -> 18 Oktober = 38.
	if all.Items[0].DaysOverdue != 38 {
		t.Errorf("days overdue = %d, want 38", all.Items[0].DaysOverdue)
	}

	tbl, _, err := ArrearsReport(ctx, f.tx, f.schoolA, "SD Laporan", billing.ArrearsFilter{}, []string{"Kelas: Semua kelas"}, today)
	if err != nil {
		t.Fatal(err)
	}
	tbl.Uncompressed = true
	var pdf bytes.Buffer
	if err := writePDF(&pdf, tbl); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Daftar Tunggakan", "SD Laporan", "Per tanggal: 18 Oktober 2026", "Kelas 1A", "Kelas 1B", "Subtotal", "Total", "Rp325.000", "Dicetak dari AKRA, 18 Oktober 2026"} {
		if !strings.Contains(pdf.String(), want) {
			t.Errorf("pdf missing %q", want)
		}
	}
	if rec := f.get("/laporan/tunggakan.pdf", "kepala_sekolah"); rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Errorf("pdf download: %d", rec.Code)
	}
	if rec := f.get("/laporan/tunggakan.xlsx?dari=kemarin", "bendahara"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad date: status = %d, want 400", rec.Code)
	}
}

func TestArrearsReportPerformance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// 500 tagihan menunggak.
	if _, err := f.tx.Exec(ctx, `
		WITH y AS (SELECT id FROM academic_years WHERE school_id = $1 AND is_active),
		st AS (INSERT INTO students (school_id, nis, full_name)
			SELECT $1, 'P' || lpad(g::text, 4, '0'), 'Siswa ' || g FROM generate_series(1, 500) g RETURNING id),
		e AS (INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
			SELECT $1, st.id, $2, y.id, '2026-07-13' FROM st, y RETURNING id, academic_year_id)
		INSERT INTO invoices (school_id, academic_year_id, enrollment_id, period, due_date, total_amount)
		SELECT $1, academic_year_id, id, '2026-09', '2026-09-10', 150000 FROM e`, f.schoolA, f.c1B); err != nil {
		t.Fatal(err)
	}
	// Statistik tabel seperti di produksi (autovacuum). Tanpa ini, di database
	// baru (CI) planner mengira invoices berisi 1 baris dan memilih nested
	// loop yang mencocokkan setiap tagihan dengan semua siswa (500 x 500).
	if _, err := f.tx.Exec(ctx, `ANALYZE students, enrollments, invoices, invoice_items, academic_years, classes`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/laporan/tunggakan.xlsx", "/laporan/tunggakan.pdf"} {
		start := time.Now()
		rec := f.get(p, "bendahara")
		elapsed := time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", p, rec.Code)
		}
		t.Logf("%s with 500 rows: %v, %d bytes", p, elapsed, rec.Body.Len())
		if elapsed > 2*time.Second {
			t.Errorf("%s took %v, want under 2s", p, elapsed)
		}
	}
}
