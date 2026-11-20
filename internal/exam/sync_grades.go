package exam

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/auth"
	"akra/internal/grades"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Kirim nilai ujian ke rapor (T150). Ujian ulangan/PTS/PAS ditautkan ke
// komponen nilai (T69) mapelnya; guru mengirim nilai final dengan sekali
// konfirmasi setelah melihat pratinjau. Penulisan memakai layanan input
// nilai (T70): satu transaksi, validasi yang sama, audit per nilai, dan hak
// guru pengampu kelas. Ujian remedial/pengayaan (T137) mengirim ke komponen
// ujian induknya dengan aturan rapor (T73): tidak pernah menurunkan nilai;
// remedial dibatasi KKM/KKTP bila sekolah memilih "maks_kktp".

// Aturan nilai bila komponen sudah bernilai.
const (
	GradeOverwrite  = "timpa"
	GradeHighest    = "tertinggi"
	GradeRemedial   = "remedial"
	GradeEnrichment = "pengayaan"
)

var gradePolicyLabels = map[string]string{
	GradeOverwrite: "Timpa nilai yang ada", GradeHighest: "Ambil yang tertinggi",
	GradeRemedial: "Remedial (aturan rapor sekolah)", GradeEnrichment: "Pengayaan (tidak menurunkan nilai)",
}

// GradePolicyLabel menampilkan aturan kirim.
func GradePolicyLabel(p string) string { return gradePolicyLabels[p] }

// Error kirim nilai.
var (
	ErrNoGradeComponent = errors.New("exam: exam has no grade component")
	ErrGradePurpose     = errors.New("exam: exam purpose does not feed report grades")
	// ErrNotYourClasses: pengirim tidak mengampu mapel di kelas peserta
	// mana pun (hak sama dengan input nilai T70).
	ErrNotYourClasses    = errors.New("exam: sender teaches none of the participants' classes")
	ErrBadGradeComponent = &FieldError{Fields: map[string]string{"grade_component_id": "Pilih komponen nilai aktif mapel ujian ini di tahun ajaran ujian."}}
)

// FeedsGrades melaporkan ujian yang nilainya bisa dikirim ke rapor.
func (e Exam) FeedsGrades() bool {
	switch e.Purpose {
	case PurposeQuiz, PurposeMidterm, PurposeFinal:
		return e.SubjectID != ""
	}
	return false
}

// examYear mengembalikan tahun ajaran ujian, atau tahun ajaran aktif.
func examYear(ctx context.Context, q database.DBTX, e Exam) (string, error) {
	if e.AcademicYearID != "" {
		return e.AcademicYearID, nil
	}
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM academic_years WHERE school_id = $1 AND is_active`, e.SchoolID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("exam: active year: %w", err)
	}
	return id, nil
}

// GradeComponentOptions mengembalikan komponen aktif mapel ujian di tahun
// ajaran ujian, untuk dipilih.
func GradeComponentOptions(ctx context.Context, q database.DBTX, e Exam) ([]grades.Component, error) {
	if !e.FeedsGrades() {
		return nil, nil
	}
	year, err := examYear(ctx, q, e)
	if err != nil || year == "" {
		return nil, err
	}
	all, err := grades.ListComponents(ctx, q, e.SchoolID, year, e.SubjectID)
	if err != nil {
		return nil, err
	}
	var out []grades.Component
	for _, c := range all {
		if c.IsActive {
			out = append(out, c)
		}
	}
	return out, nil
}

// checkGradeComponent memastikan componentID ("" = tanpa) komponen aktif
// mapel ujian e.
func checkGradeComponent(ctx context.Context, q database.DBTX, e Exam, componentID string) error {
	if componentID == "" {
		return nil
	}
	if !e.FeedsGrades() {
		return ErrGradePurpose
	}
	opts, err := GradeComponentOptions(ctx, q, e)
	if err != nil {
		return err
	}
	for _, c := range opts {
		if c.ID == componentID {
			return nil
		}
	}
	return ErrBadGradeComponent
}

// SetGradeComponent menautkan ujian ke komponen nilai (atau melepasnya
// bila kosong). Boleh setelah ujian terkunci; hak ubah ujian diperiksa
// pemanggil.
func SetGradeComponent(ctx context.Context, db database.DB, sa StaffActor, e Exam, componentID string) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := checkGradeComponent(ctx, tx, e, componentID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET grade_component_id = $3, updated_at = now() WHERE school_id = $1 AND id = $2`,
			e.SchoolID, e.ID, nullable(componentID)); err != nil {
			return fmt.Errorf("exam: set grade component: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.grade_component", "exam", e.ID, map[string]any{"component": e.GradeComponentID},
			map[string]any{"component": componentID})
	})
}

// gradeTarget menentukan ujian sumber aturan (induk untuk remedial/
// pengayaan), komponen tujuan, dan aturan efektif.
func gradeTarget(ctx context.Context, q database.DBTX, e Exam, policy string) (base Exam, componentID, effective string, err error) {
	base = e
	if e.FollowUp != "" && e.ParentExamID != "" {
		if base, err = GetExam(ctx, q, e.SchoolID, e.ParentExamID); err != nil {
			return e, "", "", err
		}
	}
	componentID = e.GradeComponentID
	if componentID == "" {
		componentID = base.GradeComponentID
	}
	switch e.FollowUp {
	case FollowRemedial:
		effective = GradeRemedial
	case FollowEnrichment:
		effective = GradeEnrichment
	default:
		effective = GradeOverwrite
		if policy == GradeHighest {
			effective = GradeHighest
		}
	}
	return base, componentID, effective, nil
}

// NewGradeValue menghitung nilai komponen baru (×100) dari nilai ujian
// value untuk aturan policy. current nil = komponen belum bernilai.
// "tertinggi", remedial, dan pengayaan tidak pernah menurunkan nilai;
// remedial dibatasi kktp (×1) bila remedialPolicy "maks_kktp".
func NewGradeValue(policy string, value int, current *int, kktp int, remedialPolicy string) int {
	v := value
	if policy == GradeRemedial && remedialPolicy != grades.RemedialHighest {
		v = min(v, kktp*100)
	}
	if policy != GradeOverwrite && current != nil {
		v = max(v, *current)
	}
	return v
}

// GradeSyncRow adalah satu siswa di pratinjau kirim ke rapor.
type GradeSyncRow struct {
	StudentID    string
	Name         string
	NIS          string
	ClassID      string
	ClassName    string
	EnrollmentID string
	ExamScore    *int // nilai final ujian ini (×100)
	Value        *int // nilai sumber komponen: ujian ini, atau rata-rata ujian komponen
	Sources      int  // jumlah ujian yang dirata-rata
	Current      *int // nilai komponen saat ini
	New          *int
	Status       string // lihat konstanta row*
	Suggestion   string // saran deskripsi capaian dari analisis TP
}

// Status baris pratinjau.
const (
	rowNew        = "baru"
	rowChange     = "ubah"
	rowSame       = "sama"
	rowAbsent     = "tidak_hadir"
	rowAwaitEssay = "menunggu_esai"
	rowNotYours   = "bukan_kelas_anda"
	rowNoClass    = "tanpa_kelas"
)

var rowLabels = map[string]string{
	rowNew: "Nilai baru", rowChange: "Diubah", rowSame: "Sama, tidak diubah", rowAbsent: "Tidak hadir/belum mengerjakan: tidak diubah",
	rowAwaitEssay: "Menunggu koreksi esai: belum dikirim", rowNotYours: "Bukan kelas yang Anda ampu", rowNoClass: "Tidak terdaftar di kelas tahun ajaran ini",
}

// StatusLabel menampilkan status baris.
func (r GradeSyncRow) StatusLabel() string { return rowLabels[r.Status] }

// Changes melaporkan baris yang akan ditulis.
func (r GradeSyncRow) Changes() bool { return r.Status == rowNew || r.Status == rowChange }

func scoreText(p *int) string {
	if p == nil {
		return ""
	}
	return grades.Score(*p).String()
}

// ExamText, ValueText, CurrentText, NewText menampilkan nilai baris.
func (r GradeSyncRow) ExamText() string    { return scoreText(r.ExamScore) }
func (r GradeSyncRow) ValueText() string   { return scoreText(r.Value) }
func (r GradeSyncRow) CurrentText() string { return scoreText(r.Current) }
func (r GradeSyncRow) NewText() string     { return scoreText(r.New) }

// GradeSync adalah satu riwayat pengiriman.
type GradeSync struct {
	Policy   string
	Students int
	Changed  int
	By       string
	At       time.Time
}

// PolicyLabel menampilkan aturan pengiriman.
func (g GradeSync) PolicyLabel() string { return GradePolicyLabel(g.Policy) }

// GradeSyncPlan adalah pratinjau kirim ke rapor.
type GradeSyncPlan struct {
	Exam          Exam
	Base          Exam // ujian induk untuk remedial/pengayaan
	Component     grades.Component
	Policy        string
	KKTP          int
	RemedialRule  string
	AverageOf     []string // nama ujian yang dirata-rata ke komponen (bila > 1)
	Rows          []GradeSyncRow
	Changes       int
	CanSendAny    bool // ada kelas yang diampu pengguna
	History       []GradeSync
	Stale         int // siswa yang nilainya berubah sejak dikirim
	HasObjectives bool
}

// PlanGradeSync menyusun pratinjau kirim nilai ujian e ke komponennya.
func PlanGradeSync(ctx context.Context, q database.DBTX, sa StaffActor, e Exam, policy string) (GradeSyncPlan, error) {
	p := GradeSyncPlan{Exam: e}
	base, compID, eff, err := gradeTarget(ctx, q, e, policy)
	if err != nil {
		return p, err
	}
	p.Base, p.Policy = base, eff
	if !base.FeedsGrades() {
		return p, ErrGradePurpose
	}
	if compID == "" {
		return p, ErrNoGradeComponent
	}
	comps, err := GradeComponentOptions(ctx, q, base)
	if err != nil {
		return p, err
	}
	for _, c := range comps {
		if c.ID == compID {
			p.Component = c
		}
	}
	if p.Component.ID == "" {
		return p, ErrBadGradeComponent
	}
	cfg, err := grades.LoadReportConfig(ctx, q, e.SchoolID)
	if err != nil {
		return p, err
	}
	p.RemedialRule = cfg.RemedialPolicy
	if p.KKTP, err = reportThreshold(ctx, q, e.SchoolID, base.SubjectID, cfg.KKMDefault); err != nil {
		return p, err
	}
	own, awaiting, err := studentScores(ctx, q, e.SchoolID, e.ID)
	if err != nil {
		return p, err
	}
	// Rata-rata ujian yang memakai komponen yang sama (bukan tindak lanjut).
	values := own
	counts := map[string]int{}
	if e.FollowUp == "" {
		rows, err := q.Query(ctx, `SELECT id::text, name FROM exams WHERE school_id = $1 AND grade_component_id = $2 AND parent_exam_id IS NULL
			AND archived_at IS NULL ORDER BY created_at, id`, e.SchoolID, compID)
		if err != nil {
			return p, fmt.Errorf("exam: component exams: %w", err)
		}
		type ex struct{ id, name string }
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ex, error) {
			var x ex
			return x, r.Scan(&x.id, &x.name)
		})
		if err != nil {
			return p, fmt.Errorf("exam: component exams: %w", err)
		}
		if len(list) > 1 {
			sums := map[string]int{}
			for _, x := range list {
				p.AverageOf = append(p.AverageOf, x.name)
				sc, _, err := studentScores(ctx, q, e.SchoolID, x.id)
				if err != nil {
					return p, err
				}
				for sid, v := range sc {
					sums[sid] += v
					counts[sid]++
				}
			}
			values = map[string]int{}
			for sid, s := range sums {
				values[sid] = (2*s + counts[sid]) / (2 * counts[sid])
			}
		}
	}
	// Siswa peserta ujian dengan kelasnya di tahun ajaran komponen.
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (s.id) s.id, s.full_name, s.nis, coalesce(en.id::text, ''), coalesce(en.class_id::text, ''), coalesce(c.name, '')
		FROM exam_session_participants p
		JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
		JOIN students s ON s.id = p.student_id
		LEFT JOIN enrollments en ON en.student_id = s.id AND en.academic_year_id = $3 AND en.end_date IS NULL
		LEFT JOIN classes c ON c.id = en.class_id
		WHERE p.school_id = $1 AND x.exam_id = $2
		ORDER BY s.id, en.start_date DESC`, e.SchoolID, e.ID, p.Component.AcademicYearID)
	if err != nil {
		return p, fmt.Errorf("exam: grade sync students: %w", err)
	}
	p.Rows, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (GradeSyncRow, error) {
		var x GradeSyncRow
		return x, r.Scan(&x.StudentID, &x.Name, &x.NIS, &x.EnrollmentID, &x.ClassID, &x.ClassName)
	})
	if err != nil {
		return p, fmt.Errorf("exam: grade sync students: %w", err)
	}
	var enrollments []string
	for _, r := range p.Rows {
		if r.EnrollmentID != "" {
			enrollments = append(enrollments, r.EnrollmentID)
		}
	}
	current := map[string]int{}
	gr, err := q.Query(ctx, `SELECT enrollment_id::text, (score * 100)::int FROM grades
		WHERE school_id = $1 AND component_id = $2 AND enrollment_id = ANY($3::uuid[]) AND score IS NOT NULL`, e.SchoolID, compID, enrollments)
	if err != nil {
		return p, fmt.Errorf("exam: current grades: %w", err)
	}
	for gr.Next() {
		var id string
		var v int
		if err := gr.Scan(&id, &v); err != nil {
			gr.Close()
			return p, fmt.Errorf("exam: current grades: %w", err)
		}
		current[id] = v
	}
	gr.Close()
	id := auth.Identity{UserID: sa.UserID, SchoolID: sa.SchoolID, Roles: sa.Roles}
	teaches := map[string]bool{}
	for i := range p.Rows {
		r := &p.Rows[i]
		if v, ok := own[r.StudentID]; ok {
			r.ExamScore = &v
		}
		if v, ok := values[r.StudentID]; ok {
			r.Value, r.Sources = &v, max(counts[r.StudentID], 1)
		}
		if v, ok := current[r.EnrollmentID]; ok {
			r.Current = &v
		}
		if r.ClassID != "" {
			ok, seen := teaches[r.ClassID]
			if !seen {
				if ok, err = grades.CanEnterGrades(ctx, q, id, r.ClassID, base.SubjectID); err != nil {
					return p, err
				}
				teaches[r.ClassID] = ok
			}
			p.CanSendAny = p.CanSendAny || ok
		}
		switch {
		case r.EnrollmentID == "":
			r.Status = rowNoClass
		case r.ExamScore == nil && awaiting[r.StudentID]:
			r.Status = rowAwaitEssay
		case r.Value == nil || (e.FollowUp != "" && r.ExamScore == nil):
			r.Status = rowAbsent
		case !teaches[r.ClassID]:
			r.Status = rowNotYours
		default:
			v := NewGradeValue(p.Policy, *r.Value, r.Current, p.KKTP, p.RemedialRule)
			r.New = &v
			switch {
			case r.Current == nil:
				r.Status = rowNew
			case *r.Current == v:
				r.Status = rowSame
			default:
				r.Status = rowChange
			}
		}
		if r.Changes() {
			p.Changes++
		}
	}
	sort.SliceStable(p.Rows, func(i, j int) bool {
		if p.Rows[i].ClassName != p.Rows[j].ClassName {
			return p.Rows[i].ClassName < p.Rows[j].ClassName
		}
		return strings.ToLower(p.Rows[i].Name) < strings.ToLower(p.Rows[j].Name)
	})
	if p.History, err = gradeSyncHistory(ctx, q, e.SchoolID, e.ID); err != nil {
		return p, err
	}
	p.Stale, err = GradesStale(ctx, q, e.SchoolID, e.ID)
	return p, err
}

// reportThreshold mengembalikan KKM/KKTP rapor mapel (T73): KKM mapel,
// atau bawaan sekolah.
func reportThreshold(ctx context.Context, q database.DBTX, schoolID, subjectID string, def int) (int, error) {
	var kkm *int16
	if err := q.QueryRow(ctx, `SELECT kkm FROM subjects WHERE school_id = $1 AND id = $2`, schoolID, subjectID).Scan(&kkm); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("exam: subject kkm: %w", err)
	}
	if kkm != nil {
		return academic.EffectiveKKM(academic.Subject{KKM: ptrInt(int(*kkm))}, def), nil
	}
	return def, nil
}

func ptrInt(v int) *int { return &v }

// studentScores mengembalikan nilai final attempt terakhir per siswa
// ujian examID, dan siswa yang attempt terakhirnya menunggu koreksi esai.
func studentScores(ctx context.Context, q database.DBTX, schoolID, examID string) (map[string]int, map[string]bool, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (p.student_id) p.student_id::text, t.score_x100, t.score_status
		FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id
		WHERE t.school_id = $1 AND t.exam_id = $2 AND t.status = 'dikumpulkan' AND p.student_id IS NOT NULL
		ORDER BY p.student_id, t.submitted_at DESC, t.attempt_no DESC`, schoolID, examID)
	if err != nil {
		return nil, nil, fmt.Errorf("exam: student scores: %w", err)
	}
	defer rows.Close()
	final, awaiting := map[string]int{}, map[string]bool{}
	for rows.Next() {
		var sid string
		var score *int
		var status *string
		if err := rows.Scan(&sid, &score, &status); err != nil {
			return nil, nil, fmt.Errorf("exam: student scores: %w", err)
		}
		switch {
		case status != nil && *status == ScoreFinal && score != nil:
			final[sid] = *score
		case status != nil && *status == ScoreAwaitEssay:
			awaiting[sid] = true
		}
	}
	return final, awaiting, rows.Err()
}

func gradeSyncHistory(ctx context.Context, q database.DBTX, schoolID, examID string) ([]GradeSync, error) {
	rows, err := q.Query(ctx, `SELECT g.policy, g.students, g.changed, u.full_name, g.sent_at FROM exam_grade_syncs g
		JOIN users u ON u.id = g.sent_by WHERE g.school_id = $1 AND g.exam_id = $2 ORDER BY g.sent_at DESC LIMIT 20`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: grade sync history: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (GradeSync, error) {
		var g GradeSync
		return g, r.Scan(&g.Policy, &g.Students, &g.Changed, &g.By, &g.At)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: grade sync history: %w", err)
	}
	return out, nil
}

// GradesStale menghitung siswa yang nilai ujiannya berubah sejak terakhir
// dikirim ke rapor (nilai ulang, koreksi esai, nilai manual), termasuk yang
// baru bernilai final setelah pengiriman. 0 bila belum pernah dikirim.
func GradesStale(ctx context.Context, q database.DBTX, schoolID, examID string) (int, error) {
	rows, err := q.Query(ctx, `SELECT student_id::text, exam_score_x100 FROM exam_grade_sent WHERE school_id = $1 AND exam_id = $2`, schoolID, examID)
	if err != nil {
		return 0, fmt.Errorf("exam: grades sent: %w", err)
	}
	sent := map[string]int{}
	for rows.Next() {
		var sid string
		var v int
		if err := rows.Scan(&sid, &v); err != nil {
			rows.Close()
			return 0, fmt.Errorf("exam: grades sent: %w", err)
		}
		sent[sid] = v
	}
	rows.Close()
	if len(sent) == 0 {
		return 0, rows.Err()
	}
	final, _, err := studentScores(ctx, q, schoolID, examID)
	if err != nil {
		return 0, err
	}
	n := 0
	for sid, v := range sent {
		if cur, ok := final[sid]; !ok || cur != v {
			n++
		}
	}
	for sid := range final {
		if _, ok := sent[sid]; !ok {
			n++
		}
	}
	return n, nil
}

// GradeSendResult merangkum pengiriman.
type GradeSendResult struct {
	Changed  int
	Students int
}

// SendGrades menulis nilai ke komponen rapor sesuai pratinjau (dihitung
// ulang di transaksi): hanya siswa di kelas yang diampu pengirim, siswa
// tidak hadir tidak diubah. Semua kelas dalam satu transaksi; validasi dan
// audit nilai lewat layanan input nilai (T70). Riwayat pengiriman dan
// nilai ujian yang dikirim dicatat untuk tanda kirim ulang.
func SendGrades(ctx context.Context, db database.DB, sa StaffActor, e Exam, policy string, now time.Time) (GradeSendResult, error) {
	var res GradeSendResult
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// Satu pengiriman per ujian pada satu waktu.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM exams WHERE id = $1 FOR UPDATE`, e.ID); err != nil {
			return fmt.Errorf("exam: lock exam: %w", err)
		}
		p, err := PlanGradeSync(ctx, tx, sa, e, policy)
		if err != nil {
			return err
		}
		if !p.CanSendAny {
			return ErrNotYourClasses
		}
		byClass := map[string]map[string]string{}
		var sentRows []GradeSyncRow
		for _, r := range p.Rows {
			if r.Status == rowNotYours || r.Status == rowNoClass || r.ExamScore == nil || r.New == nil {
				continue
			}
			sentRows = append(sentRows, r)
			if !r.Changes() {
				continue
			}
			if byClass[r.ClassID] == nil {
				byClass[r.ClassID] = map[string]string{}
			}
			byClass[r.ClassID][r.EnrollmentID] = fmt.Sprintf("%d.%02d", *r.New/100, *r.New%100)
		}
		svc := grades.NewInputService(tx)
		actor := school.Actor{SchoolID: sa.SchoolID, UserID: sa.UserID, IP: sa.IP}
		classes := make([]string, 0, len(byClass))
		for c := range byClass {
			classes = append(classes, c)
		}
		sort.Strings(classes)
		for _, classID := range classes {
			t, err := grades.ResolveComponent(ctx, tx, sa.SchoolID, classID, p.Component.ID)
			if err != nil {
				return err
			}
			out, err := svc.Save(ctx, actor, t, byClass[classID])
			if err != nil {
				return err
			}
			res.Changed += out.Changed
		}
		res.Students = len(sentRows)
		var syncID string
		if err := tx.QueryRow(ctx, `INSERT INTO exam_grade_syncs (school_id, exam_id, component_id, policy, students, changed, sent_by, sent_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, sa.SchoolID, e.ID, p.Component.ID, p.Policy, res.Students, res.Changed,
			sa.UserID, now).Scan(&syncID); err != nil {
			return fmt.Errorf("exam: record grade sync: %w", err)
		}
		for _, r := range sentRows {
			if _, err := tx.Exec(ctx, `INSERT INTO exam_grade_sent (school_id, exam_id, student_id, sync_id, exam_score_x100) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (exam_id, student_id) DO UPDATE SET sync_id = excluded.sync_id, exam_score_x100 = excluded.exam_score_x100`,
				sa.SchoolID, e.ID, r.StudentID, syncID, *r.ExamScore); err != nil {
				return fmt.Errorf("exam: record sent grade: %w", err)
			}
		}
		return recordAudit(ctx, tx, sa, "exam.grades_send", "exam", e.ID, nil, map[string]any{"component": p.Component.ID, "policy": p.Policy,
			"students": res.Students, "changed": res.Changed})
	})
	return res, err
}
