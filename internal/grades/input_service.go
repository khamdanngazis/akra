package grades

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit nilai. Setiap perubahan satu nilai dicatat dengan nilai
// sebelum dan sesudah; itulah riwayat nilai.
const (
	ActionGradeCreate = "grade.create"
	ActionGradeUpdate = "grade.update"
)

var (
	// ErrClassNotFound: kelas tidak ada, milik sekolah lain, atau bukan kelas
	// tahun ajaran aktif.
	ErrClassNotFound = errors.New("grades: class not found in the active academic year")
	// ErrComponentInactive: komponen nonaktif tidak bisa diisi.
	ErrComponentInactive = errors.New("grades: component is inactive")
	// ErrUnknownEnrollment: isian memuat siswa yang tidak ada di tabel kelas.
	ErrUnknownEnrollment = errors.New("grades: enrollment is not part of this class")
)

// ScoreError berisi pesan per enrollment untuk isian nilai yang tidak valid.
type ScoreError struct {
	Fields map[string]string
}

func (e *ScoreError) Error() string { return fmt.Sprintf("grades: %d invalid scores", len(e.Fields)) }

// InputService mengatur pengisian nilai oleh guru mapel.
type InputService struct {
	db database.DB
}

// NewInputService mengembalikan InputService.
func NewInputService(db database.DB) *InputService { return &InputService{db: db} }

// CanEnterGrades melaporkan apakah id boleh mengisi nilai mapel subjectID di
// kelas classID: hanya guru yang punya penugasan mapel itu di kelas itu
// (T65). Wali kelas tidak otomatis boleh mengisi mapel yang bukan miliknya.
func CanEnterGrades(ctx context.Context, q database.DBTX, id auth.Identity, classID, subjectID string) (bool, error) {
	if !id.HasRole(auth.RoleGuru) || !database.IsUUID(classID) || !database.IsUUID(subjectID) {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM teaching_assignments
		WHERE school_id = $1 AND class_id = $2 AND subject_id = $3 AND teacher_id = $4)`,
		id.SchoolID, classID, subjectID, id.UserID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("grades: check grade access: %w", err)
	}
	return ok, nil
}

// Teaching adalah satu pasangan kelas–mapel yang diampu guru.
type Teaching struct {
	ClassID, ClassName     string
	SubjectID, SubjectName string
	ActiveComponents       int
}

// MyTeaching mengembalikan kelas dan mapel yang diampu teacherID di tahun
// ajaran aktif, urut tingkat, kelas, lalu mapel.
func (s *InputService) MyTeaching(ctx context.Context, schoolID, teacherID string) ([]Teaching, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.name, sub.id, sub.name,
		       (SELECT count(*) FROM grade_components gc
		        WHERE gc.school_id = ta.school_id AND gc.academic_year_id = ta.academic_year_id AND gc.subject_id = sub.id AND gc.is_active)
		FROM teaching_assignments ta
		JOIN classes c ON c.school_id = ta.school_id AND c.id = ta.class_id
		JOIN subjects sub ON sub.school_id = ta.school_id AND sub.id = ta.subject_id
		JOIN academic_years y ON y.school_id = ta.school_id AND y.id = ta.academic_year_id AND y.is_active
		WHERE ta.school_id = $1 AND ta.teacher_id = $2
		ORDER BY c.grade_level, lower(c.name), lower(sub.name)`, schoolID, teacherID)
	if err != nil {
		return nil, fmt.Errorf("grades: my teaching: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Teaching, error) {
		var t Teaching
		err := row.Scan(&t.ClassID, &t.ClassName, &t.SubjectID, &t.SubjectName, &t.ActiveComponents)
		return t, err
	})
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
		return classes.Class{}, fmt.Errorf("grades: class year: %w", err)
	}
	if !active {
		return classes.Class{}, ErrClassNotFound
	}
	return c, nil
}

// getComponent mengembalikan komponen milik schoolID.
func getComponent(ctx context.Context, q database.DBTX, schoolID, id string) (Component, error) {
	if !database.IsUUID(id) {
		return Component{}, ErrComponentNotFound
	}
	var c Component
	err := q.QueryRow(ctx, `SELECT `+componentColumns+` FROM grade_components WHERE school_id = $1 AND id = $2`, schoolID, id).
		Scan(&c.ID, &c.AcademicYearID, &c.SubjectID, &c.Name, &c.Weight, &c.SortOrder, &c.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return Component{}, ErrComponentNotFound
	}
	if err != nil {
		return Component{}, mapError("get component", err)
	}
	return c, nil
}

// Target adalah kelas, mapel, dan komponen yang sudah diperiksa
// kepemilikannya (sekolah dan tahun ajaran yang sama).
type Target struct {
	Class     classes.Class
	Subject   academic.Subject
	Component Component
}

// ResolveComponent memeriksa bahwa kelas classID (tahun ajaran aktif) dan
// komponen componentID milik schoolID dan berada di tahun ajaran yang sama.
// Hak akses guru diperiksa terpisah dengan CanEnterGrades.
func ResolveComponent(ctx context.Context, q database.DBTX, schoolID, classID, componentID string) (Target, error) {
	c, err := activeClass(ctx, q, schoolID, classID)
	if err != nil {
		return Target{}, err
	}
	comp, err := getComponent(ctx, q, schoolID, componentID)
	if err != nil {
		return Target{}, err
	}
	if comp.AcademicYearID != c.AcademicYearID {
		return Target{}, ErrComponentNotFound
	}
	sub, err := getSubject(ctx, q, schoolID, comp.SubjectID)
	if err != nil {
		return Target{}, err
	}
	return Target{Class: c, Subject: sub, Component: comp}, nil
}

// InputPage adalah isi halaman input nilai satu kelas, mapel, dan komponen.
type InputPage struct {
	Target
	Components []Component // komponen aktif mapel, untuk tab
	Rows       []StudentRow
}

// Page menyusun halaman input. componentID kosong memilih komponen aktif
// pertama; ErrComponentNotFound jika mapel belum punya komponen aktif.
func (s *InputService) Page(ctx context.Context, schoolID, classID, subjectID, componentID string) (InputPage, error) {
	c, err := activeClass(ctx, s.db, schoolID, classID)
	if err != nil {
		return InputPage{}, err
	}
	sub, err := getSubject(ctx, s.db, schoolID, subjectID)
	if err != nil {
		return InputPage{}, err
	}
	all, err := ListComponents(ctx, s.db, schoolID, c.AcademicYearID, sub.ID)
	if err != nil {
		return InputPage{}, err
	}
	p := InputPage{Target: Target{Class: c, Subject: sub}}
	for _, comp := range all {
		if comp.IsActive {
			p.Components = append(p.Components, comp)
		}
	}
	switch {
	case componentID != "":
		found := false
		for _, comp := range all {
			if comp.ID == componentID {
				p.Component, found = comp, true
			}
		}
		if !found {
			return InputPage{}, ErrComponentNotFound
		}
	case len(p.Components) > 0:
		p.Component = p.Components[0]
	default:
		return p, nil
	}
	if p.Rows, err = ClassComponentRows(ctx, s.db, schoolID, c.ID, p.Component.ID); err != nil {
		return InputPage{}, err
	}
	return p, nil
}

// InputResult merangkum hasil simpan.
type InputResult struct {
	Changed int // nilai yang berubah
	Graded  int // siswa yang sudah punya nilai setelah simpan
	Empty   int // siswa yang belum dinilai setelah simpan
}

// Save menyimpan nilai kelas t.Class untuk komponen t.Component dalam satu
// transaksi (semua atau tidak sama sekali). scores berisi enrollment_id ->
// isian; kosong disimpan sebagai NULL (belum dinilai), bukan 0. Satu isian
// tidak valid membatalkan semuanya (ScoreError). Nilai yang tidak berubah
// tidak disentuh. Setiap perubahan diaudit dengan nilai sebelum dan sesudah.
func (s *InputService) Save(ctx context.Context, actor school.Actor, t Target, scores map[string]string) (InputResult, error) {
	if !t.Component.IsActive {
		return InputResult{}, ErrComponentInactive
	}
	var res InputResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Kunci komponen agar dua simpan bersamaan untuk komponen yang sama
		// berurutan; yang kedua membandingkan dengan hasil yang pertama.
		var active bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM grade_components WHERE school_id = $1 AND id = $2 FOR UPDATE`,
			actor.SchoolID, t.Component.ID).Scan(&active); err != nil {
			return mapError("lock component", err)
		}
		if !active {
			return ErrComponentInactive
		}
		rows, err := ClassComponentRows(ctx, tx, actor.SchoolID, t.Class.ID, t.Component.ID)
		if err != nil {
			return err
		}
		byEnrollment := make(map[string]StudentRow, len(rows))
		for _, r := range rows {
			byEnrollment[r.EnrollmentID] = r
		}
		bad := map[string]string{}
		parsed := make(map[string]*Score, len(scores))
		for enrollmentID, raw := range scores {
			if _, ok := byEnrollment[enrollmentID]; !ok {
				return ErrUnknownEnrollment
			}
			score, empty, ok := ParseScore(raw)
			switch {
			case !ok:
				bad[enrollmentID] = "Nilai 0–100, maksimal dua angka di belakang koma."
			case empty:
				parsed[enrollmentID] = nil
			default:
				parsed[enrollmentID] = &score
			}
		}
		if len(bad) > 0 {
			return &ScoreError{Fields: bad}
		}

		for _, r := range rows {
			newScore, submitted := parsed[r.EnrollmentID]
			if submitted {
				var oldScore *Score
				if r.Grade != nil {
					oldScore = r.Grade.Score
				}
				if !sameScore(oldScore, newScore) {
					if err := s.record(ctx, tx, actor, t, r, oldScore, newScore); err != nil {
						return err
					}
					res.Changed++
				}
			} else if r.Grade != nil {
				newScore = r.Grade.Score
			}
			if newScore != nil {
				res.Graded++
			} else if r.Active {
				res.Empty++
			}
		}
		return nil
	})
	if err != nil {
		return InputResult{}, err
	}
	return res, nil
}

// record menyimpan satu perubahan nilai beserta auditnya.
func (s *InputService) record(ctx context.Context, tx pgx.Tx, actor school.Actor, t Target, r StudentRow, before, after *Score) error {
	action, gradeID := ActionGradeUpdate, ""
	if r.Grade == nil {
		id, err := insertGrade(ctx, tx, actor.SchoolID, t.Class.AcademicYearID, r.EnrollmentID, t.Component.ID, after, actor.UserID)
		if err != nil {
			return err
		}
		action, gradeID = ActionGradeCreate, id
	} else {
		gradeID = r.Grade.ID
		if err := updateGrade(ctx, tx, actor.SchoolID, gradeID, after, actor.UserID); err != nil {
			return err
		}
	}
	e := audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP, Action: action, EntityType: "grade", EntityID: gradeID,
		After: gradeSnapshot(r.EnrollmentID, t.Component.ID, after)}
	if action == ActionGradeUpdate {
		e.Before = gradeSnapshot(r.EnrollmentID, t.Component.ID, before)
	}
	return audit.Record(ctx, tx, e)
}

// gradeSnapshot hanya memuat ID dan nilai, tanpa nama siswa.
func gradeSnapshot(enrollmentID, componentID string, s *Score) map[string]any {
	var v any
	if s != nil {
		v = s.decimal()
	}
	return map[string]any{"enrollment_id": enrollmentID, "component_id": componentID, "score": v}
}

func sameScore(a, b *Score) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
