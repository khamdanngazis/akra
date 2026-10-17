package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

// newInvoice adalah tagihan siap simpan: total sudah dihitung dan nominal
// item sudah final.
type newInvoice struct {
	EnrollmentID string
	Period       string
	DueDate      time.Time
	Status       string
	CreatedBy    string
	Items        []InvoiceItem
}

// insertInvoice menyimpan tagihan dan itemnya di dalam transaksi tx.
// total_amount diambil dari jumlah item; database memeriksa ulang saat
// commit (constraint trigger invoice_total_matches_items).
func insertInvoice(ctx context.Context, tx pgx.Tx, schoolID string, in newInvoice) (Invoice, error) {
	if !database.IsUUID(in.EnrollmentID) {
		return Invoice{}, ErrEnrollmentNotFound
	}
	var yearID string
	err := tx.QueryRow(ctx, `SELECT academic_year_id FROM enrollments WHERE school_id = $1 AND id = $2`, schoolID, in.EnrollmentID).Scan(&yearID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrEnrollmentNotFound
	}
	if err != nil {
		return Invoice{}, mapInvoiceError("get enrollment", err)
	}

	var total int64
	for _, it := range in.Items {
		total += it.Amount
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO invoices (school_id, academic_year_id, enrollment_id, period, due_date, total_amount, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		schoolID, yearID, in.EnrollmentID, in.Period, in.DueDate, total, in.Status, nullIfEmpty(in.CreatedBy)).Scan(&id)
	if err != nil {
		return Invoice{}, mapInvoiceError("create invoice", err)
	}
	for i, it := range in.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_items (school_id, invoice_id, enrollment_id, period, fee_type_id, description, amount, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			schoolID, id, in.EnrollmentID, in.Period, it.FeeTypeID, nullIfEmpty(it.Description), it.Amount, i); err != nil {
			return Invoice{}, mapInvoiceError("create invoice item", err)
		}
	}
	return GetInvoice(ctx, tx, schoolID, id)
}

const invoiceColumns = `id, school_id, academic_year_id, enrollment_id, period, due_date, total_amount, status, voided_at,
	coalesce(created_by::text, ''), created_at, updated_at`

// GetInvoice mengembalikan tagihan beserta itemnya, milik schoolID.
func GetInvoice(ctx context.Context, q database.DBTX, schoolID, id string) (Invoice, error) {
	if !database.IsUUID(id) {
		return Invoice{}, ErrInvoiceNotFound
	}
	rows, err := q.Query(ctx, `SELECT `+invoiceColumns+` FROM invoices WHERE school_id = $1 AND id = $2`, schoolID, id)
	if err != nil {
		return Invoice{}, mapInvoiceError("get invoice", err)
	}
	inv, err := pgx.CollectExactlyOneRow(rows, scanInvoice)
	if err != nil {
		return Invoice{}, mapInvoiceError("get invoice", err)
	}
	if inv.Items, err = listItems(ctx, q, schoolID, id); err != nil {
		return Invoice{}, err
	}
	return inv, nil
}

// ListInvoicesByEnrollment mengembalikan tagihan satu enrollment (tanpa
// item), urut periode.
func ListInvoicesByEnrollment(ctx context.Context, q database.DBTX, schoolID, enrollmentID string) ([]Invoice, error) {
	if !database.IsUUID(enrollmentID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT `+invoiceColumns+` FROM invoices
		WHERE school_id = $1 AND enrollment_id = $2 ORDER BY period, created_at`, schoolID, enrollmentID)
	if err != nil {
		return nil, mapInvoiceError("list invoices", err)
	}
	list, err := pgx.CollectRows(rows, scanInvoice)
	if err != nil {
		return nil, mapInvoiceError("list invoices", err)
	}
	return list, nil
}

func listItems(ctx context.Context, q database.DBTX, schoolID, invoiceID string) ([]InvoiceItem, error) {
	rows, err := q.Query(ctx, `
		SELECT i.id, i.fee_type_id, f.name, coalesce(i.description, ''), i.amount
		FROM invoice_items i JOIN fee_types f ON f.id = i.fee_type_id AND f.school_id = i.school_id
		WHERE i.school_id = $1 AND i.invoice_id = $2
		ORDER BY i.position, i.created_at`, schoolID, invoiceID)
	if err != nil {
		return nil, mapInvoiceError("list items", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (InvoiceItem, error) {
		var it InvoiceItem
		err := row.Scan(&it.ID, &it.FeeTypeID, &it.FeeTypeName, &it.Description, &it.Amount)
		return it, err
	})
	if err != nil {
		return nil, mapInvoiceError("list items", err)
	}
	return items, nil
}

func scanInvoice(row pgx.CollectableRow) (Invoice, error) {
	var inv Invoice
	err := row.Scan(&inv.ID, &inv.SchoolID, &inv.AcademicYearID, &inv.EnrollmentID, &inv.Period, &inv.DueDate,
		&inv.TotalAmount, &inv.Status, &inv.VoidedAt, &inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt)
	return inv, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func mapInvoiceError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvoiceNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "invoice_items_enrollment_fee_period_key":
			return ErrDuplicateItem
		case pgErr.Code == "23514" && pgErr.ConstraintName == "invoice_items_amount_check":
			return ErrNegativeAmount
		case pgErr.Code == "23514" && pgErr.ConstraintName == "invoices_period_check":
			return ErrInvalidPeriod
		case pgErr.Code == "23503" && pgErr.ConstraintName == "invoice_items_fee_type_fkey":
			return ErrFeeTypeNotFound
		case pgErr.Code == "23503" && pgErr.ConstraintName == "invoices_enrollment_fkey":
			return ErrEnrollmentNotFound
		}
		return fmt.Errorf("billing: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("billing: %s: %w", op, err)
}
