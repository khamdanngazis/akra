package exam

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
)

// Error umum bank soal.
var (
	ErrNotFound = errors.New("exam: not found")
	// ErrNotEditable: pengguna boleh melihat/memakai tetapi tidak boleh
	// mengubah (soal guru lain, keputusan 7).
	ErrNotEditable = errors.New("exam: not editable by this user")
)

// FieldError berisi pesan per field untuk form.
type FieldError struct{ Fields map[string]string }

func (e *FieldError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	return "exam: invalid input: " + strings.Join(keys, ", ")
}

// StaffActor adalah pelaku aksi staf: identitas dan fakta akses.
type StaffActor struct {
	Actor
	SchoolID string
	IP       netip.Addr
}

// Bank adalah satu bank soal.
type Bank struct {
	ID          string
	SchoolID    string
	Name        string
	SubjectID   string
	SubjectName string
	GradeLevel  int // 0 = semua tingkat
	CreatedBy   string
	CreatorName string
	CreatedAt   time.Time
	ArchivedAt  *time.Time
	Sample      bool // bank contoh bawaan (T154)
}

// resource mengembalikan sumber daya cek akses bank.
func (b Bank) resource() Resource { return Resource{SubjectID: b.SubjectID, OwnerID: b.CreatedBy} }

// BankForm adalah isian form bank.
type BankForm struct {
	Name       string
	SubjectID  string // kosong = bank umum (tes seleksi)
	GradeLevel string
}

const bankCols = `b.id, b.school_id, b.name, coalesce(b.subject_id::text, ''), coalesce(s.name, ''), coalesce(b.grade_level, 0),
	b.created_by, u.full_name, b.created_at, b.archived_at, b.sample`

const bankFrom = ` FROM exam_banks b
	LEFT JOIN subjects s ON s.id = b.subject_id AND s.school_id = b.school_id
	JOIN users u ON u.id = b.created_by AND u.school_id = b.school_id`

func scanBank(r pgx.CollectableRow) (Bank, error) {
	var b Bank
	err := r.Scan(&b.ID, &b.SchoolID, &b.Name, &b.SubjectID, &b.SubjectName, &b.GradeLevel, &b.CreatedBy, &b.CreatorName, &b.CreatedAt, &b.ArchivedAt, &b.Sample)
	return b, err
}

// GetBank mengembalikan bank milik sekolah schoolID (termasuk yang
// diarsipkan), atau ErrNotFound.
func GetBank(ctx context.Context, q database.DBTX, schoolID, id string) (Bank, error) {
	if !database.IsUUID(id) {
		return Bank{}, ErrNotFound
	}
	rows, err := q.Query(ctx, `SELECT `+bankCols+bankFrom+` WHERE b.school_id = $1 AND b.id = $2`, schoolID, id)
	if err != nil {
		return Bank{}, fmt.Errorf("exam: get bank: %w", err)
	}
	b, err := pgx.CollectExactlyOneRow(rows, scanBank)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bank{}, ErrNotFound
	}
	if err != nil {
		return Bank{}, fmt.Errorf("exam: get bank: %w", err)
	}
	return b, nil
}

// BankSummary adalah kartu bank di daftar bank.
type BankSummary struct {
	Bank
	Ready, Draft int
	ByKind       map[Kind]int
	ByDifficulty map[string]int
	// UncoveredObjectives: TP mapel dan tingkat bank yang belum punya soal.
	UncoveredObjectives int
	CanEdit             bool
}

// Total mengembalikan jumlah soal aktif.
func (s BankSummary) Total() int { return s.Ready + s.Draft }

// BankFilter menyaring daftar bank: tingkat (0 = semua; bank tanpa tingkat
// selalu ikut) dan mapel ("" = semua).
type BankFilter struct {
	Grade     int
	SubjectID string
}

// ListBanks mengembalikan bank aktif yang boleh dilihat a, dengan ringkasan
// isi.

func ListBanks(ctx context.Context, q database.DBTX, schoolID string, a Actor, f BankFilter, now time.Time) ([]BankSummary, error) {
	// Bank contoh selalu di urutan akhir (T161).
	rows, err := q.Query(ctx, `SELECT `+bankCols+bankFrom+`
		WHERE b.school_id = $1 AND b.archived_at IS NULL AND ($2 = 0 OR b.grade_level = $2 OR b.grade_level IS NULL)
		  AND ($3 = '' OR b.subject_id::text = $3)
		ORDER BY b.sample, coalesce(s.name, ''), b.grade_level NULLS FIRST, lower(b.name)`, schoolID, f.Grade, f.SubjectID)
	if err != nil {
		return nil, fmt.Errorf("exam: list banks: %w", err)
	}
	banks, err := pgx.CollectRows(rows, scanBank)
	if err != nil {
		return nil, fmt.Errorf("exam: list banks: %w", err)
	}
	var out []BankSummary
	idx := map[string]int{}
	var ids []string
	for _, b := range banks {
		if a.Can(ActBank, b.resource(), now) < Read {
			continue
		}
		idx[b.ID] = len(out)
		ids = append(ids, b.ID)
		out = append(out, BankSummary{Bank: b, ByKind: map[Kind]int{}, ByDifficulty: map[string]int{},
			CanEdit: a.Can(ActQuestionEdit, b.resource(), now) >= Write})
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err = q.Query(ctx, `
		SELECT bank_id, status, kind, coalesce(difficulty, ''), count(*) FROM exam_questions
		WHERE school_id = $1 AND bank_id = ANY($2) AND archived_at IS NULL
		GROUP BY 1, 2, 3, 4`, schoolID, ids)
	if err != nil {
		return nil, fmt.Errorf("exam: bank stats: %w", err)
	}
	for rows.Next() {
		var bankID, status, kind, diff string
		var n int
		if err := rows.Scan(&bankID, &status, &kind, &diff, &n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("exam: bank stats: %w", err)
		}
		s := &out[idx[bankID]]
		if status == StatusReady {
			s.Ready += n
		} else {
			s.Draft += n
		}
		s.ByKind[Kind(kind)] += n
		if diff != "" {
			s.ByDifficulty[diff] += n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("exam: bank stats: %w", err)
	}
	rows, err = q.Query(ctx, `
		SELECT b.id, count(o.id) FROM exam_banks b
		JOIN learning_objectives o ON o.school_id = b.school_id AND o.subject_id = b.subject_id
		     AND (b.grade_level IS NULL OR o.grade_level = b.grade_level) AND o.archived_at IS NULL
		WHERE b.school_id = $1 AND b.id = ANY($2)
		  AND NOT EXISTS (SELECT 1 FROM exam_questions q WHERE q.school_id = b.school_id AND q.bank_id = b.id
		                  AND q.objective_id = o.id AND q.archived_at IS NULL)
		GROUP BY b.id`, schoolID, ids)
	if err != nil {
		return nil, fmt.Errorf("exam: uncovered objectives: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bankID string
		var n int
		if err := rows.Scan(&bankID, &n); err != nil {
			return nil, fmt.Errorf("exam: uncovered objectives: %w", err)
		}
		out[idx[bankID]].UncoveredObjectives = n
	}
	return out, rows.Err()
}

// validateBank memeriksa form bank. Mapel harus milik sekolah dan aktif.
func validateBank(ctx context.Context, q database.DBTX, schoolID string, f BankForm) (name string, subjectID *string, grade *int, err error) {
	fields := map[string]string{}
	name = strings.Join(strings.Fields(f.Name), " ")
	switch {
	case name == "":
		fields["name"] = "Nama bank wajib diisi."
	case utf8.RuneCountInString(name) > 120:
		fields["name"] = "Nama bank maksimal 120 karakter."
	}
	if f.SubjectID != "" {
		var ok bool
		if database.IsUUID(f.SubjectID) {
			if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM subjects WHERE school_id = $1 AND id = $2 AND is_active)`,
				schoolID, f.SubjectID).Scan(&ok); err != nil {
				return "", nil, nil, fmt.Errorf("exam: check subject: %w", err)
			}
		}
		if !ok {
			fields["subject_id"] = "Pilih mata pelajaran yang aktif."
		} else {
			s := f.SubjectID
			subjectID = &s
		}
	}
	if g, ok := parseGrade(f.GradeLevel); !ok {
		fields["grade_level"] = "Tingkat harus 1–12 atau kosong."
	} else if g > 0 {
		grade = &g
	}
	if len(fields) > 0 {
		return "", nil, nil, &FieldError{Fields: fields}
	}
	return name, subjectID, grade, nil
}

func parseGrade(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
		if n > 12 {
			return 0, false
		}
	}
	return n, n >= 1
}

// CreateBank membuat bank. Pembuat harus boleh menulis bank mapel itu.
func CreateBank(ctx context.Context, db database.DB, sa StaffActor, f BankForm, now time.Time) (Bank, error) {
	var out Bank
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		name, subjectID, grade, err := validateBank(ctx, tx, sa.SchoolID, f)
		if err != nil {
			return err
		}
		res := Resource{}
		if subjectID != nil {
			res.SubjectID = *subjectID
		}
		if sa.Can(ActBank, res, now) < Write {
			return ErrNotEditable
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO exam_banks (school_id, name, subject_id, grade_level, created_by)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`, sa.SchoolID, name, subjectID, grade, sa.UserID).Scan(&id); err != nil {
			return fmt.Errorf("exam: create bank: %w", err)
		}
		if out, err = GetBank(ctx, tx, sa.SchoolID, id); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam_bank.create", "exam_bank", id, nil, map[string]any{"name": name, "subject_id": subjectID, "grade_level": grade})
	})
	return out, err
}

// UpdateBank mengubah nama dan tingkat bank (mapel tidak bisa diubah
// karena TP dan soal terikat mapel). Hanya pembuat dan admin_tu.
func UpdateBank(ctx context.Context, db database.DB, sa StaffActor, id string, f BankForm, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		b, err := GetBank(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		if sa.Can(ActQuestionEdit, b.resource(), now) < Write {
			return ErrNotEditable
		}
		f.SubjectID = b.SubjectID
		name, _, grade, err := validateBank(ctx, tx, sa.SchoolID, f)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_banks SET name = $3, grade_level = $4, updated_at = now() WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id, name, grade); err != nil {
			return fmt.Errorf("exam: update bank: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam_bank.update", "exam_bank", id,
			map[string]any{"name": b.Name, "grade_level": b.GradeLevel}, map[string]any{"name": name, "grade_level": grade})
	})
}

// ArchiveBank mengarsipkan bank beserta isinya tetap utuh (tidak dihapus).
func ArchiveBank(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		b, err := GetBank(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		if sa.Can(ActQuestionEdit, b.resource(), now) < Write {
			return ErrNotEditable
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_banks SET archived_at = coalesce(archived_at, now()), updated_at = now() WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id); err != nil {
			return fmt.Errorf("exam: archive bank: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam_bank.archive", "exam_bank", id, nil, nil)
	})
}

func recordAudit(ctx context.Context, tx pgx.Tx, sa StaffActor, action, entity, id string, before, after any) error {
	return audit.Record(ctx, tx, audit.Entry{SchoolID: sa.SchoolID, UserID: sa.UserID, Action: action,
		EntityType: entity, EntityID: id, Before: before, After: after, IP: sa.IP})
}

// BankSubject adalah pilihan saringan mapel di daftar bank.
type BankSubject struct {
	ID, Name string
}

// ListBankSubjects mengembalikan mapel dari bank aktif yang boleh dilihat a,
// untuk saringan daftar bank (T161).
func ListBankSubjects(ctx context.Context, q database.DBTX, schoolID string, a Actor, now time.Time) ([]BankSubject, error) {
	rows, err := q.Query(ctx, `SELECT s.id, s.name, b.created_by FROM exam_banks b
		JOIN subjects s ON s.school_id = b.school_id AND s.id = b.subject_id
		WHERE b.school_id = $1 AND b.archived_at IS NULL
		ORDER BY lower(s.name)`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: bank subjects: %w", err)
	}
	defer rows.Close()
	var out []BankSubject
	seen := map[string]bool{}
	for rows.Next() {
		var s BankSubject
		var owner string
		if err := rows.Scan(&s.ID, &s.Name, &owner); err != nil {
			return nil, fmt.Errorf("exam: bank subjects: %w", err)
		}
		if seen[s.ID] || a.Can(ActBank, Resource{SubjectID: s.ID, OwnerID: owner}, now) < Read {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out, rows.Err()
}
