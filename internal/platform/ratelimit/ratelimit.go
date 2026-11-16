// Package ratelimit membatasi percobaan (login, cek status, dsb.) per IP dan
// per kunci. Percobaan dicatat di database (tabel rate_limit_attempts) agar
// batasnya berlaku walau aplikasi berjalan di beberapa replika. Dipindah
// dari paket admission (T124) agar dipakai bersama.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"time"

	"akra/internal/platform/database"
)

// Limit adalah batas percobaan dalam satu jendela waktu.
type Limit struct {
	Window time.Duration
	PerIP  int // semua percobaan dari satu IP
	PerKey int // percobaan gagal untuk satu kunci (mis. nomor HP, NIS)
}

// Limiter mencatat dan memeriksa percobaan.
type Limiter struct {
	db     database.DBTX
	limits map[string]Limit
	now    func() time.Time
}

// New mengembalikan Limiter dengan batas per jenis percobaan. Jenis yang
// tidak terdaftar ditolak.
func New(db database.DBTX, limits map[string]Limit) *Limiter {
	return &Limiter{db: db, limits: limits, now: time.Now}
}

// SetClock mengganti jam Limiter (hanya untuk uji).
func (l *Limiter) SetClock(now func() time.Time) { l.now = now }

// KeyHash meng-hash kunci agar nilai aslinya (mis. nomor HP) tidak
// tersimpan.
func KeyHash(kind, key string) []byte {
	if key == "" {
		return nil
	}
	h := sha256.Sum256([]byte("akra-attempt\x00" + kind + "\x00" + key))
	return h[:]
}

// Allow melaporkan apakah percobaan baru boleh dilakukan.
func (l *Limiter) Allow(ctx context.Context, kind, key string, ip netip.Addr) (bool, error) {
	lim, ok := l.limits[kind]
	if !ok {
		return false, fmt.Errorf("ratelimit: unknown attempt kind %q", kind)
	}
	since := l.now().Add(-lim.Window)
	var byIP, byKey int
	err := l.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE $3::inet IS NOT NULL AND ip = $3::inet),
		       count(*) FILTER (WHERE $4::bytea IS NOT NULL AND key_hash = $4::bytea AND NOT success)
		FROM rate_limit_attempts
		WHERE kind = $1 AND attempted_at > $2 AND (ip = $3::inet OR key_hash = $4::bytea)`,
		kind, since, inetOrNil(ip), KeyHash(kind, key)).Scan(&byIP, &byKey)
	if err != nil {
		return false, fmt.Errorf("ratelimit: count attempts: %w", err)
	}
	return byIP < lim.PerIP && byKey < lim.PerKey, nil
}

// Record mencatat satu percobaan, lalu membuang catatan yang sudah lebih
// dari sehari.
func (l *Limiter) Record(ctx context.Context, schoolID, kind, key string, ip netip.Addr, success bool) error {
	if _, err := l.db.Exec(ctx, `INSERT INTO rate_limit_attempts (school_id, kind, key_hash, ip, success, attempted_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		schoolID, kind, KeyHash(kind, key), inetOrNil(ip), success, l.now()); err != nil {
		return fmt.Errorf("ratelimit: record attempt: %w", err)
	}
	if _, err := l.db.Exec(ctx, `DELETE FROM rate_limit_attempts WHERE attempted_at < $1`, l.now().Add(-24*time.Hour)); err != nil {
		return fmt.Errorf("ratelimit: prune attempts: %w", err)
	}
	return nil
}

func inetOrNil(ip netip.Addr) any {
	if !ip.IsValid() {
		return nil
	}
	return ip
}
