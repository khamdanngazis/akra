package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// MaintenanceNotice: pemeliharaan tampil di halaman status sejak sekian
// lama sebelum mulai.
const MaintenanceNotice = 24 * time.Hour

// Maintenance adalah pemeliharaan terjadwal.
type Maintenance struct {
	ID       int64
	StartsAt time.Time
	EndsAt   time.Time
	Message  string
}

// Active melaporkan apakah pemeliharaan sedang berlangsung.
func (m Maintenance) Active(now time.Time) bool {
	return !now.Before(m.StartsAt) && now.Before(m.EndsAt)
}

// ErrBadMaintenance: jadwal atau pesan pemeliharaan tidak valid.
var ErrBadMaintenance = errors.New("ops: invalid maintenance")

// AddMaintenance menjadwalkan pemeliharaan.
func AddMaintenance(ctx context.Context, q database.DBTX, start, end time.Time, message string) (int64, error) {
	message = strings.TrimSpace(message)
	if !end.After(start) || message == "" || len([]rune(message)) > 500 {
		return 0, ErrBadMaintenance
	}
	var id int64
	if err := q.QueryRow(ctx, `INSERT INTO ops_maintenance (starts_at, ends_at, message) VALUES ($1, $2, $3) RETURNING id`,
		start, end, message).Scan(&id); err != nil {
		return 0, fmt.Errorf("ops: add maintenance: %w", err)
	}
	return id, nil
}

// CancelMaintenance membatalkan pemeliharaan id.
func CancelMaintenance(ctx context.Context, q database.DBTX, id int64, now time.Time) error {
	tag, err := q.Exec(ctx, `UPDATE ops_maintenance SET cancelled_at = $2 WHERE id = $1 AND cancelled_at IS NULL`, id, now)
	if err != nil {
		return fmt.Errorf("ops: cancel maintenance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBadMaintenance
	}
	return nil
}

// UpcomingMaintenance mengembalikan pemeliharaan yang belum selesai dan
// mulai sebelum now+within, urut waktu mulai.
func UpcomingMaintenance(ctx context.Context, q database.DBTX, now time.Time, within time.Duration) ([]Maintenance, error) {
	rows, err := q.Query(ctx, `SELECT id, starts_at, ends_at, message FROM ops_maintenance
		WHERE cancelled_at IS NULL AND ends_at > $1 AND starts_at <= $1::timestamptz + $2::interval ORDER BY starts_at`, now, within.String())
	if err != nil {
		return nil, fmt.Errorf("ops: maintenance: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Maintenance, error) {
		var m Maintenance
		return m, r.Scan(&m.ID, &m.StartsAt, &m.EndsAt, &m.Message)
	})
	if err != nil {
		return nil, fmt.Errorf("ops: maintenance: %w", err)
	}
	return out, nil
}

// Status halaman status.
const (
	StatusNormal      = "normal"
	StatusDisrupted   = "gangguan"
	StatusMaintenance = "pemeliharaan"
)

// Status adalah isi halaman status publik.
type Status struct {
	State       string
	Incidents   []Incident // terbuka
	Maintenance []Maintenance
}

// LoadStatus menyusun status: gangguan bila ada kejadian terbuka,
// pemeliharaan bila sedang berlangsung, selain itu normal. Pemeliharaan
// 24 jam ke depan ikut ditampilkan.
func LoadStatus(ctx context.Context, q database.DBTX, now time.Time) (Status, error) {
	var st Status
	all, err := ListIncidents(ctx, q, now)
	if err != nil {
		return st, err
	}
	for _, i := range all {
		if i.ResolvedAt == nil {
			st.Incidents = append(st.Incidents, i)
		}
	}
	if st.Maintenance, err = UpcomingMaintenance(ctx, q, now, MaintenanceNotice); err != nil {
		return st, err
	}
	st.State = StatusNormal
	for _, m := range st.Maintenance {
		if m.Active(now) {
			st.State = StatusMaintenance
		}
	}
	if len(st.Incidents) > 0 {
		st.State = StatusDisrupted
	}
	return st, nil
}
