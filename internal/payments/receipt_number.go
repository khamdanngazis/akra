package payments

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// NextReceiptNumber mengambil nomor kuitansi berikutnya sekolah untuk tahun
// year, format KW/{tahun}/{6 digit}, mis. KW/2026/000001.
//
// Wajib dipanggil di transaksi yang sama dengan insert pembayaran. Upsert
// mengunci baris counter sampai transaksi selesai, sehingga transaksi lain
// yang meminta nomor menunggu (tidak ada nomor kembar), dan rollback ikut
// mengembalikan nomornya (tidak ada celah).
func NextReceiptNumber(ctx context.Context, tx pgx.Tx, schoolID string, year int) (string, error) {
	var n int64
	err := tx.QueryRow(ctx, `
		INSERT INTO receipt_counters (school_id, year, last_number) VALUES ($1, $2, 1)
		ON CONFLICT (school_id, year) DO UPDATE SET last_number = receipt_counters.last_number + 1
		RETURNING last_number`, schoolID, year).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("payments: next receipt number: %w", err)
	}
	return fmt.Sprintf("KW/%04d/%06d", year, n), nil
}
