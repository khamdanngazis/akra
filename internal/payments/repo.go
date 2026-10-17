package payments

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	if err = tx.QueryRow(ctx, `SELECT `+paidSumSQL+` FROM invoices i WHERE i.school_id = $1 AND i.id = $2`,
		schoolID, invoiceID).Scan(&paid); err != nil {
		return 0, 0, false, mapError("sum payments", err)
	}
	return total, paid, voided, nil
}

// paidSumSQL adalah jumlah pembayaran aktif tagihan beralias i: pembayaran
// yang tidak punya pembatalan disetujui. Pembatalan yang masih menunggu
// persetujuan tidak mengurangi. Semua perhitungan sisa tagihan memakai
// ekspresi ini.
const paidSumSQL = `(SELECT coalesce(sum(p.amount), 0) FROM payments p
	WHERE p.school_id = i.school_id AND p.invoice_id = i.id
	  AND NOT EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved'))`

// feeNamesSQL adalah nama jenis tagihan item tagihan beralias i, dipisah koma.
const feeNamesSQL = `(SELECT string_agg(f.name, ', ' ORDER BY it.position, it.created_at)
	FROM invoice_items it JOIN fee_types f ON f.school_id = it.school_id AND f.id = it.fee_type_id
	WHERE it.school_id = i.school_id AND it.invoice_id = i.id)`

// OpenInvoice adalah tagihan yang belum lunas beserta sisanya.
type OpenInvoice struct {
	ID        string
	Period    string
	DueDate   time.Time
	Total     int64
	Paid      int64
	Status    billing.Status
	FeeNames  string
	ClassName string
	YearName  string
}

// Remaining adalah sisa tagihan.
func (o OpenInvoice) Remaining() int64 { return o.Total - o.Paid }

// listOpenInvoices mengembalikan tagihan siswa yang belum lunas dan tidak
// dibatalkan, di semua tahun ajaran, urut jatuh tempo.
func listOpenInvoices(ctx context.Context, q database.DBTX, schoolID, studentID string) ([]OpenInvoice, error) {
	rows, err := q.Query(ctx, `
		SELECT i.id, i.period, i.due_date, i.total_amount, `+paidSumSQL+`, i.status, coalesce(`+feeNamesSQL+`, ''), c.name, y.name
		FROM invoices i
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		JOIN academic_years y ON y.school_id = i.school_id AND y.id = i.academic_year_id
		WHERE i.school_id = $1 AND e.student_id = $2 AND i.voided_at IS NULL AND i.status <> 'lunas'
		ORDER BY i.due_date, i.period, i.created_at`, schoolID, studentID)
	if err != nil {
		return nil, mapError("list open invoices", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OpenInvoice, error) {
		var o OpenInvoice
		err := row.Scan(&o.ID, &o.Period, &o.DueDate, &o.Total, &o.Paid, &o.Status, &o.FeeNames, &o.ClassName, &o.YearName)
		return o, err
	})
	if err != nil {
		return nil, mapError("list open invoices", err)
	}
	return list, nil
}

// InvoiceDetail adalah tagihan beserta siswa, kelas, dan jumlah terbayar.
type InvoiceDetail struct {
	billing.Invoice
	StudentID   string
	StudentName string
	NIS         string
	ClassName   string
	YearName    string
	Paid        int64
}

// Remaining adalah sisa tagihan.
func (d InvoiceDetail) Remaining() int64 { return d.TotalAmount - d.Paid }

func getInvoiceDetail(ctx context.Context, q database.DBTX, schoolID, invoiceID string) (InvoiceDetail, error) {
	inv, err := billing.GetInvoice(ctx, q, schoolID, invoiceID)
	if err != nil {
		return InvoiceDetail{}, err
	}
	d := InvoiceDetail{Invoice: inv}
	err = q.QueryRow(ctx, `
		SELECT s.id, s.full_name, s.nis, c.name, y.name, `+paidSumSQL+`
		FROM invoices i
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN students s ON s.school_id = e.school_id AND s.id = e.student_id
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		JOIN academic_years y ON y.school_id = i.school_id AND y.id = i.academic_year_id
		WHERE i.school_id = $1 AND i.id = $2`, schoolID, invoiceID).
		Scan(&d.StudentID, &d.StudentName, &d.NIS, &d.ClassName, &d.YearName, &d.Paid)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvoiceDetail{}, billing.ErrInvoiceNotFound
	}
	if err != nil {
		return InvoiceDetail{}, mapError("get invoice detail", err)
	}
	return d, nil
}

// invoiceStudentID mengembalikan siswa pemilik tagihan.
func invoiceStudentID(ctx context.Context, q database.DBTX, schoolID, invoiceID string) (string, error) {
	if !database.IsUUID(invoiceID) {
		return "", billing.ErrInvoiceNotFound
	}
	var id string
	err := q.QueryRow(ctx, `SELECT e.student_id FROM invoices i
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		WHERE i.school_id = $1 AND i.id = $2`, schoolID, invoiceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", billing.ErrInvoiceNotFound
	}
	if err != nil {
		return "", mapError("get invoice student", err)
	}
	return id, nil
}

// recentBankAccounts mengembalikan rekening tujuan yang pernah dipakai
// sekolah, paling baru dulu. Dipakai sebagai saran isian sampai pengaturan
// rekening sekolah tersedia.
func recentBankAccounts(ctx context.Context, q database.DBTX, schoolID string, limit int) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT bank_account FROM payments
		WHERE school_id = $1 AND bank_account IS NOT NULL
		GROUP BY bank_account ORDER BY max(created_at) DESC LIMIT $2`, schoolID, limit)
	if err != nil {
		return nil, mapError("list bank accounts", err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, mapError("list bank accounts", err)
	}
	return list, nil
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
