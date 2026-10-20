package schedule

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/audit"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit jadwal.
const (
	ActionScheduleCreate = "schedule.create"
	ActionScheduleUpdate = "schedule.update"
	ActionScheduleDelete = "schedule.delete"
)

// ErrClassNotFound: kelas tidak ada, milik sekolah lain, atau bukan kelas
// tahun ajaran aktif.
var ErrClassNotFound = errors.New("schedule: class not found in the active academic year")

// ErrSubjectNotFound: mapel yang dikirim tidak ada atau milik sekolah lain.
// Form hanya menawarkan mapel sekolah sendiri, jadi ini berarti isian diubah.
var ErrSubjectNotFound = errors.New("schedule: subject not found")

// Form adalah isian form slot apa adanya.
type Form struct {
	SubjectID string
	Day       string // "1".."6"
	Start     string // "07:00"
	End       string
}

// FormOf mengisi form dari slot tersimpan (form ubah).
func FormOf(s Slot) Form {
	return Form{SubjectID: s.SubjectID, Day: strconv.Itoa(s.Day), Start: s.Start.Input(), End: s.End.Input()}
}

// ValidationError berisi pesan per field.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return "schedule: invalid input: " + strings.Join(keys, ", ")
}

// ConflictError: slot berpotongan dengan slot lain di kelas yang sama atau
// untuk guru yang sama.
type ConflictError struct {
	With    Slot
	ByClass bool // bentrok dengan jadwal kelas yang sama; jika false, jadwal guru
}

func (e *ConflictError) Error() string { return "schedule: slot conflicts with " + e.With.ID }

// Message adalah pesan untuk pengguna.
func (e *ConflictError) Message() string {
	when := fmt.Sprintf("%s %s–%s", DayName(e.With.Day), e.With.Start, e.With.End)
	if e.ByClass {
		return fmt.Sprintf("Bentrok dengan %s di kelas ini (%s).", e.With.SubjectName, when)
	}
	return fmt.Sprintf("Guru %s sudah mengajar %s di kelas %s pada %s.", e.With.TeacherName, e.With.SubjectName, e.With.ClassName, when)
}

// Service mengatur jadwal pelajaran.
type Service struct {
	db database.DB
}

// NewService mengembalikan Service.
func NewService(db database.DB) *Service { return &Service{db: db} }

// ClassOption adalah mapel yang bisa dijadwalkan di kelas: mapel aktif yang
// punya guru (penugasan T65).
type ClassOption struct {
	SubjectID, SubjectName, SubjectCode string
	TeacherID, TeacherName              string
}

// ClassPage adalah isi halaman jadwal satu kelas.
type ClassPage struct {
	Class   classes.Class
	Slots   []Slot
	Options []ClassOption
}

// activeClass mengembalikan kelas milik schoolID di tahun ajaran aktif.
func activeClass(ctx context.Context, q database.DBTX, schoolID, classID string) (classes.Class, error) {
	c, err := classes.GetByID(ctx, q, schoolID, classID)
	if errors.Is(err, classes.ErrNotFound) {
		return classes.Class{}, ErrClassNotFound
	}
	if err != nil {
		return classes.Class{}, err
	}
	var active bool
	if err := q.QueryRow(ctx, `SELECT is_active FROM academic_years WHERE school_id = $1 AND id = $2`, schoolID, c.AcademicYearID).Scan(&active); err != nil {
		return classes.Class{}, fmt.Errorf("schedule: class year: %w", err)
	}
	if !active {
		return classes.Class{}, ErrClassNotFound
	}
	return c, nil
}

// Class menyusun halaman jadwal kelas classID.
func (s *Service) Class(ctx context.Context, schoolID, classID string) (ClassPage, error) {
	c, err := activeClass(ctx, s.db, schoolID, classID)
	if err != nil {
		return ClassPage{}, err
	}
	p := ClassPage{Class: c}
	if p.Slots, err = ListClassSlots(ctx, s.db, schoolID, c.ID); err != nil {
		return ClassPage{}, err
	}
	assignments, err := academic.ListClassAssignments(ctx, s.db, schoolID, c.ID)
	if err != nil {
		return ClassPage{}, err
	}
	for _, a := range assignments {
		if a.SubjectActive {
			p.Options = append(p.Options, ClassOption{SubjectID: a.SubjectID, SubjectName: a.SubjectName, SubjectCode: a.SubjectCode,
				TeacherID: a.TeacherID, TeacherName: a.TeacherName})
		}
	}
	return p, nil
}

// Get mengembalikan satu slot.
func (s *Service) Get(ctx context.Context, schoolID, id string) (Slot, error) {
	return GetSlot(ctx, s.db, schoolID, id)
}

// Create menambah slot ke kelas classID. Guru slot adalah guru pengampu mapel
// itu di kelas tersebut (T65); mapel tanpa penugasan ditolak.
func (s *Service) Create(ctx context.Context, actor school.Actor, classID string, f Form) (Slot, error) {
	var created Slot
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		c, err := activeClass(ctx, tx, actor.SchoolID, classID)
		if err != nil {
			return err
		}
		in, err := s.prepare(ctx, tx, actor.SchoolID, c, f, "")
		if err != nil {
			return err
		}
		id, err := insertSlot(ctx, tx, actor.SchoolID, in)
		if err != nil {
			return err
		}
		if created, err = GetSlot(ctx, tx, actor.SchoolID, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, slotAudit(actor, ActionScheduleCreate, created, nil, snapshot(created)))
	})
	if err != nil {
		return Slot{}, err
	}
	return created, nil
}

// Update mengubah mapel, hari, dan jam slot id. Kelas slot tidak berubah.
func (s *Service) Update(ctx context.Context, actor school.Actor, id string, f Form) (Slot, error) {
	var after Slot
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := GetSlot(ctx, tx, actor.SchoolID, id)
		if err != nil {
			return err
		}
		c, err := activeClass(ctx, tx, actor.SchoolID, before.ClassID)
		if err != nil {
			return err
		}
		in, err := s.prepare(ctx, tx, actor.SchoolID, c, f, before.ID)
		if err != nil {
			return err
		}
		if in.SubjectID == before.SubjectID && in.TeacherID == before.TeacherID && in.Day == before.Day && in.Start == before.Start && in.End == before.End {
			after = before
			return nil
		}
		if err := updateSlot(ctx, tx, actor.SchoolID, id, in); err != nil {
			return err
		}
		if after, err = GetSlot(ctx, tx, actor.SchoolID, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, slotAudit(actor, ActionScheduleUpdate, after, snapshot(before), snapshot(after)))
	})
	if err != nil {
		return Slot{}, err
	}
	return after, nil
}

// Delete menghapus slot. Jadwal bukan data keuangan, jadi boleh dihapus;
// isinya tetap tercatat di audit.
func (s *Service) Delete(ctx context.Context, actor school.Actor, id string) (Slot, error) {
	var before Slot
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if before, err = GetSlot(ctx, tx, actor.SchoolID, id); err != nil {
			return err
		}
		if err := deleteSlot(ctx, tx, actor.SchoolID, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, slotAudit(actor, ActionScheduleDelete, before, snapshot(before), nil))
	})
	if err != nil {
		return Slot{}, err
	}
	return before, nil
}

// prepare memvalidasi form, mencari guru dari penugasan, mengunci baris kelas
// lalu guru (urutan tetap agar tidak deadlock), dan memeriksa bentrok.
// Kunci berlaku sampai transaksi selesai, jadi dua permintaan bentrok yang
// bersamaan diproses bergantian dan yang kedua melihat slot yang pertama.
func (s *Service) prepare(ctx context.Context, tx pgx.Tx, schoolID string, c classes.Class, f Form, exceptID string) (slotInput, error) {
	fields := map[string]string{}
	in := slotInput{AcademicYearID: c.AcademicYearID, ClassID: c.ID}
	day, err := strconv.Atoi(strings.TrimSpace(f.Day))
	if err != nil || DayName(day) == "" {
		fields["day"] = "Pilih hari Senin–Sabtu."
	}
	in.Day = day
	start, okS := ParseTime(f.Start)
	end, okE := ParseTime(f.End)
	switch {
	case !okS:
		fields["start"] = "Isi jam mulai, mis. 07:00."
	case !okE:
		fields["end"] = "Isi jam selesai, mis. 07:40."
	case end <= start:
		fields["end"] = "Jam selesai harus setelah jam mulai."
	}
	in.Start, in.End = start, end

	subjectID := strings.TrimSpace(f.SubjectID)
	if subjectID == "" {
		fields["subject"] = "Pilih mata pelajaran dari daftar."
		return slotInput{}, &ValidationError{Fields: fields}
	}
	subject, err := academic.GetSubjectByID(ctx, tx, schoolID, subjectID)
	switch {
	case errors.Is(err, academic.ErrSubjectNotFound):
		return slotInput{}, ErrSubjectNotFound
	case err != nil:
		return slotInput{}, err
	case !subject.IsActive:
		fields["subject"] = "Mata pelajaran nonaktif tidak bisa dijadwalkan."
	default:
		in.SubjectID = subject.ID
		err := tx.QueryRow(ctx, `SELECT teacher_id FROM teaching_assignments WHERE school_id = $1 AND class_id = $2 AND subject_id = $3`,
			schoolID, c.ID, subject.ID).Scan(&in.TeacherID)
		if errors.Is(err, pgx.ErrNoRows) {
			fields["subject"] = "Mata pelajaran ini belum punya guru di kelas ini. Atur di Penugasan Guru."
		} else if err != nil {
			return slotInput{}, fmt.Errorf("schedule: get assignment: %w", err)
		}
	}
	if len(fields) > 0 {
		return slotInput{}, &ValidationError{Fields: fields}
	}

	if _, err := tx.Exec(ctx, `SELECT 1 FROM classes WHERE school_id = $1 AND id = $2 FOR UPDATE`, schoolID, c.ID); err != nil {
		return slotInput{}, fmt.Errorf("schedule: lock class: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE school_id = $1 AND id = $2 FOR UPDATE`, schoolID, in.TeacherID); err != nil {
		return slotInput{}, fmt.Errorf("schedule: lock teacher: %w", err)
	}
	others, err := overlapping(ctx, tx, schoolID, c.ID, in.TeacherID, in.Day, in.Start, in.End, exceptID)
	if err != nil {
		return slotInput{}, err
	}
	for _, o := range others {
		if o.ClassID == c.ID {
			return slotInput{}, &ConflictError{With: o, ByClass: true}
		}
	}
	if len(others) > 0 {
		return slotInput{}, &ConflictError{With: others[0]}
	}
	return in, nil
}

type slotJSON struct {
	ClassID   string `json:"class_id"`
	SubjectID string `json:"subject_id"`
	TeacherID string `json:"teacher_id"`
	Day       int    `json:"day_of_week"`
	Start     string `json:"start_time"`
	End       string `json:"end_time"`
}

func snapshot(s Slot) slotJSON {
	return slotJSON{ClassID: s.ClassID, SubjectID: s.SubjectID, TeacherID: s.TeacherID, Day: s.Day, Start: s.Start.Input(), End: s.End.Input()}
}

func slotAudit(actor school.Actor, action string, s Slot, before, after any) audit.Entry {
	e := audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP, Action: action, EntityType: "schedule_slot", EntityID: s.ID}
	if before != nil {
		e.Before = before
	}
	if after != nil {
		e.After = after
	}
	return e
}
