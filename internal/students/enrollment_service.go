package students

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// ActionStudentPlace adalah aksi audit untuk penempatan atau pindah kelas.
const ActionStudentPlace = "student.place"

// EnrollmentService menjalankan penempatan siswa di kelas.
type EnrollmentService struct {
	db database.DB
	// today mengembalikan tanggal hari ini (Asia/Jakarta); bisa diganti di uji.
	today func() time.Time
}

// NewEnrollmentService mengembalikan EnrollmentService.
func NewEnrollmentService(db database.DB) *EnrollmentService {
	return &EnrollmentService{db: db, today: clock.Today}
}

// History mengembalikan riwayat penempatan siswa, terbaru dulu.
func (s *EnrollmentService) History(ctx context.Context, schoolID, studentID string) ([]Enrollment, error) {
	return ListEnrollments(ctx, s.db, schoolID, studentID)
}

// Place menempatkan atau memindahkan siswa ke classID mulai date (nil
// berarti hari ini di Asia/Jakarta), beserta auditnya, dalam satu transaksi.
// Tahun ajaran diambil dari kelas.
func (s *EnrollmentService) Place(ctx context.Context, actor school.Actor, studentID, classID string, date *time.Time) (PlaceResult, error) {
	day := s.today()
	if date != nil {
		day = *date
	}
	var res PlaceResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		yearID, err := classYear(ctx, tx, actor.SchoolID, classID)
		if err != nil {
			return err
		}
		if res, err = Place(ctx, tx, actor.SchoolID, studentID, classID, yearID, day); err != nil {
			return err
		}
		if !res.Changed {
			return nil
		}
		var before any
		if res.Closed != nil {
			before = placementSnapshot(*res.Closed)
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionStudentPlace, EntityType: "student", EntityID: studentID,
			Before: before, After: placementSnapshot(res.Enrollment),
		})
	})
	if err != nil {
		return PlaceResult{}, err
	}
	return res, nil
}

// classYear mengembalikan tahun ajaran kelas milik schoolID.
func classYear(ctx context.Context, q database.DBTX, schoolID, classID string) (string, error) {
	if !database.IsUUID(classID) {
		return "", ErrClassNotFound
	}
	var yearID string
	err := q.QueryRow(ctx, `SELECT academic_year_id FROM classes WHERE school_id = $1 AND id = $2`, schoolID, classID).Scan(&yearID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrClassNotFound
		}
		return "", mapEnrollmentError("get class year", err)
	}
	return yearID, nil
}

type placementJSON struct {
	EnrollmentID   string  `json:"enrollment_id"`
	ClassID        string  `json:"class_id"`
	AcademicYearID string  `json:"academic_year_id"`
	StartDate      string  `json:"start_date"`
	EndDate        *string `json:"end_date"`
}

func placementSnapshot(e Enrollment) placementJSON {
	p := placementJSON{EnrollmentID: e.ID, ClassID: e.ClassID, AcademicYearID: e.AcademicYearID, StartDate: e.StartDate.Format("2006-01-02")}
	if e.EndDate != nil {
		end := e.EndDate.Format("2006-01-02")
		p.EndDate = &end
	}
	return p
}
