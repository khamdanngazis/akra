// Package ops berisi alat keandalan platform (T145): kejadian (alert),
// pemeliharaan terjadwal, halaman status, jendela deploy, dan dasbor
// internal tim AKRA. Data lintas sekolah; tidak pernah tampil di halaman
// sekolah.
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

// Incident adalah satu kejadian (kondisi alert) platform.
type Incident struct {
	ID         int64
	Kind       string
	Detail     string
	OpenedAt   time.Time
	SeenAt     time.Time
	ResolvedAt *time.Time
}

// Title adalah nama kejadian untuk manusia.
func (i Incident) Title() string { return KindTitle(i.Kind) }

// KindTitle mengembalikan nama jenis kejadian.
func KindTitle(kind string) string {
	if r, ok := ruleTitles[kind]; ok {
		return r
	}
	return kind
}

// OpenIncident membuka kejadian kind bila belum ada yang terbuka, atau
// memperbarui seen_at dan detailnya. opened = true hanya untuk pemanggil
// yang benar-benar membukanya (satu replika), yang lalu mengirim alert.
func OpenIncident(ctx context.Context, q database.DBTX, kind, detail string, now time.Time) (Incident, bool, error) {
	detail = trim(detail, 1000)
	var in Incident
	err := q.QueryRow(ctx, `INSERT INTO ops_incidents (kind, detail, opened_at, seen_at) VALUES ($1, $2, $3, $3)
		ON CONFLICT (kind) WHERE resolved_at IS NULL DO NOTHING
		RETURNING id, kind, detail, opened_at, seen_at`, kind, detail, now).Scan(&in.ID, &in.Kind, &in.Detail, &in.OpenedAt, &in.SeenAt)
	if err == nil {
		return in, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return in, false, fmt.Errorf("ops: open incident: %w", err)
	}
	err = q.QueryRow(ctx, `UPDATE ops_incidents SET seen_at = greatest(seen_at, $3), detail = $2
		WHERE kind = $1 AND resolved_at IS NULL RETURNING id, kind, detail, opened_at, seen_at`, kind, detail, now).
		Scan(&in.ID, &in.Kind, &in.Detail, &in.OpenedAt, &in.SeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Baru saja ditutup replika lain; putaran berikutnya membuka lagi.
		return in, false, nil
	}
	if err != nil {
		return in, false, fmt.Errorf("ops: touch incident: %w", err)
	}
	return in, false, nil
}

// MarkNotified mencatat alert kejadian sudah terkirim.
func MarkNotified(ctx context.Context, q database.DBTX, id int64, now time.Time) error {
	if _, err := q.Exec(ctx, `UPDATE ops_incidents SET notified_at = $2 WHERE id = $1`, id, now); err != nil {
		return fmt.Errorf("ops: mark notified: %w", err)
	}
	return nil
}

// ResolveIncident menutup kejadian kind yang kondisinya sudah tidak
// terlihat buruk selama quiet (dari replika mana pun). resolved = true
// hanya untuk satu pemanggil, yang lalu mengirim kabar pulih.
func ResolveIncident(ctx context.Context, q database.DBTX, kind string, quiet time.Duration, now time.Time) (Incident, bool, error) {
	var in Incident
	err := q.QueryRow(ctx, `UPDATE ops_incidents SET resolved_at = $2
		WHERE kind = $1 AND resolved_at IS NULL AND seen_at <= $2::timestamptz - $3::interval
		RETURNING id, kind, detail, opened_at, seen_at, resolved_at`, kind, now, quiet.String()).
		Scan(&in.ID, &in.Kind, &in.Detail, &in.OpenedAt, &in.SeenAt, &in.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return in, false, nil
	}
	if err != nil {
		return in, false, fmt.Errorf("ops: resolve incident: %w", err)
	}
	return in, true, nil
}

// ListIncidents mengembalikan kejadian terbuka dan yang ditutup sejak
// since, terbaru dulu.
func ListIncidents(ctx context.Context, q database.DBTX, since time.Time) ([]Incident, error) {
	rows, err := q.Query(ctx, `SELECT id, kind, detail, opened_at, seen_at, resolved_at FROM ops_incidents
		WHERE resolved_at IS NULL OR resolved_at > $1 ORDER BY resolved_at IS NULL DESC, opened_at DESC LIMIT 50`, since)
	if err != nil {
		return nil, fmt.Errorf("ops: list incidents: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Incident, error) {
		var i Incident
		return i, r.Scan(&i.ID, &i.Kind, &i.Detail, &i.OpenedAt, &i.SeenAt, &i.ResolvedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("ops: list incidents: %w", err)
	}
	return out, nil
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
