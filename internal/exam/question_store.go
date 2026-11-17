package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

const questionCols = `q.id, q.school_id, q.bank_id, coalesce(q.stimulus_id::text, ''), coalesce(q.stimulus_order, 0), q.kind,
	q.body_src, q.body_html, q.options, q.answer_key, q.points, coalesce(q.difficulty, ''), coalesce(q.cognitive_level, ''),
	coalesce(q.objective_id::text, ''), q.indicator, q.tags, q.explanation_src, q.explanation_html, q.status, q.version,
	q.created_by, q.updated_by, q.created_at, q.updated_at, q.archived_at`

func scanQuestion(r pgx.Row) (Question, error) {
	var q Question
	var opts, key []byte
	err := r.Scan(&q.ID, &q.SchoolID, &q.BankID, &q.StimulusID, &q.StimulusOrder, &q.Kind, &q.BodySrc, &q.BodyHTML, &opts, &key,
		&q.Points, &q.Difficulty, &q.CognitiveLevel, &q.ObjectiveID, &q.Indicator, &q.Tags, &q.ExplanationSrc, &q.ExplanationHTML,
		&q.Status, &q.Version, &q.CreatedBy, &q.UpdatedBy, &q.CreatedAt, &q.UpdatedAt, &q.ArchivedAt)
	if err != nil {
		return q, err
	}
	if err := json.Unmarshal(opts, &q.Options); err != nil {
		return q, fmt.Errorf("exam: decode options: %w", err)
	}
	if err := json.Unmarshal(key, &q.Key); err != nil {
		return q, fmt.Errorf("exam: decode answer key: %w", err)
	}
	return q, nil
}

// GetQuestion mengembalikan soal milik sekolah (termasuk diarsipkan).
func GetQuestion(ctx context.Context, q database.DBTX, schoolID, id string) (Question, error) {
	if !database.IsUUID(id) {
		return Question{}, ErrNotFound
	}
	out, err := scanQuestion(q.QueryRow(ctx, `SELECT `+questionCols+` FROM exam_questions q WHERE q.school_id = $1 AND q.id = $2`, schoolID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, ErrNotFound
	}
	if err != nil {
		return Question{}, fmt.Errorf("exam: get question: %w", err)
	}
	return out, nil
}

// QuestionVersion mengembalikan snapshot soal versi tertentu (dipakai ujian,
// T133). Snapshot tidak pernah berubah.
func QuestionVersion(ctx context.Context, q database.DBTX, schoolID, id string, version int) (Question, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT snapshot FROM exam_question_versions WHERE school_id = $1 AND question_id = $2 AND version = $3`,
		schoolID, id, version).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, ErrNotFound
	}
	if err != nil {
		return Question{}, fmt.Errorf("exam: get question version: %w", err)
	}
	var out Question
	if err := json.Unmarshal(raw, &out); err != nil {
		return Question{}, fmt.Errorf("exam: decode question version: %w", err)
	}
	return out, nil
}

// normalizeMeta merapikan dan memeriksa metadata soal yang sama untuk
// semua jenis. Isi per jenis diperiksa ValidateContent (T128).
func normalizeMeta(q *Question) error {
	fields := map[string]string{}
	if !q.Kind.Valid() {
		fields["kind"] = "Pilih jenis soal."
	}
	if q.Points == 0 {
		q.Points = 1
	}
	if q.Points < 1 || q.Points > 100 {
		fields["points"] = "Poin harus 1–100."
	}
	if q.Difficulty != "" && !slices.Contains(Difficulties, q.Difficulty) {
		fields["difficulty"] = "Pilih kesulitan mudah, sedang, atau sulit."
	}
	if q.CognitiveLevel != "" && !slices.Contains(CognitiveLevels, q.CognitiveLevel) {
		fields["cognitive_level"] = "Pilih level kognitif C1–C6."
	}
	if utf8.RuneCountInString(q.Indicator) > 500 {
		fields["indicator"] = "Indikator maksimal 500 karakter."
	}
	tags, msg := normalizeTags(q.Tags)
	if msg != "" {
		fields["tags"] = msg
	}
	q.Tags = tags
	if q.Status == "" {
		q.Status = StatusDraft
	}
	if q.Status != StatusDraft && q.Status != StatusReady && q.Status != StatusInReview {
		fields["status"] = "Status tidak dikenal."
	}
	if len(fields) > 0 {
		return &FieldError{Fields: fields}
	}
	return nil
}

// normalizeTags merapikan tag: huruf kecil, tanpa duplikat, maks. 10 tag
// masing-masing maks. 40 karakter.
func normalizeTags(in []string) ([]string, string) {
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.Join(strings.Fields(t), " "))
		if t == "" || slices.Contains(out, t) {
			continue
		}
		if utf8.RuneCountInString(t) > 40 {
			return nil, "Setiap tag maksimal 40 karakter."
		}
		out = append(out, t)
	}
	if len(out) > 10 {
		return nil, "Maksimal 10 tag per soal."
	}
	return out, ""
}

// ParseTags memecah isian tag yang dipisah koma.
func ParseTags(s string) []string { return strings.Split(s, ",") }

// checkRefs memastikan TP dan stimulus soal milik bank yang sama (mapel
// bank) dan sekolah yang sama.
func checkRefs(ctx context.Context, tx pgx.Tx, schoolID string, b Bank, q Question) error {
	if q.ObjectiveID != "" {
		o, err := getObjective(ctx, tx, schoolID, q.ObjectiveID)
		if errors.Is(err, ErrNotFound) || (err == nil && o.SubjectID != b.SubjectID) {
			return &FieldError{Fields: map[string]string{"objective_id": "Pilih TP dari mapel bank ini."}}
		}
		if err != nil {
			return err
		}
	}
	if q.StimulusID != "" {
		s, err := GetStimulus(ctx, tx, schoolID, q.StimulusID)
		if errors.Is(err, ErrNotFound) || (err == nil && (s.BankID != b.ID || s.ArchivedAt != nil)) {
			return &FieldError{Fields: map[string]string{"stimulus_id": "Stimulus tidak ditemukan di bank ini."}}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func questionArgs(q Question) (opts, key []byte, err error) {
	if opts, err = json.Marshal(q.Options); err != nil {
		return nil, nil, err
	}
	key, err = json.Marshal(q.Key)
	return opts, key, err
}

func writeQuestionVersion(ctx context.Context, tx pgx.Tx, q Question, userID string) error {
	snap, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("exam: encode snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO exam_question_versions (school_id, question_id, version, snapshot, created_by) VALUES ($1, $2, $3, $4, $5)`,
		q.SchoolID, q.ID, q.Version, snap, userID); err != nil {
		return fmt.Errorf("exam: write question version: %w", err)
	}
	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CreateQuestion menyimpan soal baru (versi 1) di bank bankID. q sudah
// divalidasi dan dirender (T128); fungsi ini memeriksa akses, metadata,
// dan referensi.
func CreateQuestion(ctx context.Context, db database.DB, sa StaffActor, bankID string, q Question, now time.Time) (Question, error) {
	var out Question
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = createQuestionTx(ctx, tx, sa, bankID, q, now)
		return err
	})
	return out, err
}

func createQuestionTx(ctx context.Context, tx pgx.Tx, sa StaffActor, bankID string, q Question, now time.Time) (Question, error) {
	b, err := GetBank(ctx, tx, sa.SchoolID, bankID)
	if err != nil {
		return Question{}, err
	}
	if b.ArchivedAt != nil || sa.Can(ActBank, b.resource(), now) < Write {
		return Question{}, ErrNotEditable
	}
	if err := normalizeMeta(&q); err != nil {
		return Question{}, err
	}
	if err := checkRefs(ctx, tx, sa.SchoolID, b, q); err != nil {
		return Question{}, err
	}
	opts, key, err := questionArgs(q)
	if err != nil {
		return Question{}, err
	}
	var order any
	if q.StimulusID != "" {
		var n int
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(stimulus_order), 0) + 1 FROM exam_questions WHERE school_id = $1 AND stimulus_id = $2`,
			sa.SchoolID, q.StimulusID).Scan(&n); err != nil {
			return Question{}, fmt.Errorf("exam: stimulus order: %w", err)
		}
		order = n
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO exam_questions (school_id, bank_id, stimulus_id, stimulus_order, kind, body_src, body_html, options, answer_key,
		       points, difficulty, cognitive_level, objective_id, indicator, tags, explanation_src, explanation_html, status,
		       search_text, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $20)
		RETURNING id`,
		sa.SchoolID, bankID, nullable(q.StimulusID), order, q.Kind, q.BodySrc, q.BodyHTML, opts, key,
		q.Points, nullable(q.Difficulty), nullable(q.CognitiveLevel), nullable(q.ObjectiveID), q.Indicator, q.Tags,
		q.ExplanationSrc, q.ExplanationHTML, q.Status, searchText(q), sa.UserID).Scan(&id)
	if err != nil {
		return Question{}, fmt.Errorf("exam: create question: %w", err)
	}
	out, err := GetQuestion(ctx, tx, sa.SchoolID, id)
	if err != nil {
		return Question{}, err
	}
	if err := writeQuestionVersion(ctx, tx, out, sa.UserID); err != nil {
		return Question{}, err
	}
	return out, recordAudit(ctx, tx, sa, "exam_question.create", "exam_question", id, nil, map[string]any{"bank_id": bankID, "kind": q.Kind})
}

// CanEditQuestion melaporkan apakah a boleh mengubah soal q di bank b.
func CanEditQuestion(a Actor, b Bank, q Question, now time.Time) bool {
	return q.ArchivedAt == nil && b.ArchivedAt == nil &&
		a.Can(ActBank, b.resource(), now) >= Read &&
		a.Can(ActQuestionEdit, Resource{SubjectID: b.SubjectID, OwnerID: q.CreatedBy}, now) >= Write
}

// UpdateQuestion menyimpan perubahan soal sebagai versi baru. Versi lama
// tetap ada di exam_question_versions, sehingga ujian yang memakainya tidak
// berubah. Hanya pembuat soal dan admin_tu.
func UpdateQuestion(ctx context.Context, db database.DB, sa StaffActor, id string, q Question, now time.Time) (Question, error) {
	var out Question
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		cur, err := scanQuestion(tx.QueryRow(ctx, `SELECT `+questionCols+` FROM exam_questions q WHERE q.school_id = $1 AND q.id = $2 FOR UPDATE`,
			sa.SchoolID, id))
		if errors.Is(err, pgx.ErrNoRows) || !database.IsUUID(id) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("exam: lock question: %w", err)
		}
		b, err := GetBank(ctx, tx, sa.SchoolID, cur.BankID)
		if err != nil {
			return err
		}
		if !CanEditQuestion(sa.Actor, b, cur, now) {
			return ErrNotEditable
		}
		q.StimulusID = cur.StimulusID // pindah stimulus lewat aksi khusus
		if err := normalizeMeta(&q); err != nil {
			return err
		}
		if err := checkRefs(ctx, tx, sa.SchoolID, b, q); err != nil {
			return err
		}
		opts, key, err := questionArgs(q)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE exam_questions SET kind = $3, body_src = $4, body_html = $5, options = $6, answer_key = $7, points = $8,
			       difficulty = $9, cognitive_level = $10, objective_id = $11, indicator = $12, tags = $13,
			       explanation_src = $14, explanation_html = $15, status = $16, search_text = $17,
			       version = version + 1, updated_by = $18, updated_at = now()
			WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id, q.Kind, q.BodySrc, q.BodyHTML, opts, key, q.Points, nullable(q.Difficulty), nullable(q.CognitiveLevel),
			nullable(q.ObjectiveID), q.Indicator, q.Tags, q.ExplanationSrc, q.ExplanationHTML, q.Status, searchText(q), sa.UserID); err != nil {
			return fmt.Errorf("exam: update question: %w", err)
		}
		if out, err = GetQuestion(ctx, tx, sa.SchoolID, id); err != nil {
			return err
		}
		if err := writeQuestionVersion(ctx, tx, out, sa.UserID); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam_question.update", "exam_question", id,
			map[string]any{"version": cur.Version}, map[string]any{"version": out.Version})
	})
	return out, err
}

// ArchiveQuestion mengarsipkan soal (tidak ada hapus permanen).
func ArchiveQuestion(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q, err := GetQuestion(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		b, err := GetBank(ctx, tx, sa.SchoolID, q.BankID)
		if err != nil {
			return err
		}
		if !CanEditQuestion(sa.Actor, b, q, now) {
			return ErrNotEditable
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_questions SET archived_at = now(), updated_at = now(), updated_by = $3 WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, id, sa.UserID); err != nil {
			return fmt.Errorf("exam: archive question: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam_question.archive", "exam_question", id, nil, nil)
	})
}

// DuplicateQuestion menyalin soal (milik siapa pun yang bisa dilihat) ke
// bank targetBankID sebagai soal baru milik sa, berstatus draf. Ini cara
// guru "mengubah" soal guru lain (keputusan 7).
func DuplicateQuestion(ctx context.Context, db database.DB, sa StaffActor, id, targetBankID string, now time.Time) (Question, error) {
	var out Question
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		src, err := GetQuestion(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		b, err := GetBank(ctx, tx, sa.SchoolID, src.BankID)
		if err != nil {
			return err
		}
		if sa.Can(ActBank, b.resource(), now) < Read {
			return ErrNotFound
		}
		if targetBankID == "" {
			targetBankID = src.BankID
		}
		cp := src
		cp.ID, cp.Status, cp.Version = "", StatusDraft, 1
		if targetBankID != src.BankID {
			// Stimulus dan TP terikat bank/mapel asal.
			cp.StimulusID = ""
			tb, err := GetBank(ctx, tx, sa.SchoolID, targetBankID)
			if err != nil {
				return err
			}
			if tb.SubjectID != b.SubjectID {
				cp.ObjectiveID = ""
			}
		}
		if out, err = createQuestionTx(ctx, tx, sa, targetBankID, cp, now); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam_question.duplicate", "exam_question", out.ID, map[string]any{"source": id}, nil)
	})
	return out, err
}

// QuestionFilter adalah filter daftar soal di detail bank.
type QuestionFilter struct {
	Text        string
	Kind        string
	ObjectiveID string
	Difficulty  string
	Status      string
	CreatedBy   string
	Page        int
}

// QuestionPageSize adalah jumlah soal per halaman detail bank.
const QuestionPageSize = 50

// QuestionRow adalah satu baris daftar soal.
type QuestionRow struct {
	Question
	Snippet       string
	CreatorName   string
	ObjectiveCode string
	StimulusTitle string
	UsedIn        int
	LastP         *float64 // tingkat kesukaran terakhir (T148)
	LastD         *float64 // daya beda terakhir
	CanEdit       bool
}

// QuestionPage adalah satu halaman daftar soal.
type QuestionPage struct {
	Rows  []QuestionRow
	Total int
	Page  int
	Pages int
}

func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// questionWhere menyusun FROM ... WHERE daftar soal bank dengan filter.
func questionWhere(b Bank, f QuestionFilter) (string, []any) {
	where := []string{"q.school_id = $1", "q.bank_id = $2", "q.archived_at IS NULL"}
	args := []any{b.SchoolID, b.ID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if t := NormalizeSearch(f.Text); t != "" {
		// Cocok di teks soal atau judul/isi stimulusnya.
		add(`(q.search_text LIKE ? OR st.search_text LIKE ?)`, likePattern(t))
	}
	if Kind(f.Kind).Valid() {
		add("q.kind = ?", f.Kind)
	}
	if f.ObjectiveID == "none" {
		where = append(where, "q.objective_id IS NULL")
	} else if database.IsUUID(f.ObjectiveID) {
		add("q.objective_id = ?", f.ObjectiveID)
	}
	if slices.Contains(Difficulties, f.Difficulty) {
		add("q.difficulty = ?", f.Difficulty)
	}
	if f.Status == StatusDraft || f.Status == StatusReady || f.Status == StatusInReview {
		add("q.status = ?", f.Status)
	}
	if database.IsUUID(f.CreatedBy) {
		add("q.created_by = ?", f.CreatedBy)
	}
	from := ` FROM exam_questions q
		LEFT JOIN exam_stimuli st ON st.id = q.stimulus_id AND st.school_id = q.school_id
		WHERE ` + strings.Join(where, " AND ")
	return from, args
}

// ListQuestions mengembalikan soal aktif bank b sesuai filter. Soal
// berstimulus diurutkan berkelompok di bawah stimulusnya.
func ListQuestions(ctx context.Context, q database.DBTX, a Actor, b Bank, f QuestionFilter, now time.Time) (QuestionPage, error) {
	from, args := questionWhere(b, f)
	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+from, args...).Scan(&total); err != nil {
		return QuestionPage{}, fmt.Errorf("exam: count questions: %w", err)
	}
	pages := max(1, (total+QuestionPageSize-1)/QuestionPageSize)
	page := min(max(f.Page, 1), pages)
	return listQuestionRows(ctx, q, a, b, from, args, total, page, pages, QuestionPageSize, now)
}

// AllQuestions mengembalikan semua soal aktif bank sesuai filter (maks.
// 5.000), untuk kisi-kisi, kartu soal, dan cek mutu.
func AllQuestions(ctx context.Context, q database.DBTX, a Actor, b Bank, f QuestionFilter, ids []string, now time.Time) ([]QuestionRow, error) {
	if len(ids) > 0 {
		f = QuestionFilter{}
	}
	f.Page = 1
	where, args := questionWhere(b, f)
	if len(ids) > 0 {
		args = append(args, ids)
		where += fmt.Sprintf(" AND q.id = ANY($%d)", len(args))
	}
	p, err := listQuestionRows(ctx, q, a, b, where, args, 0, 1, 1, 5000, now)
	return p.Rows, err
}

func listQuestionRows(ctx context.Context, q database.DBTX, a Actor, b Bank, from string, args []any, total, page, pages, size int, now time.Time) (QuestionPage, error) {
	rows, err := q.Query(ctx, `
		SELECT `+questionCols+`, u.full_name, coalesce(o.code, ''), coalesce(st.title, ''),
		       (SELECT count(DISTINCT s.exam_id) FROM exam_question_stats s WHERE s.question_id = q.id AND s.exam_id IS NOT NULL),
		       ls.difficulty_index::float8, ls.discrimination::float8`+
		strings.Replace(from, " FROM exam_questions q", ` FROM exam_questions q
		JOIN users u ON u.id = q.created_by AND u.school_id = q.school_id
		LEFT JOIN learning_objectives o ON o.id = q.objective_id AND o.school_id = q.school_id
		LEFT JOIN LATERAL (SELECT difficulty_index, discrimination FROM exam_question_stats s
		                   WHERE s.question_id = q.id ORDER BY computed_at DESC LIMIT 1) ls ON true`, 1)+`
		ORDER BY coalesce(st.created_at, q.created_at), coalesce(q.stimulus_id, q.id), q.stimulus_order NULLS FIRST, q.created_at
		LIMIT `+fmt.Sprint(size)+` OFFSET `+fmt.Sprint((page-1)*size), args...)
	if err != nil {
		return QuestionPage{}, fmt.Errorf("exam: list questions: %w", err)
	}
	defer rows.Close()
	out := QuestionPage{Total: total, Page: page, Pages: pages}
	for rows.Next() {
		var r QuestionRow
		var opts, key []byte
		err := rows.Scan(&r.ID, &r.SchoolID, &r.BankID, &r.StimulusID, &r.StimulusOrder, &r.Kind, &r.BodySrc, &r.BodyHTML, &opts, &key,
			&r.Points, &r.Difficulty, &r.CognitiveLevel, &r.ObjectiveID, &r.Indicator, &r.Tags, &r.ExplanationSrc, &r.ExplanationHTML,
			&r.Status, &r.Version, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &r.ArchivedAt,
			&r.CreatorName, &r.ObjectiveCode, &r.StimulusTitle, &r.UsedIn, &r.LastP, &r.LastD)
		if err != nil {
			return QuestionPage{}, fmt.Errorf("exam: list questions: %w", err)
		}
		if err := json.Unmarshal(opts, &r.Options); err != nil {
			return QuestionPage{}, fmt.Errorf("exam: decode options: %w", err)
		}
		if err := json.Unmarshal(key, &r.Key); err != nil {
			return QuestionPage{}, fmt.Errorf("exam: decode answer key: %w", err)
		}
		r.Snippet = Snippet(PlainText(r.BodySrc), 160)
		r.CanEdit = CanEditQuestion(a, b, r.Question, now)
		out.Rows = append(out.Rows, r)
	}
	return out, rows.Err()
}

// Bulk adalah aksi untuk banyak soal sekaligus.
type Bulk struct {
	Action      string // "tp", "tag", "kesulitan", "arsip", "pindah"
	ObjectiveID string
	Tags        []string
	Difficulty  string
	TargetBank  string
}

// BulkResult melaporkan hasil aksi massal.
type BulkResult struct {
	Updated int
	// Skipped adalah soal yang tidak bisa diubah pengguna (milik guru lain).
	Skipped int
}

// BulkUpdate menerapkan aksi ke soal ids di bank bankID. Soal yang tidak
// boleh diubah pengguna dilewati (dihitung di Skipped); setiap perubahan
// isi tetap menjadi versi baru.
func BulkUpdate(ctx context.Context, db database.DB, sa StaffActor, bankID string, ids []string, op Bulk, now time.Time) (BulkResult, error) {
	var res BulkResult
	if len(ids) == 0 {
		return res, &FieldError{Fields: map[string]string{"ids": "Pilih minimal satu soal."}}
	}
	if len(ids) > 500 {
		return res, &FieldError{Fields: map[string]string{"ids": "Maksimal 500 soal sekali proses."}}
	}
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		b, err := GetBank(ctx, tx, sa.SchoolID, bankID)
		if err != nil {
			return err
		}
		var target Bank
		switch op.Action {
		case "tp":
			if op.ObjectiveID != "" {
				if err := checkRefs(ctx, tx, sa.SchoolID, b, Question{ObjectiveID: op.ObjectiveID}); err != nil {
					return err
				}
			}
		case "kesulitan":
			if !slices.Contains(Difficulties, op.Difficulty) {
				return &FieldError{Fields: map[string]string{"difficulty": "Pilih kesulitan."}}
			}
		case "tag":
			tags, msg := normalizeTags(op.Tags)
			if msg != "" || len(tags) == 0 {
				return &FieldError{Fields: map[string]string{"tags": "Isi tag yang akan ditambahkan."}}
			}
			op.Tags = tags
		case "pindah":
			if target, err = GetBank(ctx, tx, sa.SchoolID, op.TargetBank); err != nil || target.ArchivedAt != nil || target.ID == b.ID {
				return &FieldError{Fields: map[string]string{"target_bank": "Pilih bank tujuan yang lain."}}
			}
			if sa.Can(ActBank, target.resource(), now) < Write {
				return ErrNotEditable
			}
		case "arsip":
		default:
			return &FieldError{Fields: map[string]string{"action": "Pilih aksi."}}
		}
		for _, id := range ids {
			q, err := GetQuestion(ctx, tx, sa.SchoolID, id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if q.BankID != b.ID || !CanEditQuestion(sa.Actor, b, q, now) {
				res.Skipped++
				continue
			}
			var sql string
			var args []any
			switch op.Action {
			case "tp":
				sql, args = `objective_id = $3`, []any{nullable(op.ObjectiveID)}
			case "kesulitan":
				sql, args = `difficulty = $3`, []any{op.Difficulty}
			case "tag":
				merged, _ := normalizeTags(append(q.Tags, op.Tags...))
				q.Tags = merged
				sql, args = `tags = $3, search_text = $4`, []any{merged, searchText(q)}
			case "pindah":
				oid := q.ObjectiveID
				if target.SubjectID != b.SubjectID {
					oid = ""
				}
				sql, args = `bank_id = $3, stimulus_id = NULL, stimulus_order = NULL, objective_id = $4`, []any{target.ID, nullable(oid)}
			case "arsip":
				sql = `archived_at = now()`
			}
			if op.Action == "arsip" {
				if _, err := tx.Exec(ctx, `UPDATE exam_questions SET `+sql+`, updated_at = now(), updated_by = $3 WHERE school_id = $1 AND id = $2`,
					sa.SchoolID, id, sa.UserID); err != nil {
					return fmt.Errorf("exam: bulk %s: %w", op.Action, err)
				}
			} else {
				n := len(args) + 3
				if _, err := tx.Exec(ctx, `UPDATE exam_questions SET `+sql+`, version = version + 1, updated_at = now(), updated_by = $`+fmt.Sprint(n)+`
					WHERE school_id = $1 AND id = $2`, append([]any{sa.SchoolID, id}, append(args, sa.UserID)...)...); err != nil {
					return fmt.Errorf("exam: bulk %s: %w", op.Action, err)
				}
				nq, err := GetQuestion(ctx, tx, sa.SchoolID, id)
				if err != nil {
					return err
				}
				if err := writeQuestionVersion(ctx, tx, nq, sa.UserID); err != nil {
					return err
				}
			}
			res.Updated++
		}
		return recordAudit(ctx, tx, sa, "exam_question.bulk", "exam_bank", bankID, nil,
			map[string]any{"action": op.Action, "updated": res.Updated, "skipped": res.Skipped})
	})
	return res, err
}
