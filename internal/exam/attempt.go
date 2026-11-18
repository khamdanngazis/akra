package exam

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/database"
)

// Pengerjaan ujian oleh peserta (T138). Attempt menyimpan varian, urutan
// soal dan opsi hasil acak, perangkat, dan tenggat; jawaban di
// exam_answers.

// Status attempt.
const (
	AttemptRunning   = "berjalan"
	AttemptSubmitted = "dikumpulkan"
)

// Alasan pengumpulan.
const (
	SubmitByParticipant = "peserta"
	SubmitByTime        = "waktu"
)

// Attempt adalah satu kali mengerjakan ujian.
type Attempt struct {
	ID              string
	SchoolID        string
	ExamID          string
	SessionID       string
	ParticipantID   string
	No              int
	VariantID       string
	QuestionOrder   []string
	OptionOrder     map[string]OptionOrder
	Status          string
	DeviceID        string
	StartedAt       time.Time
	DeadlineAt      time.Time
	SubmittedAt     *time.Time
	SubmitReason    string
	CurrentPosition int
}

// Running melaporkan apakah attempt masih bisa dikerjakan.
func (a Attempt) Running() bool { return a.Status == AttemptRunning }

// OptionOrder adalah urutan opsi satu soal hasil acak (ID opsi).
type OptionOrder struct {
	Choices    []string `json:"c,omitempty"`
	Statements []string `json:"s,omitempty"`
	Right      []string `json:"r,omitempty"`
}

// Error pengerjaan. Pesan untuk peserta dibuat handler.
var (
	// ErrNotOpen: sesi belum mulai.
	ErrNotOpen = errors.New("exam: session not open yet")
	// ErrSessionOver: sesi sudah berakhir.
	ErrSessionOver = errors.New("exam: session over")
	// ErrBadToken: token sesi kosong atau salah.
	ErrBadToken = errors.New("exam: invalid session token")
	// ErrNoAttemptsLeft: kesempatan mengerjakan sudah habis.
	ErrNoAttemptsLeft = errors.New("exam: no attempts left")
	// ErrAttemptClosed: attempt sudah dikumpulkan atau lewat tenggat.
	ErrAttemptClosed = errors.New("exam: attempt closed")
)

// SaveGrace adalah tenggang setelah tenggat: jawaban yang terkirim sedikit
// terlambat (jaringan lambat) masih diterima.
const SaveGrace = 2 * time.Minute

// SessionInfo adalah sesi ujian dari sisi peserta: ujian, waktu, aturan,
// dan attempt terakhirnya.
type SessionInfo struct {
	SessionID     string
	ParticipantID string
	ExamID        string
	ExamName      string
	Purpose       Purpose
	Subject       string
	Duration      int // menit, termasuk waktu tambahan peserta
	ExtraMinutes  int
	Questions     int
	MaxAttempts   int
	Location      string
	TokenRequired bool
	StartsAt      time.Time
	EndsAt        time.Time
	Cancelled     bool
	Locked        bool
	Last          *Attempt // attempt terakhir (bila ada)
	Attempts      int
}

// AlertSound melaporkan apakah sesi memakai bunyi tanda keluar halaman:
// bawaan untuk ujian di ruang, mati untuk jarak jauh (T141).
func (s SessionInfo) AlertSound() bool { return s.Location == "di_ruang" }

// Phase adalah keadaan sesi untuk peserta pada waktu now.
func (s SessionInfo) Phase(now time.Time) string {
	switch {
	case s.Last != nil && s.Last.Running() && now.Before(s.Last.DeadlineAt.Add(SaveGrace)):
		return "berjalan"
	case s.Cancelled:
		return "batal"
	case now.Before(s.StartsAt):
		return "belum"
	case !now.Before(s.EndsAt):
		if s.Attempts > 0 {
			return "selesai"
		}
		return "terlewat"
	case s.Attempts >= s.MaxAttempts:
		return "selesai"
	}
	return "buka"
}

func ownerArgs(p auth.Participant) (any, any) {
	return nilIfEmpty(p.StudentID), nilIfEmpty(p.ApplicantID)
}

const sessionInfoCols = `p.id, x.id, e.id, e.name, e.purpose, coalesce(sub.name, ''), e.duration_minutes, p.extra_minutes,
	e.max_attempts, x.location, x.token_required, x.starts_at, x.ends_at, x.cancelled_at IS NOT NULL, e.locked_at IS NOT NULL,
	(SELECT count(*) FROM exam_variant_items vi WHERE vi.variant_id = p.variant_id),
	(SELECT count(*) FROM exam_attempts t WHERE t.participant_id = p.id)`

const sessionInfoFrom = `FROM exam_session_participants p
	JOIN exam_sessions x ON x.id = p.session_id AND x.school_id = p.school_id
	JOIN exams e ON e.id = x.exam_id AND e.school_id = x.school_id
	LEFT JOIN subjects sub ON sub.id = e.subject_id
	WHERE p.school_id = $1 AND p.student_id IS NOT DISTINCT FROM $2 AND p.applicant_id IS NOT DISTINCT FROM $3
	  AND e.archived_at IS NULL AND e.status IN ('terjadwal', 'berlangsung', 'selesai')`

func scanSessionInfo(r pgx.Row) (SessionInfo, error) {
	var s SessionInfo
	var questions, attempts int64
	var purpose string
	err := r.Scan(&s.ParticipantID, &s.SessionID, &s.ExamID, &s.ExamName, &purpose, &s.Subject, &s.Duration, &s.ExtraMinutes,
		&s.MaxAttempts, &s.Location, &s.TokenRequired, &s.StartsAt, &s.EndsAt, &s.Cancelled, &s.Locked, &questions, &attempts)
	s.Purpose, s.Questions, s.Attempts = Purpose(purpose), int(questions), int(attempts)
	s.Duration += s.ExtraMinutes
	return s, err
}

// ParticipantSessions mengembalikan sesi ujian peserta p yang belum lama
// berakhir, urut waktu mulai, beserta attempt terakhir masing-masing.
func ParticipantSessions(ctx context.Context, q database.DBTX, p auth.Participant, now time.Time) ([]SessionInfo, error) {
	sid, aid := ownerArgs(p)
	rows, err := q.Query(ctx, `SELECT `+sessionInfoCols+` `+sessionInfoFrom+` AND x.ends_at > $4 AND x.cancelled_at IS NULL
		ORDER BY x.starts_at, e.name`, p.SchoolID, sid, aid, now.Add(-recentWindow))
	if err != nil {
		return nil, fmt.Errorf("exam: participant sessions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SessionInfo, error) { return scanSessionInfo(r) })
	if err != nil {
		return nil, fmt.Errorf("exam: participant sessions: %w", err)
	}
	for i := range out {
		if out[i].Attempts > 0 {
			a, err := lastAttempt(ctx, q, p.SchoolID, out[i].ParticipantID)
			if err != nil {
				return nil, err
			}
			out[i].Last = &a
		}
	}
	return out, nil
}

// GetSessionInfo mengembalikan sesi sessionID bila p pesertanya.
func GetSessionInfo(ctx context.Context, q database.DBTX, p auth.Participant, sessionID string) (SessionInfo, error) {
	if !database.IsUUID(sessionID) {
		return SessionInfo{}, ErrNotFound
	}
	sid, aid := ownerArgs(p)
	s, err := scanSessionInfo(q.QueryRow(ctx, `SELECT `+sessionInfoCols+` `+sessionInfoFrom+` AND x.id = $4`, p.SchoolID, sid, aid, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionInfo{}, ErrNotFound
	}
	if err != nil {
		return SessionInfo{}, fmt.Errorf("exam: session info: %w", err)
	}
	if s.Attempts > 0 {
		a, err := lastAttempt(ctx, q, p.SchoolID, s.ParticipantID)
		if err != nil {
			return SessionInfo{}, err
		}
		s.Last = &a
	}
	return s, nil
}

const attemptCols = `id, school_id, exam_id, session_id, participant_id, attempt_no, variant_id, question_order::text[], option_order,
	status, device_id::text, started_at, deadline_at, submitted_at, coalesce(submit_reason, ''), current_position`

func scanAttempt(r pgx.Row) (Attempt, error) {
	var a Attempt
	var opts []byte
	err := r.Scan(&a.ID, &a.SchoolID, &a.ExamID, &a.SessionID, &a.ParticipantID, &a.No, &a.VariantID, &a.QuestionOrder, &opts,
		&a.Status, &a.DeviceID, &a.StartedAt, &a.DeadlineAt, &a.SubmittedAt, &a.SubmitReason, &a.CurrentPosition)
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(opts, &a.OptionOrder); err != nil {
		return a, fmt.Errorf("exam: decode option order: %w", err)
	}
	return a, nil
}

func lastAttempt(ctx context.Context, q database.DBTX, schoolID, participantID string) (Attempt, error) {
	a, err := scanAttempt(q.QueryRow(ctx, `SELECT `+attemptCols+` FROM exam_attempts
		WHERE school_id = $1 AND participant_id = $2 ORDER BY attempt_no DESC LIMIT 1`, schoolID, participantID))
	if err != nil {
		return a, fmt.Errorf("exam: last attempt: %w", err)
	}
	return a, nil
}

// GetAttempt mengembalikan attempt id milik peserta p.
func GetAttempt(ctx context.Context, q database.DBTX, p auth.Participant, id string) (Attempt, error) {
	if !database.IsUUID(id) {
		return Attempt{}, ErrNotFound
	}
	sid, aid := ownerArgs(p)
	a, err := scanAttempt(q.QueryRow(ctx, `SELECT `+attemptCols+` FROM exam_attempts t WHERE t.school_id = $1 AND t.id = $4
		AND EXISTS (SELECT 1 FROM exam_session_participants p WHERE p.id = t.participant_id
		            AND p.student_id IS NOT DISTINCT FROM $2 AND p.applicant_id IS NOT DISTINCT FROM $3)`, p.SchoolID, sid, aid, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Attempt{}, ErrNotFound
	}
	if err != nil {
		return Attempt{}, fmt.Errorf("exam: get attempt: %w", err)
	}
	return a, nil
}

// NormalizeToken menyeragamkan token sesi ketikan peserta.
func NormalizeToken(s string) string { return NormalizeCode(s) }

// StartRequest adalah permintaan memulai ujian.
type StartRequest struct {
	SessionID string
	Token     string
	DeviceID  string
	Now       time.Time
}

// StartAttempt memulai attempt baru untuk peserta p di sesi req.SessionID,
// atau mengembalikan attempt yang masih berjalan. Urutan soal dan opsi
// diacak sekali di sini lalu disimpan.
func StartAttempt(ctx context.Context, db database.DB, p auth.Participant, req StartRequest) (Attempt, error) {
	if !CanAttempt(p) {
		return Attempt{}, ErrSessionOver
	}
	if !database.IsUUID(req.DeviceID) {
		return Attempt{}, ErrOtherDevice
	}
	info, err := GetSessionInfo(ctx, db, p, req.SessionID)
	if err != nil {
		return Attempt{}, err
	}
	e, err := GetExam(ctx, db, p.SchoolID, info.ExamID)
	if err != nil {
		return Attempt{}, err
	}
	// Soal dibekukan paling lambat saat peserta pertama mulai.
	if e, err = EnsureLocked(ctx, db, e, req.Now); err != nil {
		return Attempt{}, err
	}
	var out Attempt
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var variantID *string
		var cancelled bool
		var startsAt, endsAt time.Time
		var token *string
		var tokenRequired bool
		if err := tx.QueryRow(ctx, `SELECT p.variant_id, x.cancelled_at IS NOT NULL, x.starts_at, x.ends_at, x.token_required, x.token
			FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
			WHERE p.school_id = $1 AND p.id = $2 FOR UPDATE OF p`, p.SchoolID, info.ParticipantID).
			Scan(&variantID, &cancelled, &startsAt, &endsAt, &tokenRequired, &token); err != nil {
			return fmt.Errorf("exam: lock participant: %w", err)
		}
		running, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM exam_attempts
			WHERE participant_id = $1 AND status = 'berjalan'`, info.ParticipantID))
		switch {
		case err == nil:
			if running.DeviceID != req.DeviceID {
				return ErrOtherDevice
			}
			out = running
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("exam: running attempt: %w", err)
		}
		// Dihitung ulang setelah baris peserta dikunci (dua klik Mulai bersamaan).
		var used int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM exam_attempts WHERE participant_id = $1`, info.ParticipantID).Scan(&used); err != nil {
			return fmt.Errorf("exam: count attempts: %w", err)
		}
		switch {
		case cancelled:
			return ErrNotFound
		case req.Now.Before(startsAt):
			return ErrNotOpen
		case !req.Now.Before(endsAt):
			return ErrSessionOver
		case used >= e.MaxAttempts:
			return ErrNoAttemptsLeft
		case variantID == nil || !e.Locked():
			return ErrNotFound
		}
		if tokenRequired {
			given := NormalizeToken(req.Token)
			if token == nil || given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(*token)) != 1 {
				return ErrBadToken
			}
		}
		paper, err := LoadPaper(ctx, tx, e, *variantID)
		if err != nil {
			return err
		}
		if len(paper) == 0 {
			return ErrNotFound
		}
		seed := rand.Int63()
		order, opts := arrange(paper, e, seed)
		deadline := req.Now.Add(time.Duration(e.DurationMinutes+info.ExtraMinutes) * time.Minute)
		if deadline.After(endsAt) {
			deadline = endsAt
		}
		optJSON, err := json.Marshal(opts)
		if err != nil {
			return err
		}
		out, err = scanAttempt(tx.QueryRow(ctx, `INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, attempt_no,
				variant_id, seed, question_order, option_order, device_id, started_at, deadline_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING `+attemptCols,
			p.SchoolID, e.ID, info.SessionID, info.ParticipantID, used+1, *variantID, seed, order, optJSON, req.DeviceID, req.Now, deadline))
		if err != nil {
			return fmt.Errorf("exam: start attempt: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: p.SchoolID, Action: "exam.attempt_start", EntityType: "exam_attempt", EntityID: out.ID,
			After: map[string]any{"student_id": p.StudentID, "applicant_id": p.ApplicantID, "session_id": info.SessionID, "attempt_no": out.No}})
	})
	return out, err
}

// arrange mengacak urutan soal (aturan ujian) dan opsi tiap soal dengan
// seed. Opsi PG/PG kompleks dan pernyataan B/S diacak, juga kolom kanan
// menjodohkan; kolom kiri tetap.
func arrange(paper []PaperItem, e Exam, seed int64) ([]string, map[string]OptionOrder) {
	items := make([]VariantItem, len(paper))
	for i, it := range paper {
		items[i] = VariantItem{SectionID: it.SectionID, QuestionID: it.Question.ID, StimulusID: it.Question.StimulusID}
		if it.Stimulus != nil {
			items[i].StimulusID = it.Stimulus.ID
		}
	}
	shuffled := ShuffleForParticipant(items, seed, e.ShuffleQuestions, e.ShuffleInStimulus)
	order := make([]string, len(shuffled))
	for i, it := range shuffled {
		order[i] = it.QuestionID
	}
	opts := map[string]OptionOrder{}
	if !e.ShuffleOptions {
		return order, opts
	}
	rng := rand.New(rand.NewSource(seed ^ 0x5eed))
	ids := func(cs []Choice) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = c.ID
		}
		rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}
	for _, it := range paper {
		q := it.Question
		var o OptionOrder
		switch q.Kind {
		case KindMC, KindMulti:
			o.Choices = ids(q.Options.Choices)
		case KindTF:
			o.Statements = ids(q.Options.Statements)
		case KindMatch:
			o.Right = ids(q.Options.Right)
		default:
			continue
		}
		opts[q.ID] = o
	}
	return order, opts
}

// reorder menyusun cs menurut ids; opsi yang tidak disebut ids (mestinya
// tidak ada) ditaruh di akhir.
func reorder(cs []PublicChoice, ids []string) []PublicChoice {
	if len(ids) == 0 {
		return cs
	}
	byID := make(map[string]PublicChoice, len(cs))
	for _, c := range cs {
		byID[c.ID] = c
	}
	out := make([]PublicChoice, 0, len(cs))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
			delete(byID, id)
		}
	}
	for _, c := range cs {
		if _, ok := byID[c.ID]; ok {
			out = append(out, c)
		}
	}
	return out
}

// AnswerState adalah jawaban tersimpan satu soal.
type AnswerState struct {
	Value   json.RawMessage `json:"v"`
	Flagged bool            `json:"f,omitempty"`
	Seq     int64           `json:"s"`
}

// LoadAnswers mengembalikan jawaban tersimpan attempt, per question_id.
func LoadAnswers(ctx context.Context, q database.DBTX, schoolID, attemptID string) (map[string]AnswerState, error) {
	rows, err := q.Query(ctx, `SELECT question_id::text, value, flagged, client_seq FROM exam_answers WHERE school_id = $1 AND attempt_id = $2`,
		schoolID, attemptID)
	if err != nil {
		return nil, fmt.Errorf("exam: load answers: %w", err)
	}
	defer rows.Close()
	out := map[string]AnswerState{}
	for rows.Next() {
		var id string
		var s AnswerState
		if err := rows.Scan(&id, &s.Value, &s.Flagged, &s.Seq); err != nil {
			return nil, fmt.Errorf("exam: load answers: %w", err)
		}
		out[id] = s
	}
	return out, rows.Err()
}

// AnswerChange adalah satu perubahan jawaban dari perangkat.
type AnswerChange struct {
	QuestionID string          `json:"q"`
	Value      json.RawMessage `json:"v"`
	Flagged    bool            `json:"f"`
	Seq        int64           `json:"s"`
}

// Batas isi simpan: jumlah perubahan per request dan ukuran satu jawaban
// (esai panjang masih muat).
const (
	MaxChangesPerSave = 200
	MaxAnswerBytes    = 32 << 10
)

// ErrBadAnswer: isi simpan tidak valid.
var ErrBadAnswer = errors.New("exam: invalid answer payload")

// SaveResult adalah balasan simpan.
type SaveResult struct {
	Deadline time.Time
	// Seq adalah seq tertinggi dalam batch. Semua perubahan sampai seq ini
	// aman di server: tersimpan, atau tergantikan jawaban yang lebih baru.
	Seq int64
}

// SaveAnswers menyimpan perubahan jawaban attempt dalam satu pernyataan
// SQL: attempt harus masih berjalan, dari perangkat yang sama, dan belum
// lewat tenggat + tenggang. Perubahan dengan seq tidak lebih besar dari
// yang tersimpan diabaikan. pos (indeks soal terakhir dibuka) disimpan
// bila >= 0. ErrAttemptClosed bila attempt tidak lagi menerima jawaban.
func SaveAnswers(ctx context.Context, q database.DBTX, schoolID, attemptID, deviceID string, changes []AnswerChange, pos int, now time.Time) (SaveResult, error) {
	if !database.IsUUID(attemptID) || !database.IsUUID(deviceID) || len(changes) > MaxChangesPerSave {
		return SaveResult{}, ErrBadAnswer
	}
	qs := make([]string, 0, len(changes))
	vs := make([]string, 0, len(changes))
	fs := make([]bool, 0, len(changes))
	ss := make([]int64, 0, len(changes))
	seen := map[string]int{}
	for _, c := range changes {
		if !database.IsUUID(c.QuestionID) || c.Seq <= 0 || len(c.Value) > MaxAnswerBytes {
			return SaveResult{}, ErrBadAnswer
		}
		v := strings.TrimSpace(string(c.Value))
		if v == "" {
			v = "null"
		}
		if !json.Valid([]byte(v)) {
			return SaveResult{}, ErrBadAnswer
		}
		// Satu soal dua kali dalam satu batch: ambil seq terbesar.
		if i, ok := seen[c.QuestionID]; ok {
			if c.Seq > ss[i] {
				vs[i], fs[i], ss[i] = v, c.Flagged, c.Seq
			}
			continue
		}
		seen[c.QuestionID] = len(qs)
		qs, vs, fs, ss = append(qs, c.QuestionID), append(vs, v), append(fs, c.Flagged), append(ss, c.Seq)
	}
	var found bool
	var deadline time.Time
	err := q.QueryRow(ctx, `
		WITH a AS (
			SELECT id, school_id, question_order, deadline_at FROM exam_attempts
			WHERE school_id = $1 AND id = $2 AND status = 'berjalan' AND device_id = $3 AND deadline_at + $4::interval > $5),
		ins AS (
			INSERT INTO exam_answers (school_id, attempt_id, question_id, value, flagged, client_seq, saved_at)
			SELECT a.school_id, a.id, u.q, u.v, u.f, u.s, $5
			FROM a, unnest($6::uuid[], $7::jsonb[], $8::bool[], $9::bigint[]) AS u(q, v, f, s)
			WHERE u.q = ANY (a.question_order)
			ON CONFLICT (attempt_id, question_id) DO UPDATE
			SET value = excluded.value, flagged = excluded.flagged, client_seq = excluded.client_seq, saved_at = excluded.saved_at
			WHERE exam_answers.client_seq < excluded.client_seq
			RETURNING 1),
		pos AS (
			UPDATE exam_attempts t SET current_position = least($10::int, cardinality(a.question_order) - 1)
			FROM a WHERE t.id = a.id AND $10::int >= 0 AND t.current_position <> $10::int
			RETURNING 1)
		SELECT EXISTS (SELECT 1 FROM a), coalesce((SELECT deadline_at FROM a), $5)`,
		schoolID, attemptID, deviceID, SaveGrace.String(), now, qs, vs, fs, ss, pos).Scan(&found, &deadline)
	if err != nil {
		return SaveResult{}, fmt.Errorf("exam: save answers: %w", err)
	}
	if !found {
		return SaveResult{}, ErrAttemptClosed
	}
	var maxSeq int64
	for _, x := range ss {
		maxSeq = max(maxSeq, x)
	}
	return SaveResult{Deadline: deadline, Seq: maxSeq}, nil
}

// SubmitAttempt mengumpulkan attempt. Jawaban yang sudah tersimpan
// menjadi jawaban akhir. Mengumpulkan ulang attempt yang sudah dikumpulkan
// tidak mengubah apa pun.
func SubmitAttempt(ctx context.Context, db database.DB, p auth.Participant, a Attempt, reason string, now time.Time) (Attempt, error) {
	if !a.Running() {
		return a, nil
	}
	var out Attempt
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = scanAttempt(tx.QueryRow(ctx, `UPDATE exam_attempts SET status = 'dikumpulkan', submitted_at = $3, submit_reason = $4
			WHERE school_id = $1 AND id = $2 AND status = 'berjalan' RETURNING `+attemptCols, a.SchoolID, a.ID, now, reason))
		if errors.Is(err, pgx.ErrNoRows) {
			out, err = scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM exam_attempts WHERE id = $1`, a.ID))
			return err
		}
		if err != nil {
			return fmt.Errorf("exam: submit attempt: %w", err)
		}
		var answered int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM exam_answers WHERE attempt_id = $1 AND value <> 'null'::jsonb`, a.ID).Scan(&answered); err != nil {
			return fmt.Errorf("exam: count answers: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: a.SchoolID, Action: "exam.attempt_submit", EntityType: "exam_attempt", EntityID: a.ID,
			After: map[string]any{"student_id": p.StudentID, "applicant_id": p.ApplicantID, "reason": reason, "answered": answered,
				"questions": len(a.QuestionOrder)}})
	})
	return out, err
}
