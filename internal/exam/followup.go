package exam

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Susulan, remedial, dan pengayaan (T137).

// Jenis ujian tindak lanjut.
const (
	FollowRemedial   = "remedial"
	FollowEnrichment = "pengayaan"
)

// MakeupCandidate adalah peserta ujian utama yang belum ikut: belum pernah
// masuk di sesinya (sampai halaman ujian T138 mencatat pengumpulan).
type MakeupCandidate struct {
	ParticipantRow
	SessionID string
	VariantID string
	Absent    bool // terpilih otomatis
}

// MakeupCandidates mengembalikan peserta sesi utama ujian e yang belum
// punya sesi susulan, dengan tanda Absent untuk yang tidak pernah masuk di
// sesi utamanya. Sesi yang belum mulai tidak dihitung tidak hadir.
func MakeupCandidates(ctx context.Context, q database.DBTX, e Exam, now time.Time) ([]MakeupCandidate, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, coalesce(p.student_id::text, ''), coalesce(p.applicant_id::text, ''), coalesce(s.full_name, a.full_name, ''),
		       coalesce(s.nis, a.registration_number, ''), p.participant_no,
		       coalesce((SELECT c.name FROM enrollments en JOIN classes c ON c.id = en.class_id
		                 WHERE en.student_id = p.student_id AND en.end_date IS NULL ORDER BY en.start_date DESC LIMIT 1), ''),
		       coalesce(v.label, ''), x.id, coalesce(p.variant_id::text, ''),
		       x.starts_at <= $3 AND NOT EXISTS (
		           SELECT 1 FROM student_credentials c WHERE c.exam_session_id = x.id AND c.device_id IS NOT NULL
		             AND c.student_id IS NOT DISTINCT FROM p.student_id AND c.applicant_id IS NOT DISTINCT FROM p.applicant_id)
		FROM exam_session_participants p
		JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL AND x.kind = 'utama'
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants a ON a.id = p.applicant_id
		LEFT JOIN exam_variants v ON v.id = p.variant_id
		WHERE p.school_id = $1 AND x.exam_id = $2 AND NOT EXISTS (
			SELECT 1 FROM exam_session_participants mp JOIN exam_sessions mx ON mx.id = mp.session_id
			WHERE mx.exam_id = x.exam_id AND mx.kind = 'susulan' AND mx.cancelled_at IS NULL
			  AND mp.student_id IS NOT DISTINCT FROM p.student_id AND mp.applicant_id IS NOT DISTINCT FROM p.applicant_id)
		ORDER BY 11 DESC, 7, 4`, e.SchoolID, e.ID, now)
	if err != nil {
		return nil, fmt.Errorf("exam: makeup candidates: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MakeupCandidate, error) {
		var c MakeupCandidate
		err := r.Scan(&c.ID, &c.StudentID, &c.ApplicantID, &c.Name, &c.Identifier, &c.Number, &c.ClassName, &c.Variant, &c.SessionID,
			&c.VariantID, &c.Absent)
		return c, err
	})
}

// MakeupForm adalah isian sesi susulan.
type MakeupForm struct {
	Participants []string // id baris peserta sesi utama
	Start, End   time.Time
	Location     string
}

// otherVariant memilih varian berbeda dari current (bergilir) bila ujian
// punya lebih dari satu varian, agar soal tidak bocor dari teman.
func otherVariant(variants []Variant, current string, i int) string {
	if len(variants) == 0 {
		return ""
	}
	if len(variants) == 1 {
		return variants[0].ID
	}
	var others []string
	for _, v := range variants {
		if v.ID != current {
			others = append(others, v.ID)
		}
	}
	return others[i%len(others)]
}

// CreateMakeupSession membuat sesi susulan di ujian yang sama (satu kolom
// nilai) untuk peserta terpilih, masing-masing dengan varian lain bila ada.
func CreateMakeupSession(ctx context.Context, db database.DB, sa StaffActor, examID string, f MakeupForm, now time.Time) (Session, error) {
	var out Session
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, examID, now, false)
		if err != nil {
			return err
		}
		if e.ArchivedAt != nil {
			return ErrNotFound
		}
		errs := checkWindow(e, f.Start, f.End, now)
		if len(f.Participants) == 0 {
			errs["participants"] = "Pilih minimal satu peserta."
		}
		if len(errs) > 0 {
			return &FieldError{Fields: errs}
		}
		cands, err := MakeupCandidates(ctx, tx, e, now)
		if err != nil {
			return err
		}
		byID := map[string]MakeupCandidate{}
		for _, c := range cands {
			byID[c.ID] = c
		}
		variants, err := ListVariants(ctx, tx, sa.SchoolID, e.ID)
		if err != nil {
			return err
		}
		loc := f.Location
		if loc != LocationRemote {
			loc = LocationRoom
		}
		var sid string
		if err := tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, kind, label, location, starts_at, ends_at, created_by)
			VALUES ($1, $2, 'susulan', 'Susulan', $3, $4, $5, $6) RETURNING id`, sa.SchoolID, e.ID, loc, f.Start, f.End, sa.UserID).Scan(&sid); err != nil {
			return fmt.Errorf("exam: create makeup: %w", err)
		}
		n := 0
		for i, pid := range f.Participants {
			c, ok := byID[pid]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, applicant_id, participant_no, variant_id)
				VALUES ($1, $2, $3, $4, $5, $6)`, sa.SchoolID, sid, nullable(c.StudentID), nullable(c.ApplicantID), c.Number,
				nullable(otherVariant(variants, c.VariantID, i))); err != nil {
				return fmt.Errorf("exam: makeup participant: %w", err)
			}
			n++
		}
		if n == 0 {
			return &FieldError{Fields: map[string]string{"participants": "Peserta yang dipilih tidak ada di ujian ini atau sudah punya sesi susulan."}}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by) VALUES ($1, $2, $3, $3)`,
			sa.SchoolID, sid, sa.UserID); err != nil {
			return fmt.Errorf("exam: makeup proctor: %w", err)
		}
		if out, err = GetSession(ctx, tx, sa.SchoolID, sid); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.makeup_create", "exam_session", sid, nil, map[string]any{"exam": e.ID, "participants": n})
	})
	return out, err
}

// StudentMastery adalah hasil satu siswa untuk perencanaan remedial: nilai
// akhir dan capaian per TP (0–100). Diisi analisis hasil (T148).
type StudentMastery struct {
	StudentID  string
	Score      float64
	Objectives map[string]float64
}

// RemedialPlan adalah rencana remedial: siswa di bawah KKTP dan TP yang
// belum tuntas, gabungan dan per siswa.
type RemedialPlan struct {
	Students   []string
	Objectives []string
	PerStudent map[string][]string
}

// PlanRemedial memilih siswa dengan nilai di bawah kktp dan TP yang belum
// tuntas (capaian di bawah kktp). Siswa belum tuntas yang semua TP-nya
// tuntas tetap ikut dengan semua TP yang ia kerjakan.
func PlanRemedial(ms []StudentMastery, kktp int) RemedialPlan {
	p := RemedialPlan{PerStudent: map[string][]string{}}
	union := map[string]bool{}
	for _, m := range ms {
		if m.Score >= float64(kktp) {
			continue
		}
		var weak, all []string
		for o, v := range m.Objectives {
			all = append(all, o)
			if v < float64(kktp) {
				weak = append(weak, o)
			}
		}
		if len(weak) == 0 {
			weak = all
		}
		sort.Strings(weak)
		p.Students = append(p.Students, m.StudentID)
		p.PerStudent[m.StudentID] = weak
		for _, o := range weak {
			union[o] = true
		}
	}
	for o := range union {
		p.Objectives = append(p.Objectives, o)
	}
	sort.Strings(p.Objectives)
	sort.Strings(p.Students)
	return p
}

// RemedialRules menyusun aturan acak remedial: satu aturan per TP belum
// tuntas, dari bank tempat soal TP itu diambil di ujian induk, dengan
// jumlah sama seperti di ujian induk (minimal minPerTP).
func RemedialRules(objectives []string, parent []VariantItem, minPerTP int) map[string]Rule {
	count := map[string]int{}
	banks := map[string]map[string]int{}
	for _, it := range parent {
		o := it.Q.ObjectiveID
		count[o]++
		if banks[o] == nil {
			banks[o] = map[string]int{}
		}
		banks[o][it.Q.BankID]++
	}
	out := map[string]Rule{}
	for _, o := range objectives {
		best, bestN := "", -1
		for b, n := range banks[o] {
			if n > bestN || (n == bestN && b < best) {
				best, bestN = b, n
			}
		}
		if best == "" {
			continue
		}
		out[o] = Rule{BankID: best, ObjectiveID: o, Count: max(count[o], minPerTP)}
	}
	return out
}

// RemedialReportScore menghitung nilai rapor setelah remedial menurut
// aturan sekolah (T73): tidak pernah menurunkan; "maks_kktp" membatasi
// nilai remedial sampai KKTP.
func RemedialReportScore(original, remedial float64, kktp int, policy string) float64 {
	r := remedial
	if policy != "tertinggi" {
		r = math.Min(remedial, float64(kktp))
	}
	return math.Max(original, r)
}

// EnrichmentReportScore: nilai pengayaan tidak pernah menurunkan nilai.
func EnrichmentReportScore(original, enrichment float64) float64 {
	return math.Max(original, enrichment)
}

// FollowUpForm adalah isian remedial/pengayaan.
type FollowUpForm struct {
	Kind       string
	Students   []string // id siswa peserta ujian induk
	Objectives []string // TP fokus (remedial)
	Start, End time.Time
	Location   string
}

// ErrNoObjectives: remedial terfokus butuh TP; ujian induk tanpa TP.
var ErrNoObjectives = errors.New("exam: no objectives to focus")

// CreateFollowUp membuat ujian remedial atau pengayaan dari ujian induk:
// remedial berisi satu bagian per TP fokus dengan aturan acak dari bank
// yang sama (jumlah seperti ujian induk); pengayaan dibuat kosong untuk
// disusun guru. Sesi untuk siswa terpilih langsung dibuat, dengan TP fokus
// per peserta. Ujian baru berstatus draf dan dijadwalkan lewat cek
// kesiapan seperti biasa.
func CreateFollowUp(ctx context.Context, db database.DB, sa StaffActor, parentID string, f FollowUpForm, now time.Time) (Exam, error) {
	var out Exam
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		parent, err := loadForWrite(ctx, tx, sa, parentID, now, false)
		if err != nil {
			return err
		}
		if f.Kind != FollowRemedial && f.Kind != FollowEnrichment {
			return &FieldError{Fields: map[string]string{"kind": "Pilih remedial atau pengayaan."}}
		}
		errs := checkWindow(parent, f.Start, f.End, now)
		if len(f.Students) == 0 {
			errs["students"] = "Pilih minimal satu siswa."
		}
		if len(errs) > 0 {
			return &FieldError{Fields: errs}
		}
		// Siswa harus peserta ujian induk.
		rows, err := tx.Query(ctx, `SELECT DISTINCT p.student_id::text FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
			WHERE x.exam_id = $1 AND x.cancelled_at IS NULL AND p.student_id = ANY($2)`, parent.ID, f.Students)
		if err != nil {
			return fmt.Errorf("exam: followup students: %w", err)
		}
		students, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("exam: followup students: %w", err)
		}
		if len(students) == 0 {
			return &FieldError{Fields: map[string]string{"students": "Siswa yang dipilih bukan peserta ujian ini."}}
		}
		e := parent
		e.ID, e.Status, e.LockedAt, e.ScheduledAt = "", ExamDraft, nil, nil
		e.VariantCount = 1
		e.OwnerID = sa.UserID
		if parent.OwnerID != "" && sa.CanAny(ActClassExam, Write) {
			e.OwnerID = parent.OwnerID
		}
		label := map[string]string{FollowRemedial: "Remedial", FollowEnrichment: "Pengayaan"}[f.Kind]
		e.Name = label + " · " + parent.Name
		if len([]rune(e.Name)) > 120 {
			e.Name = string([]rune(e.Name)[:120])
		}
		created, err := insertExam(ctx, tx, sa, e, "")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET parent_exam_id = $2, followup = $3, wizard_step = 'soal' WHERE id = $1`,
			created.ID, parent.ID, f.Kind); err != nil {
			return fmt.Errorf("exam: link followup: %w", err)
		}
		focus := []string{}
		if f.Kind == FollowRemedial {
			plan, err := BuildPlan(ctx, tx, parent)
			if err != nil {
				return err
			}
			var items []VariantItem
			if len(plan.Assembled) > 0 {
				items = plan.Assembled[0].Items
			}
			objs := f.Objectives
			if len(objs) == 0 {
				seen := map[string]bool{}
				for _, it := range items {
					if o := it.Q.ObjectiveID; o != "" && !seen[o] {
						seen[o] = true
						objs = append(objs, o)
					}
				}
			}
			rules := RemedialRules(objs, items, 2)
			if len(rules) == 0 {
				return ErrNoObjectives
			}
			codes := map[string]string{}
			if r, err := tx.Query(ctx, `SELECT id::text, code FROM learning_objectives WHERE school_id = $1 AND id = ANY($2)`, sa.SchoolID, objs); err == nil {
				for r.Next() {
					var id, code string
					if r.Scan(&id, &code) == nil {
						codes[id] = code
					}
				}
				r.Close()
			}
			if _, err := tx.Exec(ctx, `DELETE FROM exam_sections WHERE exam_id = $1`, created.ID); err != nil {
				return fmt.Errorf("exam: followup sections: %w", err)
			}
			pos := 0
			for _, o := range objs {
				r, ok := rules[o]
				if !ok {
					continue
				}
				pos++
				var sec string
				if err := tx.QueryRow(ctx, `INSERT INTO exam_sections (school_id, exam_id, position, title) VALUES ($1, $2, $3, $4) RETURNING id`,
					sa.SchoolID, created.ID, pos, "TP "+codes[o]).Scan(&sec); err != nil {
					return fmt.Errorf("exam: followup section: %w", err)
				}
				if _, err := tx.Exec(ctx, `INSERT INTO exam_items (school_id, exam_id, section_id, position, kind, bank_id, rule_objective, rule_count)
					VALUES ($1, $2, $3, 1, 'acak', $4, $5, $6)`, sa.SchoolID, created.ID, sec, r.BankID, o, r.Count); err != nil {
					return fmt.Errorf("exam: followup rule: %w", err)
				}
				focus = append(focus, o)
			}
		}
		if out, err = GetExam(ctx, tx, sa.SchoolID, created.ID); err != nil {
			return err
		}
		if _, err := Reassemble(ctx, tx, out); err != nil {
			return err
		}
		loc := f.Location
		if loc != LocationRemote {
			loc = LocationRoom
		}
		var sid string
		if err := tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, kind, label, location, starts_at, ends_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, sa.SchoolID, out.ID, f.Kind, label, loc, f.Start, f.End, sa.UserID).Scan(&sid); err != nil {
			return fmt.Errorf("exam: followup session: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no, focus_objectives)
			SELECT $1, $2, s.id, s.nis, $4 FROM students s WHERE s.school_id = $1 AND s.id = ANY($3)`,
			sa.SchoolID, sid, students, focus); err != nil {
			return fmt.Errorf("exam: followup participants: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by) VALUES ($1, $2, $3, $3)`,
			sa.SchoolID, sid, sa.UserID); err != nil {
			return fmt.Errorf("exam: followup proctor: %w", err)
		}
		if err := assignParticipantVariants(ctx, tx, out.ID); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.followup_create", "exam", out.ID, nil,
			map[string]any{"parent": parent.ID, "kind": f.Kind, "students": len(students), "objectives": len(focus)})
	})
	return out, err
}

// HistoryEntry adalah satu percobaan siswa: sesi utama, susulan,
// remedial, atau pengayaan.
type HistoryEntry struct {
	Kind     string
	ExamName string
	ExamID   string
	When     time.Time
}

// KindLabel mengembalikan nama jenis percobaan.
func (h HistoryEntry) KindLabel() string { return sessionKindLabels[h.Kind] }

// StudentHistory adalah riwayat percobaan satu siswa di ujian dan tindak
// lanjutnya.
type StudentHistory struct {
	StudentID string
	Name      string
	ClassName string
	Entries   []HistoryEntry
}

// FollowUpHistory mengembalikan riwayat percobaan per siswa untuk ujian
// root dan semua ujian tindak lanjutnya, urut nama.
func FollowUpHistory(ctx context.Context, q database.DBTX, schoolID, rootID string) ([]StudentHistory, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id, s.full_name,
		       coalesce((SELECT c.name FROM enrollments en JOIN classes c ON c.id = en.class_id
		                 WHERE en.student_id = s.id AND en.end_date IS NULL ORDER BY en.start_date DESC LIMIT 1), ''),
		       x.kind, e.name, e.id, x.starts_at
		FROM exam_session_participants p
		JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
		JOIN exams e ON e.id = x.exam_id
		JOIN students s ON s.id = p.student_id
		WHERE p.school_id = $1 AND (e.id = $2 OR e.parent_exam_id = $2)
		ORDER BY s.full_name, x.starts_at, e.created_at, x.created_at`, schoolID, rootID)
	if err != nil {
		return nil, fmt.Errorf("exam: followup history: %w", err)
	}
	defer rows.Close()
	var out []StudentHistory
	idx := map[string]int{}
	for rows.Next() {
		var id, name, class string
		var h HistoryEntry
		if err := rows.Scan(&id, &name, &class, &h.Kind, &h.ExamName, &h.ExamID, &h.When); err != nil {
			return nil, fmt.Errorf("exam: followup history: %w", err)
		}
		i, ok := idx[id]
		if !ok {
			i = len(out)
			idx[id] = i
			out = append(out, StudentHistory{StudentID: id, Name: name, ClassName: class})
		}
		out[i].Entries = append(out[i].Entries, h)
	}
	return out, rows.Err()
}

// ListFollowUps mengembalikan ujian remedial/pengayaan dari ujian induk.
func ListFollowUps(ctx context.Context, q database.DBTX, schoolID, parentID string) ([]Exam, error) {
	rows, err := q.Query(ctx, `SELECT `+examCols+examFrom+` WHERE e.school_id = $1 AND e.parent_exam_id = $2 AND e.archived_at IS NULL
		ORDER BY e.created_at`, schoolID, parentID)
	if err != nil {
		return nil, fmt.Errorf("exam: list followups: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Exam, error) { return scanExam(r) })
}

// historyKinds menjelaskan isi riwayat untuk tampilan.
func (s StudentHistory) Summary() string {
	parts := make([]string, len(s.Entries))
	for i, e := range s.Entries {
		parts[i] = e.KindLabel()
	}
	return strings.Join(parts, " → ")
}
