// Package payments mencatat pembayaran tagihan. Pembayaran tidak pernah
// diubah atau dihapus (dijaga trigger database); pembatalan dicatat terpisah
// (T38). Semua nominal adalah bilangan bulat rupiah (int64).
package payments

import (
	"errors"
	"time"
)

// Method adalah cara bayar.
type Method string

const (
	MethodCash     Method = "tunai"
	MethodTransfer Method = "transfer"
)

// Batas panjang teks isian bebas.
const (
	MaxBankAccountLen = 100
	MaxReferenceLen   = 100
	MaxNoteLen        = 500
)

var (
	// ErrInvalidAmount dikembalikan untuk nominal 0 atau negatif.
	ErrInvalidAmount = errors.New("payments: amount must be positive")
	// ErrInvalidMethod dikembalikan untuk metode selain tunai dan transfer.
	ErrInvalidMethod = errors.New("payments: method must be tunai or transfer")
	// ErrBankAccountRequired dikembalikan untuk transfer tanpa rekening tujuan.
	ErrBankAccountRequired = errors.New("payments: bank account is required for transfer")
	// ErrPaidOnRequired dikembalikan jika tanggal bayar tidak diisi.
	ErrPaidOnRequired = errors.New("payments: paid on date is required")
	// ErrPaidOnFuture dikembalikan jika tanggal bayar setelah hari ini.
	ErrPaidOnFuture = errors.New("payments: paid on date is in the future")
	// ErrTextTooLong dikembalikan jika rekening, referensi, atau catatan
	// melebihi batas panjang.
	ErrTextTooLong = errors.New("payments: text field too long")
	// ErrInvoiceVoided dikembalikan jika tagihan sudah dibatalkan.
	ErrInvoiceVoided = errors.New("payments: invoice is voided")
)

// Payment adalah satu pembayaran untuk satu tagihan.
type Payment struct {
	ID            string
	SchoolID      string
	InvoiceID     string
	Amount        int64
	PaidOn        time.Time
	Method        Method
	BankAccount   string
	Reference     string
	Note          string
	ReceiptNumber string
	RecordedBy    string
	CreatedAt     time.Time
}

// Request adalah permintaan mencatat pembayaran satu tagihan.
type Request struct {
	InvoiceID   string
	Amount      int64
	PaidOn      time.Time // tanggal (Asia/Jakarta), wajib
	Method      Method
	BankAccount string // wajib untuk transfer
	Reference   string
	Note        string
}
