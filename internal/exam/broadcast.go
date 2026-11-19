package exam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Pesan koordinator ke layar peserta (T144). Pesan disimpan per sesi
// tujuan dan dikirim ke perangkat lewat balasan simpan/ping berikutnya
// (paling lama ~10 detik), lalu tampil sebagai banner. Setiap pesan juga
// dicatat sebagai catatan sesi sehingga masuk berita acara.

// MaxBroadcastLen: panjang pesan ke peserta.
const MaxBroadcastLen = 300

// BroadcastsPerSave: pesan terbaru yang ikut satu balasan simpan.
const BroadcastsPerSave = 3

// ErrBadMessage: pesan kosong atau terlalu panjang.
var ErrBadMessage = errors.New("exam: invalid broadcast message")

// Broadcast adalah satu pesan untuk layar peserta.
type Broadcast struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	At   int64  `json:"at"` // ms Unix
}

// SendBroadcast mengirim pesan ke sesi-sesi sessionIDs (semuanya harus
// sesi sekolah yang tidak dibatalkan) dan mencatatnya di berita acara
// masing-masing. Mengembalikan jumlah sesi yang dikirimi.
func SendBroadcast(ctx context.Context, db database.DB, sa StaffActor, sessionIDs []string, body string, now time.Time) (int, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > MaxBroadcastLen {
		return 0, ErrBadMessage
	}
	ids := uniqueUUIDs(sessionIDs)
	if len(ids) == 0 {
		return 0, ErrNotFound
	}
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		return sendBroadcastTx(ctx, tx, sa, ids, body, now)
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

func sendBroadcastTx(ctx context.Context, tx pgx.Tx, sa StaffActor, ids []string, body string, now time.Time) error {
	tag, err := tx.Exec(ctx, `
		WITH x AS (
			SELECT id FROM exam_sessions WHERE school_id = $1 AND id = ANY ($2::uuid[]) AND cancelled_at IS NULL),
		n AS (
			INSERT INTO exam_session_notes (school_id, session_id, body, created_by, created_at)
			SELECT $1, x.id, left('Pesan ke peserta: ' || $3, 500), $4, $5 FROM x)
		INSERT INTO exam_broadcasts (school_id, session_id, body, created_by, created_at)
		SELECT $1, x.id, $3, $4, $5 FROM x`, sa.SchoolID, ids, body, sa.UserID, now)
	if err != nil {
		return fmt.Errorf("exam: send broadcast: %w", err)
	}
	if int(tag.RowsAffected()) != len(ids) {
		return ErrNotFound
	}
	return recordAudit(ctx, tx, sa, "exam.broadcast", "exam_session", ids[0], nil,
		map[string]any{"sessions": ids, "body": body})
}

// uniqueUUIDs membuang id kosong, bukan UUID, dan duplikat.
func uniqueUUIDs(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		for _, id := range strings.Fields(s) {
			if database.IsUUID(id) && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}
