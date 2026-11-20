package exam

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Purpose adalah tujuan ujian (T133).
type Purpose string

const (
	PurposeQuiz      Purpose = "ulangan"
	PurposeMidterm   Purpose = "pts"
	PurposeFinal     Purpose = "pas"
	PurposeAdmission Purpose = "seleksi"
	PurposePractice  Purpose = "latihan"
)

var purposeLabels = map[Purpose]string{
	PurposeQuiz: "Ulangan", PurposeMidterm: "PTS", PurposeFinal: "PAS", PurposeAdmission: "Tes seleksi", PurposePractice: "Ujian latihan",
}

// Purposes adalah urutan tujuan untuk pilihan form.
var Purposes = []Purpose{PurposeQuiz, PurposePractice, PurposeMidterm, PurposeFinal, PurposeAdmission}

// purposeRelease adalah tahap rilis yang membuka tujuan ujian (indeks MVP 4
// bagian 7): tes seleksi dan latihan di R1, ulangan di R2, PTS/PAS di R3.
var purposeRelease = map[Purpose]Release{
	PurposeAdmission: R1, PurposePractice: R1, PurposeQuiz: R2, PurposeMidterm: R3, PurposeFinal: R3,
}

// Release mengembalikan tahap rilis yang membuka tujuan p.
func (p Purpose) Release() Release { return purposeRelease[p] }

// Label mengembalikan nama tujuan untuk tampilan.
func (p Purpose) Label() string { return purposeLabels[p] }

// Valid melaporkan apakah p tujuan yang dikenal.
func (p Purpose) Valid() bool { _, ok := purposeLabels[p]; return ok }

// SchoolWide melaporkan apakah ujian dijadwalkan lewat agenda (PTS/PAS).
func (p Purpose) SchoolWide() bool { return p == PurposeMidterm || p == PurposeFinal }

// Status ujian.
const (
	ExamDraft     = "draf"
	ExamScheduled = "terjadwal"
	ExamRunning   = "berlangsung"
	ExamDone      = "selesai"
	ExamArchived  = "arsip"
)

var examStatusLabels = map[string]string{
	ExamDraft: "Draf", ExamScheduled: "Terjadwal", ExamRunning: "Berlangsung", ExamDone: "Selesai", ExamArchived: "Arsip",
}

// Hasil untuk peserta.
const (
	ResultsNone        = "tidak"
	ResultsScore       = "nilai"
	ResultsExplanation = "nilai_dan_pembahasan"
)

// Aturan integritas (T141).
const (
	IntegrityLog  = "catat"
	IntegrityLock = "kunci"
)

// Exam adalah satu ujian.
type Exam struct {
	ID                string
	SchoolID          string
	AcademicYearID    string
	Name              string
	Purpose           Purpose
	SubjectID         string
	SubjectName       string
	GradeLevel        int
	DurationMinutes   int
	ShuffleQuestions  bool
	ShuffleOptions    bool
	ShuffleInStimulus bool
	ResultsMode       string
	ResultsAfterAll   bool
	MaxAttempts       int
	PassScore         *int
	IntegrityMode     string
	IntegrityLimit    int
	AllowBack         bool
	VariantCount      int
	Status            string
	WizardStep        string
	OwnerID           string
	OwnerName         string
	CopiedFrom        string
	ScheduledAt       *time.Time
	LockedAt          *time.Time
	CreatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	// Builtin: paket ujian latihan bawaan sekolah (T136).
	Builtin bool
	// ParentExamID dan FollowUp ("remedial"/"pengayaan") untuk ujian
	// tindak lanjut (T137).
	ParentExamID string
	FollowUp     string
	// GradeComponentID: komponen nilai rapor tujuan kirim nilai (T150).
	GradeComponentID string
	// ResultsReleasedAt: guru membuka hasil untuk siswa dan orang tua (T151).
	ResultsReleasedAt *time.Time
}

// StatusLabel mengembalikan nama status untuk tampilan.
func (e Exam) StatusLabel() string { return examStatusLabels[e.Status] }

// Locked melaporkan apakah soal ujian sudah dibekukan.
func (e Exam) Locked() bool { return e.LockedAt != nil }

// Editable melaporkan apakah isi dan aturan ujian masih boleh diubah.
func (e Exam) Editable() bool { return e.LockedAt == nil && e.ArchivedAt == nil }

// defaultsFor mengisi aturan bawaan yang masuk akal untuk tujuan p.
func defaultsFor(p Purpose) Exam {
	e := Exam{Purpose: p, DurationMinutes: 60, ShuffleQuestions: true, ShuffleOptions: true, ResultsMode: ResultsScore,
		ResultsAfterAll: true, MaxAttempts: 1, IntegrityMode: IntegrityLog, AllowBack: true, VariantCount: 1}
	switch p {
	case PurposeMidterm, PurposeFinal:
		e.DurationMinutes = 90
	case PurposeAdmission:
		// Calon siswa tidak melihat nilai maupun pembahasan (keputusan 6).
		e.DurationMinutes, e.ResultsMode = 90, ResultsNone
	case PurposePractice:
		e.DurationMinutes, e.ResultsMode, e.ResultsAfterAll, e.MaxAttempts = 30, ResultsExplanation, false, 5
		e.ShuffleQuestions, e.ShuffleOptions = false, false
	}
	return e
}

// Access mengembalikan tingkat akses a ke ujian e (matriks T125):
// tes seleksi mengikuti hak tes seleksi; PTS/PAS juga terbuka untuk
// pengelola agenda; guru menulis ujian yang ia pegang.
func (e Exam) Access(a Actor, now time.Time) Level {
	if e.Builtin {
		// Paket latihan bawaan: dilihat semua staf ujian, diubah admin_tu.
		if a.HasAnyRole(roleAdmin) {
			return Write
		}
		if a.HasAnyRole(roleTeacher, rolePrincipal) {
			return Read
		}
		return None
	}
	if e.Purpose == PurposeAdmission {
		return a.Can(ActAdmissionTest, Resource{}, now)
	}
	best := None
	// Kolom admin/koordinator/kepala sekolah berlaku untuk semua kelas.
	if lv := a.Can(ActClassExam, Resource{SubjectID: e.SubjectID, ClassID: "*"}, now); lv > best {
		best = lv
	}
	if e.Purpose.SchoolWide() {
		if lv := a.Can(ActAgenda, Resource{}, now); lv > best {
			best = lv
		}
	}
	if e.OwnerID == a.UserID && slices.Contains(a.Roles, roleTeacher) {
		best = Write
	}
	return best
}

// CanCreate melaporkan apakah a boleh membuat ujian dengan tujuan p untuk
// mapel subjectID.
func CanCreate(a Actor, p Purpose, subjectID string, now time.Time) bool {
	switch {
	case p == PurposeAdmission:
		return a.Can(ActAdmissionTest, Resource{}, now) >= Write
	case p.SchoolWide():
		return a.CanAny(ActAgenda, Write)
	case a.Can(ActClassExam, Resource{SubjectID: subjectID, ClassID: "*"}, now) >= Write:
		return true
	}
	return slices.Contains(a.Roles, roleTeacher) && slices.Contains(a.Subjects, subjectID)
}

const examCols = `e.id, e.school_id, coalesce(e.academic_year_id::text, ''), e.name, e.purpose, coalesce(e.subject_id::text, ''),
	coalesce(s.name, ''), coalesce(e.grade_level, 0), e.duration_minutes, e.shuffle_questions, e.shuffle_options, e.shuffle_in_stimulus,
	e.results_mode, e.results_after_all, e.max_attempts, e.pass_score, e.integrity_mode, coalesce(e.integrity_limit, 0), e.allow_back,
	e.variant_count, e.status, e.wizard_step, e.owner_id, ou.full_name, coalesce(e.copied_from::text, ''), e.scheduled_at, e.locked_at,
	e.created_by, e.created_at, e.updated_at, e.archived_at, e.builtin, coalesce(e.parent_exam_id::text, ''), coalesce(e.followup, ''),
	coalesce(e.grade_component_id::text, ''), e.results_released_at`

const examFrom = ` FROM exams e
	LEFT JOIN subjects s ON s.id = e.subject_id AND s.school_id = e.school_id
	JOIN users ou ON ou.id = e.owner_id AND ou.school_id = e.school_id`

func scanExam(r pgx.Row) (Exam, error) {
	var e Exam
	var pass *int16
	var grade, dur, attempts, limit, variants int16
	err := r.Scan(&e.ID, &e.SchoolID, &e.AcademicYearID, &e.Name, &e.Purpose, &e.SubjectID, &e.SubjectName, &grade, &dur,
		&e.ShuffleQuestions, &e.ShuffleOptions, &e.ShuffleInStimulus, &e.ResultsMode, &e.ResultsAfterAll, &attempts, &pass,
		&e.IntegrityMode, &limit, &e.AllowBack, &variants, &e.Status, &e.WizardStep, &e.OwnerID, &e.OwnerName, &e.CopiedFrom,
		&e.ScheduledAt, &e.LockedAt, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.ArchivedAt, &e.Builtin, &e.ParentExamID, &e.FollowUp,
		&e.GradeComponentID, &e.ResultsReleasedAt)
	e.GradeLevel, e.DurationMinutes, e.MaxAttempts, e.IntegrityLimit, e.VariantCount = int(grade), int(dur), int(attempts), int(limit), int(variants)
	if pass != nil {
		v := int(*pass)
		e.PassScore = &v
	}
	return e, err
}

// GetExam mengembalikan ujian milik sekolah (termasuk diarsipkan).
func GetExam(ctx context.Context, q database.DBTX, schoolID, id string) (Exam, error) {
	if !database.IsUUID(id) {
		return Exam{}, ErrNotFound
	}
	e, err := scanExam(q.QueryRow(ctx, `SELECT `+examCols+examFrom+` WHERE e.school_id = $1 AND e.id = $2`, schoolID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Exam{}, ErrNotFound
	}
	if err != nil {
		return Exam{}, fmt.Errorf("exam: get exam: %w", err)
	}
	return e, nil
}

// ExamListRow adalah satu baris daftar ujian.
type ExamListRow struct {
	Exam
	Sessions   int
	FirstStart *time.Time
	LastEnd    *time.Time
	Questions  int
}

// ListExams mengembalikan ujian aktif yang boleh dilihat a, terbaru dulu.
// Admin dan kepala sekolah melihat semua; koordinator semua kecuali tes
// seleksi; guru hanya ujian yang ia pegang.
func ListExams(ctx context.Context, q database.DBTX, schoolID string, a Actor, archived bool) ([]ExamListRow, error) {
	where := "e.school_id = $1 AND (e.archived_at IS NOT NULL) = $2"
	args := []any{schoolID, archived}
	switch {
	case slices.Contains(a.Roles, roleAdmin) || slices.Contains(a.Roles, rolePrincipal):
	case a.Coordinator:
		args = append(args, a.UserID)
		where += " AND (e.purpose <> 'seleksi' OR e.owner_id = $3)"
	default:
		args = append(args, a.UserID)
		where += " AND e.owner_id = $3 AND e.purpose <> 'seleksi'"
	}
	rows, err := q.Query(ctx, `SELECT `+examCols+`,
		(SELECT count(*) FROM exam_sessions x WHERE x.exam_id = e.id AND x.cancelled_at IS NULL),
		(SELECT min(starts_at) FROM exam_sessions x WHERE x.exam_id = e.id AND x.cancelled_at IS NULL),
		(SELECT max(ends_at) FROM exam_sessions x WHERE x.exam_id = e.id AND x.cancelled_at IS NULL),
		(SELECT count(*) FROM exam_variant_items vi JOIN exam_variants v ON v.id = vi.variant_id WHERE v.exam_id = e.id AND v.label = 'A')`+
		examFrom+` WHERE `+where+` ORDER BY e.created_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, fmt.Errorf("exam: list exams: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExamListRow, error) {
		var x ExamListRow
		var pass *int16
		var grade, dur, attempts, limit, variants int16
		e := &x.Exam
		err := r.Scan(&e.ID, &e.SchoolID, &e.AcademicYearID, &e.Name, &e.Purpose, &e.SubjectID, &e.SubjectName, &grade, &dur,
			&e.ShuffleQuestions, &e.ShuffleOptions, &e.ShuffleInStimulus, &e.ResultsMode, &e.ResultsAfterAll, &attempts, &pass,
			&e.IntegrityMode, &limit, &e.AllowBack, &variants, &e.Status, &e.WizardStep, &e.OwnerID, &e.OwnerName, &e.CopiedFrom,
			&e.ScheduledAt, &e.LockedAt, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.ArchivedAt, &e.Builtin, &e.ParentExamID, &e.FollowUp,
			&e.GradeComponentID, &e.ResultsReleasedAt, &x.Sessions, &x.FirstStart, &x.LastEnd, &x.Questions)
		e.GradeLevel, e.DurationMinutes, e.MaxAttempts, e.IntegrityLimit, e.VariantCount = int(grade), int(dur), int(attempts), int(limit), int(variants)
		if pass != nil {
			v := int(*pass)
			e.PassScore = &v
		}
		return x, err
	})
}

// BasicsForm adalah isian langkah Dasar.
type BasicsForm struct {
	Name, Purpose, SubjectID, GradeLevel, Duration string
	OwnerID                                        string // PTS/PAS: guru pemegang soal
	// AdmissionTestID: tes gelombang yang langsung memakai ujian seleksi
	// baru ini (dibuat dari tab Tes seleksi, T149).
	AdmissionTestID string
}

// validateBasics memeriksa isian Dasar dan mengisi e.
func validateBasics(ctx context.Context, q database.DBTX, schoolID string, f BasicsForm, e *Exam, isNew bool) error {
	errs := map[string]string{}
	name := strings.TrimSpace(f.Name)
	switch {
	case name == "":
		errs["name"] = "Nama ujian wajib diisi."
	case utf8.RuneCountInString(name) > 120:
		errs["name"] = "Nama maksimal 120 karakter."
	}
	e.Name = name
	if isNew {
		p := Purpose(f.Purpose)
		if !p.Valid() {
			errs["purpose"] = "Pilih tujuan ujian."
		}
		e.Purpose = p
	}
	e.SubjectID = ""
	if e.Purpose != PurposeAdmission || f.SubjectID != "" {
		var ok bool
		if database.IsUUID(f.SubjectID) {
			if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM subjects WHERE school_id = $1 AND id = $2)`, schoolID, f.SubjectID).Scan(&ok); err != nil {
				return fmt.Errorf("exam: check subject: %w", err)
			}
		}
		if ok {
			e.SubjectID = f.SubjectID
		} else if e.Purpose != PurposeAdmission {
			errs["subject_id"] = "Pilih mapel."
		}
	}
	e.GradeLevel = 0
	if strings.TrimSpace(f.GradeLevel) != "" {
		g, ok := parseGrade(f.GradeLevel)
		if !ok {
			errs["grade_level"] = "Tingkat 1–12."
		}
		e.GradeLevel = g
	}
	d, err := strconv.Atoi(strings.TrimSpace(f.Duration))
	if err != nil || d < 5 || d > 600 {
		errs["duration"] = "Durasi 5–600 menit."
	}
	e.DurationMinutes = d
	if len(errs) > 0 {
		return &FieldError{Fields: errs}
	}
	return nil
}

func randomSeed() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]) >> 1)
}

// CreateExam membuat ujian draf dari isian Dasar beserta satu bagian dan
// varian A. Aturan diisi bawaan tujuan.
func CreateExam(ctx context.Context, db database.DB, sa StaffActor, f BasicsForm, now time.Time) (Exam, error) {
	var out Exam
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e := defaultsFor(Purpose(f.Purpose))
		if err := validateBasics(ctx, tx, sa.SchoolID, f, &e, true); err != nil {
			return err
		}
		if !CanCreate(sa.Actor, e.Purpose, e.SubjectID, now) {
			return ErrForbidden
		}
		e.OwnerID = sa.UserID
		if database.IsUUID(f.OwnerID) && f.OwnerID != sa.UserID && sa.CanAny(ActAgenda, Write) {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE school_id = $1 AND id = $2 AND is_active)`, sa.SchoolID, f.OwnerID).Scan(&ok); err != nil {
				return fmt.Errorf("exam: check owner: %w", err)
			}
			if ok {
				e.OwnerID = f.OwnerID
			}
		}
		var err error
		out, err = insertExam(ctx, tx, sa, e, "")
		if err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.create", "exam", out.ID, nil, map[string]any{"name": e.Name, "purpose": e.Purpose})
	})
	return out, err
}

func insertExam(ctx context.Context, tx pgx.Tx, sa StaffActor, e Exam, copiedFrom string) (Exam, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO exams (school_id, academic_year_id, name, purpose, subject_id, grade_level, duration_minutes, shuffle_questions,
		                   shuffle_options, shuffle_in_stimulus, results_mode, results_after_all, max_attempts, pass_score,
		                   integrity_mode, integrity_limit, allow_back, variant_count, owner_id, copied_from, created_by, wizard_step)
		VALUES ($1, (SELECT id FROM academic_years WHERE school_id = $1 AND is_active LIMIT 1), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		        $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		RETURNING id`,
		sa.SchoolID, e.Name, e.Purpose, nullable(e.SubjectID), nullInt(e.GradeLevel), e.DurationMinutes, e.ShuffleQuestions,
		e.ShuffleOptions, e.ShuffleInStimulus, e.ResultsMode, e.ResultsAfterAll, e.MaxAttempts, e.PassScore, e.IntegrityMode,
		nullInt(e.IntegrityLimit), e.AllowBack, e.VariantCount, e.OwnerID, nullable(copiedFrom), sa.UserID, stepQuestions).Scan(&id)
	if err != nil {
		return Exam{}, fmt.Errorf("exam: create exam: %w", err)
	}
	if copiedFrom == "" {
		if _, err := tx.Exec(ctx, `INSERT INTO exam_sections (school_id, exam_id, position) VALUES ($1, $2, 1)`, sa.SchoolID, id); err != nil {
			return Exam{}, fmt.Errorf("exam: create section: %w", err)
		}
	}
	if err := syncVariants(ctx, tx, sa.SchoolID, id, e.VariantCount); err != nil {
		return Exam{}, err
	}
	return GetExam(ctx, tx, sa.SchoolID, id)
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// variantLabels adalah label varian paket.
var variantLabels = []string{"A", "B", "C", "D", "E"}

// syncVariants menyesuaikan baris varian dengan n: menambah varian baru
// (seed acak) atau menghapus varian berlebih. Peserta di varian yang
// dihapus dibagi ulang saat penjadwalan.
func syncVariants(ctx context.Context, tx pgx.Tx, schoolID, examID string, n int) error {
	for i, l := range variantLabels {
		if i < n {
			if _, err := tx.Exec(ctx, `INSERT INTO exam_variants (school_id, exam_id, label, seed) VALUES ($1, $2, $3, $4)
				ON CONFLICT (exam_id, label) DO NOTHING`, schoolID, examID, l, randomSeed()); err != nil {
				return fmt.Errorf("exam: create variant: %w", err)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_session_participants SET variant_id = NULL
			WHERE variant_id IN (SELECT id FROM exam_variants WHERE exam_id = $1 AND label = $2)`, examID, l); err != nil {
			return fmt.Errorf("exam: release variant: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exam_variants WHERE exam_id = $1 AND label = $2`, examID, l); err != nil {
			return fmt.Errorf("exam: delete variant: %w", err)
		}
	}
	return nil
}

// loadForWrite mengunci ujian untuk diubah dan memeriksa hak tulis.
func loadForWrite(ctx context.Context, tx pgx.Tx, sa StaffActor, id string, now time.Time, needEditable bool) (Exam, error) {
	if !database.IsUUID(id) {
		return Exam{}, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM exams WHERE school_id = $1 AND id = $2 FOR UPDATE`, sa.SchoolID, id); err != nil {
		return Exam{}, fmt.Errorf("exam: lock exam: %w", err)
	}
	e, err := GetExam(ctx, tx, sa.SchoolID, id)
	if err != nil {
		return Exam{}, err
	}
	if e.Access(sa.Actor, now) < Write {
		return Exam{}, ErrNotEditable
	}
	if needEditable && !e.Editable() {
		return Exam{}, ErrExamLocked
	}
	return e, nil
}

// ErrExamLocked: soal dan aturan ujian sudah dibekukan (sesi sudah mulai).
var ErrExamLocked = errors.New("exam: exam locked")

// UpdateBasics menyimpan langkah Dasar. Tujuan tidak bisa diubah.
func UpdateBasics(ctx context.Context, db database.DB, sa StaffActor, id string, f BasicsForm, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, id, now, true)
		if err != nil {
			return err
		}
		if err := validateBasics(ctx, tx, sa.SchoolID, f, &e, false); err != nil {
			return err
		}
		if e.SubjectID != "" && e.Purpose != PurposeAdmission && !CanCreate(sa.Actor, e.Purpose, e.SubjectID, now) {
			return &FieldError{Fields: map[string]string{"subject_id": "Anda tidak mengampu mapel ini."}}
		}
		_, err = tx.Exec(ctx, `UPDATE exams SET name = $3, subject_id = $4, grade_level = $5, duration_minutes = $6, updated_at = now()
			WHERE school_id = $1 AND id = $2`, sa.SchoolID, id, e.Name, nullable(e.SubjectID), nullInt(e.GradeLevel), e.DurationMinutes)
		if err != nil {
			return fmt.Errorf("exam: update basics: %w", err)
		}
		return nil
	})
}

// RulesForm adalah isian langkah Aturan.
type RulesForm struct {
	ShuffleQuestions, ShuffleOptions, ShuffleInStimulus bool
	ResultsMode                                         string
	ResultsAfterAll                                     bool
	MaxAttempts, PassScore                              string
	IntegrityMode, IntegrityLimit                       string
	AllowBack                                           bool
	// GradeComponentID: komponen nilai rapor tujuan (T150), opsional.
	GradeComponentID string
}

// RulesFormOf mengubah ujian menjadi isian Aturan.
func RulesFormOf(e Exam) RulesForm {
	f := RulesForm{ShuffleQuestions: e.ShuffleQuestions, ShuffleOptions: e.ShuffleOptions, ShuffleInStimulus: e.ShuffleInStimulus,
		ResultsMode: e.ResultsMode, ResultsAfterAll: e.ResultsAfterAll, MaxAttempts: strconv.Itoa(e.MaxAttempts),
		IntegrityMode: e.IntegrityMode, AllowBack: e.AllowBack, GradeComponentID: e.GradeComponentID}
	if e.PassScore != nil {
		f.PassScore = strconv.Itoa(*e.PassScore)
	}
	if e.IntegrityLimit > 0 {
		f.IntegrityLimit = strconv.Itoa(e.IntegrityLimit)
	}
	return f
}

// UpdateRules menyimpan langkah Aturan.
func UpdateRules(ctx context.Context, db database.DB, sa StaffActor, id string, f RulesForm, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, id, now, true)
		if err != nil {
			return err
		}
		errs := map[string]string{}
		if !slices.Contains([]string{ResultsNone, ResultsScore, ResultsExplanation}, f.ResultsMode) {
			errs["results_mode"] = "Pilih hasil untuk peserta."
		}
		// Calon siswa tidak pernah melihat nilai maupun pembahasan.
		if e.Purpose == PurposeAdmission {
			f.ResultsMode = ResultsNone
			delete(errs, "results_mode")
		}
		attempts, err := strconv.Atoi(strings.TrimSpace(f.MaxAttempts))
		if err != nil || attempts < 1 || attempts > 10 {
			errs["max_attempts"] = "Jumlah percobaan 1–10."
		}
		var pass *int
		if s := strings.TrimSpace(f.PassScore); s != "" {
			v, err := strconv.Atoi(s)
			if err != nil || v < 0 || v > 100 {
				errs["pass_score"] = "KKTP 0–100."
			}
			pass = &v
		}
		limit := 0
		switch f.IntegrityMode {
		case IntegrityLog:
		case IntegrityLock:
			limit, err = strconv.Atoi(strings.TrimSpace(f.IntegrityLimit))
			if err != nil || limit < 1 || limit > 20 {
				errs["integrity_limit"] = "Isi 1–20 kali keluar halaman."
			}
		default:
			errs["integrity_mode"] = "Pilih aturan integritas."
		}
		if err := checkGradeComponent(ctx, tx, e, f.GradeComponentID); err != nil {
			var fe *FieldError
			if !errors.As(err, &fe) && !errors.Is(err, ErrGradePurpose) {
				return err
			}
			errs["grade_component_id"] = ErrBadGradeComponent.Fields["grade_component_id"]
		}
		if len(errs) > 0 {
			return &FieldError{Fields: errs}
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET grade_component_id = $3 WHERE school_id = $1 AND id = $2`, sa.SchoolID, e.ID,
			nullable(f.GradeComponentID)); err != nil {
			return fmt.Errorf("exam: update grade component: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE exams SET shuffle_questions = $3, shuffle_options = $4, shuffle_in_stimulus = $5, results_mode = $6,
			results_after_all = $7, max_attempts = $8, pass_score = $9, integrity_mode = $10, integrity_limit = $11, allow_back = $12,
			updated_at = now() WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id, f.ShuffleQuestions, f.ShuffleOptions, f.ShuffleInStimulus, f.ResultsMode, f.ResultsAfterAll, attempts, pass,
			f.IntegrityMode, nullInt(limit), f.AllowBack)
		if err != nil {
			return fmt.Errorf("exam: update rules: %w", err)
		}
		return nil
	})
}

// SetWizardStep mencatat langkah wizard terakhir (untuk "lanjutkan").
func SetWizardStep(ctx context.Context, q database.DBTX, schoolID, id, step string) error {
	_, err := q.Exec(ctx, `UPDATE exams SET wizard_step = $3 WHERE school_id = $1 AND id = $2 AND status = 'draf'`, schoolID, id, step)
	if err != nil {
		return fmt.Errorf("exam: set wizard step: %w", err)
	}
	return nil
}

// ArchiveExam mengarsipkan ujian. Ujian yang sedang berlangsung tidak bisa
// diarsipkan.
func ArchiveExam(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, id, now, false)
		if err != nil {
			return err
		}
		if e.Status == ExamRunning {
			return ErrExamRunning
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET archived_at = coalesce(archived_at, now()), status = 'arsip', updated_at = now()
			WHERE school_id = $1 AND id = $2`, sa.SchoolID, id); err != nil {
			return fmt.Errorf("exam: archive exam: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET cancelled_at = now() WHERE exam_id = $1 AND cancelled_at IS NULL AND starts_at > $2`,
			id, now); err != nil {
			return fmt.Errorf("exam: cancel sessions: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.archive", "exam", id, nil, nil)
	})
}

// ErrExamRunning: aksi tidak boleh saat ujian berlangsung.
var ErrExamRunning = errors.New("exam: exam running")

// CopyExam menyalin ujian (dasar, aturan, bagian, dan isi) menjadi draf
// baru tanpa sesi, untuk kelas lain atau tahun depan.
func CopyExam(ctx context.Context, db database.DB, sa StaffActor, id, name string, now time.Time) (Exam, error) {
	var out Exam
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		src, err := GetExam(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		if src.Access(sa.Actor, now) < Read || !CanCreate(sa.Actor, src.Purpose, src.SubjectID, now) {
			return ErrNotEditable
		}
		e := src
		e.Name = strings.TrimSpace(name)
		if e.Name == "" {
			e.Name = src.Name + " (salinan)"
		}
		if utf8.RuneCountInString(e.Name) > 120 {
			return &FieldError{Fields: map[string]string{"name": "Nama maksimal 120 karakter."}}
		}
		e.OwnerID = sa.UserID
		if out, err = insertExam(ctx, tx, sa, e, src.ID); err != nil {
			return err
		}
		// Bagian dan isi, dengan id bagian baru.
		_, err = tx.Exec(ctx, `
			WITH m AS (
				SELECT id AS old_id, gen_random_uuid() AS new_id, position, title, instructions_src, instructions_html
				FROM exam_sections WHERE exam_id = $2),
			ins AS (
				INSERT INTO exam_sections (id, school_id, exam_id, position, title, instructions_src, instructions_html)
				SELECT new_id, $1, $3, position, title, instructions_src, instructions_html FROM m)
			INSERT INTO exam_items (school_id, exam_id, section_id, position, kind, question_id, bank_id, rule_kind, rule_difficulty,
			                        rule_objective, rule_stimulus, rule_count)
			SELECT $1, $3, m.new_id, i.position, i.kind, i.question_id, i.bank_id, i.rule_kind, i.rule_difficulty, i.rule_objective,
			       i.rule_stimulus, i.rule_count
			FROM exam_items i JOIN m ON m.old_id = i.section_id
			LEFT JOIN exam_questions q ON q.id = i.question_id
			WHERE i.exam_id = $2 AND (q.id IS NULL OR q.archived_at IS NULL)`, sa.SchoolID, src.ID, out.ID)
		if err != nil {
			return fmt.Errorf("exam: copy items: %w", err)
		}
		if _, err := Reassemble(ctx, tx, out); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.copy", "exam", out.ID, map[string]any{"source": src.ID}, nil)
	})
	return out, err
}
