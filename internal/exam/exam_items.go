package exam

import (
	"context"
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Section adalah satu bagian ujian beserta isinya.
type Section struct {
	ID               string
	Position         int
	Title            string
	InstructionsSrc  string
	InstructionsHTML string
	Items            []ItemRow
}

// ItemRow adalah satu isi bagian: soal tetap atau aturan acak.
type ItemRow struct {
	ID        string
	SectionID string
	Position  int
	Kind      string // soal, acak
	// Soal tetap.
	Question      *PoolQuestion
	Snippet       string
	StimulusTitle string
	ObjectiveCode string
	Archived      bool
	// Aturan acak.
	Rule      *Rule
	BankName  string
	Available int
	Short     int // kekurangan saat dirakit (0 = cukup)
}

// IsRule melaporkan apakah isi ini aturan acak.
func (it ItemRow) IsRule() bool { return it.Kind == "acak" }

// RuleLabel menjelaskan aturan acak, mis. "10 PG mudah TP 3.1".
func (it ItemRow) RuleLabel() string {
	if it.Rule == nil {
		return ""
	}
	r := it.Rule
	parts := []string{strconv.Itoa(r.Count)}
	if r.Stimulus {
		parts = append(parts, "teks/stimulus beserta soalnya")
	} else if r.Kind != "" {
		parts = append(parts, r.Kind.Label())
	} else {
		parts = append(parts, "soal")
	}
	if r.Kind != "" && r.Stimulus {
		parts = append(parts, "berisi "+r.Kind.Label())
	}
	if r.Difficulty != "" {
		parts = append(parts, r.Difficulty)
	}
	if it.ObjectiveCode != "" {
		parts = append(parts, "TP "+it.ObjectiveCode)
	}
	return strings.Join(parts, " ")
}

// LoadContent mengembalikan bagian dan isi ujian e, berurutan.
func LoadContent(ctx context.Context, q database.DBTX, e Exam) ([]Section, error) {
	rows, err := q.Query(ctx, `SELECT id, position, title, instructions_src, instructions_html FROM exam_sections
		WHERE school_id = $1 AND exam_id = $2 ORDER BY position, id`, e.SchoolID, e.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: load sections: %w", err)
	}
	secs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Section, error) {
		var s Section
		return s, r.Scan(&s.ID, &s.Position, &s.Title, &s.InstructionsSrc, &s.InstructionsHTML)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: load sections: %w", err)
	}
	idx := map[string]int{}
	for i, s := range secs {
		idx[s.ID] = i
	}
	rows, err = q.Query(ctx, `
		SELECT i.id, i.section_id, i.position, i.kind,
		       coalesce(q.id::text, ''), coalesce(q.bank_id::text, ''), coalesce(q.stimulus_id::text, ''), coalesce(q.stimulus_order, 0),
		       coalesce(q.kind, ''), coalesce(q.difficulty, ''), coalesce(q.cognitive_level, ''), coalesce(q.objective_id::text, ''),
		       coalesce(q.points, 0), coalesce(q.status, ''), coalesce(q.body_src, ''), coalesce(st.title, ''), q.archived_at IS NOT NULL,
		       coalesce(i.bank_id::text, ''), coalesce(i.rule_kind, ''), coalesce(i.rule_difficulty, ''), coalesce(i.rule_objective::text, ''),
		       i.rule_stimulus, coalesce(i.rule_count, 0), coalesce(b.name, ''), coalesce(o.code, '')
		FROM exam_items i
		LEFT JOIN exam_questions q ON q.id = i.question_id AND q.school_id = i.school_id
		LEFT JOIN exam_stimuli st ON st.id = q.stimulus_id AND st.school_id = i.school_id
		LEFT JOIN exam_banks b ON b.id = i.bank_id AND b.school_id = i.school_id
		LEFT JOIN learning_objectives o ON o.id = coalesce(i.rule_objective, q.objective_id) AND o.school_id = i.school_id
		WHERE i.school_id = $1 AND i.exam_id = $2
		ORDER BY i.position, i.id`, e.SchoolID, e.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: load items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it ItemRow
		var q PoolQuestion
		var body string
		var r Rule
		err := rows.Scan(&it.ID, &it.SectionID, &it.Position, &it.Kind, &q.ID, &q.BankID, &q.StimulusID, &q.StimulusOrder, &q.Kind,
			&q.Difficulty, &q.Cognitive, &q.ObjectiveID, &q.Points, &q.Status, &body, &it.StimulusTitle, &it.Archived,
			&r.BankID, &r.Kind, &r.Difficulty, &r.ObjectiveID, &r.Stimulus, &r.Count, &it.BankName, &it.ObjectiveCode)
		if err != nil {
			return nil, fmt.Errorf("exam: load items: %w", err)
		}
		if it.Kind == "soal" {
			it.Question = &q
			it.Snippet = Snippet(PlainText(body), 140)
		} else {
			it.Rule = &r
		}
		i, ok := idx[it.SectionID]
		if !ok {
			continue
		}
		secs[i].Items = append(secs[i].Items, it)
	}
	return secs, rows.Err()
}

// loadPool membaca soal siap pakai dari bank-bank yang dirujuk isi ujian,
// diurutkan stabil agar hasil rakit dengan seed sama selalu sama.
func loadPool(ctx context.Context, q database.DBTX, schoolID string, secs []Section) (Pool, error) {
	var banks []string
	for _, s := range secs {
		for _, it := range s.Items {
			b := it.Question.bankOr(it.Rule)
			if b != "" && !slices.Contains(banks, b) {
				banks = append(banks, b)
			}
		}
	}
	pool := Pool{}
	if len(banks) == 0 {
		return pool, nil
	}
	rows, err := q.Query(ctx, `SELECT id, bank_id, coalesce(stimulus_id::text, ''), coalesce(stimulus_order, 0), kind,
		       coalesce(difficulty, ''), coalesce(cognitive_level, ''), coalesce(objective_id::text, ''), points, status
		FROM exam_questions WHERE school_id = $1 AND bank_id = ANY($2) AND archived_at IS NULL AND status = 'siap'
		ORDER BY id`, schoolID, banks)
	if err != nil {
		return nil, fmt.Errorf("exam: load pool: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p PoolQuestion
		if err := rows.Scan(&p.ID, &p.BankID, &p.StimulusID, &p.StimulusOrder, &p.Kind, &p.Difficulty, &p.Cognitive, &p.ObjectiveID,
			&p.Points, &p.Status); err != nil {
			return nil, fmt.Errorf("exam: load pool: %w", err)
		}
		pool[p.BankID] = append(pool[p.BankID], p)
	}
	return pool, rows.Err()
}

func (q *PoolQuestion) bankOr(r *Rule) string {
	if q != nil {
		return q.BankID
	}
	if r != nil {
		return r.BankID
	}
	return ""
}

// asmItems mengubah isi bagian menjadi masukan Assemble. Soal yang sudah
// diarsipkan dilewati.
func asmItems(secs []Section) []AsmItem {
	var out []AsmItem
	for _, s := range secs {
		for _, it := range s.Items {
			if it.Archived {
				continue
			}
			out = append(out, AsmItem{ID: it.ID, SectionID: s.ID, Question: it.Question, Rule: it.Rule})
		}
	}
	return out
}

// Variant adalah satu varian paket tersimpan.
type Variant struct {
	ID    string
	Label string
	Seed  int64
}

// ListVariants mengembalikan varian ujian berurutan A, B, ...
func ListVariants(ctx context.Context, q database.DBTX, schoolID, examID string) ([]Variant, error) {
	rows, err := q.Query(ctx, `SELECT id, label, seed FROM exam_variants WHERE school_id = $1 AND exam_id = $2 ORDER BY label`, schoolID, examID)
	if err != nil {
		return nil, fmt.Errorf("exam: list variants: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Variant, error) {
		var v Variant
		return v, r.Scan(&v.ID, &v.Label, &v.Seed)
	})
}

// Plan adalah isi dan hasil rakit ujian untuk wizard dan detail.
type Plan struct {
	Sections  []Section
	Variants  []Variant
	Assembled []Assembled
	Pool      Pool
}

// Composition mengembalikan komposisi varian A.
func (p Plan) Composition() Composition {
	if len(p.Assembled) == 0 {
		return Compose(nil)
	}
	return Compose(p.Assembled[0].Items)
}

// BuildPlan memuat isi ujian dan merakit semua varian di memori (tanpa
// menyimpan). Jumlah tersedia per aturan juga diisi.
func BuildPlan(ctx context.Context, q database.DBTX, e Exam) (Plan, error) {
	secs, err := LoadContent(ctx, q, e)
	if err != nil {
		return Plan{}, err
	}
	pool, err := loadPool(ctx, q, e.SchoolID, secs)
	if err != nil {
		return Plan{}, err
	}
	vs, err := ListVariants(ctx, q, e.SchoolID, e.ID)
	if err != nil {
		return Plan{}, err
	}
	seeds := make([]int64, len(vs))
	for i, v := range vs {
		seeds[i] = v.Seed
	}
	items := asmItems(secs)
	p := Plan{Sections: secs, Variants: vs, Pool: pool, Assembled: Assemble(items, pool, seeds)}
	fixed := map[string]bool{}
	for _, it := range items {
		if it.Question != nil {
			fixed[it.Question.ID] = true
		}
	}
	short := map[string]int{}
	if len(p.Assembled) > 0 {
		for _, s := range p.Assembled[0].Short {
			short[s.ItemID] = s.Want - s.Got
		}
	}
	for i := range p.Sections {
		for j := range p.Sections[i].Items {
			it := &p.Sections[i].Items[j]
			if it.Rule != nil {
				it.Available = pool.Available(*it.Rule, fixed)
				it.Short = short[it.ID]
			}
		}
	}
	return p, nil
}

// Reassemble merakit ulang semua varian dan menyimpan hasilnya. Hanya
// untuk ujian yang belum terkunci; dipanggil setelah setiap perubahan isi.
func Reassemble(ctx context.Context, tx pgx.Tx, e Exam) (Plan, error) {
	if e.Locked() {
		return Plan{}, ErrExamLocked
	}
	p, err := BuildPlan(ctx, tx, e)
	if err != nil {
		return Plan{}, err
	}
	for i, v := range p.Variants {
		if _, err := tx.Exec(ctx, `DELETE FROM exam_variant_items WHERE variant_id = $1`, v.ID); err != nil {
			return Plan{}, fmt.Errorf("exam: clear variant: %w", err)
		}
		if i >= len(p.Assembled) || len(p.Assembled[i].Items) == 0 {
			continue
		}
		rows := make([][]any, len(p.Assembled[i].Items))
		for k, it := range p.Assembled[i].Items {
			rows[k] = []any{e.SchoolID, v.ID, k + 1, it.SectionID, it.QuestionID, nullable(it.StimulusID)}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"exam_variant_items"},
			[]string{"school_id", "variant_id", "position", "section_id", "question_id", "stimulus_id"}, pgx.CopyFromRows(rows)); err != nil {
			return Plan{}, fmt.Errorf("exam: save variant: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_variants SET assembled_at = now() WHERE id = $1`, v.ID); err != nil {
			return Plan{}, fmt.Errorf("exam: save variant: %w", err)
		}
	}
	return p, nil
}

// mutate menjalankan perubahan isi ujian dalam satu transaksi lalu merakit
// ulang varian.
func mutate(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time, fn func(tx pgx.Tx, e Exam) error) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, id, now, true)
		if err != nil {
			return err
		}
		if err := fn(tx, e); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET updated_at = now() WHERE id = $1`, id); err != nil {
			return fmt.Errorf("exam: touch exam: %w", err)
		}
		_, err = Reassemble(ctx, tx, e)
		return err
	})
}

func sectionOf(ctx context.Context, tx pgx.Tx, e Exam, sectionID string) (string, error) {
	if !database.IsUUID(sectionID) {
		// Bawaan: bagian terakhir.
		err := tx.QueryRow(ctx, `SELECT id FROM exam_sections WHERE exam_id = $1 ORDER BY position DESC LIMIT 1`, e.ID).Scan(&sectionID)
		if err != nil {
			return "", fmt.Errorf("exam: default section: %w", err)
		}
		return sectionID, nil
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_sections WHERE school_id = $1 AND exam_id = $2 AND id = $3)`,
		e.SchoolID, e.ID, sectionID).Scan(&ok); err != nil {
		return "", fmt.Errorf("exam: check section: %w", err)
	}
	if !ok {
		return "", ErrNotFound
	}
	return sectionID, nil
}

// bankUsable melaporkan apakah bank b boleh dipakai di ujian e oleh a:
// bank aktif yang boleh dibaca a, dengan mapel sama (atau ujian tanpa mapel).
func bankUsable(a Actor, e Exam, b Bank, now time.Time) bool {
	if b.ArchivedAt != nil || a.Can(ActBank, b.resource(), now) < Read {
		return false
	}
	return e.SubjectID == "" || b.SubjectID == e.SubjectID
}

// AddQuestions menambahkan soal ids ke bagian sectionID. Soal berstimulus
// menambahkan seluruh kelompoknya (guru bisa melepas soal tertentu
// sesudahnya). Soal yang sudah ada di ujian dilewati.
func AddQuestions(ctx context.Context, db database.DB, sa StaffActor, examID, sectionID string, ids []string, now time.Time) (int, error) {
	added := 0
	err := mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		sec, err := sectionOf(ctx, tx, e, sectionID)
		if err != nil {
			return err
		}
		var pos int
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(position), 0) FROM exam_items WHERE exam_id = $1`, e.ID).Scan(&pos); err != nil {
			return fmt.Errorf("exam: next position: %w", err)
		}
		banks := map[string]bool{}
		seenStim := map[string]bool{}
		for _, id := range ids {
			q, err := GetQuestion(ctx, tx, sa.SchoolID, id)
			if err != nil || q.ArchivedAt != nil {
				continue
			}
			ok, seen := banks[q.BankID]
			if !seen {
				b, err := GetBank(ctx, tx, sa.SchoolID, q.BankID)
				ok = err == nil && bankUsable(sa.Actor, e, b, now)
				banks[q.BankID] = ok
			}
			if !ok {
				continue
			}
			group := []Question{q}
			if q.StimulusID != "" {
				if seenStim[q.StimulusID] {
					continue
				}
				seenStim[q.StimulusID] = true
				if group, err = StimulusQuestions(ctx, tx, sa.SchoolID, q.StimulusID); err != nil {
					return err
				}
			}
			for _, g := range group {
				if g.ArchivedAt != nil {
					continue
				}
				pos++
				tag, err := tx.Exec(ctx, `INSERT INTO exam_items (school_id, exam_id, section_id, position, kind, question_id)
					VALUES ($1, $2, $3, $4, 'soal', $5) ON CONFLICT (exam_id, question_id) WHERE question_id IS NOT NULL DO NOTHING`,
					sa.SchoolID, e.ID, sec, pos, g.ID)
				if err != nil {
					return fmt.Errorf("exam: add question: %w", err)
				}
				added += int(tag.RowsAffected())
			}
		}
		return nil
	})
	return added, err
}

// RuleForm adalah isian aturan ambil acak.
type RuleForm struct {
	SectionID, BankID, Kind, Difficulty, ObjectiveID, Count string
	Stimulus                                                bool
}

// AddRule menambahkan aturan ambil acak. Aturan yang melebihi isi bank
// (setelah dikurangi soal tetap dan aturan lain) ditolak dengan jumlah
// tersedia.
func AddRule(ctx context.Context, db database.DB, sa StaffActor, examID string, f RuleForm, now time.Time) error {
	return mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		sec, err := sectionOf(ctx, tx, e, f.SectionID)
		if err != nil {
			return err
		}
		errs := map[string]string{}
		b, err := GetBank(ctx, tx, sa.SchoolID, f.BankID)
		if err != nil || !bankUsable(sa.Actor, e, b, now) {
			errs["bank_id"] = "Pilih bank soal."
		}
		r := Rule{BankID: f.BankID, Stimulus: f.Stimulus}
		if f.Kind != "" {
			if !Kind(f.Kind).Valid() {
				errs["kind"] = "Jenis soal tidak dikenal."
			}
			r.Kind = Kind(f.Kind)
		}
		if f.Difficulty != "" {
			if !slices.Contains(Difficulties, f.Difficulty) {
				errs["difficulty"] = "Kesulitan tidak dikenal."
			}
			r.Difficulty = f.Difficulty
		}
		if f.ObjectiveID != "" {
			var ok bool
			if database.IsUUID(f.ObjectiveID) {
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM learning_objectives WHERE school_id = $1 AND id = $2 AND archived_at IS NULL)`,
					sa.SchoolID, f.ObjectiveID).Scan(&ok); err != nil {
					return fmt.Errorf("exam: check objective: %w", err)
				}
			}
			if !ok {
				errs["objective_id"] = "TP tidak ditemukan."
			}
			r.ObjectiveID = f.ObjectiveID
		}
		n, err := strconv.Atoi(strings.TrimSpace(f.Count))
		if err != nil || n < 1 || n > 200 {
			errs["count"] = "Jumlah 1–200."
		}
		r.Count = n
		if len(errs) > 0 {
			return &FieldError{Fields: errs}
		}
		// Rakit varian A dengan aturan baru untuk menghitung yang tersedia.
		p, err := BuildPlan(ctx, tx, e)
		if err != nil {
			return err
		}
		pool := p.Pool
		if _, ok := pool[r.BankID]; !ok {
			extra, err := loadPool(ctx, tx, sa.SchoolID, []Section{{Items: []ItemRow{{Rule: &r}}}})
			if err != nil {
				return err
			}
			pool[r.BankID] = extra[r.BankID]
		}
		items := append(asmItems(p.Sections), AsmItem{ID: "baru", SectionID: sec, Rule: &r})
		seed := int64(1)
		if len(p.Variants) > 0 {
			seed = p.Variants[0].Seed
		}
		for _, s := range Assemble(items, pool, []int64{seed})[0].Short {
			if s.ItemID == "baru" {
				unit := "soal"
				if r.Stimulus {
					unit = "kelompok stimulus"
				}
				return &FieldError{Fields: map[string]string{"count": fmt.Sprintf("Hanya tersedia %d %s yang cocok di bank (setelah dikurangi soal yang sudah dipakai di ujian ini).", s.Got, unit)}}
			}
		}
		var pos int
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(position), 0) + 1 FROM exam_items WHERE exam_id = $1`, e.ID).Scan(&pos); err != nil {
			return fmt.Errorf("exam: next position: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO exam_items (school_id, exam_id, section_id, position, kind, bank_id, rule_kind, rule_difficulty,
			rule_objective, rule_stimulus, rule_count) VALUES ($1, $2, $3, $4, 'acak', $5, $6, $7, $8, $9, $10)`,
			sa.SchoolID, e.ID, sec, pos, r.BankID, nullable(string(r.Kind)), nullable(r.Difficulty), nullable(r.ObjectiveID), r.Stimulus, r.Count)
		if err != nil {
			return fmt.Errorf("exam: add rule: %w", err)
		}
		return nil
	})
}

// RemoveItem melepas satu isi (soal atau aturan) dari ujian.
func RemoveItem(ctx context.Context, db database.DB, sa StaffActor, examID, itemID string, now time.Time) error {
	return mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		if !database.IsUUID(itemID) {
			return ErrNotFound
		}
		tag, err := tx.Exec(ctx, `DELETE FROM exam_items WHERE school_id = $1 AND exam_id = $2 AND id = $3`, sa.SchoolID, e.ID, itemID)
		if err != nil {
			return fmt.Errorf("exam: remove item: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ReorderItems menyimpan urutan isi satu bagian. ids boleh berisi isi dari
// bagian lain (diseret antarbagian): isi itu dipindah ke bagian ini. Soal
// satu stimulus selalu dikumpulkan kembali di posisi soal pertamanya.
func ReorderItems(ctx context.Context, db database.DB, sa StaffActor, examID, sectionID string, ids []string, now time.Time) error {
	return mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		sec, err := sectionOf(ctx, tx, e, sectionID)
		if err != nil {
			return err
		}
		secs, err := LoadContent(ctx, tx, e)
		if err != nil {
			return err
		}
		all := map[string]ItemRow{}
		var current []string
		for _, s := range secs {
			for _, it := range s.Items {
				all[it.ID] = it
				if s.ID == sec {
					current = append(current, it.ID)
				}
			}
		}
		var order []string
		seen := map[string]bool{}
		for _, id := range ids {
			if _, ok := all[id]; ok && !seen[id] {
				order = append(order, id)
				seen[id] = true
			}
		}
		// Isi bagian ini yang tidak disebut tetap di akhir.
		for _, id := range current {
			if !seen[id] {
				order = append(order, id)
				seen[id] = true
			}
		}
		order = groupStimuli(order, func(id string) string {
			if q := all[id].Question; q != nil {
				return q.StimulusID
			}
			return ""
		})
		for i, id := range order {
			if _, err := tx.Exec(ctx, `UPDATE exam_items SET section_id = $3, position = $4 WHERE school_id = $1 AND id = $2`,
				sa.SchoolID, id, sec, i+1); err != nil {
				return fmt.Errorf("exam: reorder: %w", err)
			}
		}
		return nil
	})
}

// groupStimuli mengumpulkan id dengan stimulus sama di posisi kemunculan
// pertamanya, urutan lain tetap.
func groupStimuli(ids []string, stim func(string) string) []string {
	var out []string
	done := map[string]bool{}
	for i, id := range ids {
		s := stim(id)
		if s == "" {
			out = append(out, id)
			continue
		}
		if done[s] {
			continue
		}
		done[s] = true
		for _, other := range ids[i:] {
			if stim(other) == s {
				out = append(out, other)
			}
		}
	}
	return out
}

// MoveItem menggeser satu isi ke atas (-1) atau ke bawah (+1) di
// bagiannya; satu kelompok stimulus bergeser sebagai satu blok.
func MoveItem(ctx context.Context, db database.DB, sa StaffActor, examID, itemID string, dir int, now time.Time) error {
	var sectionID string
	var order []string
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := GetExam(ctx, tx, sa.SchoolID, examID)
		if err != nil {
			return err
		}
		secs, err := LoadContent(ctx, tx, e)
		if err != nil {
			return err
		}
		for _, s := range secs {
			// Blok: soal tunggal/aturan, atau satu kelompok stimulus.
			var blocks [][]string
			found := -1
			for _, it := range s.Items {
				stim := ""
				if it.Question != nil {
					stim = it.Question.StimulusID
				}
				n := len(blocks)
				if stim != "" && n > 0 && blockStim(s.Items, blocks[n-1]) == stim {
					blocks[n-1] = append(blocks[n-1], it.ID)
				} else {
					blocks = append(blocks, []string{it.ID})
				}
				if it.ID == itemID {
					found = len(blocks) - 1
				}
			}
			if found < 0 {
				continue
			}
			sectionID = s.ID
			to := found + dir
			if to >= 0 && to < len(blocks) {
				blocks[found], blocks[to] = blocks[to], blocks[found]
			}
			for _, b := range blocks {
				order = append(order, b...)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if sectionID == "" {
		return ErrNotFound
	}
	return ReorderItems(ctx, db, sa, examID, sectionID, order, now)
}

func blockStim(items []ItemRow, ids []string) string {
	for _, it := range items {
		if it.ID == ids[0] && it.Question != nil {
			return it.Question.StimulusID
		}
	}
	return ""
}

// SectionForm adalah isian bagian ujian.
type SectionForm struct{ Title, Instructions string }

// plainHTML mengubah teks polos menjadi paragraf HTML yang aman.
func plainHTML(s string) string {
	var b strings.Builder
	for _, p := range strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(p), "\n", "<br>") + "</p>")
		}
	}
	return b.String()
}

func validateSection(f SectionForm) (string, string, error) {
	title := strings.TrimSpace(f.Title)
	ins := strings.TrimSpace(f.Instructions)
	errs := map[string]string{}
	if utf8.RuneCountInString(title) > 120 {
		errs["title"] = "Judul bagian maksimal 120 karakter."
	}
	if utf8.RuneCountInString(ins) > 4000 {
		errs["instructions"] = "Instruksi maksimal 4.000 karakter."
	}
	if len(errs) > 0 {
		return "", "", &FieldError{Fields: errs}
	}
	return title, ins, nil
}

// AddSection menambahkan bagian baru di akhir.
func AddSection(ctx context.Context, db database.DB, sa StaffActor, examID string, f SectionForm, now time.Time) error {
	title, ins, err := validateSection(f)
	if err != nil {
		return err
	}
	return mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		_, err := tx.Exec(ctx, `INSERT INTO exam_sections (school_id, exam_id, position, title, instructions_src, instructions_html)
			SELECT $1, $2, coalesce(max(position), 0) + 1, $3, $4, $5 FROM exam_sections WHERE exam_id = $2`,
			sa.SchoolID, e.ID, title, ins, plainHTML(ins))
		if err != nil {
			return fmt.Errorf("exam: add section: %w", err)
		}
		return nil
	})
}

// UpdateSection mengubah judul dan instruksi bagian.
func UpdateSection(ctx context.Context, db database.DB, sa StaffActor, examID, sectionID string, f SectionForm, now time.Time) error {
	title, ins, err := validateSection(f)
	if err != nil {
		return err
	}
	return mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		if !database.IsUUID(sectionID) {
			return ErrNotFound
		}
		tag, err := tx.Exec(ctx, `UPDATE exam_sections SET title = $4, instructions_src = $5, instructions_html = $6
			WHERE school_id = $1 AND exam_id = $2 AND id = $3`, sa.SchoolID, e.ID, sectionID, title, ins, plainHTML(ins))
		if err != nil {
			return fmt.Errorf("exam: update section: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ErrSectionNotEmpty: bagian masih berisi soal atau aturan, atau bagian
// terakhir yang tersisa.
var ErrSectionNotEmpty = fmt.Errorf("exam: section not empty")

// RemoveSection menghapus bagian kosong (bukan bagian terakhir).
func RemoveSection(ctx context.Context, db database.DB, sa StaffActor, examID, sectionID string, now time.Time) error {
	return mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		if !database.IsUUID(sectionID) {
			return ErrNotFound
		}
		var items, total int
		err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM exam_items WHERE section_id = $2), (SELECT count(*) FROM exam_sections WHERE exam_id = $1)`,
			e.ID, sectionID).Scan(&items, &total)
		if err != nil {
			return fmt.Errorf("exam: check section: %w", err)
		}
		if items > 0 || total <= 1 {
			return ErrSectionNotEmpty
		}
		tag, err := tx.Exec(ctx, `DELETE FROM exam_sections WHERE school_id = $1 AND exam_id = $2 AND id = $3`, sa.SchoolID, e.ID, sectionID)
		if err != nil {
			return fmt.Errorf("exam: remove section: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// SetVariantCount mengatur jumlah varian paket (1–5).
func SetVariantCount(ctx context.Context, db database.DB, sa StaffActor, examID string, n int, now time.Time) error {
	if n < 1 || n > len(variantLabels) {
		return &FieldError{Fields: map[string]string{"variants": "Jumlah paket 1–5."}}
	}
	return mutate(ctx, db, sa, examID, now, func(tx pgx.Tx, e Exam) error {
		if _, err := tx.Exec(ctx, `UPDATE exams SET variant_count = $2 WHERE id = $1`, e.ID, n); err != nil {
			return fmt.Errorf("exam: set variants: %w", err)
		}
		if err := syncVariants(ctx, tx, sa.SchoolID, e.ID, n); err != nil {
			return err
		}
		return assignParticipantVariants(ctx, tx, e.ID)
	})
}
