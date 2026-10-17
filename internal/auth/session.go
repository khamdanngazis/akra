package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
)

// Pengaturan sesi.
const (
	SessionCookieName  = "akra_session"
	SessionLifetime    = 12 * time.Hour
	SessionIdleTimeout = 2 * time.Hour
	// SessionCleanupInterval adalah jeda antar-pembersihan sesi kedaluwarsa
	// oleh pgxstore.
	SessionCleanupInterval = 5 * time.Minute
)

// Kunci nilai di sesi. Sesi hanya berisi identitas, tidak pernah password,
// hash, atau data sensitif lain.
const (
	sessionKeyUserID   = "user_id"
	sessionKeySchoolID = "school_id"
	sessionKeyRoles    = "roles"
	// Nama untuk tampilan. Bisa usang jika diubah setelah login; diperbarui
	// saat login berikutnya.
	sessionKeyFullName   = "full_name"
	sessionKeySchoolName = "school_name"
	sessionKeyFlash      = "flash"
)

// NewSessionManager mengembalikan scs.SessionManager dengan store yang
// diberikan. secure mengatur atribut Secure pada cookie; isi true di luar
// development.
func NewSessionManager(store scs.Store, secure bool, logger *slog.Logger) *scs.SessionManager {
	sm := scs.New()
	sm.Store = store
	sm.Lifetime = SessionLifetime
	sm.IdleTimeout = SessionIdleTimeout
	sm.Cookie.Name = SessionCookieName
	sm.Cookie.Path = "/"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.ErrorFunc = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("session store error", "error", err, "path", r.URL.Path)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
	return sm
}

// startSession mengganti token sesi (mencegah session fixation) lalu
// menyimpan identitas u ke sesi.
func startSession(ctx context.Context, sm *scs.SessionManager, u User) error {
	if err := sm.RenewToken(ctx); err != nil {
		return err
	}
	sm.Put(ctx, sessionKeyUserID, u.ID)
	sm.Put(ctx, sessionKeySchoolID, u.SchoolID)
	sm.Put(ctx, sessionKeyRoles, u.Roles)
	sm.Put(ctx, sessionKeyFullName, u.FullName)
	sm.Put(ctx, sessionKeySchoolName, u.SchoolName)
	return nil
}

// SetFlash menyimpan pesan satu kali tampil untuk request berikutnya.
func SetFlash(ctx context.Context, sm *scs.SessionManager, msg string) {
	sm.Put(ctx, sessionKeyFlash, msg)
}

// PopFlash mengambil lalu menghapus pesan flash, atau "" jika tidak ada.
func PopFlash(ctx context.Context, sm *scs.SessionManager) string {
	return sm.PopString(ctx, sessionKeyFlash)
}

// ScsSessionRevoker mengimplementasikan SessionRevoker dengan scs. Store
// harus mendukung iterasi (pgxstore dan memstore mendukung). Iterasi membaca
// semua sesi aktif; cukup untuk skala MVP.
type ScsSessionRevoker struct {
	sm *scs.SessionManager
}

// NewSessionRevoker mengembalikan ScsSessionRevoker untuk sm.
func NewSessionRevoker(sm *scs.SessionManager) *ScsSessionRevoker {
	return &ScsSessionRevoker{sm: sm}
}

// RevokeUserSessions menghapus semua sesi yang berisi user_id = userID.
func (r *ScsSessionRevoker) RevokeUserSessions(ctx context.Context, userID string) error {
	return r.sm.Iterate(ctx, func(ctx context.Context) error {
		if r.sm.GetString(ctx, sessionKeyUserID) != userID {
			return nil
		}
		return r.sm.Destroy(ctx)
	})
}
