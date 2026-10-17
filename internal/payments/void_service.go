package payments

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

// Status pembatalan.
const (
	VoidPending  = "pending"
	VoidApproved = "approved"
	VoidRejected = "rejected"
)

// Aksi audit pembatalan.
const (
	ActionVoidRequest = audit.ActionVoidRequest
	ActionVoidApprove = audit.ActionVoidApprove
	ActionVoidReject  = audit.ActionVoidReject
)

// MaxVoidReasonLen adalah panjang maksimal alasan pembatalan.
const MaxVoidReasonLen = 500

// DefaultVoidApprovalThreshold dipakai jika sekolah belum mengatur
// void_approval_threshold: 0 berarti setiap pembatalan perlu persetujuan.
const DefaultVoidApprovalThreshold int64 = 0

var (
	// ErrVoidReasonRequired dikembalikan jika alasan kosong.
	ErrVoidReasonRequired = errors.New("payments: void reason is required")
	// ErrVoidReasonTooLong dikembalikan jika alasan melebihi MaxVoidReasonLen.
	ErrVoidReasonTooLong = errors.New("payments: void reason too long")
	// ErrVoidPeriodClosed dikembalikan jika tanggal bayar tidak di bulan
	// berjalan (Asia/Jakarta).
	ErrVoidPeriodClosed = errors.New("payments: payment can only be voided in the month it was paid")
	// ErrAlreadyVoided dikembalikan jika pembayaran sudah dibatalkan atau
	// pembatalannya sedang menunggu persetujuan.
	ErrAlreadyVoided = errors.New("payments: payment already voided or pending")
	// ErrVoidNotFound dikembalikan jika pembatalan tidak ada atau milik
	// sekolah lain.
	ErrVoidNotFound = errors.New("payments: void not found")
	// ErrVoidNotPending dikembalikan jika pembatalan sudah diputuskan.
	ErrVoidNotPending = errors.New("payments: void already decided")
	// ErrSelfApproval dikembalikan jika yang memutuskan adalah pengaju.
	ErrSelfApproval = errors.New("payments: requester cannot decide own void")
)

// Void adalah pembatalan satu pembayaran.
type Void struct {
	ID               string
	PaymentID        string
	Reason           string
	RequestedBy      string
	RequiresApproval bool
	Status           string
	DecidedBy        string
	DecidedAt        *time.Time
	CreatedAt        time.Time
}

// VoidService menjalankan aturan pembatalan pembayaran. Peran (hanya
// bendahara yang mengajukan, hanya kepala sekolah yang memutuskan) dicek di
// handler; service menegakkan aturan data.
type VoidService struct {
	db database.DB
}

// NewVoidService mengembalikan VoidService.
func NewVoidService(db database.DB) *VoidService {
	return &VoidService{db: db}
}

// Request mengajukan pembatalan pembayaran:
//
//   - alasan wajib (maksimal 500 karakter);
//   - hanya untuk pembayaran yang tanggal bayarnya di bulan berjalan
//     (Asia/Jakarta);
//   - pembayaran yang sudah dibatalkan atau sedang menunggu persetujuan
//     ditolak;
//   - nominal di atas void_approval_threshold sekolah menunggu persetujuan
//     kepala sekolah; selain itu langsung disetujui dan status tagihan
//     dihitung ulang dalam transaksi yang sama.
//
// Baris payments tidak diubah.
func (s *VoidService) Request(ctx context.Context, actor school.Actor, paymentID, reason string) (Void, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return Void{}, ErrVoidReasonRequired
	case utf8.RuneCountInString(reason) > MaxVoidReasonLen:
		return Void{}, ErrVoidReasonTooLong
	}
	if !database.IsUUID(paymentID) {
		return Void{}, ErrPaymentNotFound
	}
	var v Void
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			amount    int64
			paidOn    time.Time
			invoiceID string
		)
		err := tx.QueryRow(ctx, `SELECT amount, paid_on, invoice_id FROM payments WHERE school_id = $1 AND id = $2`,
			actor.SchoolID, paymentID).Scan(&amount, &paidOn, &invoiceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPaymentNotFound
		}
		if err != nil {
			return mapError("get payment", err)
		}
		// Kunci tagihan: pembatalan dan pembayaran baru untuk tagihan yang
		// sama diproses berurutan.
		if _, _, _, err := invoiceForPayment(ctx, tx, actor.SchoolID, invoiceID); err != nil {
			return err
		}
		if !sameMonth(paidOn, clock.Today()) {
			return ErrVoidPeriodClosed
		}
		threshold, err := settings.Int64(ctx, tx, actor.SchoolID, settings.VoidApprovalThreshold, DefaultVoidApprovalThreshold)
		if err != nil {
			return err
		}
		requires := amount > threshold
		status := VoidApproved
		if requires {
			status = VoidPending
		}
		rows, err := tx.Query(ctx, `
			INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status, decided_at)
			VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $5 THEN NULL ELSE now() END)
			RETURNING `+voidColumns, actor.SchoolID, paymentID, reason, actor.UserID, requires, status)
		if err != nil {
			return mapVoidError("insert void", err)
		}
		if v, err = pgx.CollectExactlyOneRow(rows, scanVoid); err != nil {
			return mapVoidError("insert void", err)
		}
		if !requires {
			if _, err := billing.RecalculateInvoice(ctx, tx, actor.SchoolID, invoiceID); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionVoidRequest, EntityType: "payment", EntityID: paymentID,
			After: map[string]any{"void_id": v.ID, "amount": amount, "reason": reason, "status": status, "requires_approval": requires},
		})
	})
	return v, err
}

// Approve menyetujui pembatalan yang menunggu, lalu menghitung ulang status
// tagihan dalam transaksi yang sama. Pengaju tidak boleh menyetujui
// pengajuannya sendiri.
func (s *VoidService) Approve(ctx context.Context, actor school.Actor, voidID string) (Void, error) {
	return s.decide(ctx, actor, voidID, VoidApproved, ActionVoidApprove)
}

// Reject menolak pembatalan yang menunggu. Pembayaran tetap aktif dan bisa
// diajukan pembatalan lagi.
func (s *VoidService) Reject(ctx context.Context, actor school.Actor, voidID string) (Void, error) {
	return s.decide(ctx, actor, voidID, VoidRejected, ActionVoidReject)
}

func (s *VoidService) decide(ctx context.Context, actor school.Actor, voidID, status, action string) (Void, error) {
	if !database.IsUUID(voidID) {
		return Void{}, ErrVoidNotFound
	}
	var v Void
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var invoiceID string
		err := tx.QueryRow(ctx, `SELECT p.invoice_id FROM payment_voids v
			JOIN payments p ON p.school_id = v.school_id AND p.id = v.payment_id
			WHERE v.school_id = $1 AND v.id = $2`, actor.SchoolID, voidID).Scan(&invoiceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVoidNotFound
		}
		if err != nil {
			return mapVoidError("get void", err)
		}
		// Urutan kunci sama dengan Request: tagihan dulu, lalu pembatalan.
		if _, _, _, err := invoiceForPayment(ctx, tx, actor.SchoolID, invoiceID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+voidColumns+` FROM payment_voids WHERE school_id = $1 AND id = $2 FOR UPDATE`, actor.SchoolID, voidID)
		if err != nil {
			return mapVoidError("lock void", err)
		}
		if v, err = pgx.CollectExactlyOneRow(rows, scanVoid); err != nil {
			return mapVoidError("lock void", err)
		}
		switch {
		case v.Status != VoidPending:
			return ErrVoidNotPending
		case v.RequestedBy == actor.UserID:
			return ErrSelfApproval
		}
		rows, err = tx.Query(ctx, `UPDATE payment_voids SET status = $3, decided_by = $4, decided_at = now()
			WHERE school_id = $1 AND id = $2 RETURNING `+voidColumns, actor.SchoolID, voidID, status, actor.UserID)
		if err != nil {
			return mapVoidError("decide void", err)
		}
		if v, err = pgx.CollectExactlyOneRow(rows, scanVoid); err != nil {
			return mapVoidError("decide void", err)
		}
		if status == VoidApproved {
			if _, err := billing.RecalculateInvoice(ctx, tx, actor.SchoolID, invoiceID); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: action, EntityType: "payment", EntityID: v.PaymentID,
			Before: map[string]any{"void_id": v.ID, "status": VoidPending},
			After:  map[string]any{"void_id": v.ID, "status": status},
		})
	})
	return v, err
}

func sameMonth(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month()
}

const voidColumns = `id, payment_id, reason, requested_by, requires_approval, status, coalesce(decided_by::text, ''), decided_at, created_at`

func scanVoid(row pgx.CollectableRow) (Void, error) {
	var v Void
	err := row.Scan(&v.ID, &v.PaymentID, &v.Reason, &v.RequestedBy, &v.RequiresApproval, &v.Status, &v.DecidedBy, &v.DecidedAt, &v.CreatedAt)
	return v, err
}

func mapVoidError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrVoidNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "payment_voids_payment_active_key" {
		return ErrAlreadyVoided
	}
	return mapError(op, err)
}

// PendingVoid adalah pengajuan pembatalan yang menunggu persetujuan beserta
// data pembayarannya.
type PendingVoid struct {
	Void
	Amount        int64
	PaidOn        time.Time
	Method        Method
	InvoiceID     string
	Period        string
	StudentName   string
	NIS           string
	RequesterName string
}

// ListPending mengembalikan pengajuan pembatalan yang menunggu persetujuan,
// paling lama dulu.
func (s *VoidService) ListPending(ctx context.Context, schoolID string) ([]PendingVoid, error) {
	rows, err := s.db.Query(ctx, `
		SELECT v.id, v.payment_id, v.reason, v.requested_by, v.requires_approval, v.status, '', v.decided_at, v.created_at,
		       p.amount, p.paid_on, p.method, i.id, i.period, st.full_name, st.nis, u.full_name
		FROM payment_voids v
		JOIN payments p ON p.school_id = v.school_id AND p.id = v.payment_id
		JOIN invoices i ON i.school_id = p.school_id AND i.id = p.invoice_id
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		JOIN users u ON u.school_id = v.school_id AND u.id = v.requested_by
		WHERE v.school_id = $1 AND v.status = 'pending'
		ORDER BY v.created_at, v.id`, schoolID)
	if err != nil {
		return nil, mapVoidError("list pending voids", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingVoid, error) {
		var p PendingVoid
		err := row.Scan(&p.ID, &p.PaymentID, &p.Reason, &p.RequestedBy, &p.RequiresApproval, &p.Status, &p.DecidedBy, &p.DecidedAt, &p.CreatedAt,
			&p.Amount, &p.PaidOn, &p.Method, &p.InvoiceID, &p.Period, &p.StudentName, &p.NIS, &p.RequesterName)
		return p, err
	})
	if err != nil {
		return nil, mapVoidError("list pending voids", err)
	}
	return list, nil
}

// voidsByInvoice mengembalikan pembatalan terakhir tiap pembayaran satu
// tagihan (yang menunggu atau disetujui diutamakan atas yang ditolak).
func voidsByInvoice(ctx context.Context, q database.DBTX, schoolID, invoiceID string) (map[string]Void, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (v.payment_id) v.id, v.payment_id, v.reason, v.requested_by, v.requires_approval, v.status,
		       coalesce(v.decided_by::text, ''), v.decided_at, v.created_at
		FROM payment_voids v JOIN payments p ON p.school_id = v.school_id AND p.id = v.payment_id
		WHERE v.school_id = $1 AND p.invoice_id = $2
		ORDER BY v.payment_id, (v.status = 'rejected'), v.created_at DESC`, schoolID, invoiceID)
	if err != nil {
		return nil, mapVoidError("list voids", err)
	}
	list, err := pgx.CollectRows(rows, scanVoid)
	if err != nil {
		return nil, mapVoidError("list voids", err)
	}
	out := make(map[string]Void, len(list))
	for _, v := range list {
		out[v.PaymentID] = v
	}
	return out, nil
}
