package admission

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"time"

	"akra/internal/platform/database"
)

// Jenis percobaan di halaman publik.
const (
	attemptDraft  = "draft"  // membuat draf baru
	attemptResume = "resume" // melanjutkan draf dengan HP + kode
	attemptStatus = "status" // cek status dengan nomor + tanggal lahir (T95)
)

// limit adalah batas percobaan dalam satu jendela waktu.
type limit struct {
	window time.Duration
	perIP  int // semua percobaan dari satu IP
	perKey int // percobaan gagal untuk satu kunci (mis. nomor HP)
}

var limits = map[string]limit{
	// Draf: cukup longgar untuk satu sekolah yang memakai satu IP (wifi
	// sekolah saat pendaftaran langsung), tapi menahan bot.
	attemptDraft:  {window: time.Hour, perIP: 30, perKey: 5},
	attemptResume: {window: 15 * time.Minute, perIP: 20, perKey: 10},
	attemptStatus: {window: 15 * time.Minute, perIP: 10, perKey: 10},
}

// Limiter mencatat percobaan di database agar batasnya berlaku walau
// aplikasi berjalan di beberapa instance.
type Limiter struct {
	db  database.DBTX
	now func() time.Time
}

// NewLimiter mengembalikan Limiter.
func NewLimiter(db database.DBTX) *Limiter {
	return &Limiter{db: db, now: time.Now}
}

// keyHash meng-hash kunci (mis. nomor HP) agar nilai aslinya tidak
// tersimpan.
func keyHash(kind, key string) []byte {
	if key == "" {
		return nil
	}
	h := sha256.Sum256([]byte("akra-attempt\x00" + kind + "\x00" + key))
	return h[:]
}

// Allow melaporkan apakah percobaan baru boleh dilakukan.
func (l *Limiter) Allow(ctx context.Context, kind, key string, ip netip.Addr) (bool, error) {
	lim, ok := limits[kind]
	if !ok {
		return false, fmt.Errorf("admission: unknown attempt kind %q", kind)
	}
	since := l.now().Add(-lim.window)
	var byIP, byKey int
	err := l.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE $3::inet IS NOT NULL AND ip = $3::inet),
		       count(*) FILTER (WHERE $4::bytea IS NOT NULL AND key_hash = $4::bytea AND NOT success)
		FROM admission_attempts
		WHERE kind = $1 AND attempted_at > $2 AND (ip = $3::inet OR key_hash = $4::bytea)`,
		kind, since, inetOrNil(ip), keyHash(kind, key)).Scan(&byIP, &byKey)
	if err != nil {
		return false, fmt.Errorf("admission: count attempts: %w", err)
	}
	return byIP < lim.perIP && byKey < lim.perKey, nil
}

// Record mencatat satu percobaan, lalu membuang catatan yang sudah lebih
// dari sehari.
func (l *Limiter) Record(ctx context.Context, schoolID, kind, key string, ip netip.Addr, success bool) error {
	if _, err := l.db.Exec(ctx, `INSERT INTO admission_attempts (school_id, kind, key_hash, ip, success, attempted_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		schoolID, kind, keyHash(kind, key), inetOrNil(ip), success, l.now()); err != nil {
		return fmt.Errorf("admission: record attempt: %w", err)
	}
	if _, err := l.db.Exec(ctx, `DELETE FROM admission_attempts WHERE attempted_at < $1`, l.now().Add(-24*time.Hour)); err != nil {
		return fmt.Errorf("admission: prune attempts: %w", err)
	}
	return nil
}

func inetOrNil(ip netip.Addr) any {
	if !ip.IsValid() {
		return nil
	}
	return ip
}
