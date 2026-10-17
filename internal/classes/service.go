package classes

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

const maxClassNameLen = 20

// Aksi audit untuk kelas.
const (
	ActionClassCreate        = "class.create"
	ActionClassUpdate        = "class.update"
	ActionClassAssignTeacher = "class.assign_teacher"

	entityClass = "class"
)

// ErrTeacherNotEligible dikembalikan jika pengguna yang dipilih sebagai wali
// kelas bukan guru aktif di sekolah yang sama.
var ErrTeacherNotEligible = errors.New("classes: homeroom teacher must be an active teacher of the same school")

// Form adalah isian form kelas apa adanya.
type Form struct {
	AcademicYearID    string // hanya dipakai saat membuat kelas
	Name              string
	GradeLevel        string
	HomeroomTeacherID string // kosong berarti tanpa wali kelas
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
	return "classes: invalid input: " + strings.Join(keys, ", ")
}

// Service menjalankan aturan bisnis kelas.
type Service struct {
	db database.DB
}

// NewService mengembalikan Service.
func NewService(db database.DB) *Service {
	return &Service{db: db}
}

// List mengembalikan kelas sekolah pada tahun ajaran yearID.
func (s *Service) List(ctx context.Context, schoolID, yearID string) ([]Class, error) {
	return ListByYear(ctx, s.db, schoolID, yearID)
}

// Get mengembalikan satu kelas, atau ErrNotFound jika milik sekolah lain.
func (s *Service) Get(ctx context.Context, schoolID, id string) (Class, error) {
	return GetByID(ctx, s.db, schoolID, id)
}

// ListTeachers mengembalikan guru aktif yang bisa dipilih sebagai wali kelas.
func (s *Service) ListTeachers(ctx context.Context, schoolID string) ([]Teacher, error) {
	return ListTeachers(ctx, s.db, schoolID)
}

// Create memvalidasi form lalu membuat kelas beserta auditnya.
func (s *Service) Create(ctx context.Context, actor school.Actor, f Form) (Class, error) {
	name, grade, err := validate(f)
	if err != nil {
		return Class{}, err
	}
	var created Class
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.checkTeacher(ctx, tx, actor.SchoolID, f.HomeroomTeacherID); err != nil {
			return err
		}
		c, err := Create(ctx, tx, actor.SchoolID, ClassInput{
			AcademicYearID: f.AcademicYearID, Name: name, GradeLevel: grade, HomeroomTeacherID: f.HomeroomTeacherID,
		})
		if err != nil {
			return err
		}
		created = c
		return audit.Record(ctx, tx, entry(actor, ActionClassCreate, c.ID, nil, snapshot(c)))
	})
	if err != nil {
		return Class{}, fieldError(err)
	}
	return created, nil
}

// Update mengubah nama, tingkat, dan wali kelas. Perubahan nama/tingkat
// dicatat sebagai class.update, perubahan wali kelas sebagai
// class.assign_teacher.
func (s *Service) Update(ctx context.Context, actor school.Actor, id string, f Form) (Class, error) {
	name, grade, err := validate(f)
	if err != nil {
		return Class{}, err
	}
	var updated Class
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := GetByID(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		updated = before
		if before.Name != name || before.GradeLevel != grade {
			if updated, err = Update(ctx, tx, actor.SchoolID, id, ClassUpdate{Name: name, GradeLevel: grade}); err != nil {
				return err
			}
			if err := audit.Record(ctx, tx, entry(actor, ActionClassUpdate, id, snapshot(before), snapshot(updated))); err != nil {
				return err
			}
		}
		if f.HomeroomTeacherID != before.HomeroomTeacherID {
			updated, err = s.assign(ctx, tx, actor, updated, f.HomeroomTeacherID)
			return err
		}
		return nil
	})
	if err != nil {
		return Class{}, fieldError(err)
	}
	return updated, nil
}

// AssignHomeroomTeacher mengganti wali kelas kelas id. teacherID kosong
// menghapus wali kelas.
func (s *Service) AssignHomeroomTeacher(ctx context.Context, actor school.Actor, id, teacherID string) (Class, error) {
	var updated Class
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		c, err := GetByID(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		if c.HomeroomTeacherID == teacherID {
			updated = c
			return nil
		}
		updated, err = s.assign(ctx, tx, actor, c, teacherID)
		return err
	})
	if err != nil {
		return Class{}, fieldError(err)
	}
	return updated, nil
}

func (s *Service) assign(ctx context.Context, tx pgx.Tx, actor school.Actor, c Class, teacherID string) (Class, error) {
	if err := s.checkTeacher(ctx, tx, actor.SchoolID, teacherID); err != nil {
		return Class{}, err
	}
	updated, err := SetHomeroomTeacher(ctx, tx, actor.SchoolID, c.ID, teacherID)
	if err != nil {
		return Class{}, err
	}
	err = audit.Record(ctx, tx, entry(actor, ActionClassAssignTeacher, c.ID,
		map[string]any{"homeroom_teacher_id": nullable(c.HomeroomTeacherID)},
		map[string]any{"homeroom_teacher_id": nullable(teacherID)}))
	return updated, err
}

// checkTeacher memastikan teacherID (jika diisi) adalah guru aktif di sekolah.
func (s *Service) checkTeacher(ctx context.Context, q database.DBTX, schoolID, teacherID string) error {
	if teacherID == "" {
		return nil
	}
	ok, err := IsActiveTeacher(ctx, q, schoolID, teacherID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTeacherNotEligible
	}
	return nil
}

func validate(f Form) (name string, grade int, err error) {
	fields := map[string]string{}
	name = strings.TrimSpace(f.Name)
	switch {
	case name == "":
		fields["name"] = "Nama kelas wajib diisi."
	case utf8.RuneCountInString(name) > maxClassNameLen:
		fields["name"] = fmt.Sprintf("Nama kelas maksimal %d karakter.", maxClassNameLen)
	}
	grade, convErr := strconv.Atoi(strings.TrimSpace(f.GradeLevel))
	if convErr != nil || grade < MinGradeLevel || grade > MaxGradeLevel {
		fields["grade_level"] = fmt.Sprintf("Pilih tingkat %d sampai %d.", MinGradeLevel, MaxGradeLevel)
	}
	if len(fields) > 0 {
		return "", 0, &ValidationError{Fields: fields}
	}
	return name, grade, nil
}

// fieldError mengubah error yang berkaitan dengan isian form menjadi
// ValidationError.
func fieldError(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateName):
		return &ValidationError{Fields: map[string]string{"name": "Nama kelas sudah dipakai di tahun ajaran ini."}}
	case errors.Is(err, ErrInvalidGradeLevel):
		return &ValidationError{Fields: map[string]string{"grade_level": "Pilih tingkat 1 sampai 12."}}
	case errors.Is(err, ErrAcademicYearNotFound):
		return &ValidationError{Fields: map[string]string{"academic_year_id": "Pilih tahun ajaran yang tersedia."}}
	case errors.Is(err, ErrTeacherNotEligible), errors.Is(err, ErrTeacherNotFound):
		return &ValidationError{Fields: map[string]string{"homeroom_teacher_id": "Wali kelas harus guru aktif di sekolah ini."}}
	}
	return err
}

type classSnapshot struct {
	AcademicYearID    string `json:"academic_year_id"`
	Name              string `json:"name"`
	GradeLevel        int    `json:"grade_level"`
	HomeroomTeacherID any    `json:"homeroom_teacher_id"`
}

func snapshot(c Class) classSnapshot {
	return classSnapshot{AcademicYearID: c.AcademicYearID, Name: c.Name, GradeLevel: c.GradeLevel, HomeroomTeacherID: nullable(c.HomeroomTeacherID)}
}

func entry(actor school.Actor, action, id string, before, after any) audit.Entry {
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: entityClass, EntityID: id,
		Before: before, After: after,
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
