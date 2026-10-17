// Package students mengelola data pribadi siswa. Keterikatan siswa ke kelas
// ada di enrollments (T21).
//
// Data siswa adalah data pribadi: jangan tulis NIS, nama, atau tanggal lahir
// ke log.
package students

import (
	"errors"
	"strings"
	"time"
)

// Status siswa.
const (
	StatusActive      = "aktif"
	StatusTransferred = "pindah"
	StatusGraduated   = "lulus"
	StatusLeft        = "keluar"
)

// Statuses berisi semua status yang valid.
var Statuses = []string{StatusActive, StatusTransferred, StatusGraduated, StatusLeft}

// Jenis kelamin.
const (
	GenderMale   = "L"
	GenderFemale = "P"
)

var (
	// ErrNotFound dikembalikan jika siswa tidak ada atau milik sekolah lain.
	ErrNotFound = errors.New("students: not found")
	// ErrDuplicateNIS dikembalikan jika NIS sudah dipakai di sekolah yang sama.
	ErrDuplicateNIS = errors.New("students: NIS already exists in this school")
	// ErrInvalidNIS dikembalikan jika NIS kosong atau berawalan/berakhiran spasi.
	ErrInvalidNIS = errors.New("students: invalid NIS")
	// ErrInvalidGender dikembalikan jika jenis kelamin bukan L, P, atau kosong.
	ErrInvalidGender = errors.New("students: gender must be L, P, or empty")
	// ErrInvalidStatus dikembalikan jika status tidak dikenal.
	ErrInvalidStatus = errors.New("students: unknown status")
)

// Student adalah data pribadi satu siswa.
type Student struct {
	ID        string
	SchoolID  string
	NIS       string
	FullName  string
	BirthDate *time.Time // nil jika tidak diisi
	Gender    string     // GenderMale, GenderFemale, atau "" jika tidak diisi
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// StudentInput adalah data untuk membuat atau mengubah siswa. Status kosong
// berarti StatusActive.
type StudentInput struct {
	NIS       string
	FullName  string
	BirthDate *time.Time
	Gender    string
	Status    string
}

// NormalizeNIS membuang spasi di awal dan akhir NIS. Nol di depan tetap
// dipertahankan.
func NormalizeNIS(nis string) string {
	return strings.TrimSpace(nis)
}
