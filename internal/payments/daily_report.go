package payments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Rekap pembayaran harian dan bulanan (T80). "Harian" memakai tanggal bayar
// (paid_on), bukan tanggal uang masuk rekening.
//
// Pembayaran yang pembatalannya sudah disetujui tidak dihitung (sama dengan
// billing.ActivePaid, T39). Pembayaran dengan pembatalan yang masih menunggu
// persetujuan tetap dihitung di total, dan jumlahnya ditampilkan terpisah di
// kolom Pending agar terlihat.

var (
	// ErrInvalidRecapRange: tanggal awal setelah tanggal akhir.
	ErrInvalidRecapRange = errors.New("payments: recap start date is after end date")
	// ErrRecapRangeTooLong: rentang harian lebih dari MaxDailyRecapDays.
	ErrRecapRangeTooLong = errors.New("payments: daily recap range is too long")
)

// MaxDailyRecapDays adalah rentang terpanjang rekap harian (satu tahun).
const MaxDailyRecapDays = 366

// PeriodTotal adalah total pembayaran aktif satu tanggal atau satu bulan.
type PeriodTotal struct {
	// Date adalah tanggal (harian) atau tanggal 1 bulan itu (bulanan).
	Date     time.Time
	Cash     int64 // tunai
	Transfer int64
	Total    int64 // Cash + Transfer
	Count    int   // jumlah pembayaran aktif
	// Pending adalah bagian dari Total yang pembatalannya menunggu
	// persetujuan kepala sekolah; PendingCount jumlah pembayarannya.
	Pending      int64
	PendingCount int
}

// RecapTotals menjumlahkan baris rekap.
func RecapTotals(rows []PeriodTotal) PeriodTotal {
	var t PeriodTotal
	for _, r := range rows {
		t.Cash += r.Cash
		t.Transfer += r.Transfer
		t.Total += r.Total
		t.Count += r.Count
		t.Pending += r.Pending
		t.PendingCount += r.PendingCount
	}
	return t
}

// recapSelect menjumlahkan pembayaran aktif per periode bucket (ekspresi
// tanggal p.paid_on). Hanya bigint, tanpa pecahan.
func recapSelect(bucket string) string {
	return `
		SELECT ` + bucket + ` AS d,
		       coalesce(sum(p.amount) FILTER (WHERE p.method = 'tunai'), 0)::bigint,
		       coalesce(sum(p.amount) FILTER (WHERE p.method = 'transfer'), 0)::bigint,
		       coalesce(sum(p.amount), 0)::bigint,
		       count(*),
		       coalesce(sum(p.amount) FILTER (WHERE pv.id IS NOT NULL), 0)::bigint,
		       count(pv.id)
		FROM payments p
		LEFT JOIN payment_voids pv ON pv.school_id = p.school_id AND pv.payment_id = p.id AND pv.status = '` + VoidPending + `'
		WHERE p.school_id = $1 AND p.paid_on BETWEEN $2 AND $3
		  AND NOT EXISTS (SELECT 1 FROM payment_voids v
		                  WHERE v.school_id = p.school_id AND v.payment_id = p.id AND v.status = '` + VoidApproved + `')
		GROUP BY d ORDER BY d`
}

func queryRecap(ctx context.Context, q database.DBTX, sql, schoolID string, from, to time.Time) (map[string]PeriodTotal, error) {
	rows, err := q.Query(ctx, sql, schoolID, from, to)
	if err != nil {
		return nil, fmt.Errorf("payments: recap: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PeriodTotal, error) {
		var t PeriodTotal
		err := row.Scan(&t.Date, &t.Cash, &t.Transfer, &t.Total, &t.Count, &t.Pending, &t.PendingCount)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("payments: recap: %w", err)
	}
	out := make(map[string]PeriodTotal, len(list))
	for _, t := range list {
		out[t.Date.Format(time.DateOnly)] = t
	}
	return out, nil
}

// DailyTotals mengembalikan satu baris per tanggal bayar from..to (inklusif)
// yang punya pembayaran aktif, urut tanggal. Rentang maksimal
// MaxDailyRecapDays.
func DailyTotals(ctx context.Context, q database.DBTX, schoolID string, from, to time.Time) ([]PeriodTotal, error) {
	if from.After(to) {
		return nil, ErrInvalidRecapRange
	}
	if to.Sub(from) > (MaxDailyRecapDays-1)*24*time.Hour {
		return nil, ErrRecapRangeTooLong
	}
	byDay, err := queryRecap(ctx, q, recapSelect("p.paid_on"), schoolID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]PeriodTotal, 0, len(byDay))
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if t, ok := byDay[d.Format(time.DateOnly)]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// MonthlyTotals mengembalikan 12 baris (Januari–Desember tahun kalender
// year), termasuk bulan tanpa pembayaran (nol).
func MonthlyTotals(ctx context.Context, q database.DBTX, schoolID string, year int) ([]PeriodTotal, error) {
	from := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
	byMonth, err := queryRecap(ctx, q, recapSelect("date_trunc('month', p.paid_on)::date"), schoolID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]PeriodTotal, 0, 12)
	for m := from; m.Year() == year; m = m.AddDate(0, 1, 0) {
		t := byMonth[m.Format(time.DateOnly)]
		t.Date = m
		out = append(out, t)
	}
	return out, nil
}
