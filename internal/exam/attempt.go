package exam

import (
	"context"
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
	// SubmitBySystem: dikumpulkan job karena lewat tenggat dan perangkat
	// tidak mengumpulkan sendiri (HP mati, browser ditutup; T140).
	SubmitBySystem = "sistem"
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
	// PausedAt: attempt sedang dijeda sejak waktu ini (T140).
	PausedAt *time.Time
	PausedMS int64 // total jeda yang sudah selesai
	// Integritas (T141).
	LeaveCount        int
	AwayMS            int64
	DeviceMoves       int
	DeviceMoveAllowed bool
	LockedAt          *time.Time // dikunci sementara sampai pengawas membuka
	LockBase          int        // leave_count saat kunci terakhir dibuka
}

// LeavesBeforeLock mengembalikan sisa keluar halaman sebelum dikunci
// menurut batas ujian limit (0 = aturan hanya catat).
func (a Attempt) LeavesBeforeLock(limit int) int {
	if limit <= 0 {
		return 0
	}
	return max(0, limit-(a.LeaveCount-a.LockBase))
}

// Running melaporkan apakah attempt masih bisa dikerjakan.
func (a Attempt) Running() bool { return a.Status == AttemptRunning }

// Expired melaporkan apakah attempt berjalan sudah lewat tenggat +
// tenggang sehingga tidak menerima jawaban lagi. Attempt yang dijeda tidak
// pernah kedaluwarsa: waktunya berhenti.
func (a Attempt) Expired(now time.Time) bool {
	return a.Running() && a.PausedAt == nil && !now.Before(a.DeadlineAt.Add(SaveGrace))
}

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
	// ErrSessionPaused: sesi sedang dijeda pengawas.
	ErrSessionPaused = errors.New("exam: session paused")
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
	Duration      int // menit, termasuk waktu tambahan peserta dan sesi
	ExtraMinutes  int // waktu tambahan peserta
	Questions     int
	MaxAttempts   int
	Location      string
	TokenRequired bool
	StartsAt      time.Time
	EndsAt        time.Time
	Cancelled     bool
	Paused        bool   // sesi sedang dijeda
	LatePolicy    string // aturan terlambat masuk (T140)
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
	case s.Last != nil && s.Last.Running() && !s.Last.Expired(now):
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
	e.max_attempts, x.location, x.token_required, x.starts_at, x.ends_at, x.cancelled_at IS NOT NULL, x.paused_at IS NOT NULL,
	x.extra_minutes, x.late_policy, e.locked_at IS NOT NULL,
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
	var sessionExtra int
	err := r.Scan(&s.ParticipantID, &s.SessionID, &s.ExamID, &s.ExamName, &purpose, &s.Subject, &s.Duration, &s.ExtraMinutes,
		&s.MaxAttempts, &s.Location, &s.TokenRequired, &s.StartsAt, &s.EndsAt, &s.Cancelled, &s.Paused, &sessionExtra, &s.LatePolicy, &s.Locked,
		&questions, &attempts)
	s.Purpose, s.Questions, s.Attempts = Purpose(purpose), int(questions), int(attempts)
	s.Duration += s.ExtraMinutes + sessionExtra
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
	status, device_id::text, started_at, deadline_at, submitted_at, coalesce(submit_reason, ''), current_position, paused_at, paused_ms,
	leave_count, away_ms, device_moves, device_move_allowed, locked_at, lock_base`

func scanAttempt(r pgx.Row) (Attempt, error) {
	var a Attempt
	var opts []byte
	err := r.Scan(&a.ID, &a.SchoolID, &a.ExamID, &a.SessionID, &a.ParticipantID, &a.No, &a.VariantID, &a.QuestionOrder, &opts,
		&a.Status, &a.DeviceID, &a.StartedAt, &a.DeadlineAt, &a.SubmittedAt, &a.SubmitReason, &a.CurrentPosition, &a.PausedAt, &a.PausedMS,
		&a.LeaveCount, &a.AwayMS, &a.DeviceMoves, &a.DeviceMoveAllowed, &a.LockedAt, &a.LockBase)
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
		var token, tokenPrev *string
		var tokenRotated *time.Time
		var tokenRequired, paused, closed bool
		var latePolicy string
		var participantExtra, sessionExtra int
		if err := tx.QueryRow(ctx, `SELECT p.variant_id, x.cancelled_at IS NOT NULL, x.starts_at, x.ends_at, x.token_required, x.token,
				x.paused_at IS NOT NULL, x.late_policy, p.extra_minutes, x.extra_minutes, x.token_prev, x.token_rotated_at,
				x.closed_at IS NOT NULL
			FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
			WHERE p.school_id = $1 AND p.id = $2 FOR UPDATE OF p`, p.SchoolID, info.ParticipantID).
			Scan(&variantID, &cancelled, &startsAt, &endsAt, &tokenRequired, &token, &paused, &latePolicy, &participantExtra, &sessionExtra,
				&tokenPrev, &tokenRotated, &closed); err != nil {
			return fmt.Errorf("exam: lock participant: %w", err)
		}
		running, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptCols+` FROM exam_attempts
			WHERE participant_id = $1 AND status = 'berjalan'`, info.ParticipantID))
		switch {
		case err == nil:
			out = running
			if running.DeviceID != req.DeviceID {
				claimed, ok, err := claimDevice(ctx, tx, running, req.DeviceID, req.Now)
				if err != nil {
					return err
				}
				if !ok {
					return ErrOtherDevice
				}
				out = claimed
			}
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
		case !req.Now.Before(endsAt), closed:
			return ErrSessionOver
		case used >= e.MaxAttempts:
			return ErrNoAttemptsLeft
		case variantID == nil || !e.Locked():
			return ErrNotFound
		case paused:
			return ErrSessionPaused
		}
		if tokenRequired && !tokenMatches(NormalizeToken(req.Token), token, tokenPrev, tokenRotated, req.Now) {
			return ErrBadToken
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
		deadline := ComputeDeadline(DeadlineInput{StartedAt: req.Now, SessionStart: startsAt, SessionEnd: endsAt, LatePolicy: latePolicy,
			DurationMinutes: e.DurationMinutes, ParticipantExtra: participantExtra, SessionExtra: sessionExtra})
		if !deadline.After(req.Now) {
			// Aturan potong: keterlambatan menghabiskan seluruh waktu.
			return ErrSessionOver
		}
		optJSON, err := json.Marshal(opts)
		if err != nil {
			return err
		}
		out, err = scanAttempt(tx.QueryRow(ctx, `INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, attempt_no,
				variant_id, seed, question_order, option_order, device_id, started_at, deadline_at, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $11) RETURNING `+attemptCols,
			p.SchoolID, e.ID, info.SessionID, info.ParticipantID, used+1, *variantID, seed, order, optJSON, req.DeviceID, req.Now, deadline))
		if err != nil {
			return fmt.Errorf("exam: start attempt: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: p.SchoolID, Action: "exam.attempt_start", EntityType: "exam_attempt", EntityID: out.ID,
			After: map[string]any{"student_id": p.StudentID, "applicant_id": p.ApplicantID, "session_id": info.SessionID, "attempt_no": out.No}})
	})
	if errors.Is(err, ErrOtherDevice) && out.ID != "" {
		// Dicatat di luar transaksi yang dibatalkan.
		if rerr := recordEvent(ctx, db, out.SchoolID, out.ID, EventOtherDevice, req.Now); rerr != nil {
			return Attempt{}, rerr
		}
		return Attempt{}, err
	}
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
	// PausedAt: attempt sedang dijeda (layar peserta dikunci).
	PausedAt *time.Time
	// LockedAt: attempt dikunci sementara (aturan kunci, T141).
	LockedAt *time.Time
	// Seq adalah seq tertinggi dalam batch. Semua perubahan sampai seq ini
	// aman di server: tersimpan, atau tergantikan jawaban yang lebih baru.
	Seq int64
	// EventSeq adalah seq kejadian tertinggi yang sudah tercatat.
	EventSeq int64
	// Messages: pesan koordinator untuk sesi attempt dengan id lebih besar
	// dari SaveRequest.MsgAfter (paling banyak BroadcastsPerSave, urut id).
	Messages []Broadcast
}

// SaveRequest adalah isi satu kirim simpan dari perangkat.
type SaveRequest struct {
	Changes []AnswerChange
	Events  []EventInput
	Pos     int // indeks soal terakhir dibuka; < 0 = tidak berubah
	// MsgAfter: id pesan koordinator terakhir yang sudah diterima perangkat.
	MsgAfter int64
}

// SaveAnswers menyimpan perubahan jawaban dan kejadian integritas attempt
// dalam satu pernyataan SQL. Attempt harus masih berjalan, dari perangkat
// yang sama, dan belum lewat tenggat + tenggang (attempt yang dijeda tetap
// menerima antrian). Perubahan dengan seq tidak lebih besar dari yang
// tersimpan diabaikan; kejadian yang sudah tercatat (seq sama) diabaikan.
// Keluar halaman menambah hitungan dan, bila aturan ujian kunci, mengunci
// attempt setelah N kali. ErrAttemptClosed bila attempt tidak lagi
// menerima jawaban.
func SaveAnswers(ctx context.Context, q database.DBTX, schoolID, attemptID, deviceID string, req SaveRequest, now time.Time) (SaveResult, error) {
	changes := req.Changes
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
	es, ek, et, ed, maxEvent, err := eventArrays(req.Events)
	if err != nil {
		return SaveResult{}, err
	}
	var found bool
	var deadline time.Time
	var pausedAt, lockedAt *time.Time
	var msgs []Broadcast
	// Catatan: pernyataan data-modifying dalam WITH tidak melihat perubahan
	// satu sama lain pada tabel, sehingga posisi, hitungan keluar, dan
	// kunci diubah dalam satu UPDATE (upd).
	err = q.QueryRow(ctx, `
		WITH a AS (
			SELECT t.id, t.school_id, t.session_id, t.question_order, t.deadline_at, t.paused_at, t.started_at, t.locked_at, e.integrity_limit
			FROM exam_attempts t JOIN exams e ON e.id = t.exam_id
			WHERE t.school_id = $1 AND t.id = $2 AND t.status = 'berjalan' AND t.device_id = $3
			  AND (t.paused_at IS NOT NULL OR t.deadline_at + $4::interval > $5)),
		ins AS (
			INSERT INTO exam_answers (school_id, attempt_id, question_id, value, flagged, client_seq, saved_at)
			SELECT a.school_id, a.id, u.q, u.v, u.f, u.s, $5
			FROM a, unnest($6::uuid[], $7::jsonb[], $8::bool[], $9::bigint[]) AS u(q, v, f, s)
			WHERE u.q = ANY (a.question_order)
			ON CONFLICT (attempt_id, question_id) DO UPDATE
			SET value = excluded.value, flagged = excluded.flagged, client_seq = excluded.client_seq, saved_at = excluded.saved_at
			WHERE exam_answers.client_seq < excluded.client_seq
			RETURNING 1),
		ev AS (
			INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at, duration_ms, client_seq)
			SELECT a.school_id, a.id, v.k, least(greatest(to_timestamp(v.t / 1000.0), a.started_at), $5),
				CASE WHEN v.d > 0 THEN least(v.d, 86400000) END, v.s
			FROM a, unnest($11::bigint[], $12::text[], $13::bigint[], $14::bigint[]) AS v(s, k, t, d)
			ON CONFLICT (attempt_id, client_seq) WHERE client_seq IS NOT NULL DO NOTHING
			RETURNING kind, coalesce(duration_ms, 0) AS d),
		lv AS (
			SELECT count(*) FILTER (WHERE kind = 'keluar') AS n, coalesce(sum(d) FILTER (WHERE kind = 'keluar'), 0) AS ms FROM ev),
		upd AS (
			UPDATE exam_attempts t SET
				current_position = CASE WHEN $10::int >= 0 THEN least($10::int, cardinality(a.question_order) - 1) ELSE t.current_position END,
				leave_count = t.leave_count + lv.n,
				away_ms = t.away_ms + lv.ms,
				last_seen_at = $5,
				locked_at = CASE WHEN t.locked_at IS NULL AND lv.n > 0 AND a.integrity_limit IS NOT NULL
				                      AND t.leave_count + lv.n - t.lock_base >= a.integrity_limit THEN $5 ELSE t.locked_at END
			FROM a, lv
			WHERE t.id = a.id
			  AND (lv.n > 0 OR ($10::int >= 0 AND t.current_position <> least($10::int, cardinality(a.question_order) - 1))
			       OR t.last_seen_at < $5::timestamptz - interval '30 seconds')
			RETURNING t.locked_at, t.locked_at IS NOT NULL AND a.locked_at IS NULL AS newly),
		lk AS (
			INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at)
			SELECT a.school_id, a.id, 'terkunci', $5 FROM a, upd WHERE upd.newly
			RETURNING 1)
		SELECT EXISTS (SELECT 1 FROM a), coalesce((SELECT deadline_at FROM a), $5), (SELECT paused_at FROM a),
			coalesce((SELECT locked_at FROM upd), (SELECT locked_at FROM a)), (SELECT count(*) FROM lk),
			(SELECT coalesce(json_agg(json_build_object('id', m.id, 'body', m.body, 'at', (extract(epoch FROM m.created_at) * 1000)::bigint)
			                          ORDER BY m.id), '[]')
			 FROM (SELECT b.id, b.body, b.created_at FROM exam_broadcasts b, a
			       WHERE b.session_id = a.session_id AND b.id > $15 ORDER BY b.id DESC LIMIT $16) m)`,
		schoolID, attemptID, deviceID, SaveGrace.String(), now, qs, vs, fs, ss, req.Pos, es, ek, et, ed, req.MsgAfter, BroadcastsPerSave).
		Scan(&found, &deadline, &pausedAt, &lockedAt, new(int64), &msgs)
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
	return SaveResult{Deadline: deadline, PausedAt: pausedAt, LockedAt: lockedAt, Seq: maxSeq, EventSeq: maxEvent, Messages: msgs}, nil
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
		out, err = scanAttempt(tx.QueryRow(ctx, `UPDATE exam_attempts SET status = 'dikumpulkan', submitted_at = $3, submit_reason = $4,
				paused_at = NULL
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
