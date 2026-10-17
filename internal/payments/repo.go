package payments

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/billing"
	"akra/internal/platform/database"
)

// Repository sengaja hanya menyediakan insert dan baca. Tidak ada fungsi
// update atau delete; database juga menolaknya (trigger payments_append_only).

// insertPayment menyimpan pembayaran yang sudah divalidasi service.
func insertPayment(ctx context.Context, q database.DBTX, schoolID, recordedBy string, r Request) (Payment, error) {
	rows, err := q.Query(ctx, `
		INSERT INTO payments (school_id, invoice_id, amount, paid_on, method, bank_account, reference, note, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+paymentColumns,
		schoolID, r.InvoiceID, r.Amount, r.PaidOn, r.Method,
		nullIfEmpty(r.BankAccount), nullIfEmpty(r.Reference), nullIfEmpty(r.Note), recordedBy)
	if err != nil {
		return Payment{}, mapError("insert payment", err)
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanPayment)
	if err != nil {
		return Payment{}, mapError("insert payment", err)
	}
	return p, nil
}

const paymentColumns = `id, school_id, invoice_id, amount, paid_on, method, coalesce(bank_account, ''),
	coalesce(reference, ''), coalesce(note, ''), coalesce(receipt_number, ''), recorded_by, created_at`

// ListByInvoice mengembalikan pembayaran satu tagihan milik schoolID, urut
// tanggal bayar lalu waktu pencatatan.
func ListByInvoice(ctx context.Context, q database.DBTX, schoolID, invoiceID string) ([]Payment, error) {
	if !database.IsUUID(invoiceID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT `+paymentColumns+` FROM payments
		WHERE school_id = $1 AND invoice_id = $2 ORDER BY paid_on, created_at, id`, schoolID, invoiceID)
	if err != nil {
		return nil, mapError("list payments", err)
	}
	list, err := pgx.CollectRows(rows, scanPayment)
	if err != nil {
		return nil, mapError("list payments", err)
	}
	return list, nil
}

// invoiceForPayment mengunci tagihan (FOR UPDATE) agar dua pembayaran
// bersamaan untuk tagihan yang sama diproses berurutan, lalu mengembalikan
// total, jumlah pembayaran aktif, dan apakah tagihan sudah dibatalkan.
func invoiceForPayment(ctx context.Context, tx pgx.Tx, schoolID, invoiceID string) (total, paid int64, voided bool, err error) {
	if !database.IsUUID(invoiceID) {
		return 0, 0, false, billing.ErrInvoiceNotFound
	}
	err = tx.QueryRow(ctx, `SELECT total_amount, voided_at IS NOT NULL FROM invoices
		WHERE school_id = $1 AND id = $2 FOR UPDATE`, schoolID, invoiceID).Scan(&total, &voided)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, billing.ErrInvoiceNotFound
	}
	if err != nil {
		return 0, 0, false, mapError("lock invoice", err)
	}
	// TODO(T38): kecualikan pembayaran yang pembatalannya sudah disetujui.
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(amount), 0) FROM payments
		WHERE school_id = $1 AND invoice_id = $2`, schoolID, invoiceID).Scan(&paid); err != nil {
		return 0, 0, false, mapError("sum payments", err)
	}
	return total, paid, voided, nil
}

func updateInvoiceStatus(ctx context.Context, tx pgx.Tx, schoolID, invoiceID string, status billing.Status) error {
	if _, err := tx.Exec(ctx, `UPDATE invoices SET status = $3, updated_at = now() WHERE school_id = $1 AND id = $2`,
		schoolID, invoiceID, status); err != nil {
		return mapError("update invoice status", err)
	}
	return nil
}

func scanPayment(row pgx.CollectableRow) (Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.SchoolID, &p.InvoiceID, &p.Amount, &p.PaidOn, &p.Method, &p.BankAccount,
		&p.Reference, &p.Note, &p.ReceiptNumber, &p.RecordedBy, &p.CreatedAt)
	return p, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func mapError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23514" && pgErr.ConstraintName == "payments_amount_check":
			return ErrInvalidAmount
		case pgErr.Code == "23514" && pgErr.ConstraintName == "payments_method_check":
			return ErrInvalidMethod
		case pgErr.Code == "23514" && pgErr.ConstraintName == "payments_bank_account_check":
			return ErrBankAccountRequired
		case pgErr.Code == "23503" && pgErr.ConstraintName == "payments_invoice_fkey":
			return billing.ErrInvoiceNotFound
		}
		return fmt.Errorf("payments: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("payments: %s: %w", op, err)
}
