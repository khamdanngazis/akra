package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/platform/database"
)

// Hasil ujian (T148): ringkasan, tabel peserta, analisis butir, analisis
// TP, dan daftar remedial/pengayaan dari satu kali muat (beberapa query,
// lalu dihitung di memori). Statistik hanya memakai nilai final; peserta
// yang menunggu koreksi esai ditampilkan tetapi belum dihitung.

// ResultFilter menyaring hasil per kelas, sesi, atau paket.
type ResultFilter struct {
	ClassID, SessionID, VariantID string
}

// Empty melaporkan tidak ada saringan.
func (f ResultFilter) Empty() bool { return f.ClassID == "" && f.SessionID == "" && f.VariantID == "" }

// Query mengembalikan saringan sebagai query string (tanpa "?").
func (f ResultFilter) Query() string {
	v := url.Values{}
	for _, kv := range [][2]string{{"kelas", f.ClassID}, {"sesi", f.SessionID}, {"paket", f.VariantID}} {
		if kv[1] != "" {
			v.Set(kv[0], kv[1])
		}
	}
	return v.Encode()
}

// ParticipantResult adalah satu peserta dengan attempt terakhirnya.
type ParticipantResult struct {
	ParticipantID string
	StudentID     string
	ApplicantID   string
	Name          string
	Number        string
	ClassID       string
	ClassName     string
	SessionID     string
	SessionTitle  string
	SessionKind   string
	VariantID     string
	VariantLabel  string
	AttemptID     string
	AttemptNo     int
	Status        string // kosong: tidak hadir
	ScoreX100     *int
	ScoreStat     string
	StartedAt     *time.Time
	SubmittedAt   *time.Time
	Leaves        int
	AwayMS        int64
	DeviceMoves   int
	Locked        bool
}

// Key adalah id peserta unik (siswa atau pendaftar).
func (p ParticipantResult) Key() string {
	if p.StudentID != "" {
		return p.StudentID
	}
	return p.ApplicantID
}

// Final melaporkan nilai final (masuk statistik).
func (p ParticipantResult) Final() bool {
	return p.Status == AttemptSubmitted && p.ScoreStat == ScoreFinal && p.ScoreX100 != nil
}

// Score menampilkan nilai, kosong bila belum ada.
func (p ParticipantResult) Score() string {
	if p.ScoreX100 == nil || p.Status != AttemptSubmitted {
		return ""
	}
	return ScoreLabel(*p.ScoreX100)
}

// StatusLabel menampilkan status peserta.
func (p ParticipantResult) StatusLabel() string {
	switch {
	case p.Status == "":
		return "Tidak hadir"
	case p.Status == AttemptRunning:
		return "Mengerjakan"
	case p.ScoreStat == ScoreAwaitEssay:
		return "Menunggu koreksi esai"
	case p.ScoreStat == ScoreFinal:
		return "Selesai"
	}
	return "Dikumpulkan, belum dinilai"
}

// KindLabel menampilkan jenis sesi selain sesi utama (susulan).
func (p ParticipantResult) KindLabel() string {
	if p.SessionKind == "" || p.SessionKind == "utama" {
		return ""
	}
	return sessionKindLabels[p.SessionKind]
}

// Duration menampilkan lama mengerjakan, mis. "47 mnt".
func (p ParticipantResult) Duration() string {
	if p.StartedAt == nil || p.SubmittedAt == nil {
		return ""
	}
	return durationLabel(p.SubmittedAt.Sub(*p.StartedAt))
}

func durationLabel(d time.Duration) string {
	m := int(math.Round(d.Minutes()))
	if m < 1 {
		return "< 1 mnt"
	}
	if m < 60 {
		return strconv.Itoa(m) + " mnt"
	}
	if m%60 == 0 {
		return strconv.Itoa(m/60) + " j"
	}
	return fmt.Sprintf("%d j %d mnt", m/60, m%60)
}

// IntegrityText meringkas integritas (T141), kosong bila bersih.
func (p ParticipantResult) IntegrityText() string {
	var parts []string
	if p.Leaves > 0 {
		s := strconv.Itoa(p.Leaves) + "× keluar halaman"
		if p.AwayMS >= 1000 {
			s += " (" + awayLabel(time.Duration(p.AwayMS)*time.Millisecond) + ")"
		}
		parts = append(parts, s)
	}
	if p.DeviceMoves > 0 {
		parts = append(parts, strconv.Itoa(p.DeviceMoves)+"× pindah perangkat")
	}
	if p.Locked {
		parts = append(parts, "terkunci")
	}
	return strings.Join(parts, ", ")
}

// resultCols adalah kolom ParticipantResult; dipakai dengan FROM resultFrom.
const resultCols = `p.id, coalesce(p.student_id::text, ''), coalesce(p.applicant_id::text, ''), coalesce(s.full_name, ap.full_name, ''),
	coalesce(nullif(p.participant_no, ''), s.nis, ap.registration_number, ''), coalesce(cl.id::text, ''), coalesce(cl.name, ''),
	x.id, x.kind, coalesce(nullif(x.label, ''), r.name, xc.name, ''), coalesce(t.variant_id::text, p.variant_id::text, ''),
	coalesce(t.id::text, ''), coalesce(t.attempt_no, 0), coalesce(t.status, ''), t.score_x100, coalesce(t.score_status, ''),
	t.started_at, t.submitted_at, coalesce(t.leave_count, 0), coalesce(t.away_ms, 0), coalesce(t.device_moves, 0), t.locked_at IS NOT NULL`

const resultFrom = `
	FROM exam_session_participants p
	JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
	LEFT JOIN exam_rooms r ON r.id = x.room_id
	LEFT JOIN classes xc ON xc.id = x.class_id
	LEFT JOIN students s ON s.id = p.student_id
	LEFT JOIN applicants ap ON ap.id = p.applicant_id
	LEFT JOIN LATERAL (SELECT c.id, c.name FROM enrollments en JOIN classes c ON c.id = en.class_id
	                   WHERE en.student_id = p.student_id AND en.end_date IS NULL ORDER BY en.start_date DESC LIMIT 1) cl ON true`

func scanResult(row pgx.Row) (ParticipantResult, error) {
	var p ParticipantResult
	var no int16
	var dm int16
	err := row.Scan(&p.ParticipantID, &p.StudentID, &p.ApplicantID, &p.Name, &p.Number, &p.ClassID, &p.ClassName,
		&p.SessionID, &p.SessionKind, &p.SessionTitle, &p.VariantID, &p.AttemptID, &no, &p.Status, &p.ScoreX100, &p.ScoreStat,
		&p.StartedAt, &p.SubmittedAt, &p.Leaves, &p.AwayMS, &dm, &p.Locked)
	p.AttemptNo, p.DeviceMoves = int(no), int(dm)
	return p, err
}

// LoadParticipantResults mengembalikan satu baris per peserta (siswa atau
// pendaftar) ujian e dengan attempt terakhirnya. Peserta yang punya sesi
// utama dan susulan ditampilkan dari sesi tempat ia mengerjakan. Urut
// kelas, nama.
func LoadParticipantResults(ctx context.Context, q database.DBTX, e Exam) ([]ParticipantResult, error) {
	rows, err := q.Query(ctx, `
		SELECT * FROM (
			SELECT DISTINCT ON (coalesce(p.student_id, p.applicant_id, p.id)) `+resultCols+resultFrom+`
			LEFT JOIN LATERAL (SELECT * FROM exam_attempts t WHERE t.participant_id = p.id ORDER BY t.attempt_no DESC LIMIT 1) t ON true
			WHERE p.school_id = $1 AND x.exam_id = $2
			ORDER BY coalesce(p.student_id, p.applicant_id, p.id), t.id IS NOT NULL DESC, t.started_at DESC NULLS LAST, x.kind = 'utama' DESC
		) z ORDER BY 7, 4, 5`, e.SchoolID, e.ID)
	if err != nil {
		return nil, fmt.Errorf("exam: participant results: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ParticipantResult, error) { return scanResult(r) })
	if err != nil {
		return nil, fmt.Errorf("exam: participant results: %w", err)
	}
	return out, nil
}

// loadAttemptResult mengembalikan peserta untuk satu attempt ujian e.
func loadAttemptResult(ctx context.Context, q database.DBTX, e Exam, attemptID string) (ParticipantResult, error) {
	if !database.IsUUID(attemptID) {
		return ParticipantResult{}, ErrNotFound
	}
	p, err := scanResult(q.QueryRow(ctx, `SELECT `+resultCols+resultFrom+`
		JOIN exam_attempts t ON t.participant_id = p.id
		WHERE p.school_id = $1 AND x.exam_id = $2 AND t.id = $3`, e.SchoolID, e.ID, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("exam: attempt result: %w", err)
	}
	return p, nil
}

// answerRec adalah jawaban tersimpan satu soal.
type answerRec struct {
	Value  json.RawMessage
	Points *int
}

// loadAnswerRecs membaca jawaban attempt-attempt (attempt → soal → jawaban).
func loadAnswerRecs(ctx context.Context, q database.DBTX, attemptIDs []string) (map[string]map[string]answerRec, error) {
	out := map[string]map[string]answerRec{}
	if len(attemptIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT attempt_id::text, question_id::text, value, points_x10000 FROM exam_answers WHERE attempt_id = ANY($1::uuid[])`, attemptIDs)
	if err != nil {
		return nil, fmt.Errorf("exam: analysis answers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var aid, qid string
		var a answerRec
		if err := rows.Scan(&aid, &qid, &a.Value, &a.Points); err != nil {
			return nil, fmt.Errorf("exam: analysis answers: %w", err)
		}
		m := out[aid]
		if m == nil {
			m = map[string]answerRec{}
			out[aid] = m
		}
		m[qid] = a
	}
	return out, rows.Err()
}

// blankAnswer: tidak ada jawaban berisi.
func blankAnswer(raw json.RawMessage) bool {
	switch trimSpace(string(raw)) {
	case "", "null", `""`, "[]", "{}":
		return true
	}
	return false
}

// pickedOptions membaca opsi yang dipilih (PG: string, PG kompleks: array).
func pickedOptions(k Kind, raw json.RawMessage) []string {
	switch k {
	case KindMC:
		var v string
		if json.Unmarshal(raw, &v) == nil && v != "" {
			return []string{v}
		}
	case KindMulti:
		var vs []string
		if json.Unmarshal(raw, &vs) == nil {
			return vs
		}
	}
	return nil
}

// KKTP adalah ambang ketuntasan untuk analisis.
type KKTP struct {
	Value  int
	Label  string // "KKTP" atau "KKM" (format rapor)
	Source string // asal nilai, untuk tampilan
}

// ResolveKKTP menentukan ambang ketuntasan ujian: aturan ujian, lalu KKM
// mapel, lalu bawaan sekolah (pengaturan rapor T73).
func ResolveKKTP(ctx context.Context, q database.DBTX, e Exam) (KKTP, error) {
	def, label, err := academic.SchoolThreshold(ctx, q, e.SchoolID)
	if err != nil {
		return KKTP{}, err
	}
	k := KKTP{Value: def, Label: label, Source: "bawaan sekolah"}
	if e.PassScore != nil {
		k.Value, k.Source = *e.PassScore, "aturan ujian"
		return k, nil
	}
	if database.IsUUID(e.SubjectID) {
		var kkm *int16
		if err := q.QueryRow(ctx, `SELECT kkm FROM subjects WHERE school_id = $1 AND id = $2`, e.SchoolID, e.SubjectID).Scan(&kkm); err == nil && kkm != nil {
			k.Value, k.Source = int(*kkm), "mapel "+e.SubjectName
		}
	}
	return k, nil
}

// HistBin adalah satu kelas interval histogram nilai.
type HistBin struct {
	Label string
	Count int
	Width int // lebar batang relatif terhadap kelas terbanyak (0–100)
}

// ResultSummary adalah ringkasan hasil.
type ResultSummary struct {
	Registered int
	Present    int // pernah mulai mengerjakan
	Absent     int
	Working    int
	Awaiting   int // menunggu koreksi esai
	Final      int
	Mean       int // ×100
	Max        int
	Min        int
	Pass       int
	Fail       int
	Bins       []HistBin
}

// MeanText, MaxText, MinText menampilkan statistik nilai.
func (s ResultSummary) MeanText() string { return ScoreLabel(s.Mean) }
func (s ResultSummary) MaxText() string  { return ScoreLabel(s.Max) }
func (s ResultSummary) MinText() string  { return ScoreLabel(s.Min) }

// PassPercent mengembalikan persentase tuntas dari nilai final.
func (s ResultSummary) PassPercent() int {
	if s.Final == 0 {
		return 0
	}
	return int(math.Round(float64(s.Pass) * 100 / float64(s.Final)))
}

// SummarizeResults menghitung ringkasan dari baris peserta. Rata-rata dibulatkan
// ke seperseratus. Histogram: 0–9, 10–19, ..., 90–100.
func SummarizeResults(rows []ParticipantResult, kktp int) ResultSummary {
	s := ResultSummary{Registered: len(rows), Bins: make([]HistBin, 10)}
	for i := range s.Bins {
		hi := i*10 + 9
		if i == 9 {
			hi = 100
		}
		s.Bins[i].Label = strconv.Itoa(i*10) + "–" + strconv.Itoa(hi)
	}
	sum := 0
	for _, r := range rows {
		switch {
		case r.Status == "":
			s.Absent++
			continue
		case r.Status == AttemptRunning:
			s.Working++
		case r.ScoreStat == ScoreAwaitEssay:
			s.Awaiting++
		}
		s.Present++
		if !r.Final() {
			continue
		}
		v := *r.ScoreX100
		if s.Final == 0 || v > s.Max {
			s.Max = v
		}
		if s.Final == 0 || v < s.Min {
			s.Min = v
		}
		s.Final++
		sum += v
		if v >= kktp*100 {
			s.Pass++
		} else {
			s.Fail++
		}
		s.Bins[min(v/1000, 9)].Count++
	}
	if s.Final > 0 {
		s.Mean = (2*sum + s.Final) / (2 * s.Final)
	}
	top := 0
	for _, b := range s.Bins {
		top = max(top, b.Count)
	}
	for i := range s.Bins {
		if top > 0 {
			s.Bins[i].Width = s.Bins[i].Count * 100 / top
		}
	}
	return s
}

// ExamAnalysis adalah seluruh hasil dan analisis satu ujian.
type ExamAnalysis struct {
	Exam       Exam
	KKTP       KKTP
	Filter     ResultFilter
	Rows       []ParticipantResult // tersaring
	Summary    ResultSummary
	Items      []ItemStat
	Objectives ObjectiveAnalysis
	Follow     FollowList
	Classes    []optionItem
	Sessions   []optionItem
	Variants   []optionItem
}

// LoadExamAnalysis memuat hasil ujian e dengan saringan f dan menghitung
// semua analisis.
func LoadExamAnalysis(ctx context.Context, q database.DBTX, e Exam, f ResultFilter) (ExamAnalysis, error) {
	a := ExamAnalysis{Exam: e, Filter: f}
	var err error
	if a.KKTP, err = ResolveKKTP(ctx, q, e); err != nil {
		return a, err
	}
	all, err := LoadParticipantResults(ctx, q, e)
	if err != nil {
		return a, err
	}
	variants, err := ListVariants(ctx, q, e.SchoolID, e.ID)
	if err != nil {
		return a, err
	}
	sessions, err := ListSessions(ctx, q, e.SchoolID, e.ID)
	if err != nil {
		return a, err
	}
	// Pilihan saringan dari semua peserta.
	classes := map[string]string{}
	for _, r := range all {
		if r.ClassID != "" {
			classes[r.ClassID] = r.ClassName
		}
	}
	for id, name := range classes {
		a.Classes = append(a.Classes, optionItem{Value: id, Label: name, Selected: id == f.ClassID})
	}
	sort.Slice(a.Classes, func(i, j int) bool { return a.Classes[i].Label < a.Classes[j].Label })
	for _, s := range sessions {
		a.Sessions = append(a.Sessions, optionItem{Value: s.ID, Label: s.Title(), Selected: s.ID == f.SessionID})
	}
	labels := map[string]string{}
	for _, v := range variants {
		labels[v.ID] = v.Label
		a.Variants = append(a.Variants, optionItem{Value: v.ID, Label: "Paket " + v.Label, Selected: v.ID == f.VariantID})
	}
	for _, r := range all {
		r.VariantLabel = labels[r.VariantID]
		if (f.ClassID == "" || r.ClassID == f.ClassID) && (f.SessionID == "" || r.SessionID == f.SessionID) &&
			(f.VariantID == "" || r.VariantID == f.VariantID) {
			a.Rows = append(a.Rows, r)
		}
	}
	a.Summary = SummarizeResults(a.Rows, a.KKTP.Value)

	// Soal per paket (snapshot, kunci koreksi, soal dibatalkan).
	items := map[string][]GradeItem{}
	for _, v := range variants {
		if items[v.ID], err = LoadGradeItems(ctx, q, e, v.ID); err != nil {
			return a, err
		}
	}
	var final []ParticipantResult
	var ids []string
	for _, r := range a.Rows {
		if r.Final() {
			final = append(final, r)
			ids = append(ids, r.AttemptID)
		}
	}
	answers, err := loadAnswerRecs(ctx, q, ids)
	if err != nil {
		return a, err
	}
	refs, err := objectiveRefs(ctx, q, e.SchoolID, items)
	if err != nil {
		return a, err
	}
	codes := map[string]string{}
	for _, r := range refs {
		codes[r.ID] = r.Code
	}
	a.Items = analyzeItems(variants, items, final, answers, codes)
	a.Objectives = AnalyzeObjectives(studentOutcomes(items, final, answers), refs, a.KKTP.Value)
	a.Follow = a.Objectives.BuildFollowList()
	return a, nil
}

// analyzeItems menyusun masukan analisis butir: soal diurutkan menurut
// paket pertama yang memuatnya, jawaban dari peserta yang mendapatkannya.
func analyzeItems(variants []Variant, items map[string][]GradeItem, final []ParticipantResult, answers map[string]map[string]answerRec, codes map[string]string) []ItemStat {
	var order []string
	inputs := map[string]*ItemInput{}
	for _, v := range variants {
		for _, it := range items[v.ID] {
			if _, ok := inputs[it.Question.ID]; !ok {
				order = append(order, it.Question.ID)
				inputs[it.Question.ID] = &ItemInput{Item: it, Number: len(order), ObjectiveCode: codes[it.Question.ObjectiveID]}
			}
		}
	}
	for _, r := range final {
		for _, it := range items[r.VariantID] {
			in := inputs[it.Question.ID]
			rec, ok := answers[r.AttemptID][it.Question.ID]
			resp := ItemResponse{Total: *r.ScoreX100, Blank: !ok || blankAnswer(rec.Value)}
			if ok {
				if rec.Points != nil {
					resp.Earned = *rec.Points
				}
				resp.Picked = pickedOptions(it.Question.Kind, rec.Value)
			}
			in.Responses = append(in.Responses, resp)
		}
	}
	out := make([]ItemStat, len(order))
	for i, id := range order {
		out[i] = AnalyzeItem(*inputs[id])
	}
	return out
}

// studentOutcomes menyusun masukan analisis TP dari nilai final.
func studentOutcomes(items map[string][]GradeItem, final []ParticipantResult, answers map[string]map[string]answerRec) []StudentOutcome {
	out := make([]StudentOutcome, 0, len(final))
	for _, r := range final {
		s := StudentOutcome{Key: r.Key(), StudentID: r.StudentID, Name: r.Name, Number: r.Number, ClassName: r.ClassName, ScoreX100: *r.ScoreX100}
		for _, it := range items[r.VariantID] {
			if it.Voided {
				continue
			}
			o := ItemOutcome{QuestionID: it.Question.ID, ObjectiveID: it.Question.ObjectiveID, Points: it.Points}
			if rec, ok := answers[r.AttemptID][it.Question.ID]; ok && rec.Points != nil {
				o.Earned = *rec.Points
			}
			s.Items = append(s.Items, o)
		}
		out = append(out, s)
	}
	return out
}

// objectiveRefs memuat TP yang dipakai soal ujian (termasuk yang sudah
// diarsipkan), urut tingkat dan urutan TP.
func objectiveRefs(ctx context.Context, q database.DBTX, schoolID string, items map[string][]GradeItem) ([]ObjectiveRef, error) {
	seen := map[string]bool{}
	var ids []string
	for _, its := range items {
		for _, it := range its {
			if id := it.Question.ObjectiveID; id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT id::text, code, description FROM learning_objectives WHERE school_id = $1 AND id = ANY($2::uuid[])
		ORDER BY grade_level, sort_order, lower(code)`, schoolID, ids)
	if err != nil {
		return nil, fmt.Errorf("exam: analysis objectives: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ObjectiveRef, error) {
		var o ObjectiveRef
		return o, r.Scan(&o.ID, &o.Code, &o.Description)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: analysis objectives: %w", err)
	}
	return out, nil
}

// --- Detail peserta ---

// AnswerDetail adalah satu soal di detail hasil peserta.
type AnswerDetail struct {
	Item           GradeItem
	Number         int
	View           QuestionView
	Answer         []string // jawaban peserta, kosong = tidak dijawab
	Key            []string
	Earned         *int
	Comment        string
	Override       bool
	OverrideReason string
	OverrideBy     string
	OverrideAt     time.Time
}

// EarnedText menampilkan poin diperoleh, mis. "1,5".
func (d AnswerDetail) EarnedText() string {
	if d.Earned == nil {
		return ""
	}
	return PointsLabel(*d.Earned)
}

// Pending: esai menunggu koreksi.
func (d AnswerDetail) Pending() bool {
	return d.Earned == nil && d.Item.Question.Kind == KindEssay && len(d.Answer) > 0
}

// LoadAttemptDetail memuat peserta attempt attemptID dan setiap soal
// paketnya dengan jawaban, kunci (kunci koreksi bila ada), dan poin.
func LoadAttemptDetail(ctx context.Context, q database.DBTX, e Exam, attemptID string) (ParticipantResult, []AnswerDetail, error) {
	p, err := loadAttemptResult(ctx, q, e, attemptID)
	if err != nil {
		return p, nil, err
	}
	if p.VariantID != "" {
		var label string
		_ = q.QueryRow(ctx, `SELECT label FROM exam_variants WHERE id = $1`, p.VariantID).Scan(&label)
		p.VariantLabel = label
	}
	items, err := LoadGradeItems(ctx, q, e, p.VariantID)
	if err != nil {
		return p, nil, err
	}
	rows, err := q.Query(ctx, `SELECT x.question_id::text, x.value, x.points_x10000, x.essay_comment, x.override_reason, coalesce(u.full_name, ''), x.override_at
		FROM exam_answers x LEFT JOIN users u ON u.id = x.override_by WHERE x.attempt_id = $1`, attemptID)
	if err != nil {
		return p, nil, fmt.Errorf("exam: attempt detail: %w", err)
	}
	type rec struct {
		value   json.RawMessage
		points  *int
		comment string
		reason  *string
		by      string
		at      *time.Time
	}
	recs := map[string]rec{}
	for rows.Next() {
		var id string
		var r rec
		if err := rows.Scan(&id, &r.value, &r.points, &r.comment, &r.reason, &r.by, &r.at); err != nil {
			rows.Close()
			return p, nil, fmt.Errorf("exam: attempt detail: %w", err)
		}
		recs[id] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, nil, fmt.Errorf("exam: attempt detail: %w", err)
	}
	out := make([]AnswerDetail, len(items))
	for i, it := range items {
		qk := it.Question
		qk.Key = it.Key
		pq := ForParticipant(it.Question)
		pq.Points = it.Points
		d := AnswerDetail{Item: it, Number: i + 1, View: NewQuestionView(pq, i+1, staffAssetURL), Key: keyLines(qk)}
		if r, ok := recs[it.Question.ID]; ok {
			d.Answer = answerLines(it.Question, r.value)
			d.Earned, d.Comment = r.points, r.comment
			if r.reason != nil {
				d.Override, d.OverrideReason, d.OverrideBy = true, *r.reason, r.by
				if r.at != nil {
					d.OverrideAt = *r.at
				}
			}
		}
		out[i] = d
	}
	return p, out, nil
}

// answerLines menampilkan jawaban peserta dengan huruf opsi urutan bank
// (bukan urutan acak peserta). nil bila tidak dijawab.
func answerLines(q Question, raw json.RawMessage) []string {
	if blankAnswer(raw) {
		return nil
	}
	letterOf := func(cs []Choice, id string) string {
		for i, c := range cs {
			if c.ID == id {
				return letter(i)
			}
		}
		return "?"
	}
	switch q.Kind {
	case KindMC:
		var v string
		if json.Unmarshal(raw, &v) == nil {
			return []string{letterOf(q.Options.Choices, v)}
		}
	case KindMulti:
		var vs []string
		if json.Unmarshal(raw, &vs) == nil {
			picked := setOf(vs)
			var ls []string
			for i, c := range q.Options.Choices {
				if picked[c.ID] {
					ls = append(ls, letter(i))
				}
			}
			return []string{strings.Join(ls, ", ")}
		}
	case KindTF:
		var v map[string]bool
		if json.Unmarshal(raw, &v) == nil {
			out := make([]string, len(q.Options.Statements))
			for i, s := range q.Options.Statements {
				ans := "—"
				if b, ok := v[s.ID]; ok {
					ans = map[bool]string{true: "Benar", false: "Salah"}[b]
				}
				out[i] = strconv.Itoa(i+1) + ". " + ans
			}
			return out
		}
	case KindMatch:
		var v map[string]string
		if json.Unmarshal(raw, &v) == nil {
			out := make([]string, len(q.Options.Left))
			for i, l := range q.Options.Left {
				ans := "—"
				if r, ok := v[l.ID]; ok && r != "" {
					ans = letterOf(q.Options.Right, r)
				}
				out[i] = strconv.Itoa(i+1) + " → " + ans
			}
			return out
		}
	case KindShort, KindEssay:
		var v string
		if json.Unmarshal(raw, &v) == nil {
			return []string{v}
		}
	}
	return []string{string(raw)}
}

// --- Nilai manual ---

// Error nilai manual.
var (
	ErrManualReason = &FieldError{Fields: map[string]string{"alasan": "Tulis alasan nilai manual (tercatat di audit)."}}
	ErrManualEssay  = &FieldError{Fields: map[string]string{"skor": "Esai dinilai di layar koreksi esai."}}
)

// SetManualPoints memberi poin manual untuk satu soal attempt yang sudah
// dikumpulkan (koreksi khusus, mis. kendala teknis), atau menghapusnya bila
// units nil sehingga soal kembali dinilai otomatis. Poin manual bertahan
// saat nilai ulang. Tercatat di audit (exam.manual_score).
func SetManualPoints(ctx context.Context, db database.DB, sa StaffActor, e Exam, attemptID, questionID string, units *int, reason string, now time.Time) (AttemptGrade, error) {
	reason = strings.TrimSpace(reason)
	if units != nil && reason == "" {
		return AttemptGrade{}, ErrManualReason
	}
	if len([]rune(reason)) > 500 {
		return AttemptGrade{}, &FieldError{Fields: map[string]string{"alasan": "Alasan paling panjang 500 karakter."}}
	}
	var g AttemptGrade
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		p, err := loadAttemptResult(ctx, tx, e, attemptID)
		if err != nil {
			return err
		}
		if p.Status != AttemptSubmitted {
			return ErrNotSubmitted
		}
		items, err := LoadGradeItems(ctx, tx, e, p.VariantID)
		if err != nil {
			return err
		}
		var item *GradeItem
		for i := range items {
			if items[i].Question.ID == questionID {
				item = &items[i]
			}
		}
		if item == nil {
			return ErrNotFound
		}
		if item.Question.Kind == KindEssay {
			return ErrManualEssay
		}
		if units != nil && (*units < 0 || *units > item.Points*PointsUnit) {
			return &FieldError{Fields: map[string]string{"skor": "Poin 0 sampai " + strconv.Itoa(item.Points) + "."}}
		}
		var before *int
		var beforeReason *string
		_ = tx.QueryRow(ctx, `SELECT points_x10000, override_reason FROM exam_answers WHERE attempt_id = $1 AND question_id = $2`,
			attemptID, questionID).Scan(&before, &beforeReason)
		if units != nil {
			_, err = tx.Exec(ctx, `INSERT INTO exam_answers (school_id, attempt_id, question_id, value, client_seq, points_x10000, override_by, override_at, override_reason)
				VALUES ($1, $2, $3, 'null', 0, $4, $5, $6, $7)
				ON CONFLICT (attempt_id, question_id) DO UPDATE SET points_x10000 = excluded.points_x10000, override_by = excluded.override_by,
					override_at = excluded.override_at, override_reason = excluded.override_reason`,
				e.SchoolID, attemptID, questionID, *units, sa.UserID, now, reason)
		} else {
			_, err = tx.Exec(ctx, `UPDATE exam_answers SET override_by = NULL, override_at = NULL, override_reason = NULL
				WHERE attempt_id = $1 AND question_id = $2`, attemptID, questionID)
		}
		if err != nil {
			return fmt.Errorf("exam: manual points: %w", err)
		}
		if g, err = GradeAttempt(ctx, tx, e.SchoolID, attemptID, now); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.manual_score", "exam_attempt", attemptID,
			map[string]any{"question": questionID, "points_x10000": before, "reason": beforeReason, "score_x100": p.ScoreX100},
			map[string]any{"question": questionID, "points_x10000": units, "reason": reason, "score_x100": g.ScoreX100})
	})
	return g, err
}
