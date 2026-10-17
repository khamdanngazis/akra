package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// MaxPageSize adalah jumlah baris maksimal satu halaman List.
const MaxPageSize = 100

// Filter membatasi baris audit yang ditampilkan. Semua kondisi digabung
// dengan AND; field kosong tidak membatasi.
type Filter struct {
	// Actions membatasi aksi; kosong berarti semua aksi.
	Actions []string
	// EntityID mencocokkan entity_id persis (mis. id pembayaran).
	EntityID string
	// From dan To membatasi created_at: From <= created_at < To.
	From, To time.Time
	// BeforeID untuk halaman berikutnya: hanya baris dengan id < BeforeID.
	BeforeID int64
	// Limit jumlah baris (1..MaxPageSize; 0 berarti 50).
	Limit int
}

// Row adalah satu baris audit beserta nama pengguna pelakunya.
type Row struct {
	ID         int64
	UserID     string
	UserName   string // kosong untuk aksi sistem atau pengguna yang sudah dihapus
	Action     string
	EntityType string
	EntityID   string
	Before     map[string]any
	After      map[string]any
	IP         string
	CreatedAt  time.Time
}

// List mengembalikan baris audit sekolah schoolID, terbaru dulu. more true
// jika masih ada baris setelah halaman ini.
func List(ctx context.Context, q database.DBTX, schoolID string, f Filter) (rows []Row, more bool, err error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, MaxPageSize)
	var from, to any
	if !f.From.IsZero() {
		from = f.From
	}
	if !f.To.IsZero() {
		to = f.To
	}
	var actions any
	if len(f.Actions) > 0 {
		actions = f.Actions
	}
	r, err := q.Query(ctx, `
		SELECT a.id, coalesce(a.user_id::text, ''), coalesce(u.full_name, ''), a.action, a.entity_type, a.entity_id,
		       a.before, a.after, coalesce(host(a.ip), ''), a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.school_id = a.school_id AND u.id = a.user_id
		WHERE a.school_id = $1
		  AND ($2::text[] IS NULL OR a.action = ANY($2))
		  AND ($3 = '' OR a.entity_id = $3)
		  AND ($4::timestamptz IS NULL OR a.created_at >= $4)
		  AND ($5::timestamptz IS NULL OR a.created_at < $5)
		  AND ($6 = 0 OR a.id < $6)
		ORDER BY a.id DESC
		LIMIT $7`, schoolID, actions, f.EntityID, from, to, f.BeforeID, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("audit: list: %w", err)
	}
	rows, err = pgx.CollectRows(r, func(row pgx.CollectableRow) (Row, error) {
		var (
			x             Row
			before, after []byte
		)
		if err := row.Scan(&x.ID, &x.UserID, &x.UserName, &x.Action, &x.EntityType, &x.EntityID, &before, &after, &x.IP, &x.CreatedAt); err != nil {
			return x, err
		}
		if err := decodeObject(before, &x.Before); err != nil {
			return x, err
		}
		return x, decodeObject(after, &x.After)
	})
	if err != nil {
		return nil, false, fmt.Errorf("audit: list: %w", err)
	}
	if len(rows) > limit {
		return rows[:limit], true, nil
	}
	return rows, false, nil
}

// decodeObject membaca JSON objek; nilai selain objek disimpan di kunci "value".
func decodeObject(b []byte, dst *map[string]any) error {
	if len(b) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if m, ok := v.(map[string]any); ok {
		*dst = m
		return nil
	}
	*dst = map[string]any{"value": v}
	return nil
}
