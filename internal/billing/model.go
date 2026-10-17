// Package billing mengelola jenis tagihan dan tagihan siswa. Semua nominal
// adalah bilangan bulat rupiah (int64).
package billing

import (
	"errors"
	"time"
)

var (
	// ErrFeeTypeNotFound dikembalikan jika jenis tagihan tidak ada atau milik
	// sekolah lain.
	ErrFeeTypeNotFound = errors.New("billing: fee type not found")
	// ErrDuplicateFeeTypeName dikembalikan jika nama sudah dipakai di sekolah.
	ErrDuplicateFeeTypeName = errors.New("billing: fee type name already exists")
	// ErrNegativeAmount dikembalikan untuk nominal negatif.
	ErrNegativeAmount = errors.New("billing: amount must not be negative")
	// ErrInvalidDueDay dikembalikan jika tanggal jatuh tempo di luar 1–28.
	ErrInvalidDueDay = errors.New("billing: due day must be between 1 and 28")
)

// Batas tanggal jatuh tempo default (1–28, menghindari masalah Februari).
const (
	MinDueDay = 1
	MaxDueDay = 28
)

// FeeType adalah jenis tagihan, mis. SPP atau uang pangkal.
//
// Nominal default bisa berbeda per kelas (mis. SPP kelas 1 dan kelas 6).
// Itu ditangani lewat nominal per item tagihan (T30), bukan dengan membuat
// banyak jenis tagihan.
type FeeType struct {
	ID            string
	SchoolID      string
	Name          string
	DefaultAmount int64 // rupiah
	DefaultDueDay int   // 0 jika tidak diisi
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// FeeTypeInput adalah data jenis tagihan yang sudah divalidasi service.
type FeeTypeInput struct {
	Name          string
	DefaultAmount int64
	DefaultDueDay int // 0 berarti tidak diisi
}

// Status tagihan yang disimpan. "Terlambat" bukan status tersimpan; ia
// dihitung saat ditampilkan (lihat IsOverdue, T31).
const (
	StatusUnpaid  = "belum_bayar"
	StatusPartial = "sebagian"
	StatusPaid    = "lunas"
)

var (
	// ErrInvoiceNotFound dikembalikan jika tagihan tidak ada atau milik sekolah lain.
	ErrInvoiceNotFound = errors.New("billing: invoice not found")
	// ErrEnrollmentNotFound dikembalikan jika enrollment tidak ada atau milik sekolah lain.
	ErrEnrollmentNotFound = errors.New("billing: enrollment not found")
	// ErrDuplicateItem dikembalikan jika jenis tagihan yang sama sudah ditagihkan
	// ke enrollment yang sama pada periode yang sama.
	ErrDuplicateItem = errors.New("billing: fee type already billed for this enrollment and period")
	// ErrInvalidPeriod dikembalikan untuk periode yang bukan YYYY-MM.
	ErrInvalidPeriod = errors.New("billing: period must be YYYY-MM")
	// ErrNoItems dikembalikan untuk tagihan tanpa item.
	ErrNoItems = errors.New("billing: invoice needs at least one item")
	// ErrFeeTypeInactive dikembalikan jika jenis tagihan nonaktif dipakai
	// untuk tagihan baru.
	ErrFeeTypeInactive = errors.New("billing: fee type is inactive")
	// ErrDueDateRequired dikembalikan jika jatuh tempo tidak diisi dan tidak
	// ada jenis tagihan dengan jatuh tempo default.
	ErrDueDateRequired = errors.New("billing: due date is required")
	// ErrAmountTooLarge dikembalikan jika nominal atau total melebihi MaxAmount.
	ErrAmountTooLarge = errors.New("billing: amount too large")
)

// Invoice adalah tagihan satu enrollment pada satu periode.
type Invoice struct {
	ID             string
	SchoolID       string
	AcademicYearID string
	EnrollmentID   string
	Period         string // YYYY-MM
	DueDate        time.Time
	TotalAmount    int64
	Status         string // StatusUnpaid, StatusPartial, atau StatusPaid
	VoidedAt       *time.Time
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Items          []InvoiceItem
}

// InvoiceItem adalah satu baris tagihan.
type InvoiceItem struct {
	ID          string
	FeeTypeID   string
	FeeTypeName string
	Description string
	Amount      int64
}

// ItemRequest adalah item yang diminta saat membuat tagihan. Amount nil
// berarti memakai nominal default jenis tagihan; diisi untuk nominal khusus
// per siswa (mis. SPP kelas 6 berbeda dengan kelas 1).
type ItemRequest struct {
	FeeTypeID   string
	Description string
	Amount      *int64
}
