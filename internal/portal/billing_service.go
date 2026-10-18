package portal

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/billing"
	"akra/internal/payments"
)

// ChildInvoice adalah tagihan anak untuk portal.
type ChildInvoice struct {
	ID        string
	Period    string
	FeeNames  string
	ClassName string
	YearName  string
	DueDate   time.Time
	Total     int64
	Paid      int64 // pembayaran aktif (billing.ActivePaidSQL)
	Remaining int64
	Status    billing.Status
	Overdue   bool // billing.IsOverdue
}

// ChildPayment adalah pembayaran anak untuk portal. Pembatalan yang belum
// disetujui tidak terlihat (tampil sebagai pembayaran biasa). Catatan,
// rekening, pencatat, dan bukti transfer sengaja tidak ada.
type ChildPayment struct {
	ID            string
	InvoiceID     string
	Period        string
	FeeNames      string
	PaidOn        time.Time
	Amount        int64
	Method        payments.Method
	ReceiptNumber string
	Voided        bool
	VoidReason    string
}

// ChildBilling adalah tagihan dan riwayat pembayaran satu anak.
type ChildBilling struct {
	Child    ChildDetail
	Invoices []ChildInvoice
	Payments []ChildPayment
	// TotalRemaining: sisa semua tagihan yang belum lunas. OverdueRemaining:
	// sisa tagihan yang terlambat, sama dengan billing.ListArrears untuk
	// siswa ini.
	TotalRemaining   int64
	OverdueRemaining int64
}

// GetChildBilling mengembalikan tagihan (semua tahun ajaran, tanpa tagihan
// yang dibatalkan) dan riwayat pembayaran anak studentID milik akun userID.
// today (Asia/Jakarta) menentukan status terlambat.
func (s *Service) GetChildBilling(ctx context.Context, schoolID, userID, studentID string, today time.Time) (ChildBilling, error) {
	if err := s.checkChild(ctx, schoolID, userID, studentID); err != nil {
		return ChildBilling{}, err
	}
	var out ChildBilling
	if err := s.db.QueryRow(ctx, `SELECT id, nis, full_name FROM students WHERE school_id = $1 AND id = $2`,
		schoolID, studentID).Scan(&out.Child.ID, &out.Child.NIS, &out.Child.FullName); err != nil {
		return ChildBilling{}, fmt.Errorf("portal: get child: %w", err)
	}

	// Filter siswa ada di query (bukan hanya di checkChild).
	rows, err := s.db.Query(ctx, `
		SELECT i.id, i.period, coalesce(`+billing.FeeNamesSQL+`, ''), c.name, y.name, i.due_date, i.total_amount,
		       `+billing.ActivePaidSQL+`, i.status
		FROM invoices i
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		JOIN academic_years y ON y.school_id = i.school_id AND y.id = i.academic_year_id
		WHERE i.school_id = $1 AND e.student_id = $2 AND i.voided_at IS NULL
		ORDER BY i.due_date DESC, i.period DESC, i.created_at DESC`, schoolID, studentID)
	if err != nil {
		return ChildBilling{}, fmt.Errorf("portal: list invoices: %w", err)
	}
	out.Invoices, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChildInvoice, error) {
		var x ChildInvoice
		err := row.Scan(&x.ID, &x.Period, &x.FeeNames, &x.ClassName, &x.YearName, &x.DueDate, &x.Total, &x.Paid, &x.Status)
		return x, err
	})
	if err != nil {
		return ChildBilling{}, fmt.Errorf("portal: list invoices: %w", err)
	}
	for i := range out.Invoices {
		inv := &out.Invoices[i]
		inv.Remaining = inv.Total - inv.Paid
		inv.Overdue = billing.IsOverdue(inv.Status, inv.DueDate, today)
		if inv.Status != billing.StatusPaid {
			out.TotalRemaining += inv.Remaining
			if inv.Overdue {
				out.OverdueRemaining += inv.Remaining
			}
		}
	}

	rows, err = s.db.Query(ctx, `
		SELECT p.id, i.id, i.period, coalesce(`+billing.FeeNamesSQL+`, ''), p.paid_on, p.amount, p.method, p.receipt_number,
		       v.id IS NOT NULL, coalesce(v.reason, '')
		FROM payments p
		JOIN invoices i ON i.school_id = p.school_id AND i.id = p.invoice_id
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		LEFT JOIN payment_voids v ON v.payment_id = p.id AND v.status = 'approved'
		WHERE p.school_id = $1 AND e.student_id = $2
		ORDER BY p.paid_on DESC, p.created_at DESC, p.id`, schoolID, studentID)
	if err != nil {
		return ChildBilling{}, fmt.Errorf("portal: list payments: %w", err)
	}
	out.Payments, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChildPayment, error) {
		var x ChildPayment
		err := row.Scan(&x.ID, &x.InvoiceID, &x.Period, &x.FeeNames, &x.PaidOn, &x.Amount, &x.Method, &x.ReceiptNumber, &x.Voided, &x.VoidReason)
		return x, err
	})
	if err != nil {
		return ChildBilling{}, fmt.Errorf("portal: list payments: %w", err)
	}
	return out, nil
}
