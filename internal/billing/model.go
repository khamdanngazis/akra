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
