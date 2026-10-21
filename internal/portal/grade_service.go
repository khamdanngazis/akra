package portal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/grades"
	"akra/internal/school"
)

// ActionReportCardDownloadParent dicatat setiap orang tua mengunduh rapor.
const ActionReportCardDownloadParent = "report_card.download_parent"

// ComponentGrade adalah nilai satu komponen untuk orang tua. Score nil =
// belum dinilai.
type ComponentGrade struct {
	Name   string
	Weight int
	Score  *grades.Score
}

// SubjectGrade adalah nilai anak di satu mapel. Jika Incomplete, Score dan
// Letter tidak boleh ditampilkan: nilai akhir hanya ada jika semua komponen
// sudah dinilai.
type SubjectGrade struct {
	Name       string
	KKM        int
	Configured bool // mapel sudah punya komponen nilai aktif
	Incomplete bool
	Missing    int
	Score      int
	Letter     string
	BelowKKM   bool
	Components []ComponentGrade // hanya jika sekolah mengaktifkan report.portal_components
}

// ChildGrades adalah halaman nilai anak.
type ChildGrades struct {
	Child          ChildDetail // ID, NIS, FullName, ClassName, YearName
	HasClass       bool
	KKMLabel       string
	ShowComponents bool
	Subjects       []SubjectGrade
	// Complete: semua mapel punya nilai akhir, rapor bisa diunduh (T74).
	Complete bool
}

// activeEnrollment mengembalikan enrollment anak yang masih aktif di tahun
// ajaran aktif; ok false jika anak belum ditempatkan.
func (s *Service) activeEnrollment(ctx context.Context, schoolID, studentID string) (string, bool, error) {
	var id string
	err := s.db.QueryRow(ctx, `
		SELECT e.id FROM enrollments e
		JOIN academic_years y ON y.school_id = e.school_id AND y.id = e.academic_year_id AND y.is_active
		WHERE e.school_id = $1 AND e.student_id = $2 AND e.end_date IS NULL`, schoolID, studentID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("portal: active enrollment: %w", err)
	}
	return id, true, nil
}

// GetChildGrades mengembalikan nilai anak studentID milik akun userID
// (kepemilikan diperiksa checkChild, T44). Angka diambil dari
// grades.ClassSummary, sehingga sama dengan rekap guru (T72).
func (s *Service) GetChildGrades(ctx context.Context, schoolID, userID, studentID string) (ChildGrades, error) {
	if err := s.checkChild(ctx, schoolID, userID, studentID); err != nil {
		return ChildGrades{}, err
	}
	var g ChildGrades
	if err := s.db.QueryRow(ctx, `SELECT id, nis, full_name FROM students WHERE school_id = $1 AND id = $2`, schoolID, studentID).
		Scan(&g.Child.ID, &g.Child.NIS, &g.Child.FullName); err != nil {
		return ChildGrades{}, fmt.Errorf("portal: get child: %w", err)
	}
	enrollmentID, ok, err := s.activeEnrollment(ctx, schoolID, studentID)
	if err != nil || !ok {
		return g, err
	}
	e, err := grades.GetCardEnrollment(ctx, s.db, schoolID, enrollmentID)
	if err != nil {
		return ChildGrades{}, err
	}
	cfg, err := grades.LoadReportConfig(ctx, s.db, schoolID)
	if err != nil {
		return ChildGrades{}, err
	}
	summary, err := grades.ClassSummary(ctx, s.db, schoolID, e.Class)
	if err != nil {
		return ChildGrades{}, err
	}
	g.HasClass, g.Child.ClassName, g.KKMLabel, g.ShowComponents = true, e.Class.Name, cfg.ThresholdLabel(), cfg.PortalComponents
	g.Complete = len(summary.Subjects) > 0
	for _, sub := range summary.Subjects {
		sg := SubjectGrade{Name: sub.Subject.Name, KKM: sub.KKM, Configured: sub.Configured}
		for _, row := range sub.Rows {
			if row.EnrollmentID != enrollmentID {
				continue
			}
			if sub.Configured {
				sg.Incomplete, sg.Missing = row.Result.Incomplete, row.Result.Missing
				if !sg.Incomplete {
					sg.Score, sg.Letter, sg.BelowKKM = row.Result.Score, row.Letter, row.BelowKKM
				}
			}
			if cfg.PortalComponents {
				for i, c := range sub.Components {
					sg.Components = append(sg.Components, ComponentGrade{Name: c.Name, Weight: c.Weight, Score: row.Scores[i]})
				}
			}
		}
		if !sg.Configured || sg.Incomplete {
			g.Complete = false
		}
		g.Subjects = append(g.Subjects, sg)
	}
	return g, nil
}

// ErrReportCardUnavailable: rapor anak belum bisa diunduh (belum ditempatkan
// di kelas atau ada mapel yang belum lengkap).
var ErrReportCardUnavailable = errors.New("portal: report card not available")

// ChildReportCard menyusun rapor anak studentID milik akun userID dan
// mencatat unduhan di audit. Aturan kelengkapan sama dengan T74.
func (s *Service) ChildReportCard(ctx context.Context, actor school.Actor, studentID string, semester int, today time.Time) (grades.ReportCard, error) {
	if err := s.checkChild(ctx, actor.SchoolID, actor.UserID, studentID); err != nil {
		return grades.ReportCard{}, err
	}
	enrollmentID, ok, err := s.activeEnrollment(ctx, actor.SchoolID, studentID)
	if err != nil {
		return grades.ReportCard{}, err
	}
	if !ok {
		return grades.ReportCard{}, ErrReportCardUnavailable
	}
	e, err := grades.GetCardEnrollment(ctx, s.db, actor.SchoolID, enrollmentID)
	if err != nil {
		return grades.ReportCard{}, err
	}
	card, err := grades.BuildReportCard(ctx, s.db, actor.SchoolID, e, semester, today)
	var ie *grades.IncompleteError
	if errors.As(err, &ie) {
		return grades.ReportCard{}, ErrReportCardUnavailable
	}
	if err != nil {
		return grades.ReportCard{}, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionReportCardDownloadParent, EntityType: "enrollment", EntityID: enrollmentID,
			After: map[string]any{"semester": semester}})
	})
	if err != nil {
		return grades.ReportCard{}, err
	}
	return card, nil
}
