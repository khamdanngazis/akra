// Package classes mengelola kelas per tahun ajaran beserta wali kelasnya.
package classes

import (
	"errors"
	"time"
)

// Batas tingkat kelas (SD 1–6, SMP 7–9, SMA/SMK 10–12).
const (
	MinGradeLevel = 1
	MaxGradeLevel = 12
)

var (
	// ErrNotFound dikembalikan jika kelas tidak ada atau milik sekolah lain.
	ErrNotFound = errors.New("classes: not found")
	// ErrAcademicYearNotFound dikembalikan jika tahun ajaran tidak ada atau
	// milik sekolah lain.
	ErrAcademicYearNotFound = errors.New("classes: academic year not found")
	// ErrTeacherNotFound dikembalikan jika wali kelas tidak ada atau milik
	// sekolah lain.
	ErrTeacherNotFound = errors.New("classes: homeroom teacher not found")
	// ErrDuplicateName dikembalikan jika nama kelas sudah dipakai di tahun
	// ajaran yang sama.
	ErrDuplicateName = errors.New("classes: class name already exists in this academic year")
	// ErrInvalidGradeLevel dikembalikan jika tingkat di luar 1–12.
	ErrInvalidGradeLevel = errors.New("classes: grade level must be between 1 and 12")
)

// Class adalah satu kelas pada satu tahun ajaran.
type Class struct {
	ID                string
	SchoolID          string
	AcademicYearID    string
	Name              string // mis. "1A"
	GradeLevel        int    // 1–12
	HomeroomTeacherID string // kosong jika belum ada wali kelas
	// HomeroomTeacherName hanya untuk tampilan (dari users.full_name).
	HomeroomTeacherName string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ClassInput adalah data untuk membuat kelas. Validasi isian pengguna
// dilakukan di service (T19); database tetap menolak tingkat di luar batas,
// nama ganda, serta tahun ajaran atau wali kelas dari sekolah lain.
type ClassInput struct {
	AcademicYearID    string
	Name              string
	GradeLevel        int
	HomeroomTeacherID string // kosong berarti tanpa wali kelas
}
