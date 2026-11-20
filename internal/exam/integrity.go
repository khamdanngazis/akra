package exam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Integritas ujian (T141). Browser tidak bisa mencegah kecurangan secara
// mutlak; yang dilakukan: mempersulit (acak, varian, token, satu
// perangkat), memberi tanda (bunyi dan layar merah di perangkat), mencatat
// kejadian di exam_attempt_events, dan memberi pengawas alat bertindak
// (buka kunci, izinkan pindah perangkat; tombolnya di layar pengawas T143).
// Tidak ada diskualifikasi otomatis.

// Jenis kejadian.
const (
	EventLeave       = "keluar"           // keluar halaman/aplikasi, dengan lama
	EventFullscreen  = "layar_penuh"      // keluar dari layar penuh
	EventReload      = "muat_ulang"       // halaman dimuat ulang atau dibuka lagi
	EventCopy        = "salin"            // salin/potong teks
	EventPaste       = "tempel"           // tempel teks
	EventOffline     = "offline"          // offline lebih dari 1 menit, dengan lama
	EventOtherDevice = "perangkat_lain"   // percobaan masuk dari perangkat lain
	EventDeviceMove  = "pindah_perangkat" // pindah perangkat diizinkan pengawas
	EventLocked      = "terkunci"         // dikunci sementara (aturan kunci)
	EventUnlocked    = "dibuka"           // kunci dibuka pengawas
)

// clientEventKinds adalah kejadian yang boleh dikirim perangkat.
var clientEventKinds = map[string]bool{EventLeave: true, EventFullscreen: true, EventReload: true, EventCopy: true,
	EventPaste: true, EventOffline: true}

// EventLabel mengembalikan nama kejadian untuk layar pengawas dan hasil.
func EventLabel(kind string) string {
	switch kind {
	case EventLeave:
		return "Keluar halaman"
	case EventFullscreen:
		return "Keluar layar penuh"
	case EventReload:
		return "Muat ulang halaman"
	case EventCopy:
		return "Menyalin teks"
	case EventPaste:
		return "Menempel teks"
	case EventOffline:
		return "Offline"
	case EventOtherDevice:
		return "Mencoba masuk dari perangkat lain"
	case EventDeviceMove:
		return "Pindah perangkat"
	case EventLocked:
		return "Dikunci sementara"
	case EventUnlocked:
		return "Kunci dibuka pengawas"
	}
	return kind
}

// MaxEventsPerSave membatasi kejadian dalam satu kirim simpan.
const MaxEventsPerSave = 50

// EventInput adalah kejadian dari perangkat, dikirim bersama simpan.
type EventInput struct {
	Seq      int64  `json:"s"` // unik per attempt, dari perangkat
	Kind     string `json:"k"`
	At       int64  `json:"t"` // jam server perkiraan perangkat, ms Unix
	Duration int64  `json:"d,omitempty"`
}

// eventArrays memeriksa kejadian dari perangkat dan menyusunnya menjadi
// kolom untuk unnest.
func eventArrays(events []EventInput) (seqs []int64, kinds []string, ats, durs []int64, maxSeq int64, err error) {
	if len(events) > MaxEventsPerSave {
		return nil, nil, nil, nil, 0, ErrBadAnswer
	}
	seen := map[int64]bool{}
	for _, e := range events {
		if e.Seq <= 0 || !clientEventKinds[e.Kind] || e.At <= 0 || e.Duration < 0 {
			return nil, nil, nil, nil, 0, ErrBadAnswer
		}
		if seen[e.Seq] {
			continue
		}
		seen[e.Seq] = true
		seqs, kinds, ats, durs = append(seqs, e.Seq), append(kinds, e.Kind), append(ats, e.At), append(durs, e.Duration)
		maxSeq = max(maxSeq, e.Seq)
	}
	return seqs, kinds, ats, durs, maxSeq, nil
}

// recordEvent mencatat kejadian dari server (tanpa seq). Kejadian jenis
// sama dalam satu menit terakhir tidak dicatat lagi (mis. login berulang).
func recordEvent(ctx context.Context, q database.DBTX, schoolID, attemptID, kind string, now time.Time) error {
	_, err := q.Exec(ctx, `INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at)
		SELECT $1, $2, $3, $4 WHERE NOT EXISTS (
			SELECT 1 FROM exam_attempt_events WHERE attempt_id = $2 AND kind = $3 AND occurred_at > $4::timestamptz - interval '1 minute')`,
		schoolID, attemptID, kind, now)
	if err != nil {
		return fmt.Errorf("exam: record event: %w", err)
	}
	return nil
}

// recordOtherDeviceLogin mencatat percobaan masuk dari perangkat lain ke
// attempt berjalan milik pemilik kredensial credentialID.
func recordOtherDeviceLogin(ctx context.Context, q database.DBTX, schoolID, credentialID string, now time.Time) error {
	_, err := q.Exec(ctx, `INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at)
		SELECT t.school_id, t.id, 'perangkat_lain', $3
		FROM student_credentials c
		JOIN exam_session_participants p ON p.session_id = c.exam_session_id
			AND p.student_id IS NOT DISTINCT FROM c.student_id AND p.applicant_id IS NOT DISTINCT FROM c.applicant_id
		JOIN exam_attempts t ON t.participant_id = p.id AND t.status = 'berjalan'
		WHERE c.school_id = $1 AND c.id = $2
		  AND NOT EXISTS (SELECT 1 FROM exam_attempt_events x WHERE x.attempt_id = t.id AND x.kind = 'perangkat_lain'
		                  AND x.occurred_at > $3::timestamptz - interval '1 minute')`, schoolID, credentialID, now)
	if err != nil {
		return fmt.Errorf("exam: record other device login: %w", err)
	}
	return nil
}

// claimDevice memindahkan attempt berjalan ke perangkat deviceID bila
// pengawas sudah mengizinkan pindah. Token perangkat lama otomatis tidak
// berlaku karena simpan memeriksa device_id. ok false: tidak diizinkan.
func claimDevice(ctx context.Context, q database.DBTX, a Attempt, deviceID string, now time.Time) (Attempt, bool, error) {
	out, err := scanAttempt(q.QueryRow(ctx, `UPDATE exam_attempts SET device_id = $3, device_move_allowed = false,
			device_moves = device_moves + 1
		WHERE school_id = $1 AND id = $2 AND status = 'berjalan' AND device_move_allowed RETURNING `+attemptCols,
		a.SchoolID, a.ID, deviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, fmt.Errorf("exam: claim device: %w", err)
	}
	if _, err := q.Exec(ctx, `INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at) VALUES ($1, $2, 'pindah_perangkat', $3)`,
		a.SchoolID, a.ID, now); err != nil {
		return a, false, fmt.Errorf("exam: record device move: %w", err)
	}
	return out, true, nil
}

// UnlockAttempt membuka kunci sementara attempt (oleh pengawas). Kunci
// berikutnya terjadi setelah peserta keluar halaman N kali lagi. Hak
// pengawas diperiksa pemanggil (T143).
func UnlockAttempt(ctx context.Context, db database.DB, sa StaffActor, attemptID string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		a, err := attemptForTimer(ctx, tx, sa.SchoolID, attemptID, now)
		if err != nil {
			return err
		}
		if a.LockedAt == nil {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_attempts SET locked_at = NULL, lock_base = leave_count WHERE id = $1`, a.ID); err != nil {
			return fmt.Errorf("exam: unlock attempt: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exam_attempt_events (school_id, attempt_id, kind, occurred_at) VALUES ($1, $2, 'dibuka', $3)`,
			a.SchoolID, a.ID, now); err != nil {
			return fmt.Errorf("exam: record unlock: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.attempt_unlock", "exam_attempt", a.ID, nil, map[string]any{"leave_count": a.LeaveCount})
	})
}

// AttemptEvent adalah satu kejadian attempt.
type AttemptEvent struct {
	Kind       string
	OccurredAt time.Time
	Duration   time.Duration
}

// Label mengembalikan nama kejadian.
func (e AttemptEvent) Label() string { return EventLabel(e.Kind) }

// DurationText menampilkan lama kejadian (mis. lama di luar halaman),
// kosong bila tidak ada.
func (e AttemptEvent) DurationText() string {
	if e.Duration < time.Second {
		return ""
	}
	return awayLabel(e.Duration)
}

// ListAttemptEvents mengembalikan kejadian attempt, terbaru dulu (paling
// banyak limit), untuk layar pengawas (T143) dan hasil (T148).
func ListAttemptEvents(ctx context.Context, q database.DBTX, schoolID, attemptID string, limit int) ([]AttemptEvent, error) {
	rows, err := q.Query(ctx, `SELECT kind, occurred_at, coalesce(duration_ms, 0) FROM exam_attempt_events
		WHERE school_id = $1 AND attempt_id = $2 ORDER BY occurred_at DESC, id DESC LIMIT $3`, schoolID, attemptID, limit)
	if err != nil {
		return nil, fmt.Errorf("exam: list attempt events: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AttemptEvent, error) {
		var e AttemptEvent
		var ms int64
		err := r.Scan(&e.Kind, &e.OccurredAt, &ms)
		e.Duration = time.Duration(ms) * time.Millisecond
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("exam: list attempt events: %w", err)
	}
	return out, nil
}

// Integrity adalah ringkasan integritas satu attempt (T148).
type Integrity struct {
	Leaves      int
	Away        time.Duration
	DeviceMoves int
	Locked      bool
}

// Integrity mengembalikan ringkasan integritas attempt.
func (a Attempt) Integrity() Integrity {
	return Integrity{Leaves: a.LeaveCount, Away: time.Duration(a.AwayMS) * time.Millisecond, DeviceMoves: a.DeviceMoves,
		Locked: a.LockedAt != nil}
}
