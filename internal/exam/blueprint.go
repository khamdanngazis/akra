package exam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"akra/internal/platform/database"
	"akra/internal/platform/settings"
)

// Pengaturan modul ujian sekolah (T132), diatur admin_tu atau koordinator.
const (
	// SettingReview: "true" bila telaah soal aktif.
	SettingReview = "exam.review_enabled"
	// SettingTargetCognitive dan SettingTargetDifficulty: target proporsi
	// kisi-kisi "LOTS/MOTS/HOTS" dan "mudah/sedang/sulit" dalam persen,
	// mis. "30/50/20".
	SettingTargetCognitive  = "exam.target_cognitive"
	SettingTargetDifficulty = "exam.target_difficulty"
	defaultTarget           = "30/50/20"
)

// Targets adalah target proporsi kisi-kisi.
type Targets struct {
	Cognitive  [3]int
	Difficulty [3]int
}

// ParseTarget membaca "30/50/20"; jumlahnya harus 100.
func ParseTarget(s string) ([3]int, bool) {
	parts := strings.Split(strings.ReplaceAll(s, " ", ""), "/")
	var out [3]int
	if len(parts) != 3 {
		return out, false
	}
	sum := 0
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 100 {
			return out, false
		}
		out[i], sum = n, sum+n
	}
	return out, sum == 100
}

// FormatTarget menulis target sebagai "30/50/20".
func FormatTarget(t [3]int) string { return fmt.Sprintf("%d/%d/%d", t[0], t[1], t[2]) }

// ExamSettings adalah pengaturan modul ujian sekolah.
type ExamSettings struct {
	Review  bool
	Targets Targets
}

// LoadExamSettings membaca pengaturan modul ujian sekolah.
func LoadExamSettings(ctx context.Context, q database.DBTX, schoolID string) (ExamSettings, error) {
	var s ExamSettings
	var err error
	if s.Review, err = settings.Bool(ctx, q, schoolID, SettingReview, false); err != nil {
		return s, err
	}
	for _, t := range []struct {
		key string
		dst *[3]int
	}{{SettingTargetCognitive, &s.Targets.Cognitive}, {SettingTargetDifficulty, &s.Targets.Difficulty}} {
		v, ok, err := settings.Get(ctx, q, schoolID, t.key)
		if err != nil {
			return s, err
		}
		if !ok {
			v = defaultTarget
		}
		parsed, valid := ParseTarget(v)
		if !valid {
			parsed, _ = ParseTarget(defaultTarget)
		}
		*t.dst = parsed
	}
	return s, nil
}

// SaveExamSettings menyimpan pengaturan beserta auditnya.
func SaveExamSettings(ctx context.Context, db database.DB, sa StaffActor, review bool, cognitive, difficulty string) error {
	fields := map[string]string{}
	if _, ok := ParseTarget(cognitive); !ok {
		fields["cognitive"] = "Tulis tiga angka yang jumlahnya 100, mis. 30/50/20."
	}
	if _, ok := ParseTarget(difficulty); !ok {
		fields["difficulty"] = "Tulis tiga angka yang jumlahnya 100, mis. 30/50/20."
	}
	if len(fields) > 0 {
		return &FieldError{Fields: fields}
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for k, v := range map[string]string{SettingReview: strconv.FormatBool(review), SettingTargetCognitive: strings.ReplaceAll(cognitive, " ", ""),
			SettingTargetDifficulty: strings.ReplaceAll(difficulty, " ", "")} {
			if err := settings.Set(ctx, tx, sa.SchoolID, k, v); err != nil {
				return err
			}
		}
		return recordAudit(ctx, tx, sa, "exam_settings.update", "school", sa.SchoolID, nil,
			map[string]any{"review": review, "cognitive": cognitive, "difficulty": difficulty})
	})
}

// UsableInSchoolExam melaporkan apakah soal boleh dipakai ujian sekolah
// (PTS/PAS), dipakai pembuatan ujian (T133/T134). Hanya soal siap yang
// tidak diarsipkan. Bila telaah aktif, status siap hanya bisa dicapai
// lewat persetujuan penelaah (editor dan impor mengubah "siap" menjadi
// "ditelaah", dan setiap perubahan soal mengembalikannya ke telaah), jadi
// status cukup. Soal yang sudah siap sebelum telaah diaktifkan dianggap
// disetujui.
func UsableInSchoolExam(q Question, s ExamSettings) bool {
	_ = s.Review
	return q.ArchivedAt == nil && q.Status == StatusReady
}

// reviewStatus menyesuaikan status simpan dengan telaah: bila telaah aktif,
// soal yang ditandai siap diajukan untuk ditelaah.
func reviewStatus(status string, s ExamSettings) string {
	if s.Review && status == StatusReady {
		return StatusInReview
	}
	return status
}

// BlueprintRow adalah satu baris kisi-kisi.
type BlueprintRow struct {
	No             int
	Objective      string // "TP 3.2 — deskripsi"
	Topic          string // materi (tag)
	Indicator      string
	CognitiveLevel string
	Group          string // LOTS/MOTS/HOTS
	Kind           string
	Difficulty     string
	Points         int
}

// Share adalah proporsi satu kelompok terhadap target.
type Share struct {
	Label   string
	Count   int
	Percent int
	Target  int
	Off     bool // menyimpang lebih dari 10 poin persen dari target
}

// Blueprint adalah kisi-kisi sekumpulan soal.
type Blueprint struct {
	Rows        []BlueprintRow
	Cognitive   []Share
	Difficulty  []Share
	Untagged    int // soal tanpa level kognitif atau kesulitan
	TotalPoints int
	Warnings    []string
}

func shares(labels []string, counts []int, target [3]int, total int) ([]Share, bool) {
	var out []Share
	off := false
	for i, l := range labels {
		p := 0
		if total > 0 {
			p = int(math.Round(float64(counts[i]) * 100 / float64(total)))
		}
		s := Share{Label: l, Count: counts[i], Percent: p, Target: target[i], Off: total > 0 && abs(p-target[i]) > 10}
		off = off || s.Off
		out = append(out, s)
	}
	return out, off
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// BuildBlueprint menyusun kisi-kisi dari soal (urut nomor ujian).
func BuildBlueprint(qs []Question, objectives map[string]Objective, t Targets) Blueprint {
	var b Blueprint
	cog, diff := make([]int, 3), make([]int, 3)
	cogN, diffN := 0, 0
	for i, q := range qs {
		row := BlueprintRow{No: i + 1, Indicator: q.Indicator, CognitiveLevel: q.CognitiveLevel, Group: CognitiveGroup(q.CognitiveLevel),
			Kind: q.Kind.Label(), Difficulty: q.Difficulty, Points: q.Points, Topic: strings.Join(q.Tags, ", ")}
		if o, ok := objectives[q.ObjectiveID]; ok {
			row.Objective = o.Code + " — " + o.Description
		}
		b.TotalPoints += q.Points
		switch row.Group {
		case "LOTS":
			cog[0]++
		case "MOTS":
			cog[1]++
		case "HOTS":
			cog[2]++
		}
		if row.Group != "" {
			cogN++
		}
		switch q.Difficulty {
		case "mudah":
			diff[0]++
		case "sedang":
			diff[1]++
		case "sulit":
			diff[2]++
		}
		if q.Difficulty != "" {
			diffN++
		}
		if row.Group == "" || q.Difficulty == "" {
			b.Untagged++
		}
		b.Rows = append(b.Rows, row)
	}
	var off bool
	b.Cognitive, off = shares([]string{"LOTS (C1–C2)", "MOTS (C3)", "HOTS (C4–C6)"}, cog, t.Cognitive, cogN)
	if off {
		b.Warnings = append(b.Warnings, "Proporsi LOTS/MOTS/HOTS menyimpang dari target "+FormatTarget(t.Cognitive)+".")
	}
	b.Difficulty, off = shares([]string{"Mudah", "Sedang", "Sulit"}, diff, t.Difficulty, diffN)
	if off {
		b.Warnings = append(b.Warnings, "Proporsi mudah/sedang/sulit menyimpang dari target "+FormatTarget(t.Difficulty)+".")
	}
	if b.Untagged > 0 {
		b.Warnings = append(b.Warnings, fmt.Sprintf("%d soal belum diberi level kognitif atau kesulitan.", b.Untagged))
	}
	return b
}

// BlueprintXLSX menulis kisi-kisi ke Excel.
func BlueprintXLSX(title string, b Blueprint) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sh := "kisi-kisi"
	if err := f.SetSheetName("Sheet1", sh); err != nil {
		return nil, err
	}
	_ = f.SetCellValue(sh, "A1", title)
	header := []any{"No", "TP", "Materi", "Indikator", "Level kognitif", "Bentuk soal", "Kesulitan", "Poin"}
	if err := f.SetSheetRow(sh, "A3", &header); err != nil {
		return nil, err
	}
	for i, r := range b.Rows {
		lvl := r.CognitiveLevel
		if r.Group != "" {
			lvl += " (" + r.Group + ")"
		}
		row := []any{r.No, r.Objective, r.Topic, r.Indicator, lvl, r.Kind, r.Difficulty, r.Points}
		if err := f.SetSheetRow(sh, fmt.Sprintf("A%d", i+4), &row); err != nil {
			return nil, err
		}
	}
	n := len(b.Rows) + 5
	_ = f.SetCellValue(sh, fmt.Sprintf("A%d", n), "Ringkasan")
	for i, s := range append(append([]Share{}, b.Cognitive...), b.Difficulty...) {
		row := []any{s.Label, fmt.Sprintf("%d soal", s.Count), fmt.Sprintf("%d%%", s.Percent), fmt.Sprintf("target %d%%", s.Target)}
		_ = f.SetSheetRow(sh, fmt.Sprintf("A%d", n+1+i), &row)
	}
	_ = f.SetColWidth(sh, "B", "B", 45)
	_ = f.SetColWidth(sh, "C", "D", 30)
	_ = f.SetColWidth(sh, "E", "G", 16)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- Telaah ---

// Review adalah satu catatan telaah.
type Review struct {
	ReviewerName string
	Decision     string
	Note         string
	Version      int
	CreatedAt    time.Time
}

// Error telaah.
var (
	ErrReviewOff     = errors.New("exam: review is not enabled")
	ErrReviewOwn     = errors.New("exam: cannot review own question")
	ErrReviewState   = errors.New("exam: question is not in the expected state")
	ErrReviewDecided = &FieldError{Fields: map[string]string{"decision": "Pilih setujui atau minta perbaikan."}}
)

// SubmitForReview mengajukan soal draf milik sa untuk ditelaah. Soal
// harus lengkap (validasi penuh dilakukan pemanggil sebelum menyimpan).
func SubmitForReview(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		s, err := LoadExamSettings(ctx, tx, sa.SchoolID)
		if err != nil {
			return err
		}
		if !s.Review {
			return ErrReviewOff
		}
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
		if q.Status != StatusDraft {
			return ErrReviewState
		}
		if !questionComplete(q) {
			return &FieldError{Fields: map[string]string{"status": "Lengkapi soal (kunci, opsi) sebelum diajukan untuk ditelaah."}}
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_questions SET status = 'ditelaah', updated_at = now() WHERE school_id = $1 AND id = $2`, sa.SchoolID, id); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam_question.submit_review", "exam_question", id, nil, nil)
	})
}

// questionComplete melaporkan apakah soal tersimpan memenuhi validasi
// penuh jenisnya (tanpa merender ulang).
func questionComplete(q Question) bool {
	switch q.Kind {
	case KindMC:
		return len(q.Options.Choices) >= minChoices && len(q.Key.Correct) == 1
	case KindMulti:
		return len(q.Options.Choices) >= minChoices && len(q.Key.Correct) >= 1
	case KindTF:
		return len(q.Options.Statements) > 0 && len(q.Key.Truth) == len(q.Options.Statements)
	case KindShort:
		return len(q.Key.Accepted) > 0
	case KindMatch:
		return len(q.Options.Left) >= 2
	}
	return true
}

// ReviewQuestion mencatat keputusan penelaah: setuju (soal menjadi siap)
// atau perbaiki (kembali ke draf dengan catatan). Penelaah tidak boleh
// pembuat soal dan harus boleh membuka bank mapelnya.
func ReviewQuestion(ctx context.Context, db database.DB, sa StaffActor, id, decision, note string, now time.Time) error {
	note = strings.TrimSpace(note)
	if decision != "setuju" && decision != "perbaiki" {
		return ErrReviewDecided
	}
	if decision == "perbaiki" && note == "" {
		return &FieldError{Fields: map[string]string{"note": "Tulis catatan perbaikan untuk pembuat soal."}}
	}
	if len([]rune(note)) > 2000 {
		return &FieldError{Fields: map[string]string{"note": "Catatan maksimal 2.000 karakter."}}
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		s, err := LoadExamSettings(ctx, tx, sa.SchoolID)
		if err != nil {
			return err
		}
		if !s.Review {
			return ErrReviewOff
		}
		q, err := GetQuestion(ctx, tx, sa.SchoolID, id)
		if err != nil {
			return err
		}
		b, err := GetBank(ctx, tx, sa.SchoolID, q.BankID)
		if err != nil {
			return err
		}
		if sa.Can(ActBank, b.resource(), now) < Write {
			return ErrNotFound
		}
		if q.CreatedBy == sa.UserID {
			return ErrReviewOwn
		}
		if q.Status != StatusInReview || q.ArchivedAt != nil {
			return ErrReviewState
		}
		status := StatusReady
		if decision == "perbaiki" {
			status = StatusDraft
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_questions SET status = $3, updated_at = now() WHERE school_id = $1 AND id = $2`, sa.SchoolID, id, status); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_question_reviews (school_id, question_id, version, reviewer_id, decision, note) VALUES ($1, $2, $3, $4, $5, $6)`,
			sa.SchoolID, id, q.Version, sa.UserID, decision, note); err != nil {
			return fmt.Errorf("exam: save review: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam_question.review", "exam_question", id, nil, map[string]any{"decision": decision})
	})
}

// ListReviews mengembalikan riwayat telaah soal, terbaru dulu.
func ListReviews(ctx context.Context, q database.DBTX, schoolID, questionID string) ([]Review, error) {
	rows, err := q.Query(ctx, `SELECT u.full_name, r.decision, r.note, r.version, r.created_at FROM exam_question_reviews r
		JOIN users u ON u.id = r.reviewer_id AND u.school_id = r.school_id
		WHERE r.school_id = $1 AND r.question_id = $2 ORDER BY r.created_at DESC`, schoolID, questionID)
	if err != nil {
		return nil, fmt.Errorf("exam: list reviews: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Review, error) {
		var v Review
		err := r.Scan(&v.ReviewerName, &v.Decision, &v.Note, &v.Version, &v.CreatedAt)
		return v, err
	})
}

// ReviewQueue mengembalikan soal berstatus ditelaah yang boleh ditelaah
// sa (bank yang bisa ditulisnya, bukan soal buatannya sendiri).
func ReviewQueue(ctx context.Context, q database.DBTX, sa StaffActor, now time.Time) ([]QuestionRow, error) {
	rows, err := q.Query(ctx, `SELECT `+questionCols+`, u.full_name, b.name FROM exam_questions q
		JOIN users u ON u.id = q.created_by AND u.school_id = q.school_id
		JOIN exam_banks b ON b.id = q.bank_id AND b.school_id = q.school_id
		WHERE q.school_id = $1 AND q.status = 'ditelaah' AND q.archived_at IS NULL AND b.archived_at IS NULL AND q.created_by <> $2
		ORDER BY q.updated_at LIMIT 500`, sa.SchoolID, sa.UserID)
	if err != nil {
		return nil, fmt.Errorf("exam: review queue: %w", err)
	}
	defer rows.Close()
	banks := map[string]Bank{}
	var out []QuestionRow
	for rows.Next() {
		var r QuestionRow
		var opts, key []byte
		var bankName string
		if err := rows.Scan(&r.ID, &r.SchoolID, &r.BankID, &r.StimulusID, &r.StimulusOrder, &r.Kind, &r.BodySrc, &r.BodyHTML, &opts, &key,
			&r.Points, &r.Difficulty, &r.CognitiveLevel, &r.ObjectiveID, &r.Indicator, &r.Tags, &r.ExplanationSrc, &r.ExplanationHTML,
			&r.Status, &r.Version, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &r.ArchivedAt, &r.CreatorName, &bankName); err != nil {
			return nil, err
		}
		r.StimulusTitle = bankName // nama bank untuk antrean
		r.Snippet = Snippet(PlainText(r.BodySrc), 160)
		out = append(out, r)
		banks[r.BankID] = Bank{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for id := range banks {
		b, err := GetBank(ctx, q, sa.SchoolID, id)
		if err != nil {
			return nil, err
		}
		banks[id] = b
	}
	filtered := out[:0]
	for _, r := range out {
		if sa.Can(ActBank, banks[r.BankID].resource(), now) >= Write {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// --- Cek mutu ---

// QualityReport adalah hasil cek mutu otomatis bank (T132).
type QualityReport struct {
	MCCount       int
	KeyShares     []Share // sebaran posisi kunci PG (A–F)
	KeySkewed     string  // mis. "B (62%)" bila satu posisi > 40% dari ≥ 10 soal
	AllCorrect    []QuestionRow
	NoObjective   int
	Duplicates    [][2]QuestionRow
	NearDuplicate bool // perbandingan kemiripan dijalankan (bank ≤ 3000 soal)
}

// allCorrectPhrases adalah opsi "semua benar" dan sejenisnya (sudah dinormalkan).
var allCorrectPhrases = []string{"semua benar", "semua jawaban benar", "semua di atas benar", "semua jawaban di atas benar", "a b dan c benar", "a b c benar"}

// isAllCorrect melaporkan apakah opsi berbunyi "semua benar" dan sejenisnya.
func isAllCorrect(text string) bool {
	t := normalizeAnswer(text, false)
	for _, w := range allCorrectPhrases {
		if t == w {
			return true
		}
	}
	return false
}

func shingles(text string) map[string]bool {
	w := strings.Fields(NormalizeSearch(normalizeAnswer(text, true)))
	out := map[string]bool{}
	if len(w) < 3 {
		out[strings.Join(w, " ")] = true
		return out
	}
	for i := 0; i+3 <= len(w); i++ {
		out[strings.Join(w[i:i+3], " ")] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

// CheckQuality menjalankan cek mutu atas soal aktif bank.
func CheckQuality(qs []Question) QualityReport {
	var rep QualityReport
	pos := make([]int, maxChoices)
	for _, q := range qs {
		if q.ObjectiveID == "" {
			rep.NoObjective++
		}
		if q.Kind == KindMC && len(q.Key.Correct) == 1 {
			rep.MCCount++
			for i, c := range q.Options.Choices {
				if c.ID == q.Key.Correct[0] {
					pos[i]++
				}
			}
		}
		if q.Kind == KindMC || q.Kind == KindMulti {
			for _, c := range q.Options.Choices {
				if isAllCorrect(PlainText(c.Src)) {
					rep.AllCorrect = append(rep.AllCorrect, QuestionRow{Question: q, Snippet: Snippet(PlainText(q.BodySrc), 120)})
					break
				}
			}
		}
	}
	for i, n := range pos {
		p := 0
		if rep.MCCount > 0 {
			p = int(math.Round(float64(n) * 100 / float64(rep.MCCount)))
		}
		rep.KeyShares = append(rep.KeyShares, Share{Label: letter(i), Count: n, Percent: p})
		if rep.MCCount >= 10 && p > 40 {
			rep.KeySkewed = fmt.Sprintf("%s (%d%%)", letter(i), p)
		}
	}
	// Soal kembar: teks soal + opsi yang sama setelah dinormalkan, dan
	// (untuk bank ≤ 3000 soal) teks yang sangat mirip (Jaccard ≥ 0,85).
	type entry struct {
		q   Question
		key string
		sh  map[string]bool
	}
	var es []entry
	byKey := map[string]int{}
	for _, q := range qs {
		parts := []string{PlainText(q.BodySrc)}
		for _, c := range q.Options.Choices {
			parts = append(parts, PlainText(c.Src))
		}
		key := normalizeAnswer(strings.Join(parts, " | "), true)
		if j, ok := byKey[key]; ok {
			rep.Duplicates = append(rep.Duplicates, [2]QuestionRow{
				{Question: es[j].q, Snippet: Snippet(PlainText(es[j].q.BodySrc), 120)}, {Question: q, Snippet: Snippet(PlainText(q.BodySrc), 120)}})
			continue
		}
		byKey[key] = len(es)
		es = append(es, entry{q: q, key: key})
	}
	if len(es) <= 3000 {
		rep.NearDuplicate = true
		for i := range es {
			es[i].sh = shingles(PlainText(es[i].q.BodySrc))
		}
		for i := 0; i < len(es) && len(rep.Duplicates) < 100; i++ {
			for j := i + 1; j < len(es); j++ {
				if len(es[i].sh) < 3 || es[i].q.Kind != es[j].q.Kind {
					continue
				}
				if jaccard(es[i].sh, es[j].sh) >= 0.85 {
					rep.Duplicates = append(rep.Duplicates, [2]QuestionRow{
						{Question: es[i].q, Snippet: Snippet(PlainText(es[i].q.BodySrc), 120)}, {Question: es[j].q, Snippet: Snippet(PlainText(es[j].q.BodySrc), 120)}})
				}
			}
		}
	}
	sort.SliceStable(rep.Duplicates, func(i, j int) bool { return rep.Duplicates[i][0].CreatedAt.Before(rep.Duplicates[j][0].CreatedAt) })
	return rep
}
