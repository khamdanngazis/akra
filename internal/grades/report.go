package grades

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/classes"
	"akra/internal/platform/database"
)

// Rekap nilai (T72). Nilai akhir selalu dihitung dengan FinalScore di Go,
// tidak di SQL, agar rekap, rapor (T74), portal (T75), dan ekspor (T79)
// memakai aturan yang sama.

// StudentGrades adalah satu siswa di rekap satu mapel.
type StudentGrades struct {
	EnrollmentID string
	StudentID    string
	NIS          string
	FullName     string
	Scores       []*Score // sejajar dengan SubjectReport.Components; nil = belum dinilai
	Result       Result
	Letter       string // kosong jika belum lengkap atau komponen belum diatur
}

// SubjectReport adalah rekap satu mapel di satu kelas.
type SubjectReport struct {
	Subject     academic.Subject
	TeacherName string      // guru pengampu di kelas ini (T65); kosong jika belum ada
	Components  []Component // komponen aktif, urut tampil
	// Configured false jika mapel belum punya komponen aktif dengan total
	// bobot 100; nilai akhir lalu tidak dihitung.
	Configured bool
	Rows       []StudentGrades
	Complete   int    // siswa dengan nilai akhir
	Incomplete int    // siswa yang belum lengkap
	EmptyCells int    // sel nilai kosong (siswa × komponen)
	Average    *Score // rata-rata nilai akhir siswa yang lengkap, perseratus; nil jika belum ada
}

// ClassReport adalah ringkasan semua mapel satu kelas.
type ClassReport struct {
	Class    classes.Class
	Students int
	Subjects []SubjectReport
}

// classStudent adalah siswa yang sedang terdaftar di kelas.
type classStudent struct {
	EnrollmentID, StudentID, NIS, FullName string
}

// SubjectSummary mengembalikan rekap mapel subjectID di kelas c: satu baris
// per siswa yang sedang terdaftar dengan nilai per komponen aktif, nilai
// akhir, dan huruf.
func SubjectSummary(ctx context.Context, q database.DBTX, schoolID string, c classes.Class, sub academic.Subject) (SubjectReport, error) {
	reports, _, err := buildReports(ctx, q, schoolID, c, []academic.Subject{sub})
	if err != nil {
		return SubjectReport{}, err
	}
	return reports[0], nil
}

// ClassSummary mengembalikan ringkasan kelas c untuk setiap mapel yang punya
// guru pengampu di kelas itu (T65), urut nama mapel.
func ClassSummary(ctx context.Context, q database.DBTX, schoolID string, c classes.Class) (ClassReport, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id, s.school_id, s.name, coalesce(s.code, ''), s.is_active
		FROM teaching_assignments ta JOIN subjects s ON s.school_id = ta.school_id AND s.id = ta.subject_id
		WHERE ta.school_id = $1 AND ta.class_id = $2
		ORDER BY lower(s.name)`, schoolID, c.ID)
	if err != nil {
		return ClassReport{}, fmt.Errorf("grades: class subjects: %w", err)
	}
	subjects, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (academic.Subject, error) {
		var s academic.Subject
		err := row.Scan(&s.ID, &s.SchoolID, &s.Name, &s.Code, &s.IsActive)
		return s, err
	})
	if err != nil {
		return ClassReport{}, fmt.Errorf("grades: class subjects: %w", err)
	}
	reports, students, err := buildReports(ctx, q, schoolID, c, subjects)
	if err != nil {
		return ClassReport{}, err
	}
	return ClassReport{Class: c, Students: students, Subjects: reports}, nil
}

// buildReports memuat siswa, komponen aktif, guru pengampu, dan nilai untuk
// semua mapel sekaligus (tiga query, tanpa N+1), lalu menghitung nilai akhir
// dengan FinalScore.
func buildReports(ctx context.Context, q database.DBTX, schoolID string, c classes.Class, subjects []academic.Subject) ([]SubjectReport, int, error) {
	scale, err := LoadScale(ctx, q, schoolID)
	if err != nil {
		return nil, 0, err
	}
	students, err := classStudents(ctx, q, schoolID, c.ID)
	if err != nil {
		return nil, 0, err
	}
	subjectIDs := make([]string, len(subjects))
	for i, s := range subjects {
		subjectIDs[i] = s.ID
	}
	comps, err := activeComponents(ctx, q, schoolID, c.AcademicYearID, subjectIDs)
	if err != nil {
		return nil, 0, err
	}
	teachers, err := classTeachers(ctx, q, schoolID, c.ID)
	if err != nil {
		return nil, 0, err
	}
	var compIDs, enrollmentIDs []string
	for _, list := range comps {
		for _, comp := range list {
			compIDs = append(compIDs, comp.ID)
		}
	}
	for _, s := range students {
		enrollmentIDs = append(enrollmentIDs, s.EnrollmentID)
	}
	scores, err := loadScores(ctx, q, schoolID, compIDs, enrollmentIDs)
	if err != nil {
		return nil, 0, err
	}

	reports := make([]SubjectReport, 0, len(subjects))
	for _, sub := range subjects {
		rep := SubjectReport{Subject: sub, TeacherName: teachers[sub.ID], Components: comps[sub.ID]}
		weights := 0
		for _, comp := range rep.Components {
			weights += comp.Weight
		}
		rep.Configured = len(rep.Components) > 0 && weights == 100
		var sum int64
		for _, st := range students {
			row := StudentGrades{EnrollmentID: st.EnrollmentID, StudentID: st.StudentID, NIS: st.NIS, FullName: st.FullName}
			in := make([]ComponentScore, len(rep.Components))
			for i, comp := range rep.Components {
				s := scores[gradeKey{st.EnrollmentID, comp.ID}]
				row.Scores = append(row.Scores, s)
				in[i] = ComponentScore{Weight: comp.Weight, Score: s}
				if s == nil {
					rep.EmptyCells++
				}
			}
			if rep.Configured {
				res, err := FinalScore(in)
				if err != nil {
					return nil, 0, fmt.Errorf("grades: final score %s: %w", sub.ID, err)
				}
				row.Result = res
				if res.Incomplete {
					rep.Incomplete++
				} else {
					rep.Complete++
					sum += int64(res.Exact)
					if row.Letter, err = Letter(res.Score, scale); err != nil {
						return nil, 0, err
					}
				}
			}
			rep.Rows = append(rep.Rows, row)
		}
		if rep.Complete > 0 {
			avg := Score(roundHalfUp(sum, int64(rep.Complete)))
			rep.Average = &avg
		}
		reports = append(reports, rep)
	}
	return reports, len(students), nil
}

func classStudents(ctx context.Context, q database.DBTX, schoolID, classID string) ([]classStudent, error) {
	rows, err := q.Query(ctx, `
		SELECT e.id, st.id, st.nis, st.full_name
		FROM enrollments e JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		WHERE e.school_id = $1 AND e.class_id = $2 AND e.end_date IS NULL
		ORDER BY lower(st.full_name), st.nis`, schoolID, classID)
	if err != nil {
		return nil, fmt.Errorf("grades: class students: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (classStudent, error) {
		var s classStudent
		err := row.Scan(&s.EnrollmentID, &s.StudentID, &s.NIS, &s.FullName)
		return s, err
	})
}

// activeComponents mengembalikan komponen aktif per mapel di tahun yearID.
func activeComponents(ctx context.Context, q database.DBTX, schoolID, yearID string, subjectIDs []string) (map[string][]Component, error) {
	rows, err := q.Query(ctx, `SELECT `+componentColumns+` FROM grade_components
		WHERE school_id = $1 AND academic_year_id = $2 AND subject_id = ANY($3::uuid[]) AND is_active
		ORDER BY sort_order, lower(name)`, schoolID, yearID, subjectIDs)
	if err != nil {
		return nil, mapError("active components", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Component, error) {
		var c Component
		err := row.Scan(&c.ID, &c.AcademicYearID, &c.SubjectID, &c.Name, &c.Weight, &c.SortOrder, &c.IsActive)
		return c, err
	})
	if err != nil {
		return nil, mapError("active components", err)
	}
	out := map[string][]Component{}
	for _, c := range list {
		out[c.SubjectID] = append(out[c.SubjectID], c)
	}
	return out, nil
}

// classTeachers mengembalikan nama guru pengampu per mapel di kelas classID.
func classTeachers(ctx context.Context, q database.DBTX, schoolID, classID string) (map[string]string, error) {
	rows, err := q.Query(ctx, `
		SELECT ta.subject_id, u.full_name FROM teaching_assignments ta
		JOIN users u ON u.school_id = ta.school_id AND u.id = ta.teacher_id
		WHERE ta.school_id = $1 AND ta.class_id = $2`, schoolID, classID)
	if err != nil {
		return nil, fmt.Errorf("grades: class teachers: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var subjectID, name string
		if err := rows.Scan(&subjectID, &name); err != nil {
			return nil, fmt.Errorf("grades: class teachers: %w", err)
		}
		out[subjectID] = name
	}
	return out, rows.Err()
}

type gradeKey struct{ enrollmentID, componentID string }

// loadScores mengembalikan nilai yang sudah diisi (bukan NULL) untuk
// pasangan enrollment dan komponen.
func loadScores(ctx context.Context, q database.DBTX, schoolID string, componentIDs, enrollmentIDs []string) (map[gradeKey]*Score, error) {
	out := map[gradeKey]*Score{}
	if len(componentIDs) == 0 || len(enrollmentIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT enrollment_id, component_id, (score * 100)::int FROM grades
		WHERE school_id = $1 AND component_id = ANY($2::uuid[]) AND enrollment_id = ANY($3::uuid[]) AND score IS NOT NULL`,
		schoolID, componentIDs, enrollmentIDs)
	if err != nil {
		return nil, fmt.Errorf("grades: load scores: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k gradeKey
		var v int32
		if err := rows.Scan(&k.enrollmentID, &k.componentID, &v); err != nil {
			return nil, fmt.Errorf("grades: load scores: %w", err)
		}
		s := Score(v)
		out[k] = &s
	}
	return out, rows.Err()
}
