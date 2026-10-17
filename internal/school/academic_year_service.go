package school

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
)

const (
	maxAcademicYearNameLen = 20
	dateLayout             = "2006-01-02"
	entityAcademicYear     = "academic_year"
)

// Aksi audit untuk tahun ajaran.
const (
	ActionAcademicYearCreate   = "academic_year.create"
	ActionAcademicYearUpdate   = "academic_year.update"
	ActionAcademicYearActivate = "academic_year.activate"
)

// Actor adalah pengguna yang melakukan aksi. SchoolID selalu berasal dari
// sesi (auth.Identity), tidak pernah dari form atau URL.
type Actor struct {
	SchoolID string
	UserID   string
	IP       netip.Addr
}

// AcademicYearForm adalah isian form tahun ajaran apa adanya.
type AcademicYearForm struct {
	Name      string
	StartDate string // YYYY-MM-DD
	EndDate   string // YYYY-MM-DD
}

// ValidationError berisi pesan per field (dalam bahasa Indonesia, untuk
// ditampilkan di form).
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	return "school: invalid input: " + strings.Join(keys, ", ")
}

// AcademicYearService menjalankan aturan bisnis tahun ajaran.
type AcademicYearService struct {
	db database.DB
}

// NewAcademicYearService mengembalikan AcademicYearService.
func NewAcademicYearService(db database.DB) *AcademicYearService {
	return &AcademicYearService{db: db}
}

// List mengembalikan tahun ajaran sekolah, terbaru dulu.
func (s *AcademicYearService) List(ctx context.Context, schoolID string) ([]AcademicYear, error) {
	return ListAcademicYears(ctx, s.db, schoolID)
}

// Get mengembalikan satu tahun ajaran, atau ErrNotFound jika milik sekolah lain.
func (s *AcademicYearService) Get(ctx context.Context, schoolID, id string) (AcademicYear, error) {
	return GetAcademicYear(ctx, s.db, schoolID, id)
}

// Create memvalidasi form lalu membuat tahun ajaran nonaktif beserta auditnya.
func (s *AcademicYearService) Create(ctx context.Context, actor Actor, form AcademicYearForm) (AcademicYear, error) {
	in, err := validateAcademicYear(form)
	if err != nil {
		return AcademicYear{}, err
	}
	var created AcademicYear
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		y, err := CreateAcademicYear(ctx, tx, actor.SchoolID, in)
		if err != nil {
			return err
		}
		created = y
		return audit.Record(ctx, tx, auditEntry(actor, ActionAcademicYearCreate, y.ID, nil, snapshot(y)))
	})
	if err != nil {
		return AcademicYear{}, fieldError(err)
	}
	return created, nil
}

// Update memvalidasi form lalu mengubah nama dan tanggal beserta auditnya.
func (s *AcademicYearService) Update(ctx context.Context, actor Actor, id string, form AcademicYearForm) (AcademicYear, error) {
	in, err := validateAcademicYear(form)
	if err != nil {
		return AcademicYear{}, err
	}
	var updated AcademicYear
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := GetAcademicYear(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		y, err := UpdateAcademicYear(ctx, tx, actor.SchoolID, id, in)
		if err != nil {
			return err
		}
		updated = y
		return audit.Record(ctx, tx, auditEntry(actor, ActionAcademicYearUpdate, y.ID, snapshot(before), snapshot(y)))
	})
	if err != nil {
		return AcademicYear{}, fieldError(err)
	}
	return updated, nil
}

// Activate menjadikan id tahun ajaran aktif sekolah. changed bernilai false
// jika tahun itu sudah aktif (tidak ada perubahan dan tidak ada audit).
func (s *AcademicYearService) Activate(ctx context.Context, actor Actor, id string) (year AcademicYear, changed bool, err error) {
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		target, err := GetAcademicYear(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		year = target
		if target.IsActive {
			return nil
		}
		previous := ""
		if prev, err := GetActiveAcademicYear(ctx, tx, actor.SchoolID); err == nil {
			previous = prev.ID
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := SetActive(ctx, tx, actor.SchoolID, id); err != nil {
			return err
		}
		year.IsActive, changed = true, true
		return audit.Record(ctx, tx, auditEntry(actor, ActionAcademicYearActivate, id,
			map[string]any{"active_academic_year_id": nullable(previous)},
			map[string]any{"active_academic_year_id": id}))
	})
	if err != nil {
		return AcademicYear{}, false, err
	}
	return year, changed, nil
}

// validateAcademicYear memeriksa isian form dan mengubahnya menjadi input
// repository.
func validateAcademicYear(f AcademicYearForm) (AcademicYearInput, error) {
	fields := map[string]string{}
	name := strings.TrimSpace(f.Name)
	switch {
	case name == "":
		fields["name"] = "Nama tahun ajaran wajib diisi."
	case utf8.RuneCountInString(name) > maxAcademicYearNameLen:
		fields["name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxAcademicYearNameLen)
	}

	start, startErr := time.Parse(dateLayout, strings.TrimSpace(f.StartDate))
	if startErr != nil {
		fields["start_date"] = "Tanggal mulai wajib diisi dengan format yang benar."
	}
	end, endErr := time.Parse(dateLayout, strings.TrimSpace(f.EndDate))
	if endErr != nil {
		fields["end_date"] = "Tanggal selesai wajib diisi dengan format yang benar."
	}
	if startErr == nil && endErr == nil && !end.After(start) {
		fields["end_date"] = "Tanggal selesai harus setelah tanggal mulai."
	}

	if len(fields) > 0 {
		return AcademicYearInput{}, &ValidationError{Fields: fields}
	}
	return AcademicYearInput{Name: name, StartDate: start, EndDate: end}, nil
}

// fieldError mengubah error database yang berkaitan dengan isian form
// menjadi ValidationError.
func fieldError(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateName):
		return &ValidationError{Fields: map[string]string{"name": "Nama tahun ajaran sudah dipakai."}}
	case errors.Is(err, ErrInvalidDates):
		return &ValidationError{Fields: map[string]string{"end_date": "Tanggal selesai harus setelah tanggal mulai."}}
	}
	return err
}

type academicYearSnapshot struct {
	Name      string `json:"name"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	IsActive  bool   `json:"is_active"`
}

func snapshot(y AcademicYear) academicYearSnapshot {
	return academicYearSnapshot{
		Name:      y.Name,
		StartDate: y.StartDate.Format(dateLayout),
		EndDate:   y.EndDate.Format(dateLayout),
		IsActive:  y.IsActive,
	}
}

func auditEntry(actor Actor, action, entityID string, before, after any) audit.Entry {
	return audit.Entry{
		SchoolID:   actor.SchoolID,
		UserID:     actor.UserID,
		Action:     action,
		EntityType: entityAcademicYear,
		EntityID:   entityID,
		Before:     before,
		After:      after,
		IP:         actor.IP,
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
