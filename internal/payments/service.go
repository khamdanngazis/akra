package payments

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// ActionPaymentCreate adalah aksi audit pencatatan pembayaran.
const ActionPaymentCreate = audit.ActionPaymentCreate

// Service menjalankan aturan bisnis pembayaran.
type Service struct {
	db database.DB
}

// NewService mengembalikan Service.
func NewService(db database.DB) *Service {
	return &Service{db: db}
}

// Record mencatat satu pembayaran dalam transaksinya sendiri.
func (s *Service) Record(ctx context.Context, actor school.Actor, req Request) (Payment, error) {
	var p Payment
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		p, err = RecordPayment(ctx, tx, actor, req)
		return err
	})
	return p, err
}

// ListByInvoice mengembalikan pembayaran satu tagihan.
func (s *Service) ListByInvoice(ctx context.Context, schoolID, invoiceID string) ([]Payment, error) {
	return ListByInvoice(ctx, s.db, schoolID, invoiceID)
}

// RecordPayment mencatat pembayaran di dalam transaksi tx, memperbarui
// status tagihan, dan menulis audit. Form pembayaran beberapa tagihan (T35)
// memanggilnya berkali-kali dalam satu transaksi: jika satu gagal, semua
// dibatalkan.
//
//   - Nominal harus > 0 dan tidak melebihi sisa tagihan (total dikurangi
//     pembayaran aktif); lebih dari sisa ditolak dengan billing.ErrOverpayment.
//   - Tagihan dikunci selama transaksi sehingga dua pembayaran bersamaan
//     tidak bisa sama-sama lolos pemeriksaan sisa.
//   - Status tagihan dihitung ulang dengan billing.RecalculateInvoice di
//     transaksi yang sama.
func RecordPayment(ctx context.Context, tx pgx.Tx, actor school.Actor, req Request) (Payment, error) {
	req, err := normalize(req)
	if err != nil {
		return Payment{}, err
	}
	total, paid, voided, err := invoiceForPayment(ctx, tx, actor.SchoolID, req.InvoiceID)
	if err != nil {
		return Payment{}, err
	}
	if voided {
		return Payment{}, ErrInvoiceVoided
	}
	if req.Amount > total-paid {
		return Payment{}, billing.ErrOverpayment
	}
	// Nomor kuitansi mengikuti tahun pencatatan (Asia/Jakarta).
	receipt, err := NextReceiptNumber(ctx, tx, actor.SchoolID, clock.Today().Year())
	if err != nil {
		return Payment{}, err
	}
	p, err := insertPayment(ctx, tx, actor.SchoolID, actor.UserID, receipt, req)
	if err != nil {
		return Payment{}, err
	}
	status, err := billing.RecalculateInvoice(ctx, tx, actor.SchoolID, req.InvoiceID)
	if err != nil {
		return Payment{}, err
	}
	err = audit.Record(ctx, tx, audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: ActionPaymentCreate, EntityType: "payment", EntityID: p.ID,
		After: paymentJSON{
			InvoiceID: p.InvoiceID, Amount: p.Amount, PaidOn: p.PaidOn.Format("2006-01-02"), Method: p.Method,
			BankAccount: p.BankAccount, Reference: p.Reference, InvoiceStatus: status,
		},
	})
	return p, err
}

// normalize merapikan dan memvalidasi isian tanpa membaca database.
func normalize(r Request) (Request, error) {
	r.BankAccount = strings.TrimSpace(r.BankAccount)
	r.Reference = strings.TrimSpace(r.Reference)
	r.Note = strings.TrimSpace(r.Note)
	if !r.PaidOn.IsZero() {
		r.PaidOn = clock.DateOf(r.PaidOn)
	}
	switch {
	case r.Amount <= 0:
		return r, ErrInvalidAmount
	case r.Amount > billing.MaxAmount:
		return r, billing.ErrAmountTooLarge
	case r.Method != MethodCash && r.Method != MethodTransfer:
		return r, ErrInvalidMethod
	case r.PaidOn.IsZero():
		return r, ErrPaidOnRequired
	case r.PaidOn.After(clock.Today()):
		return r, ErrPaidOnFuture
	case utf8.RuneCountInString(r.BankAccount) > MaxBankAccountLen,
		utf8.RuneCountInString(r.Reference) > MaxReferenceLen,
		utf8.RuneCountInString(r.Note) > MaxNoteLen:
		return r, ErrTextTooLong
	}
	if r.Method == MethodTransfer && r.BankAccount == "" {
		return r, ErrBankAccountRequired
	}
	if r.Method == MethodCash {
		// Rekening dan referensi hanya bermakna untuk transfer.
		r.BankAccount, r.Reference = "", ""
	}
	return r, nil
}

// paymentJSON adalah isi audit pembayaran. Catatan tidak disimpan karena
// bisa berisi data pribadi.
type paymentJSON struct {
	InvoiceID     string         `json:"invoice_id"`
	Amount        int64          `json:"amount"`
	PaidOn        string         `json:"paid_on"`
	Method        Method         `json:"method"`
	BankAccount   string         `json:"bank_account,omitempty"`
	Reference     string         `json:"reference,omitempty"`
	InvoiceStatus billing.Status `json:"invoice_status"`
}
