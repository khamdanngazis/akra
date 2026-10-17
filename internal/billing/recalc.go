package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// RecalculateInvoice menghitung ulang status tagihan dari pembayaran yang
// masih aktif (ActivePaid lalu ComputeStatus) dan menyimpannya. Wajib
// dipanggil di transaksi yang sama dengan aksi pemicunya (pembayaran baru,
// pembatalan disetujui) agar status ikut batal jika aksi gagal. Tagihan
// dikunci (FOR UPDATE) selama transaksi.
func RecalculateInvoice(ctx context.Context, tx pgx.Tx, schoolID, invoiceID string) (Status, error) {
	if !database.IsUUID(invoiceID) {
		return "", ErrInvoiceNotFound
	}
	var (
		total   int64
		current Status
	)
	err := tx.QueryRow(ctx, `SELECT total_amount, status FROM invoices WHERE school_id = $1 AND id = $2 FOR UPDATE`,
		schoolID, invoiceID).Scan(&total, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvoiceNotFound
	}
	if err != nil {
		return "", fmt.Errorf("billing: recalc: lock invoice: %w", err)
	}

	// Satu baris per pembayaran; approved bernilai true jika pembayaran punya
	// pembatalan yang sudah disetujui (pembatalan ditolak bisa lebih dari satu).
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.amount,
		       EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved')
		FROM payments p WHERE p.school_id = $1 AND p.invoice_id = $2`, schoolID, invoiceID)
	if err != nil {
		return "", fmt.Errorf("billing: recalc: list payments: %w", err)
	}
	type row struct {
		line     PaymentLine
		approved bool
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.line.ID, &x.line.Amount, &x.approved)
		return x, err
	})
	if err != nil {
		return "", fmt.Errorf("billing: recalc: list payments: %w", err)
	}
	payments := make([]PaymentLine, 0, len(list))
	voids := map[string]string{}
	for _, x := range list {
		payments = append(payments, x.line)
		if x.approved {
			voids[x.line.ID] = VoidApproved
		}
	}

	status, err := ComputeStatus(total, ActivePaid(payments, voids))
	if err != nil {
		return "", err
	}
	if status != current {
		if _, err := tx.Exec(ctx, `UPDATE invoices SET status = $3, updated_at = now() WHERE school_id = $1 AND id = $2`,
			schoolID, invoiceID, status); err != nil {
			return "", fmt.Errorf("billing: recalc: update status: %w", err)
		}
	}
	return status, nil
}
