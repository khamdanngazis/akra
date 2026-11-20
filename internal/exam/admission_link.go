package exam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Tes seleksi online dari sisi pendaftaran (T149): pilihan ujian seleksi
// untuk tes gelombang, ringkasan sesinya, pendaftar yang terverifikasi
// setelah sesi dibuat, dan sesi latihan untuk calon siswa. Peserta tes
// adalah pendaftar berstatus diverifikasi di jalur yang memakai tes.

// testedApplicants memilih pendaftar diverifikasi di jalur bertes pada
// gelombang $periodArg (dan jalur $trackArg bila tidak NULL).
const testedApplicants = `SELECT a.id, a.registration_number FROM applicants a
	JOIN admission_tracks tr ON tr.id = a.track_id AND tr.uses_test
	WHERE a.school_id = $1 AND a.period_id = $3 AND ($4::uuid IS NULL OR a.track_id = $4) AND a.status = 'diverifikasi'`

// AdmissionExam adalah ujian seleksi yang bisa dipakai tes gelombang.
type AdmissionExam struct {
	ID, Name, Status string
	TestID           string // tes yang sudah memakainya ("" = belum)
}

// StatusLabel menampilkan status ujian.
func (a AdmissionExam) StatusLabel() string { return examStatusLabels[a.Status] }

// AdmissionExams mengembalikan ujian seleksi sekolah yang belum diarsip.
func AdmissionExams(ctx context.Context, q database.DBTX, schoolID string) ([]AdmissionExam, error) {
	rows, err := q.Query(ctx, `SELECT e.id, e.name, e.status, coalesce(t.id::text, '') FROM exams e
		LEFT JOIN admission_tests t ON t.exam_id = e.id
		WHERE e.school_id = $1 AND e.purpose = 'seleksi' AND e.archived_at IS NULL ORDER BY e.created_at DESC`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: admission exams: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdmissionExam, error) {
		var a AdmissionExam
		return a, r.Scan(&a.ID, &a.Name, &a.Status, &a.TestID)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: admission exams: %w", err)
	}
	return out, nil
}

// CheckAdmissionExam memastikan examID ujian seleksi sekolah ini yang
// belum diarsip; ErrNotFound bila bukan (termasuk ujian sekolah lain).
func CheckAdmissionExam(ctx context.Context, q database.DBTX, schoolID, examID string) error {
	if !database.IsUUID(examID) {
		return ErrNotFound
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exams WHERE school_id = $1 AND id = $2 AND purpose = 'seleksi' AND archived_at IS NULL)`,
		schoolID, examID).Scan(&ok); err != nil {
		return fmt.Errorf("exam: check admission exam: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// AdmissionSession adalah satu sesi tes online untuk gelombang.
type AdmissionSession struct {
	ID           string
	Title        string
	Start, End   time.Time
	Remote       bool
	Participants int
	Submitted    int
	Ended        bool
}

// OnlineTest adalah ringkasan tes online satu gelombang.
type OnlineTest struct {
	ExamID, ExamName, ExamStatus string
	Sessions                     []AdmissionSession
	Open                         []AdmissionSession // sesi yang belum berakhir (tujuan pendaftar baru)
	Missing                      int                // pendaftar terverifikasi belum masuk sesi mana pun
	Practice                     *AdmissionSession  // sesi latihan pendaftar yang masih buka
}

// StatusLabel menampilkan status ujian.
func (o OnlineTest) StatusLabel() string { return examStatusLabels[o.ExamStatus] }

// LoadOnlineTest meringkas ujian examID untuk gelombang periodID.
func LoadOnlineTest(ctx context.Context, q database.DBTX, schoolID, examID, periodID string, now time.Time) (OnlineTest, error) {
	var o OnlineTest
	if err := q.QueryRow(ctx, `SELECT id, name, status FROM exams WHERE school_id = $1 AND id = $2`, schoolID, examID).
		Scan(&o.ExamID, &o.ExamName, &o.ExamStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return o, ErrNotFound
		}
		return o, fmt.Errorf("exam: online test: %w", err)
	}
	sessions, err := admissionSessions(ctx, q, schoolID, examID, periodID, now)
	if err != nil {
		return o, err
	}
	o.Sessions = sessions
	for _, s := range sessions {
		if !s.Ended {
			o.Open = append(o.Open, s)
		}
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM (`+testedApplicants+`) a
		WHERE NOT EXISTS (SELECT 1 FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
		                  WHERE x.exam_id = $2 AND p.applicant_id = a.id)`, schoolID, examID, periodID, nil).Scan(&o.Missing); err != nil {
		return o, fmt.Errorf("exam: missing applicants: %w", err)
	}
	if p, ok, err := GetPractice(ctx, q, schoolID); err != nil {
		return o, err
	} else if ok {
		ps, err := admissionSessions(ctx, q, schoolID, p.ID, periodID, now)
		if err != nil {
			return o, err
		}
		for i := range ps {
			if !ps[i].Ended {
				o.Practice = &ps[i]
			}
		}
	}
	return o, nil
}

func admissionSessions(ctx context.Context, q database.DBTX, schoolID, examID, periodID string, now time.Time) ([]AdmissionSession, error) {
	rows, err := q.Query(ctx, `SELECT x.id, x.label, x.starts_at, x.ends_at + make_interval(mins => x.extra_minutes), x.location = 'jarak_jauh',
			coalesce(tr.name, ''),
			(SELECT count(*) FROM exam_session_participants p WHERE p.session_id = x.id),
			(SELECT count(DISTINCT t.participant_id) FROM exam_attempts t WHERE t.session_id = x.id AND t.status = 'dikumpulkan')
		FROM exam_sessions x LEFT JOIN admission_tracks tr ON tr.id = x.track_id
		WHERE x.school_id = $1 AND x.exam_id = $2 AND x.period_id = $3 AND x.cancelled_at IS NULL ORDER BY x.starts_at`, schoolID, examID, periodID)
	if err != nil {
		return nil, fmt.Errorf("exam: admission sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdmissionSession, error) {
		var s AdmissionSession
		var track string
		err := r.Scan(&s.ID, &s.Title, &s.Start, &s.End, &s.Remote, &track, &s.Participants, &s.Submitted)
		if s.Title == "" {
			s.Title = "Semua jalur"
			if track != "" {
				s.Title = "Jalur " + track
			}
		}
		s.Ended = !now.Before(s.End)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: admission sessions: %w", err)
	}
	return out, nil
}

// ErrSessionEnded: sesi sudah berakhir.
var ErrSessionEnded = errors.New("exam: session already ended")

// AddMissingApplicants memasukkan pendaftar terverifikasi (jalur bertes)
// gelombang sesi sessionID yang belum masuk sesi mana pun di ujiannya ke
// sesi itu, mis. yang diverifikasi setelah sesi dibuat. Sesi harus sesi
// gelombang yang belum berakhir. Mengembalikan jumlah pendaftar baru.
func AddMissingApplicants(ctx context.Context, db database.DB, sa StaffActor, examID, sessionID string, now time.Time) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if !database.IsUUID(sessionID) || !database.IsUUID(examID) {
			return ErrNotFound
		}
		var periodID string
		var trackID *string
		var ends time.Time
		err := tx.QueryRow(ctx, `SELECT x.period_id::text, x.track_id::text, x.ends_at + make_interval(mins => x.extra_minutes) FROM exam_sessions x
			JOIN exams e ON e.id = x.exam_id AND e.purpose IN ('seleksi', 'latihan')
			WHERE x.school_id = $1 AND x.exam_id = $2 AND x.id = $3 AND x.cancelled_at IS NULL AND x.period_id IS NOT NULL`,
			sa.SchoolID, examID, sessionID).Scan(&periodID, &trackID, &ends)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("exam: add applicants session: %w", err)
		}
		if !now.Before(ends) {
			return ErrSessionEnded
		}
		tag, err := tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, applicant_id, participant_no)
			SELECT $1, $2, a.id, a.registration_number FROM (`+testedApplicants+`) a
			WHERE NOT EXISTS (SELECT 1 FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
			                  WHERE x.exam_id = $5 AND p.applicant_id = a.id)
			ORDER BY a.registration_number`, sa.SchoolID, sessionID, periodID, trackID, examID)
		if err != nil {
			return fmt.Errorf("exam: add applicants: %w", err)
		}
		if n = int(tag.RowsAffected()); n == 0 {
			return nil
		}
		if err := assignParticipantVariants(ctx, tx, examID); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.applicants_add", "exam_session", sessionID, nil, map[string]any{"added": n})
	})
	return n, err
}

// PracticeWindowApplicants adalah lama sesi latihan pendaftar bawaan.
const PracticeWindowApplicants = 7 * 24 * time.Hour

// ErrNoPractice: paket ujian latihan bawaan belum dibuat (dibuat sekali
// dari menu Ujian latihan).
var ErrNoPractice = errors.New("exam: practice exam not set up")

// OpenApplicantPractice membuka ujian latihan bawaan (T136) untuk
// pendaftar terverifikasi gelombang periodID, dari rumah, sampai end
// (paling lambat 7 hari). Bila sudah ada sesi latihan gelombang yang masih
// buka, sesi itu dikembalikan dan pendaftar baru ditambahkan.
func OpenApplicantPractice(ctx context.Context, db database.DB, sa StaffActor, periodID string, end time.Time, now time.Time) (string, int, error) {
	p, ok, err := GetPractice(ctx, db, sa.SchoolID)
	if err != nil {
		return "", 0, err
	}
	if !ok {
		return "", 0, ErrNoPractice
	}
	if !database.IsUUID(periodID) {
		return "", 0, ErrNotFound
	}
	if !end.After(now.Add(time.Hour)) || end.After(now.Add(PracticeWindowApplicants)) {
		end = now.Add(PracticeWindowApplicants)
	}
	var sid string
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admission_periods WHERE school_id = $1 AND id = $2)`, sa.SchoolID, periodID).Scan(&ok); err != nil {
			return fmt.Errorf("exam: check period: %w", err)
		}
		if !ok {
			return ErrNotFound
		}
		err := tx.QueryRow(ctx, `SELECT id FROM exam_sessions WHERE school_id = $1 AND exam_id = $2 AND period_id = $3 AND cancelled_at IS NULL
			AND ends_at > $4 ORDER BY ends_at DESC LIMIT 1`, sa.SchoolID, p.ID, periodID, now).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, period_id, label, location, starts_at, ends_at, created_by)
				VALUES ($1, $2, $3, 'Latihan pendaftar', 'jarak_jauh', $4, $5, $6) RETURNING id`, sa.SchoolID, p.ID, periodID, now, end, sa.UserID).Scan(&sid)
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by) VALUES ($1, $2, $3, $3)`,
					sa.SchoolID, sid, sa.UserID)
			}
		}
		if err != nil {
			return fmt.Errorf("exam: applicant practice: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.practice_open", "exam_session", sid, nil, map[string]any{"period": periodID})
	})
	if err != nil {
		return "", 0, err
	}
	n, err := AddMissingApplicants(ctx, db, sa, p.ID, sid, now)
	return sid, n, err
}

// ApplicantMissingSessions mengembalikan nama ujian tes online gelombang
// pendaftar yang sudah punya sesi tetapi pendaftar ini belum menjadi
// pesertanya (untuk kartu langkah berikutnya di detail pendaftar).
func ApplicantMissingSessions(ctx context.Context, q database.DBTX, schoolID, applicantID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT e.name FROM applicants a
		JOIN admission_tracks tr ON tr.id = a.track_id AND tr.uses_test
		JOIN admission_tests t ON t.period_id = a.period_id AND t.exam_id IS NOT NULL
		JOIN exams e ON e.id = t.exam_id
		WHERE a.school_id = $1 AND a.id = $2 AND a.status = 'diverifikasi'
		  AND EXISTS (SELECT 1 FROM exam_sessions x WHERE x.exam_id = e.id AND x.period_id = a.period_id AND x.cancelled_at IS NULL)
		  AND NOT EXISTS (SELECT 1 FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
		                  WHERE x.exam_id = e.id AND p.applicant_id = a.id)
		ORDER BY t.sort_order`, schoolID, applicantID)
	if err != nil {
		return nil, fmt.Errorf("exam: applicant missing sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("exam: applicant missing sessions: %w", err)
	}
	return out, nil
}

// AttemptSummary meringkas attempt terakhir pendaftar pada ujian (kartu
// Nilai tes di detail pendaftar).
type AttemptSummary struct {
	AttemptID string
	Duration  string
	Integrity string
	Status    string
}

// ApplicantAttempt mengembalikan ringkasan attempt attemptID.
func ApplicantAttempt(ctx context.Context, q database.DBTX, schoolID, examID, attemptID string) (AttemptSummary, error) {
	e, err := GetExam(ctx, q, schoolID, examID)
	if err != nil {
		return AttemptSummary{}, err
	}
	p, err := loadAttemptResult(ctx, q, e, attemptID)
	if err != nil {
		return AttemptSummary{}, err
	}
	return AttemptSummary{AttemptID: p.AttemptID, Duration: p.Duration(), Integrity: p.IntegrityText(), Status: p.StatusLabel()}, nil
}

// LinkAdmissionTest menautkan ujian seleksi baru examID ke tes gelombang
// testID (dibuat dari tab Tes seleksi). Hanya tes kertas tanpa nilai
// ketikan yang ditautkan; false bila tes tidak memenuhi.
func LinkAdmissionTest(ctx context.Context, db database.DB, sa StaffActor, examID, testID string) (bool, error) {
	if !database.IsUUID(testID) {
		return false, nil
	}
	linked := false
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := CheckAdmissionExam(ctx, tx, sa.SchoolID, examID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE admission_tests t SET exam_id = $3 WHERE t.school_id = $1 AND t.id = $2 AND t.exam_id IS NULL
			AND NOT EXISTS (SELECT 1 FROM applicant_test_scores s WHERE s.test_id = t.id AND s.score IS NOT NULL)`, sa.SchoolID, testID, examID)
		if err != nil {
			return fmt.Errorf("exam: link admission test: %w", err)
		}
		if linked = tag.RowsAffected() == 1; !linked {
			return nil
		}
		if _, err := tx.Exec(ctx, `DELETE FROM applicant_test_scores WHERE test_id = $1 AND score IS NULL`, testID); err != nil {
			return fmt.Errorf("exam: clear empty scores: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.admission_link", "exam", examID, nil, map[string]any{"admission_test": testID})
	})
	return linked, err
}
