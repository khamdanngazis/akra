package students

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/classes"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit untuk data siswa (student.place ada di enrollment_service.go).
const (
	ActionStudentCreate = "student.create"
	ActionStudentUpdate = "student.update"

	maxNISLen      = 30
	maxFullNameLen = 100
	dateLayout     = "2006-01-02"
)

var (
	// ErrNoActiveYear dikembalikan jika sekolah belum punya tahun ajaran aktif.
	ErrNoActiveYear = errors.New("students: school has no active academic year")
)

// Form adalah isian form siswa apa adanya.
type Form struct {
	NIS       string
	FullName  string
	BirthDate string // YYYY-MM-DD, boleh kosong
	Gender    string // L, P, atau kosong
	Status    string // hanya saat mengubah; kosong berarti tidak diubah
	ClassName string // hanya saat membuat: nama kelas di tahun ajaran aktif
}

// MoveForm adalah isian form pindah kelas.
type MoveForm struct {
	ClassName string // nama kelas di tahun ajaran aktif
	Date      string // YYYY-MM-DD, kosong berarti hari ini
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
	return "students: invalid input: " + strings.Join(keys, ", ")
}

func fieldErr(field, msg string) *ValidationError {
	return &ValidationError{Fields: map[string]string{field: msg}}
}

// Service menjalankan aturan bisnis data siswa.
type Service struct {
	db          database.DB
	enrollments *EnrollmentService
	today       func() time.Time
}

// NewService mengembalikan Service.
func NewService(db database.DB) *Service {
	return &Service{db: db, enrollments: NewEnrollmentService(db), today: clock.Today}
}

// Page adalah satu halaman hasil daftar siswa.
type Page struct {
	Rows     []ListRow
	Total    int
	Page     int
	LastPage int
}

// List mengembalikan siswa sesuai filter, PageSize per halaman.
func (s *Service) List(ctx context.Context, schoolID string, f ListFilter) (Page, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	rows, total, err := List(ctx, s.db, schoolID, f)
	if err != nil {
		return Page{}, err
	}
	last := (total + PageSize - 1) / PageSize
	if last < 1 {
		last = 1
	}
	return Page{Rows: rows, Total: total, Page: f.Page, LastPage: last}, nil
}

// Get mengembalikan satu siswa, atau ErrNotFound jika milik sekolah lain.
func (s *Service) Get(ctx context.Context, schoolID, id string) (Student, error) {
	return GetByID(ctx, s.db, schoolID, id)
}

// History mengembalikan riwayat kelas siswa.
func (s *Service) History(ctx context.Context, schoolID, id string) ([]Enrollment, error) {
	return ListEnrollments(ctx, s.db, schoolID, id)
}

// ActiveYearClasses mengembalikan tahun ajaran aktif beserta kelasnya, untuk
// pilihan kelas di form. ErrNoActiveYear jika belum ada tahun aktif.
func (s *Service) ActiveYearClasses(ctx context.Context, schoolID string) (school.AcademicYear, []classes.Class, error) {
	year, err := school.GetActiveAcademicYear(ctx, s.db, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return school.AcademicYear{}, nil, ErrNoActiveYear
	}
	if err != nil {
		return school.AcademicYear{}, nil, err
	}
	list, err := classes.ListByYear(ctx, s.db, schoolID, year.ID)
	return year, list, err
}

// Create membuat siswa beserta enrollment awal di kelas tahun ajaran aktif
// (mulai hari ini), dalam satu transaksi. Kelas wajib diisi.
func (s *Service) Create(ctx context.Context, actor school.Actor, f Form) (Student, error) {
	in, err := validateStudent(f, false)
	if err != nil {
		return Student{}, err
	}
	var created Student
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		class, err := s.resolveClass(ctx, tx, actor.SchoolID, f.ClassName)
		if err != nil {
			return err
		}
		st, err := Create(ctx, tx, actor.SchoolID, in)
		if err != nil {
			return err
		}
		created = st
		if err := audit.Record(ctx, tx, studentAudit(actor, ActionStudentCreate, st.ID, nil, studentSnapshot(st))); err != nil {
			return err
		}
		res, err := Place(ctx, tx, actor.SchoolID, st.ID, class.ID, class.AcademicYearID, s.today())
		if errors.Is(err, ErrInvalidPlacementDate) {
			// Hari ini di luar tahun ajaran aktif (biasanya tahun aktif sudah berakhir).
			return fieldErr("class_name", "Tahun ajaran aktif sudah berakhir. Aktifkan tahun ajaran yang sedang berjalan di menu Tahun Ajaran.")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, studentAudit(actor, ActionStudentPlace, st.ID, nil, placementSnapshot(res.Enrollment)))
	})
	if err != nil {
		return Student{}, studentFieldError(err)
	}
	return created, nil
}

// Update mengubah data pribadi siswa. Kelas tidak diubah di sini; gunakan
// Move.
func (s *Service) Update(ctx context.Context, actor school.Actor, id string, f Form) (Student, error) {
	in, err := validateStudent(f, true)
	if err != nil {
		return Student{}, err
	}
	var updated Student
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := GetByID(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		if in.Status == "" {
			in.Status = before.Status
		}
		after, err := Update(ctx, tx, actor.SchoolID, id, in)
		if err != nil {
			return err
		}
		updated = after
		if studentSnapshot(before) == studentSnapshot(after) {
			return nil
		}
		return audit.Record(ctx, tx, studentAudit(actor, ActionStudentUpdate, id, studentSnapshot(before), studentSnapshot(after)))
	})
	if err != nil {
		return Student{}, studentFieldError(err)
	}
	return updated, nil
}

// Move memindahkan siswa ke kelas lain di tahun ajaran aktif. Pindah kelas
// selalu menjadi enrollment baru (lihat Place).
func (s *Service) Move(ctx context.Context, actor school.Actor, id string, f MoveForm) (PlaceResult, error) {
	var date *time.Time
	if d := strings.TrimSpace(f.Date); d != "" {
		parsed, err := time.Parse(dateLayout, d)
		if err != nil {
			return PlaceResult{}, fieldErr("date", "Tanggal pindah tidak valid.")
		}
		date = &parsed
	}
	if _, err := GetByID(ctx, s.db, actor.SchoolID, id); err != nil {
		return PlaceResult{}, err
	}
	class, err := s.resolveClass(ctx, s.db, actor.SchoolID, f.ClassName)
	if err != nil {
		return PlaceResult{}, studentFieldError(err)
	}
	res, err := s.enrollments.Place(ctx, actor, id, class.ID, date)
	if err != nil {
		return PlaceResult{}, studentFieldError(err)
	}
	return res, nil
}

// resolveClass mencari kelas tahun ajaran aktif berdasarkan nama (tanpa
// membedakan huruf besar/kecil), seperti yang dipilih lewat datalist.
func (s *Service) resolveClass(ctx context.Context, q database.DBTX, schoolID, name string) (classes.Class, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return classes.Class{}, fieldErr("class_name", "Kelas wajib diisi.")
	}
	year, err := school.GetActiveAcademicYear(ctx, q, schoolID)
	if errors.Is(err, school.ErrNotFound) {
		return classes.Class{}, ErrNoActiveYear
	}
	if err != nil {
		return classes.Class{}, err
	}
	list, err := classes.ListByYear(ctx, q, schoolID, year.ID)
	if err != nil {
		return classes.Class{}, err
	}
	for _, c := range list {
		if strings.EqualFold(c.Name, name) {
			return c, nil
		}
	}
	return classes.Class{}, fieldErr("class_name", fmt.Sprintf("Kelas %q tidak ada di tahun ajaran %s.", name, year.Name))
}

func validateStudent(f Form, editing bool) (StudentInput, error) {
	fields := map[string]string{}
	in := StudentInput{NIS: NormalizeNIS(f.NIS), FullName: strings.Join(strings.Fields(f.FullName), " "), Gender: strings.TrimSpace(f.Gender)}

	switch {
	case in.NIS == "":
		fields["nis"] = "NIS wajib diisi."
	case utf8.RuneCountInString(in.NIS) > maxNISLen:
		fields["nis"] = fmt.Sprintf("NIS maksimal %d karakter.", maxNISLen)
	}
	switch {
	case in.FullName == "":
		fields["full_name"] = "Nama lengkap wajib diisi."
	case utf8.RuneCountInString(in.FullName) > maxFullNameLen:
		fields["full_name"] = fmt.Sprintf("Nama maksimal %d karakter.", maxFullNameLen)
	}
	if d := strings.TrimSpace(f.BirthDate); d != "" {
		bd, err := time.Parse(dateLayout, d)
		if err != nil || bd.Year() < 1950 || bd.After(clock.Today()) {
			fields["birth_date"] = "Tanggal lahir tidak valid."
		} else {
			in.BirthDate = &bd
		}
	}
	if in.Gender != "" && in.Gender != GenderMale && in.Gender != GenderFemale {
		fields["gender"] = "Pilih jenis kelamin."
	}
	if editing {
		in.Status = strings.TrimSpace(f.Status)
		if in.Status != "" && !slices.Contains(Statuses, in.Status) {
			fields["status"] = "Pilih status yang tersedia."
		}
	}
	if len(fields) > 0 {
		return StudentInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

func studentFieldError(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateNIS):
		return fieldErr("nis", "NIS sudah dipakai siswa lain di sekolah ini.")
	case errors.Is(err, ErrInvalidNIS):
		return fieldErr("nis", "NIS tidak valid.")
	case errors.Is(err, ErrInvalidGender):
		return fieldErr("gender", "Pilih jenis kelamin.")
	case errors.Is(err, ErrInvalidStatus):
		return fieldErr("status", "Pilih status yang tersedia.")
	case errors.Is(err, ErrNoActiveYear):
		return fieldErr("class_name", "Belum ada tahun ajaran aktif. Aktifkan tahun ajaran di menu Tahun Ajaran.")
	case errors.Is(err, ErrClassNotFound):
		return fieldErr("class_name", "Kelas tidak ditemukan di tahun ajaran aktif.")
	case errors.Is(err, ErrInvalidPlacementDate):
		return fieldErr("date", "Tanggal pindah harus di dalam tahun ajaran dan tidak sebelum tanggal masuk kelas saat ini.")
	}
	return err
}

type studentJSON struct {
	NIS       string `json:"nis"`
	FullName  string `json:"full_name"`
	BirthDate string `json:"birth_date"`
	Gender    string `json:"gender"`
	Status    string `json:"status"`
}

func studentSnapshot(s Student) studentJSON {
	j := studentJSON{NIS: s.NIS, FullName: s.FullName, Gender: s.Gender, Status: s.Status}
	if s.BirthDate != nil {
		j.BirthDate = s.BirthDate.Format(dateLayout)
	}
	return j
}

func studentAudit(actor school.Actor, action, id string, before, after any) audit.Entry {
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: "student", EntityID: id,
		Before: before, After: after,
	}
}
