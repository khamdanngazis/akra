package billing

import (
	"errors"
	"time"
)

// File ini hanya berisi fungsi murni: tidak membaca database, tidak memakai
// context, dan tidak membaca jam sistem. Semua modul (pembayaran,
// pembatalan, tunggakan, portal) menentukan status tagihan lewat fungsi di
// sini, bukan menghitung sendiri.

// Status adalah status tagihan yang disimpan di invoices.status.
type Status string

// ErrOverpayment dikembalikan jika pembayaran aktif melebihi total tagihan.
var ErrOverpayment = errors.New("billing: payment exceeds invoice total")

// ComputeStatus menentukan status tagihan dari total tagihan dan jumlah
// pembayaran aktif (yang belum dibatalkan):
//
//	paid == 0 dan total > 0  -> belum_bayar
//	0 < paid < total         -> sebagian
//	paid == total            -> lunas (termasuk tagihan gratis: 0 dan 0)
//	paid > total             -> ErrOverpayment
//	total < 0 atau paid < 0  -> ErrNegativeAmount
func ComputeStatus(total, paid int64) (Status, error) {
	switch {
	case total < 0 || paid < 0:
		return "", ErrNegativeAmount
	case paid > total:
		return "", ErrOverpayment
	case paid == total:
		return StatusPaid, nil
	case paid == 0:
		return StatusUnpaid, nil
	default:
		return StatusPartial, nil
	}
}

// IsOverdue melaporkan apakah tagihan terlambat: belum lunas dan jatuh
// temponya sudah lewat. Jatuh tempo hari ini belum terlambat. today adalah
// tanggal hari ini menurut Asia/Jakarta (clock.Today()); yang dibandingkan
// hanya tanggalnya, bukan jamnya.
func IsOverdue(status Status, dueDate, today time.Time) bool {
	if status == StatusPaid {
		return false
	}
	return dateOnly(today).After(dateOnly(dueDate))
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
