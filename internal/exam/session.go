package exam

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"akra/internal/platform/database"
)

// Lokasi sesi.
const (
	LocationRoom   = "di_ruang"
	LocationRemote = "jarak_jauh"
)

// Jenis sesi.
const (
	SessionMain       = "utama"
	SessionMakeup     = "susulan"
	SessionRemedial   = "remedial"
	SessionEnrichment = "pengayaan"
)

var sessionKindLabels = map[string]string{
	SessionMain: "Utama", SessionMakeup: "Susulan", SessionRemedial: "Remedial", SessionEnrichment: "Pengayaan",
}

// Session adalah satu sesi ujian.
type Session struct {
	ID           string
	ExamID       string
	Kind         string
	Label        string
	ClassID      string
	ClassName    string
	PeriodID     string
	TrackID      string
	Location     string
	StartsAt     time.Time
	EndsAt       time.Time
	Participants int
	Proctors     []string // nama pengawas
	ProctorIDs   []string
	CancelledAt  *time.Time
}

// KindLabel mengembalikan nama jenis sesi.
func (s Session) KindLabel() string { return sessionKindLabels[s.Kind] }

// Title mengembalikan nama sesi untuk tampilan.
func (s Session) Title() string {
	switch {
	case s.Label != "":
		return s.Label
	case s.ClassName != "":
		return "Kelas " + s.ClassName
	}
	return "Sesi"
}

// LocationLabel mengembalikan nama lokasi.
func (s Session) LocationLabel() string {
	if s.Location == LocationRemote {
		return "Jarak jauh"
	}
	return "Di ruang"
}

const sessionCols = `x.id, x.exam_id, x.kind, x.label, coalesce(x.class_id::text, ''), coalesce(c.name, ''), coalesce(x.period_id::text, ''),
	coalesce(x.track_id::text, ''), x.location, x.starts_at, x.ends_at,
	(SELECT count(*) FROM exam_session_participants p WHERE p.session_id = x.id),
	coalesce((SELECT array_agg(u.full_name ORDER BY u.full_name) FROM exam_session_proctors sp JOIN users u ON u.id = sp.user_id WHERE sp.session_id = x.id), '{}'),
	coalesce((SELECT array_agg(sp.user_id::text ORDER BY sp.user_id) FROM exam_session_proctors sp WHERE sp.session_id = x.id), '{}'),
	x.cancelled_at`

func scanSession(r pgx.Row) (Session, error) {
	var s Session
	err := r.Scan(&s.ID, &s.ExamID, &s.Kind, &s.Label, &s.ClassID, &s.ClassName, &s.PeriodID, &s.TrackID, &s.Location, &s.StartsAt,
		&s.EndsAt, &s.Participants, &s.Proctors, &s.ProctorIDs, &s.CancelledAt)
	return s, err
}

// ListSessions mengembalikan sesi ujian yang tidak dibatalkan, urut waktu.
func ListSessions(ctx context.Context, q database.DBTX, schoolID, examID string) ([]Session, error) {
	rows, err := q.Query(ctx, `SELECT `+sessionCols+` FROM exam_sessions x LEFT JOIN classes c ON c.id = x.class_id
		WHERE x.school_id = $1 AND x.exam_id = $2 AND x.cancelled_at IS NULL ORDER BY x.starts_at, c.name, x.label`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) { return scanSession(r) })
	if err != nil {
		return nil, fmt.Errorf("exam: list sessions: %w", err)
	}
	return out, nil
}

// GetSession mengembalikan satu sesi milik sekolah.
func GetSession(ctx context.Context, q database.DBTX, schoolID, id string) (Session, error) {
	if !database.IsUUID(id) {
		return Session{}, ErrNotFound
	}
	s, err := scanSession(q.QueryRow(ctx, `SELECT `+sessionCols+` FROM exam_sessions x LEFT JOIN classes c ON c.id = x.class_id
		WHERE x.school_id = $1 AND x.id = $2`, schoolID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("exam: get session: %w", err)
	}
	return s, nil
}

// ClassOption adalah kelas yang bisa dipilih untuk ulangan.
type ClassOption struct {
	ID, Name   string
	GradeLevel int
	Students   int
}

// ClassesFor mengembalikan kelas tahun ajaran aktif yang boleh dipilih a
// untuk ujian e: guru hanya kelas yang ia ampu untuk mapel ujian;
// admin/koordinator semua kelas (setingkat bila tingkat ujian diisi).
func ClassesFor(ctx context.Context, q database.DBTX, a Actor, e Exam, now time.Time) ([]ClassOption, error) {
	rows, err := q.Query(ctx, `
		SELECT c.id, c.name, c.grade_level,
		       (SELECT count(*) FROM enrollments en JOIN students s ON s.id = en.student_id
		        WHERE en.class_id = c.id AND en.end_date IS NULL AND s.status = 'aktif')
		FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.school_id = c.school_id AND y.is_active
		WHERE c.school_id = $1 AND ($2 = 0 OR c.grade_level = $2)
		ORDER BY c.grade_level, c.name`, e.SchoolID, e.GradeLevel)
	if err != nil {
		return nil, fmt.Errorf("exam: list classes: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ClassOption, error) {
		var c ClassOption
		var g int16
		err := r.Scan(&c.ID, &c.Name, &g, &c.Students)
		c.GradeLevel = int(g)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: list classes: %w", err)
	}
	var out []ClassOption
	for _, c := range all {
		if canPickClass(a, e, c.ID, now) {
			out = append(out, c)
		}
	}
	return out, nil
}

// canPickClass melaporkan apakah a boleh menjadwalkan ujian e untuk kelas
// classID: guru hanya kelas+mapel yang ia ampu.
func canPickClass(a Actor, e Exam, classID string, now time.Time) bool {
	if a.Can(ActClassExam, Resource{SubjectID: e.SubjectID, ClassID: "*"}, now) >= Write {
		return true
	}
	return slices.Contains(a.Classes, classID+"|"+e.SubjectID)
}

// ClassSlot adalah jadwal ulangan satu kelas.
type ClassSlot struct {
	ClassID    string
	Start, End time.Time
	Location   string
}

// ErrNotYourClass: guru memilih kelas yang tidak ia ampu.
var ErrNotYourClass = errors.New("exam: class not taught by this teacher")

func checkWindow(e Exam, start, end, now time.Time) map[string]string {
	errs := map[string]string{}
	switch {
	case start.IsZero() || end.IsZero():
		errs["time"] = "Isi tanggal, jam mulai, dan jam selesai."
	case !end.After(start):
		errs["time"] = "Jam selesai harus setelah jam mulai."
	case end.Sub(start) < time.Duration(e.DurationMinutes)*time.Minute:
		errs["time"] = fmt.Sprintf("Jendela sesi lebih pendek dari durasi ujian (%d menit).", e.DurationMinutes)
	case !end.After(now):
		errs["time"] = "Waktu sesi sudah lewat."
	case end.Sub(start) > 14*24*time.Hour:
		errs["time"] = "Jendela sesi maksimal 14 hari."
	}
	return errs
}

// CreateClassSessions membuat satu sesi per kelas dengan peserta siswa
// aktif kelas itu. Pemegang ujian otomatis menjadi pengawas sesi kelasnya.
func CreateClassSessions(ctx context.Context, db database.DB, sa StaffActor, examID string, slots []ClassSlot, now time.Time) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, examID, now, false)
		if err != nil {
			return err
		}
		if e.Purpose == PurposeAdmission || e.Purpose.SchoolWide() || e.ArchivedAt != nil {
			return ErrForbidden
		}
		if len(slots) == 0 {
			return &FieldError{Fields: map[string]string{"classes": "Centang minimal satu kelas."}}
		}
		for _, sl := range slots {
			if !database.IsUUID(sl.ClassID) {
				return ErrNotFound
			}
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM classes c JOIN academic_years y ON y.id = c.academic_year_id AND y.is_active
				WHERE c.school_id = $1 AND c.id = $2)`, sa.SchoolID, sl.ClassID).Scan(&ok); err != nil {
				return fmt.Errorf("exam: check class: %w", err)
			}
			if !ok {
				return ErrNotFound
			}
			if !canPickClass(sa.Actor, e, sl.ClassID, now) {
				return ErrNotYourClass
			}
			if errs := checkWindow(e, sl.Start, sl.End, now); len(errs) > 0 {
				return &FieldError{Fields: errs}
			}
			loc := sl.Location
			if loc != LocationRemote {
				loc = LocationRoom
			}
			var sid string
			err := tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, class_id, location, starts_at, ends_at, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, sa.SchoolID, e.ID, sl.ClassID, loc, sl.Start, sl.End, sa.UserID).Scan(&sid)
			if err != nil {
				return fmt.Errorf("exam: create session: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no)
				SELECT $1, $2, s.id, s.nis FROM enrollments en JOIN students s ON s.id = en.student_id AND s.school_id = en.school_id
				WHERE en.school_id = $1 AND en.class_id = $3 AND en.end_date IS NULL AND s.status = 'aktif'
				ON CONFLICT DO NOTHING`, sa.SchoolID, sid, sl.ClassID); err != nil {
				return fmt.Errorf("exam: add participants: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by)
				SELECT $1, $2, $3, $4 FROM users WHERE school_id = $1 AND id = $3 AND is_active`, sa.SchoolID, sid, e.OwnerID, sa.UserID); err != nil {
				return fmt.Errorf("exam: add proctor: %w", err)
			}
			n++
		}
		if err := assignParticipantVariants(ctx, tx, e.ID); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.sessions_create", "exam", e.ID, nil, map[string]any{"sessions": n})
	})
	return n, err
}

// AdmissionSlot adalah jadwal tes seleksi untuk satu gelombang (dan jalur).
type AdmissionSlot struct {
	PeriodID, TrackID string
	Start, End        time.Time
	Location          string
}

// CreateAdmissionSession membuat sesi tes seleksi dengan peserta pendaftar
// terverifikasi pada gelombang (dan jalur) yang dipilih.
func CreateAdmissionSession(ctx context.Context, db database.DB, sa StaffActor, examID string, sl AdmissionSlot, now time.Time) (Session, error) {
	var out Session
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, examID, now, false)
		if err != nil {
			return err
		}
		if e.Purpose != PurposeAdmission || e.ArchivedAt != nil {
			return ErrForbidden
		}
		errs := checkWindow(e, sl.Start, sl.End, now)
		var ok bool
		if database.IsUUID(sl.PeriodID) {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admission_periods WHERE school_id = $1 AND id = $2)`, sa.SchoolID, sl.PeriodID).Scan(&ok); err != nil {
				return fmt.Errorf("exam: check period: %w", err)
			}
		}
		if !ok {
			errs["period_id"] = "Pilih gelombang."
		}
		if sl.TrackID != "" {
			ok = false
			if database.IsUUID(sl.TrackID) {
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admission_tracks WHERE school_id = $1 AND id = $2 AND period_id = $3)`,
					sa.SchoolID, sl.TrackID, sl.PeriodID).Scan(&ok); err != nil {
					return fmt.Errorf("exam: check track: %w", err)
				}
			}
			if !ok {
				errs["track_id"] = "Jalur tidak ada di gelombang ini."
			}
		}
		if len(errs) > 0 {
			return &FieldError{Fields: errs}
		}
		loc := sl.Location
		if loc != LocationRemote {
			loc = LocationRoom
		}
		var sid string
		err = tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, period_id, track_id, location, starts_at, ends_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, sa.SchoolID, e.ID, sl.PeriodID, nullable(sl.TrackID), loc, sl.Start, sl.End,
			sa.UserID).Scan(&sid)
		if err != nil {
			return fmt.Errorf("exam: create session: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, applicant_id, participant_no)
			SELECT $1, $2, a.id, a.registration_number FROM applicants a
			WHERE a.school_id = $1 AND a.period_id = $3 AND ($4::uuid IS NULL OR a.track_id = $4) AND a.status = 'diverifikasi'
			ORDER BY a.registration_number`, sa.SchoolID, sid, sl.PeriodID, nullable(sl.TrackID)); err != nil {
			return fmt.Errorf("exam: add applicants: %w", err)
		}
		if err := assignParticipantVariants(ctx, tx, e.ID); err != nil {
			return err
		}
		if out, err = GetSession(ctx, tx, sa.SchoolID, sid); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.sessions_create", "exam", e.ID, nil, map[string]any{"sessions": 1})
	})
	return out, err
}

// ErrSessionStarted: sesi sudah mulai sehingga tidak bisa dibatalkan.
var ErrSessionStarted = errors.New("exam: session already started")

// CancelSession membatalkan sesi yang belum mulai beserta kode ujiannya.
func CancelSession(ctx context.Context, db database.DB, sa StaffActor, examID, sessionID string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, examID, now, false)
		if err != nil {
			return err
		}
		s, err := GetSession(ctx, tx, sa.SchoolID, sessionID)
		if err != nil || s.ExamID != e.ID || s.CancelledAt != nil {
			return ErrNotFound
		}
		if !now.Before(s.StartsAt) {
			return ErrSessionStarted
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET cancelled_at = now() WHERE id = $1`, s.ID); err != nil {
			return fmt.Errorf("exam: cancel session: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE student_credentials SET revoked_at = now() WHERE school_id = $1 AND exam_session_id = $2 AND revoked_at IS NULL`,
			sa.SchoolID, s.ID); err != nil {
			return fmt.Errorf("exam: revoke codes: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.session_cancel", "exam_session", s.ID, nil, nil)
	})
}

// assignParticipantVariants membagi varian ke peserta setiap sesi ujian
// secara bergilir menurut tempat duduk lalu nomor peserta, sehingga peserta
// bersebelahan mendapat paket berbeda. Setelah ujian terkunci, pembagian
// lama dipertahankan dan hanya peserta tanpa varian yang diisi.
func assignParticipantVariants(ctx context.Context, tx pgx.Tx, examID string) error {
	vs, err := tx.Query(ctx, `SELECT id FROM exam_variants WHERE exam_id = $1 ORDER BY label`, examID)
	if err != nil {
		return fmt.Errorf("exam: assign variants: %w", err)
	}
	ids, err := pgx.CollectRows(vs, pgx.RowTo[string])
	if err != nil || len(ids) == 0 {
		return err
	}
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT locked_at IS NOT NULL FROM exams WHERE id = $1`, examID).Scan(&locked); err != nil {
		return fmt.Errorf("exam: assign variants: %w", err)
	}
	cond := ""
	if locked {
		cond = " AND p.variant_id IS NULL"
	}
	_, err = tx.Exec(ctx, `
		WITH ranked AS (
			SELECT p.id, (row_number() OVER (PARTITION BY p.session_id ORDER BY p.seat NULLS LAST, p.participant_no, p.id) - 1) AS rn
			FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
			WHERE x.exam_id = $1 AND x.cancelled_at IS NULL`+cond+`)
		UPDATE exam_session_participants p SET variant_id = ($2::uuid[])[(r.rn % array_length($2::uuid[], 1)) + 1]
		FROM ranked r WHERE r.id = p.id`, examID, ids)
	if err != nil {
		return fmt.Errorf("exam: assign variants: %w", err)
	}
	return nil
}

// LockExam membekukan soal ujian: setiap soal varian dicatat bersama versi
// soal dan stimulusnya saat ini di exam_item_snapshots. Setelah ini
// menyunting soal di bank tidak mengubah ujian. Ujian yang sudah terkunci
// tidak berubah.
func LockExam(ctx context.Context, tx pgx.Tx, schoolID, examID string, now time.Time) error {
	var locked bool
	err := tx.QueryRow(ctx, `SELECT locked_at IS NOT NULL FROM exams WHERE school_id = $1 AND id = $2 FOR UPDATE`, schoolID, examID).Scan(&locked)
	if err != nil {
		return fmt.Errorf("exam: lock exam: %w", err)
	}
	if locked {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO exam_item_snapshots (school_id, exam_id, variant_id, position, section_id, question_id, question_version,
		                                 stimulus_id, stimulus_version, points)
		SELECT vi.school_id, v.exam_id, vi.variant_id, vi.position, vi.section_id, vi.question_id, q.version, vi.stimulus_id, st.version, q.points
		FROM exam_variant_items vi
		JOIN exam_variants v ON v.id = vi.variant_id
		JOIN exam_questions q ON q.id = vi.question_id
		LEFT JOIN exam_stimuli st ON st.id = vi.stimulus_id
		WHERE v.exam_id = $1`, examID); err != nil {
		return fmt.Errorf("exam: snapshot items: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE exams SET locked_at = $2, status = 'berlangsung', updated_at = now() WHERE id = $1`, examID, now); err != nil {
		return fmt.Errorf("exam: lock exam: %w", err)
	}
	return nil
}

// AdvanceExams memajukan status ujian menurut waktu: ujian terjadwal yang
// sesi pertamanya sudah mulai dikunci (berlangsung), dan ujian berlangsung
// yang semua sesinya sudah berakhir menjadi selesai. Mengembalikan jumlah
// ujian yang dikunci dan yang selesai.
func AdvanceExams(ctx context.Context, db database.DB, now time.Time) (locked, done int, err error) {
	rows, err := db.Query(ctx, `
		SELECT e.school_id, e.id FROM exams e
		WHERE e.status = 'terjadwal' AND e.archived_at IS NULL
		  AND EXISTS (SELECT 1 FROM exam_sessions x WHERE x.exam_id = e.id AND x.cancelled_at IS NULL AND x.starts_at <= $1)`, now)
	if err != nil {
		return 0, 0, fmt.Errorf("exam: due exams: %w", err)
	}
	type key struct{ school, id string }
	due, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (key, error) {
		var k key
		return k, r.Scan(&k.school, &k.id)
	})
	if err != nil {
		return 0, 0, fmt.Errorf("exam: due exams: %w", err)
	}
	for _, k := range due {
		if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return LockExam(ctx, tx, k.school, k.id, now) }); err != nil {
			return locked, done, err
		}
		locked++
	}
	tag, err := db.Exec(ctx, `UPDATE exams e SET status = 'selesai', updated_at = now()
		WHERE e.status = 'berlangsung' AND NOT EXISTS (
			SELECT 1 FROM exam_sessions x WHERE x.exam_id = e.id AND x.cancelled_at IS NULL AND x.ends_at > $1)`, now)
	if err != nil {
		return locked, done, fmt.Errorf("exam: finish exams: %w", err)
	}
	return locked, int(tag.RowsAffected()), nil
}

// EnsureLocked mengunci ujian e bila sesi pertamanya sudah mulai. Dipanggil
// saat ujian dibuka, agar penguncian tidak bergantung pada job berkala.
func EnsureLocked(ctx context.Context, db database.DB, e Exam, now time.Time) (Exam, error) {
	if e.Locked() || e.Status != ExamScheduled {
		return e, nil
	}
	var started bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_sessions WHERE exam_id = $1 AND cancelled_at IS NULL AND starts_at <= $2)`,
		e.ID, now).Scan(&started); err != nil {
		return e, fmt.Errorf("exam: check start: %w", err)
	}
	if !started {
		return e, nil
	}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return LockExam(ctx, tx, e.SchoolID, e.ID, now) }); err != nil {
		return e, err
	}
	return GetExam(ctx, db, e.SchoolID, e.ID)
}

// AdvanceArgs adalah job berkala pemaju status ujian.
type AdvanceArgs struct{}

// Kind mengembalikan nama job.
func (AdvanceArgs) Kind() string { return "exam_advance" }

// AdvanceWorker menjalankan AdvanceExams.
type AdvanceWorker struct {
	river.WorkerDefaults[AdvanceArgs]
	DB     database.DB
	Logger *slog.Logger
	Now    func() time.Time
}

// Work menjalankan satu putaran.
func (w *AdvanceWorker) Work(ctx context.Context, _ *river.Job[AdvanceArgs]) error {
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	locked, done, err := AdvanceExams(ctx, w.DB, now())
	if err != nil {
		return err
	}
	if locked+done > 0 {
		w.Logger.InfoContext(ctx, "exams advanced", "locked", locked, "finished", done)
	}
	return nil
}

// AdvancePeriodicJob menjalankan AdvanceWorker setiap menit.
func AdvancePeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Minute),
		func() (river.JobArgs, *river.InsertOpts) { return AdvanceArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// loadProctorAssignments membaca penugasan pengawas pengguna untuk sesi
// yang belum lewat jendela hak pengawas.
func loadProctorAssignments(ctx context.Context, q database.DBTX, schoolID, userID string, now time.Time) ([]ProctorAssignment, error) {
	rows, err := q.Query(ctx, `SELECT x.id, x.starts_at, x.ends_at FROM exam_session_proctors sp
		JOIN exam_sessions x ON x.id = sp.session_id AND x.cancelled_at IS NULL
		WHERE sp.school_id = $1 AND sp.user_id = $2 AND x.ends_at > $3`, schoolID, userID, now.Add(-ProctorAfter))
	if err != nil {
		return nil, fmt.Errorf("exam: load proctor assignments: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ProctorAssignment, error) {
		var p ProctorAssignment
		return p, r.Scan(&p.SessionID, &p.Start, &p.End)
	})
}

// ParticipantRow adalah satu peserta sesi.
type ParticipantRow struct {
	ID           string
	StudentID    string
	ApplicantID  string
	Name         string
	Identifier   string // NIS atau nomor pendaftaran
	Number       string // nomor peserta
	ClassName    string
	Variant      string
	Seat         int
	ExtraMinutes int
}

// ListParticipants mengembalikan peserta sesi, urut tempat duduk lalu nomor.
func ListParticipants(ctx context.Context, q database.DBTX, schoolID, sessionID string) ([]ParticipantRow, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, coalesce(p.student_id::text, ''), coalesce(p.applicant_id::text, ''), coalesce(s.full_name, a.full_name, ''),
		       coalesce(s.nis, a.registration_number, ''), p.participant_no,
		       coalesce((SELECT c.name FROM enrollments en JOIN classes c ON c.id = en.class_id
		                 WHERE en.student_id = p.student_id AND en.end_date IS NULL ORDER BY en.start_date DESC LIMIT 1), ''),
		       coalesce(v.label, ''), coalesce(p.seat, 0), p.extra_minutes
		FROM exam_session_participants p
		LEFT JOIN students s ON s.id = p.student_id AND s.school_id = p.school_id
		LEFT JOIN applicants a ON a.id = p.applicant_id AND a.school_id = p.school_id
		LEFT JOIN exam_variants v ON v.id = p.variant_id
		WHERE p.school_id = $1 AND p.session_id = $2
		ORDER BY p.seat NULLS LAST, p.participant_no, 4`, schoolID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("exam: list participants: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ParticipantRow, error) {
		var p ParticipantRow
		var seat, extra int16
		err := r.Scan(&p.ID, &p.StudentID, &p.ApplicantID, &p.Name, &p.Identifier, &p.Number, &p.ClassName, &p.Variant, &seat, &extra)
		p.Seat, p.ExtraMinutes = int(seat), int(extra)
		return p, err
	})
}
