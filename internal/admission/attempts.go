package admission

import (
	"time"

	"akra/internal/platform/database"
	"akra/internal/platform/ratelimit"
)

// Jenis percobaan di halaman publik.
const (
	attemptDraft  = "draft"  // membuat draf baru
	attemptResume = "resume" // melanjutkan draf dengan HP + kode
	attemptStatus = "status" // cek status dengan nomor + tanggal lahir (T95)
)

var limits = map[string]ratelimit.Limit{
	// Draf: cukup longgar untuk satu sekolah yang memakai satu IP (wifi
	// sekolah saat pendaftaran langsung), tapi menahan bot.
	attemptDraft:  {Window: time.Hour, PerIP: 30, PerKey: 5},
	attemptResume: {Window: 15 * time.Minute, PerIP: 20, PerKey: 10},
	attemptStatus: {Window: 15 * time.Minute, PerIP: 10, PerKey: 10},
}

// Limiter membatasi percobaan di halaman publik pendaftaran. Implementasinya
// di paket platform/ratelimit (T124).
type Limiter = ratelimit.Limiter

// NewLimiter mengembalikan Limiter untuk jenis percobaan pendaftaran.
func NewLimiter(db database.DBTX) *Limiter {
	return ratelimit.New(db, limits)
}
