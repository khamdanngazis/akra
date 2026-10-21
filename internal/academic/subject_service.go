package academic

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit mapel.
const (
	ActionSubjectCreate     = "subject.create"
	ActionSubjectUpdate     = "subject.update"
	ActionSubjectDeactivate = "subject.deactivate"
	ActionSubjectActivate   = "subject.activate"

	maxSubjectNameLen = 60
	maxSubjectCodeLen = 20

	// StatusActive dan StatusInactive adalah nilai isian status di form ubah.
	StatusActive   = "aktif"
	StatusInactive = "nonaktif"
)

// SubjectForm adalah isian form mapel apa adanya.
type SubjectForm struct {
	Name string
	Code string
	// KKM kosong berarti memakai KKM default sekolah (T73).
	KKM string
	// Status hanya dibaca saat mengubah: StatusActive atau StatusInactive.
	// Saat membuat, mapel selalu aktif.
	Status string
}

// ValidationError berisi pesan per field untuk ditampilkan di form.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return "academic: invalid input: " + strings.Join(keys, ", ")
}

// SubjectService menjalankan aturan bisnis mata pelajaran.
type SubjectService struct {
	db database.DB
}

// NewSubjectService mengembalikan SubjectService.
func NewSubjectService(db database.DB) *SubjectService {
	return &SubjectService{db: db}
}

// ListAll mengembalikan semua mapel sekolah, termasuk yang nonaktif.
func (s *SubjectService) ListAll(ctx context.Context, schoolID string) ([]Subject, error) {
	return ListAllSubjects(ctx, s.db, schoolID)
}

// ListActive mengembalikan mapel aktif (untuk penugasan dan jadwal).
func (s *SubjectService) ListActive(ctx context.Context, schoolID string) ([]Subject, error) {
	return ListActiveSubjects(ctx, s.db, schoolID)
}

// Get mengembalikan satu mapel.
func (s *SubjectService) Get(ctx context.Context, schoolID, id string) (Subject, error) {
	return GetSubjectByID(ctx, s.db, schoolID, id)
}

// Create memvalidasi form lalu membuat mapel aktif beserta auditnya.
func (s *SubjectService) Create(ctx context.Context, actor school.Actor, f SubjectForm) (Subject, error) {
	in, err := ValidateSubject(f)
	if err != nil {
		return Subject{}, err
	}
	var created Subject
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		sub, err := CreateSubject(ctx, tx, actor.SchoolID, in)
		if err != nil {
			return err
		}
		created = sub
		return audit.Record(ctx, tx, subjectAudit(actor, ActionSubjectCreate, sub.ID, nil, subjectSnapshot(sub)))
	})
	if err != nil {
		return Subject{}, subjectFieldError(err)
	}
	return created, nil
}

// Update mengubah nama, kode, dan status mapel dalam satu transaksi.
//
// Audit: subject.update jika nama atau kode berubah, subject.deactivate atau
// subject.activate jika status berubah. Menyimpan tanpa perubahan tidak
// menambah audit.
func (s *SubjectService) Update(ctx context.Context, actor school.Actor, id string, f SubjectForm) (Subject, error) {
	var result Subject
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Mapel dicari lebih dulu agar ID sekolah lain selalu "tidak ada",
		// apa pun isi form-nya.
		before, err := GetSubjectByID(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		in, err := ValidateSubject(f)
		if err != nil {
			return err
		}
		active, err := parseStatus(f.Status)
		if err != nil {
			return err
		}
		result = before

		if in.Name != before.Name || in.Code != before.Code || !sameKKM(in.KKM, before.KKM) {
			after, err := UpdateSubject(ctx, tx, actor.SchoolID, id, in)
			if err != nil {
				return err
			}
			result = after
			if err := audit.Record(ctx, tx, subjectAudit(actor, ActionSubjectUpdate, id,
				subjectSnapshot(before), subjectSnapshot(after))); err != nil {
				return err
			}
		}
		if active != before.IsActive {
			after, err := SetSubjectActive(ctx, tx, actor.SchoolID, id, active)
			if err != nil {
				return err
			}
			result = after
			action := ActionSubjectDeactivate
			if active {
				action = ActionSubjectActivate
			}
			return audit.Record(ctx, tx, subjectAudit(actor, action, id,
				map[string]any{"is_active": before.IsActive}, map[string]any{"is_active": active}))
		}
		return nil
	})
	if err != nil {
		return Subject{}, subjectFieldError(err)
	}
	return result, nil
}

// ValidateSubject memeriksa nama dan kode, lalu mengubahnya menjadi input
// repository. Spasi berlebih dirapikan.
func ValidateSubject(f SubjectForm) (SubjectInput, error) {
	fields := map[string]string{}
	in := SubjectInput{
		Name: strings.Join(strings.Fields(f.Name), " "),
		Code: strings.Join(strings.Fields(f.Code), " "),
	}
	switch {
	case in.Name == "":
		fields["name"] = "Nama mata pelajaran wajib diisi."
	case utf8.RuneCountInString(in.Name) > maxSubjectNameLen:
		fields["name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxSubjectNameLen)
	}
	if utf8.RuneCountInString(in.Code) > maxSubjectCodeLen {
		fields["code"] = fmt.Sprintf("Kode maksimal %d karakter.", maxSubjectCodeLen)
	}
	if k := strings.TrimSpace(f.KKM); k != "" {
		n, err := strconv.Atoi(k)
		if err != nil || n < 0 || n > 100 {
			fields["kkm"] = "KKM bilangan bulat 0–100, atau kosongkan untuk memakai KKM default."
		} else {
			in.KKM = &n
		}
	}
	if len(fields) > 0 {
		return SubjectInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

// parseStatus membaca isian status form ubah. Nilai selain dua yang dikenal
// ditolak, supaya isian yang hilang tidak diam-diam menonaktifkan mapel.
func parseStatus(s string) (active bool, err error) {
	switch strings.TrimSpace(s) {
	case StatusActive:
		return true, nil
	case StatusInactive:
		return false, nil
	}
	return false, &ValidationError{Fields: map[string]string{"status": "Pilih status mata pelajaran."}}
}

func subjectFieldError(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateSubjectName):
		return &ValidationError{Fields: map[string]string{"name": "Nama mata pelajaran sudah dipakai."}}
	case errors.Is(err, ErrDuplicateSubjectCode):
		return &ValidationError{Fields: map[string]string{"code": "Kode mata pelajaran sudah dipakai."}}
	}
	return err
}

type subjectJSON struct {
	Name string `json:"name"`
	Code string `json:"code"`
	KKM  *int   `json:"kkm"`
}

func subjectSnapshot(s Subject) subjectJSON {
	return subjectJSON{Name: s.Name, Code: s.Code, KKM: s.KKM}
}

func subjectAudit(actor school.Actor, action, id string, before, after any) audit.Entry {
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: "subject", EntityID: id,
		Before: before, After: after,
	}
}

func sameKKM(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
