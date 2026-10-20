package academic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit penugasan.
const (
	ActionTeachingAssign   = "teaching.assign"
	ActionTeachingUnassign = "teaching.unassign"
)

// ErrClassNotFound dikembalikan jika kelas tidak ada, milik sekolah lain,
// atau bukan kelas tahun ajaran aktif.
var ErrClassNotFound = errors.New("academic: class not found in the active academic year")

// AssignmentService mengatur penugasan guru mata pelajaran.
type AssignmentService struct {
	db database.DB
}

// NewAssignmentService mengembalikan AssignmentService.
func NewAssignmentService(db database.DB) *AssignmentService {
	return &AssignmentService{db: db}
}

// PlanRow adalah satu baris form penugasan: satu mapel dan gurunya.
type PlanRow struct {
	Subject   Subject
	TeacherID string // "" jika belum ada guru
	// TeacherName dan TeacherActive dari penugasan yang tersimpan, untuk
	// menampilkan guru yang sudah nonaktif.
	TeacherName   string
	TeacherActive bool
}

// ClassPlan adalah isi form penugasan satu kelas.
type ClassPlan struct {
	Class    classes.Class
	Rows     []PlanRow
	Teachers []classes.Teacher // guru aktif sekolah, pilihan form
}

// activeClass mengembalikan kelas classID milik schoolID di tahun ajaran
// aktif.
func activeClass(ctx context.Context, q database.DBTX, schoolID, classID string) (classes.Class, error) {
	c, err := classes.GetByID(ctx, q, schoolID, classID)
	if err != nil {
		if errors.Is(err, classes.ErrNotFound) {
			return classes.Class{}, ErrClassNotFound
		}
		return classes.Class{}, err
	}
	var active bool
	if err := q.QueryRow(ctx, `SELECT is_active FROM academic_years WHERE school_id = $1 AND id = $2`, schoolID, c.AcademicYearID).Scan(&active); err != nil {
		return classes.Class{}, fmt.Errorf("academic: class year: %w", err)
	}
	if !active {
		return classes.Class{}, ErrClassNotFound
	}
	return c, nil
}

// Plan menyusun form penugasan kelas classID: semua mapel aktif, ditambah
// mapel nonaktif yang masih punya penugasan (agar bisa dilepas).
func (s *AssignmentService) Plan(ctx context.Context, schoolID, classID string) (ClassPlan, error) {
	c, err := activeClass(ctx, s.db, schoolID, classID)
	if err != nil {
		return ClassPlan{}, err
	}
	subjects, err := ListAllSubjects(ctx, s.db, schoolID)
	if err != nil {
		return ClassPlan{}, err
	}
	current, err := ListClassAssignments(ctx, s.db, schoolID, c.ID)
	if err != nil {
		return ClassPlan{}, err
	}
	bySubject := map[string]Assignment{}
	for _, a := range current {
		bySubject[a.SubjectID] = a
	}
	plan := ClassPlan{Class: c}
	for _, sub := range subjects {
		a, assigned := bySubject[sub.ID]
		if !sub.IsActive && !assigned {
			continue
		}
		plan.Rows = append(plan.Rows, PlanRow{Subject: sub, TeacherID: a.TeacherID, TeacherName: a.TeacherName, TeacherActive: a.TeacherActive})
	}
	if plan.Teachers, err = classes.ListTeachers(ctx, s.db, schoolID); err != nil {
		return ClassPlan{}, err
	}
	return plan, nil
}

// Mine mengembalikan penugasan guru teacherID di tahun ajaran aktif.
func (s *AssignmentService) Mine(ctx context.Context, schoolID, teacherID string) ([]Assignment, error) {
	return ListTeacherAssignments(ctx, s.db, schoolID, teacherID)
}

// SaveResult meringkas perubahan yang disimpan.
type SaveResult struct {
	Assigned, Unassigned int
}

// Save menyimpan penugasan kelas classID. teachers memetakan ID mapel ke ID
// guru; "" berarti mapel itu tidak punya guru. Mapel yang tidak ada di
// teachers tidak diubah. Semua perubahan dalam satu transaksi:
//
//   - kelas harus milik sekolah dan tahun ajaran aktif (ErrClassNotFound);
//   - mapel harus milik sekolah (ErrSubjectNotFound); penugasan baru hanya
//     untuk mapel aktif;
//   - guru harus pengguna aktif berperan guru di sekolah yang sama.
//
// Guru mapel yang sudah ada di jadwal (T66) tidak bisa diganti atau dilepas
// sebelum slot jadwalnya diubah.
//
// Audit teaching.assign untuk penugasan baru atau pergantian guru, dan
// teaching.unassign untuk penugasan yang dilepas. Baris yang tidak berubah
// tidak menambah audit.
func (s *AssignmentService) Save(ctx context.Context, actor school.Actor, classID string, teachers map[string]string) (SaveResult, error) {
	var res SaveResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		c, err := activeClass(ctx, tx, actor.SchoolID, classID)
		if err != nil {
			return err
		}
		fields := map[string]string{}
		for subjectID, teacherID := range teachers {
			teacherID = strings.TrimSpace(teacherID)
			sub, err := GetSubjectByID(ctx, tx, actor.SchoolID, subjectID)
			if err != nil {
				return err
			}
			if teacherID == "" {
				if scheduled, err := hasSchedule(ctx, tx, actor.SchoolID, c.ID, sub.ID); err != nil {
					return err
				} else if scheduled {
					fields[sub.ID] = msgScheduled
					continue
				}
				prev, err := unassignTeacher(ctx, tx, actor.SchoolID, c.ID, sub.ID)
				if err != nil {
					return err
				}
				if prev != "" {
					res.Unassigned++
					if err := audit.Record(ctx, tx, assignmentAudit(actor, ActionTeachingUnassign, c, sub,
						map[string]any{"teacher_id": prev}, nil)); err != nil {
						return err
					}
				}
				continue
			}
			ok, err := classes.IsActiveTeacher(ctx, tx, actor.SchoolID, teacherID)
			if err != nil {
				return err
			}
			if !ok {
				fields[sub.ID] = "Pilih guru aktif dari daftar."
				continue
			}
			var current string
			err = tx.QueryRow(ctx, `SELECT teacher_id FROM teaching_assignments WHERE school_id = $1 AND class_id = $2 AND subject_id = $3`,
				actor.SchoolID, c.ID, sub.ID).Scan(&current)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("academic: get assignment: %w", err)
			}
			if current == teacherID {
				continue
			}
			if current == "" && !sub.IsActive {
				fields[sub.ID] = "Mata pelajaran nonaktif tidak bisa diberi guru baru."
				continue
			}
			if current != "" {
				if scheduled, err := hasSchedule(ctx, tx, actor.SchoolID, c.ID, sub.ID); err != nil {
					return err
				} else if scheduled {
					fields[sub.ID] = msgScheduled
					continue
				}
			}
			prev, err := assignTeacher(ctx, tx, actor.SchoolID, c.AcademicYearID, c.ID, sub.ID, teacherID)
			if err != nil {
				return err
			}
			res.Assigned++
			var before map[string]any
			if prev != "" {
				before = map[string]any{"teacher_id": prev}
			}
			if err := audit.Record(ctx, tx, assignmentAudit(actor, ActionTeachingAssign, c, sub, before, map[string]any{"teacher_id": teacherID})); err != nil {
				return err
			}
		}
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}
		return nil
	})
	if errors.Is(err, ErrAssignmentConflict) {
		return SaveResult{}, &ValidationError{Fields: map[string]string{"": "Penugasan berubah bersamaan. Muat ulang halaman lalu coba lagi."}}
	}
	if err != nil {
		return SaveResult{}, err
	}
	return res, nil
}

// msgScheduled: guru mapel yang sudah dijadwalkan tidak diganti diam-diam,
// karena slot jadwal (T66) menyimpan guru itu dan harus dicek bentroknya.
const msgScheduled = "Mapel ini sudah ada di jadwal kelas. Ubah atau hapus slot jadwalnya dulu."

// hasSchedule melaporkan apakah mapel subjectID sudah punya slot jadwal di
// kelas classID.
func hasSchedule(ctx context.Context, q database.DBTX, schoolID, classID, subjectID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schedule_slots WHERE school_id = $1 AND class_id = $2 AND subject_id = $3)`,
		schoolID, classID, subjectID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("academic: check schedule: %w", err)
	}
	return ok, nil
}

func assignmentAudit(actor school.Actor, action string, c classes.Class, sub Subject, before, after map[string]any) audit.Entry {
	base := map[string]any{"class_id": c.ID, "academic_year_id": c.AcademicYearID, "subject_id": sub.ID}
	merge := func(m map[string]any) any {
		if m == nil {
			return nil
		}
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: "class", EntityID: c.ID,
		Before: merge(before), After: merge(after),
	}
}
