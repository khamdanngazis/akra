package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Stimulus adalah teks bacaan, tabel, atau gambar untuk beberapa soal.
type Stimulus struct {
	ID       string
	SchoolID string
	BankID   string
	Title    string
	BodySrc  string
	BodyHTML string
	// Numbered: paragraf bacaan diberi nomor baris.
	Numbered   bool
	Version    int
	CreatedBy  string
	UpdatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
	// Questions adalah jumlah soal aktif yang terikat (diisi ListStimuli).
	Questions int
}

const stimulusCols = `id, school_id, bank_id, title, body_src, body_html, numbered, version, created_by, updated_by, created_at, updated_at, archived_at`

func scanStimulus(r pgx.Row) (Stimulus, error) {
	var s Stimulus
	err := r.Scan(&s.ID, &s.SchoolID, &s.BankID, &s.Title, &s.BodySrc, &s.BodyHTML, &s.Numbered, &s.Version, &s.CreatedBy, &s.UpdatedBy,
		&s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt)
	return s, err
}

// GetStimulus mengembalikan stimulus milik sekolah, atau ErrNotFound.
func GetStimulus(ctx context.Context, q database.DBTX, schoolID, id string) (Stimulus, error) {
	if !database.IsUUID(id) {
		return Stimulus{}, ErrNotFound
	}
	s, err := scanStimulus(q.QueryRow(ctx, `SELECT `+stimulusCols+` FROM exam_stimuli WHERE school_id = $1 AND id = $2`, schoolID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Stimulus{}, ErrNotFound
	}
	if err != nil {
		return Stimulus{}, fmt.Errorf("exam: get stimulus: %w", err)
	}
	return s, nil
}

// ListStimuli mengembalikan stimulus aktif bank, dengan jumlah soalnya.
func ListStimuli(ctx context.Context, q database.DBTX, schoolID, bankID string) ([]Stimulus, error) {
	rows, err := q.Query(ctx, `
		SELECT `+prefixCols("s.", stimulusCols)+`,
		       (SELECT count(*) FROM exam_questions q WHERE q.school_id = s.school_id AND q.stimulus_id = s.id AND q.archived_at IS NULL)
		FROM exam_stimuli s WHERE s.school_id = $1 AND s.bank_id = $2 AND s.archived_at IS NULL ORDER BY s.created_at`, schoolID, bankID)
	if err != nil {
		return nil, fmt.Errorf("exam: list stimuli: %w", err)
	}
	defer rows.Close()
	var out []Stimulus
	for rows.Next() {
		var s Stimulus
		if err := rows.Scan(&s.ID, &s.SchoolID, &s.BankID, &s.Title, &s.BodySrc, &s.BodyHTML, &s.Numbered, &s.Version, &s.CreatedBy, &s.UpdatedBy,
			&s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt, &s.Questions); err != nil {
			return nil, fmt.Errorf("exam: list stimuli: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// StimulusQuestions mengembalikan soal aktif stimulus sesuai urutan.
func StimulusQuestions(ctx context.Context, q database.DBTX, schoolID, stimulusID string) ([]Question, error) {
	rows, err := q.Query(ctx, `SELECT `+questionCols+` FROM exam_questions q
		WHERE q.school_id = $1 AND q.stimulus_id = $2 AND q.archived_at IS NULL ORDER BY q.stimulus_order`, schoolID, stimulusID)
	if err != nil {
		return nil, fmt.Errorf("exam: stimulus questions: %w", err)
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		qq, err := scanQuestion(rows)
		if err != nil {
			return nil, fmt.Errorf("exam: stimulus questions: %w", err)
		}
		out = append(out, qq)
	}
	return out, rows.Err()
}

func validateStimulus(s *Stimulus) error {
	fields := map[string]string{}
	s.Title = strings.Join(strings.Fields(s.Title), " ")
	switch {
	case s.Title == "":
		fields["title"] = "Judul stimulus wajib diisi, mis. \"Teks untuk soal 1–5\"."
	case utf8.RuneCountInString(s.Title) > 200:
		fields["title"] = "Judul stimulus maksimal 200 karakter."
	}
	if strings.TrimSpace(PlainText(s.BodySrc)) == "" && !strings.Contains(s.BodySrc, "<img") {
		fields["body"] = "Isi stimulus (teks, tabel, atau gambar) wajib diisi."
	}
	if len(fields) > 0 {
		return &FieldError{Fields: fields}
	}
	return nil
}

func writeStimulusVersion(ctx context.Context, tx pgx.Tx, s Stimulus, userID string) error {
	snap, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("exam: encode stimulus snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO exam_stimulus_versions (school_id, stimulus_id, version, snapshot, created_by) VALUES ($1, $2, $3, $4, $5)`,
		s.SchoolID, s.ID, s.Version, snap, userID); err != nil {
		return fmt.Errorf("exam: write stimulus version: %w", err)
	}
	return nil
}

// StimulusVersion mengembalikan snapshot stimulus versi tertentu (T133).
func StimulusVersion(ctx context.Context, q database.DBTX, schoolID, id string, version int) (Stimulus, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT snapshot FROM exam_stimulus_versions WHERE school_id = $1 AND stimulus_id = $2 AND version = $3`,
		schoolID, id, version).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stimulus{}, ErrNotFound
	}
	if err != nil {
		return Stimulus{}, fmt.Errorf("exam: get stimulus version: %w", err)
	}
	var out Stimulus
	if err := json.Unmarshal(raw, &out); err != nil {
		return Stimulus{}, fmt.Errorf("exam: decode stimulus version: %w", err)
	}
	return out, nil
}

// CreateStimulus menyimpan stimulus baru (versi 1) di bank.
func CreateStimulus(ctx context.Context, db database.DB, sa StaffActor, bankID string, s Stimulus, now time.Time) (Stimulus, error) {
	var out Stimulus
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = createStimulusTx(ctx, tx, sa, bankID, s, now)
		return err
	})
	return out, err
}

func createStimulusTx(ctx context.Context, tx pgx.Tx, sa StaffActor, bankID string, s Stimulus, now time.Time) (Stimulus, error) {
	b, err := GetBank(ctx, tx, sa.SchoolID, bankID)
	if err != nil {
		return Stimulus{}, err
	}
	if b.ArchivedAt != nil || sa.Can(ActBank, b.resource(), now) < Write {
		return Stimulus{}, ErrNotEditable
	}
	if err := validateStimulus(&s); err != nil {
		return Stimulus{}, err
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO exam_stimuli (school_id, bank_id, title, body_src, body_html, numbered, search_text, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8) RETURNING id`,
		sa.SchoolID, bankID, s.Title, s.BodySrc, s.BodyHTML, s.Numbered, NormalizeSearch(s.Title+" "+PlainText(s.BodySrc)), sa.UserID).Scan(&id); err != nil {
		return Stimulus{}, fmt.Errorf("exam: create stimulus: %w", err)
	}
	out, err := GetStimulus(ctx, tx, sa.SchoolID, id)
	if err != nil {
		return Stimulus{}, err
	}
	if err := writeStimulusVersion(ctx, tx, out, sa.UserID); err != nil {
		return Stimulus{}, err
	}
	return out, recordAudit(ctx, tx, sa, "exam_stimulus.create", "exam_stimulus", id, nil, map[string]any{"bank_id": bankID})
}

// CanEditStimulus melaporkan apakah a boleh mengubah stimulus s di bank b.
func CanEditStimulus(a Actor, b Bank, s Stimulus, now time.Time) bool {
	return s.ArchivedAt == nil && b.ArchivedAt == nil && a.Can(ActBank, b.resource(), now) >= Read &&
		a.Can(ActQuestionEdit, Resource{SubjectID: b.SubjectID, OwnerID: s.CreatedBy}, now) >= Write
}

// UpdateStimulus menyimpan perubahan sebagai versi baru.
func UpdateStimulus(ctx context.Context, db database.DB, sa StaffActor, id string, s Stimulus, now time.Time) (Stimulus, error) {
	var out Stimulus
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		cur, err := GetStimulus(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		b, err := GetBank(ctx, tx, sa.SchoolID, cur.BankID)
		if err != nil {
			return err
		}
		if !CanEditStimulus(sa.Actor, b, cur, now) {
			return ErrNotEditable
		}
		if err := validateStimulus(&s); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_stimuli SET title = $3, body_src = $4, body_html = $5, numbered = $6, search_text = $7,
			version = version + 1, updated_by = $8, updated_at = now() WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id, s.Title, s.BodySrc, s.BodyHTML, s.Numbered, NormalizeSearch(s.Title+" "+PlainText(s.BodySrc)), sa.UserID); err != nil {
			return fmt.Errorf("exam: update stimulus: %w", err)
		}
		if out, err = GetStimulus(ctx, tx, sa.SchoolID, id); err != nil {
			return err
		}
		if err := writeStimulusVersion(ctx, tx, out, sa.UserID); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam_stimulus.update", "exam_stimulus", id,
			map[string]any{"version": cur.Version}, map[string]any{"version": out.Version})
	})
	return out, err
}

// ErrStimulusInUse: stimulus masih punya soal aktif.
var ErrStimulusInUse = errors.New("exam: stimulus still has active questions")

// ArchiveStimulus mengarsipkan stimulus yang tidak lagi punya soal aktif.
func ArchiveStimulus(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		s, err := GetStimulus(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		b, err := GetBank(ctx, tx, sa.SchoolID, s.BankID)
		if err != nil {
			return err
		}
		if !CanEditStimulus(sa.Actor, b, s, now) {
			return ErrNotEditable
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM exam_questions WHERE school_id = $1 AND stimulus_id = $2 AND archived_at IS NULL`,
			sa.SchoolID, id).Scan(&n); err != nil {
			return fmt.Errorf("exam: count stimulus questions: %w", err)
		}
		if n > 0 {
			return ErrStimulusInUse
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_stimuli SET archived_at = now(), updated_at = now() WHERE school_id = $1 AND id = $2`, sa.SchoolID, id); err != nil {
			return fmt.Errorf("exam: archive stimulus: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam_stimulus.archive", "exam_stimulus", id, nil, nil)
	})
}

// ReorderStimulus mengatur urutan soal di dalam stimulus. ids harus berisi
// tepat semua soal aktif stimulus itu. Urutan bukan isi soal, jadi tidak
// membuat versi baru (urutan di ujian disimpan di snapshot ujian, T133).
func ReorderStimulus(ctx context.Context, db database.DB, sa StaffActor, id string, ids []string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		s, err := GetStimulus(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		b, err := GetBank(ctx, tx, sa.SchoolID, s.BankID)
		if err != nil {
			return err
		}
		if !CanEditStimulus(sa.Actor, b, s, now) {
			return ErrNotEditable
		}
		cur, err := StimulusQuestions(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		if len(cur) != len(ids) {
			return &FieldError{Fields: map[string]string{"order": "Urutan harus memuat semua soal stimulus ini."}}
		}
		in := map[string]bool{}
		for _, q := range cur {
			in[q.ID] = true
		}
		for i, qid := range ids {
			if !in[qid] {
				return &FieldError{Fields: map[string]string{"order": "Urutan berisi soal dari luar stimulus ini."}}
			}
			delete(in, qid)
			if _, err := tx.Exec(ctx, `UPDATE exam_questions SET stimulus_order = $3 WHERE school_id = $1 AND id = $2`, sa.SchoolID, qid, i+1); err != nil {
				return fmt.Errorf("exam: reorder stimulus: %w", err)
			}
		}
		return recordAudit(ctx, tx, sa, "exam_stimulus.reorder", "exam_stimulus", id, nil, map[string]any{"order": ids})
	})
}
