package payments

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"akra/internal/billing"
	"akra/internal/school"
)

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

// recapData mencatat pembayaran sekolah A di beberapa tanggal: satu
// dibatalkan (disetujui), satu menunggu persetujuan pembatalan, satu
// pembatalan ditolak (tetap aktif), dan satu pembayaran sekolah B.
func (f *fixture) recapData(t *testing.T) (voided, pending string) {
	t.Helper()
	ctx := context.Background()
	pay := func(inv billing.Invoice, amount int64, date string, method Method) string {
		t.Helper()
		r := Request{InvoiceID: inv.ID, Amount: amount, PaidOn: day(date), Method: method}
		if method == MethodTransfer {
			r.BankAccount = "BCA 123"
		}
		p, err := RecordPayment(ctx, f.tx, f.actor, r)
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	pay(f.invA, 50000, "2026-09-05", MethodCash)
	pay(f.invA, 25000, "2026-09-05", MethodTransfer)
	voided = pay(f.invA2, 40000, "2026-09-05", MethodCash)
	pending = pay(f.invA2, 30000, "2026-09-20", MethodTransfer)
	rejected := pay(f.invA2, 20000, "2026-10-01", MethodCash)
	pay(f.invA, 10000, "2026-10-15", MethodCash)
	// Bentuk baris mengikuti constraint payment_voids_status_check:
	// disetujui langsung (di bawah ambang), menunggu, dan ditolak kepala sekolah.
	var principal string
	if err := f.tx.QueryRow(ctx, `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'kepala@uji.id', 'x', 'Kepala') RETURNING id`,
		f.schoolA).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		payment, status string
		approval        bool
		decidedBy       any
		decided         bool
	}{
		{voided, VoidApproved, false, nil, true},
		{pending, VoidPending, true, nil, false},
		{rejected, "rejected", true, principal, true},
	} {
		var decidedAt any
		if v.decided {
			decidedAt = time.Now()
		}
		if _, err := f.tx.Exec(ctx, `INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status, decided_by, decided_at)
			VALUES ($1, $2, 'uji', $3, $4, $5, $6, $7)`, f.schoolA, v.payment, f.actor.UserID, v.approval, v.status, v.decidedBy, decidedAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RecordPayment(ctx, f.tx, school.Actor{SchoolID: f.schoolB, UserID: f.userB}, Request{InvoiceID: f.invB.ID, Amount: 99000, PaidOn: day("2026-09-05"), Method: MethodCash}); err != nil {
		t.Fatal(err)
	}
	return voided, pending
}

func TestDailyTotals(t *testing.T) {
	f := newFixture(t)
	f.recapData(t)
	ctx := context.Background()
	rows, err := DailyTotals(ctx, f.tx, f.schoolA, day("2026-09-01"), day("2026-10-31"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]PeriodTotal{}
	for _, r := range rows {
		got[r.Date.Format(time.DateOnly)] = r
	}
	// 5 Sep: 50.000 tunai + 25.000 transfer; 40.000 tunai dibatalkan tidak
	// dihitung; 99.000 sekolah B tidak ikut.
	if d := got["2026-09-05"]; d.Cash != 50000 || d.Transfer != 25000 || d.Total != 75000 || d.Count != 2 || d.Pending != 0 {
		t.Errorf("5 Sep: %+v", d)
	}
	// 20 Sep: menunggu pembatalan tetap dihitung, juga di kolom Pending.
	if d := got["2026-09-20"]; d.Transfer != 30000 || d.Total != 30000 || d.Pending != 30000 || d.PendingCount != 1 {
		t.Errorf("20 Sep: %+v", d)
	}
	// 1 Okt: pembatalan ditolak, pembayaran tetap aktif.
	if d := got["2026-10-01"]; d.Total != 20000 || d.Pending != 0 {
		t.Errorf("1 Okt: %+v", d)
	}
	if len(rows) != 4 || rows[0].Date.Format(time.DateOnly) != "2026-09-05" || rows[3].Date.Format(time.DateOnly) != "2026-10-15" {
		t.Errorf("rows: %+v", rows)
	}
	if tot := RecapTotals(rows); tot.Total != 135000 || tot.Count != 5 || tot.Pending != 30000 {
		t.Errorf("total: %+v", tot)
	}
	if _, err := DailyTotals(ctx, f.tx, f.schoolA, day("2026-10-02"), day("2026-10-01")); !errors.Is(err, ErrInvalidRecapRange) {
		t.Errorf("reversed range: %v", err)
	}
	if _, err := DailyTotals(ctx, f.tx, f.schoolA, day("2025-01-01"), day("2026-01-02")); !errors.Is(err, ErrRecapRangeTooLong) {
		t.Errorf("too long: %v", err)
	}
	if rows, err := DailyTotals(ctx, f.tx, f.schoolA, day("2026-01-01"), day("2026-12-31")); err != nil || len(rows) != 4 {
		t.Errorf("full year: %v %d", err, len(rows))
	}
}

// TestMonthlyTotals: total bulanan = jumlah harian bulan itu = jumlah
// pembayaran aktif (aturan ActivePaid) bulan itu.
func TestMonthlyTotals(t *testing.T) {
	f := newFixture(t)
	f.recapData(t)
	ctx := context.Background()
	months, err := MonthlyTotals(ctx, f.tx, f.schoolA, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 12 || months[0].Date.Format(time.DateOnly) != "2026-01-01" || months[11].Date.Month() != time.December {
		t.Fatalf("months: %d", len(months))
	}
	for _, m := range months {
		end := m.Date.AddDate(0, 1, -1)
		days, err := DailyTotals(ctx, f.tx, f.schoolA, m.Date, end)
		if err != nil {
			t.Fatal(err)
		}
		sum := RecapTotals(days)
		sum.Date = m.Date
		if sum != m {
			t.Errorf("%s: monthly %+v, sum of days %+v", m.Date.Month(), m, sum)
		}
		// Pembanding: billing.ActivePaid (fungsi murni T39) atas pembayaran
		// bulan itu dan status pembatalannya.
		if active := activePaidBetween(t, f, m.Date, end); active != m.Total {
			t.Errorf("%s: total %d, ActivePaid %d", m.Date.Month(), m.Total, active)
		}
	}
	if months[8].Total != 105000 || months[9].Total != 30000 || months[0].Total != 0 {
		t.Errorf("Sep %d, Okt %d, Jan %d", months[8].Total, months[9].Total, months[0].Total)
	}
	if tot := RecapTotals(months); tot.Total != activePaidBetween(t, f, day("2026-01-01"), day("2026-12-31")) {
		t.Errorf("year total %d differs from ActivePaid", tot.Total)
	}
}

// activePaidBetween memuat pembayaran sekolah A bertanggal from..to beserta
// status pembatalannya, lalu menjumlahkannya dengan billing.ActivePaid.
func activePaidBetween(t *testing.T, f *fixture, from, to time.Time) int64 {
	t.Helper()
	rows, err := f.tx.Query(context.Background(), `SELECT p.id, p.amount, coalesce(v.status, '') FROM payments p
		LEFT JOIN payment_voids v ON v.payment_id = p.id AND v.status <> 'rejected'
		WHERE p.school_id = $1 AND p.paid_on BETWEEN $2 AND $3`, f.schoolA, from, to)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []billing.PaymentLine
	voids := map[string]string{}
	for rows.Next() {
		var l billing.PaymentLine
		var status string
		if err := rows.Scan(&l.ID, &l.Amount, &status); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, l)
		voids[l.ID] = status
	}
	return billing.ActivePaid(lines, voids)
}

// TestRecap_Performance: rekap satu tahun untuk 300 siswa (12 pembayaran
// per siswa) di bawah 2 detik.
func TestRecap_Performance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.tx.Exec(ctx, `
		WITH y AS (SELECT id FROM academic_years WHERE school_id = $1 LIMIT 1),
		c AS (INSERT INTO classes (school_id, academic_year_id, name, grade_level) SELECT $1, y.id, 'Perf', 1 FROM y RETURNING id, academic_year_id),
		s AS (INSERT INTO students (school_id, nis, full_name) SELECT $1, 'P' || g, 'Siswa ' || g FROM generate_series(1, 300) g RETURNING id),
		e AS (INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
		      SELECT $1, s.id, c.id, c.academic_year_id, '2026-01-01' FROM s, c RETURNING id, academic_year_id),
		i AS (INSERT INTO invoices (school_id, academic_year_id, enrollment_id, period, due_date, total_amount, created_by)
		      SELECT $1, e.academic_year_id, e.id, '2026-01', '2026-01-10', 3600000, $2 FROM e RETURNING id)
		INSERT INTO payments (school_id, invoice_id, amount, paid_on, method, bank_account, receipt_number, recorded_by)
		SELECT $1, i.id, 300000, date '2026-01-03' + (m * 30) + (row_number() OVER () % 20)::int,
		       CASE WHEN m % 2 = 0 THEN 'tunai' ELSE 'transfer' END, CASE WHEN m % 2 = 0 THEN NULL ELSE 'BCA' END,
		       'KW/2026/' || lpad((900000 + row_number() OVER ())::text, 6, '0'), $2
		FROM i, generate_series(0, 11) m`, f.schoolA, f.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(ctx, `ANALYZE payments`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	months, err := MonthlyTotals(ctx, f.tx, f.schoolA, 2026)
	if err != nil {
		t.Fatal(err)
	}
	days, err := DailyTotals(ctx, f.tx, f.schoolA, day("2026-01-01"), day("2026-12-31"))
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	t.Logf("year recap (3600 payments): monthly + daily in %s", elapsed)
	if elapsed > 2*time.Second {
		t.Errorf("recap took %s", elapsed)
	}
	if m, d := RecapTotals(months), RecapTotals(days); m.Total != 3600*300000 || m.Count != 3600 || d != m {
		t.Errorf("totals: monthly %+v, daily %+v", m, d)
	}
}

func TestRecapHandler(t *testing.T) {
	f := newFixture(t)
	f.recapData(t)
	c := newClient(t, f)
	for _, role := range []string{"bendahara", "kepala_sekolah"} {
		rec := c.do(http.MethodGet, "/keuangan/rekap-pembayaran?mode=harian&dari=2026-09-01&sampai=2026-09-30", role, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "5 September 2026") || !strings.Contains(body, "Rp75.000") ||
			!strings.Contains(body, "Rp30.000 (1)") || !strings.Contains(body, "Rp105.000") {
			t.Errorf("%s daily page: %d", role, rec.Code)
		}
	}
	rec := c.do(http.MethodGet, "/keuangan/rekap-pembayaran?mode=bulanan&tahun=2026", "bendahara", nil)
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "September 2026") || !strings.Contains(body, "Rp135.000") {
		t.Errorf("monthly page: %d", rec.Code)
	}
	// Excel: angka sama dengan rekap, nominal sebagai angka.
	rec = c.do(http.MethodGet, "/keuangan/rekap-pembayaran.xlsx?mode=bulanan&tahun=2026", "kepala_sekolah", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "rekap-pembayaran-bulanan-2026.xlsx") {
		t.Fatalf("excel: %d", rec.Code)
	}
	book, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	rows, _ := book.GetRows("Rekap")
	sep, _ := book.GetCellValue("Rekap", "D10", excelize.Options{RawCellValue: true})
	total, _ := book.GetCellValue("Rekap", "D14", excelize.Options{RawCellValue: true})
	if len(rows) != 14 || rows[9][0] != "September 2026" || sep != "105000" || total != "135000" || rows[0][5] != "Menunggu pembatalan" {
		t.Errorf("excel rows %d, Sep %q, total %q, header %v", len(rows), sep, total, rows[0])
	}
	// Filter tidak valid 400; guru, admin, dan orang tua 403.
	for _, q := range []string{"?mode=harian&dari=2026-13-01", "?mode=harian&dari=2026-10-02&sampai=2026-10-01", "?mode=bulanan&tahun=abc", "?mode=harian&dari=2025-01-01&sampai=2026-06-30"} {
		if rec := c.do(http.MethodGet, "/keuangan/rekap-pembayaran"+q, "bendahara", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
	for _, role := range []string{"guru", "admin_tu", "orang_tua"} {
		for _, path := range []string{"/keuangan/rekap-pembayaran", "/keuangan/rekap-pembayaran.xlsx"} {
			if rec := c.do(http.MethodGet, path, role, nil); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: %d", role, path, rec.Code)
			}
		}
	}
}
