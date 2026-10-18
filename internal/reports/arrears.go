package reports

import (
	"context"
	"time"

	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/reports/export"
)

// ArrearsReport menyusun laporan tunggakan dari billing.ListArrears (T42),
// tanpa query kedua, sehingga angka file sama dengan halaman tunggakan.
// today adalah tanggal cetak (Asia/Jakarta): hari terlambat dihitung dari
// jatuh tempo ke tanggal ini.
func ArrearsReport(ctx context.Context, q database.DBTX, schoolID, schoolName string, f billing.ArrearsFilter, filterLabels []string, today time.Time) (export.Table, billing.Arrears, error) {
	a, err := billing.ListArrears(ctx, q, schoolID, f, today)
	if err != nil {
		return export.Table{}, billing.Arrears{}, err
	}
	t := export.Table{
		Title: "Daftar Tunggakan", SchoolName: schoolName, PrintedAt: today, Landscape: true,
		Meta: append([]string{"Per tanggal: " + clock.FormatDateID(today) + " (jatuh tempo hari ini belum terlambat)"}, filterLabels...),
		Columns: []export.Column{
			{Header: "NIS", Width: 22}, {Header: "Nama siswa", Width: 42}, {Header: "Kelas", Width: 16},
			{Header: "Jenis tagihan", Width: 38}, {Header: "Periode", Width: 28}, {Header: "Jatuh tempo", Width: 28},
			{Header: "Total", Width: 25, Numeric: true}, {Header: "Sudah dibayar", Width: 25, Numeric: true},
			{Header: "Sisa", Width: 25, Numeric: true}, {Header: "Hari terlambat", Width: 24, Numeric: true},
		},
	}
	var total, paid int64
	for _, it := range a.Items {
		t.Rows = append(t.Rows, []any{
			it.NIS, it.StudentName, it.ClassName, it.FeeNames, billing.PeriodLabel(it.Period), clock.FormatDateID(it.DueDate),
			export.Money(it.Total), export.Money(it.Paid), export.Money(it.Remaining), it.DaysOverdue,
		})
		t.Groups = append(t.Groups, "Kelas "+it.ClassName+" · "+it.YearName)
		total += it.Total
		paid += it.Paid
	}
	t.Total = []any{"", "Total", "", "", "", "", export.Money(total), export.Money(paid), export.Money(a.TotalRemaining), ""}
	t.Summary = []export.SummaryItem{
		{Label: "Total sisa tunggakan", Value: export.Money(a.TotalRemaining)},
		{Label: "Jumlah siswa menunggak", Value: a.StudentCount},
		{Label: "Jumlah tagihan", Value: len(a.Items)},
		{Label: "Per tanggal", Value: clock.FormatDateID(today)},
	}
	return t, a, nil
}
