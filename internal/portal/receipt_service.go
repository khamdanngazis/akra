package portal

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/payments"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// ParentReceipt mengembalikan data kuitansi pembayaran paymentID jika
// pembayaran itu milik anak akun yang login, lalu mencatat unduhan ke audit
// (receipt.download_parent). Kepemilikan dicek dua kali: di query
// pembayaran (join ke wali akun) dan lewat checkChild. Pembayaran siswa
// lain atau sekolah lain: ErrNotFound. Data kuitansi sama dengan yang
// dicetak bendahara (payments.GetReceipt).
func (s *Service) ParentReceipt(ctx context.Context, actor school.Actor, paymentID string) (payments.Receipt, error) {
	if !database.IsUUID(actor.SchoolID) || !database.IsUUID(actor.UserID) || !database.IsUUID(paymentID) {
		return payments.Receipt{}, ErrNotFound
	}
	var studentID string
	err := s.db.QueryRow(ctx, `
		SELECT e.student_id
		FROM payments p
		JOIN invoices i ON i.school_id = p.school_id AND i.id = p.invoice_id
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN student_guardians sg ON sg.school_id = e.school_id AND sg.student_id = e.student_id
		JOIN users u ON u.school_id = sg.school_id AND u.guardian_id = sg.guardian_id
		WHERE p.school_id = $1 AND p.id = $2 AND u.id = $3`, actor.SchoolID, paymentID, actor.UserID).Scan(&studentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.Receipt{}, ErrNotFound
	}
	if err != nil {
		return payments.Receipt{}, fmt.Errorf("portal: find payment: %w", err)
	}
	if err := s.checkChild(ctx, actor.SchoolID, actor.UserID, studentID); err != nil {
		return payments.Receipt{}, err
	}

	var rc payments.Receipt
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if rc, err = payments.GetReceipt(ctx, tx, actor.SchoolID, paymentID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: audit.ActionReceiptDownloadParent, EntityType: "payment", EntityID: paymentID,
			After: map[string]any{"receipt_number": rc.Number, "voided": rc.Voided},
		})
	})
	if errors.Is(err, payments.ErrPaymentNotFound) {
		return payments.Receipt{}, ErrNotFound
	}
	return rc, err
}
