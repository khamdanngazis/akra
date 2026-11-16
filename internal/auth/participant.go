package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"akra/internal/platform/clock"
)

// Jenis peserta ujian (T124). Peserta bukan pengguna staf: tidak ada baris
// users dan tidak bisa membuka route staf. Sesinya terpisah (cookie dan
// SessionManager sendiri).
const (
	// RoleSiswa: siswa, sesi terikat student_id.
	RoleSiswa = "siswa"
	// RolePendaftar: calon siswa (tes seleksi), sesi terikat applicant_id.
	RolePendaftar = "pendaftar"
)

// Pengaturan sesi peserta.
const (
	ParticipantCookieName = "akra_peserta"
	// ParticipantCookiePath membatasi cookie peserta ke route /u/...,
	// sehingga tidak pernah terkirim ke route staf.
	ParticipantCookiePath = "/u"
	// ParticipantMaxLifetime adalah batas atas sesi peserta; masa berlaku
	// sebenarnya diatur per sesi (akhir sesi ujian + 30 menit).
	ParticipantMaxLifetime = 12 * time.Hour
	// ParticipantGrace adalah tambahan masa sesi setelah sesi ujian
	// berakhir.
	ParticipantGrace = 30 * time.Minute
)

// Kunci sesi peserta.
const (
	pKeyKind         = "p_kind"
	pKeySchoolID     = "p_school_id"
	pKeySchoolCode   = "p_school_code"
	pKeySchoolName   = "p_school_name"
	pKeyTimezone     = "p_timezone"
	pKeyStudentID    = "p_student_id"
	pKeyApplicantID  = "p_applicant_id"
	pKeyCredentialID = "p_credential_id"
	pKeyName         = "p_name"
	pKeyExpiresAt    = "p_expires_at"
	pKeyReadOnly     = "p_read_only"
)

// Participant adalah identitas peserta ujian yang sedang masuk. school_id
// dan student_id/applicant_id selalu diambil dari sini, tidak pernah dari
// URL atau form.
type Participant struct {
	Kind         string // RoleSiswa atau RolePendaftar
	SchoolID     string
	SchoolCode   string
	SchoolName   string
	Timezone     string
	StudentID    string // terisi untuk RoleSiswa
	ApplicantID  string // terisi untuk RolePendaftar
	CredentialID string // kode ujian yang dipakai masuk
	Name         string
	ExpiresAt    time.Time
	// ReadOnly: masuk setelah sesi berakhir, hanya untuk melihat hasil.
	ReadOnly bool
}

// Location mengembalikan zona waktu sekolah peserta.
func (p Participant) Location() *time.Location { return clock.Location(p.Timezone) }

// NewParticipantSessionManager mengembalikan SessionManager untuk peserta,
// terpisah dari sesi staf: nama dan path cookie berbeda, sehingga staf yang
// meminjamkan laptop tidak tertukar sesinya.
func NewParticipantSessionManager(store scs.Store, secure bool, logger *slog.Logger) *scs.SessionManager {
	sm := scs.New()
	sm.Store = store
	sm.Lifetime = ParticipantMaxLifetime
	sm.Cookie.Name = ParticipantCookieName
	sm.Cookie.Path = ParticipantCookiePath
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.ErrorFunc = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("participant session store error", "error", err, "path", r.URL.Path)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
	return sm
}

// StartParticipantSession mengganti token sesi (mencegah session fixation),
// menyimpan p, dan mengatur masa berlaku sesi sampai p.ExpiresAt.
func StartParticipantSession(ctx context.Context, sm *scs.SessionManager, p Participant) error {
	if err := sm.RenewToken(ctx); err != nil {
		return err
	}
	sm.Put(ctx, pKeyKind, p.Kind)
	sm.Put(ctx, pKeySchoolID, p.SchoolID)
	sm.Put(ctx, pKeySchoolCode, p.SchoolCode)
	sm.Put(ctx, pKeySchoolName, p.SchoolName)
	sm.Put(ctx, pKeyTimezone, p.Timezone)
	sm.Put(ctx, pKeyStudentID, p.StudentID)
	sm.Put(ctx, pKeyApplicantID, p.ApplicantID)
	sm.Put(ctx, pKeyCredentialID, p.CredentialID)
	sm.Put(ctx, pKeyName, p.Name)
	sm.Put(ctx, pKeyExpiresAt, p.ExpiresAt.Unix())
	sm.Put(ctx, pKeyReadOnly, p.ReadOnly)
	sm.SetDeadline(ctx, p.ExpiresAt)
	return nil
}

// participantFromSession membaca peserta dari sesi. Sesi tidak lengkap atau
// lewat masa berlaku dianggap tidak masuk.
func participantFromSession(ctx context.Context, sm *scs.SessionManager, now time.Time) (Participant, bool) {
	p := Participant{
		Kind:         sm.GetString(ctx, pKeyKind),
		SchoolID:     sm.GetString(ctx, pKeySchoolID),
		SchoolCode:   sm.GetString(ctx, pKeySchoolCode),
		SchoolName:   sm.GetString(ctx, pKeySchoolName),
		Timezone:     sm.GetString(ctx, pKeyTimezone),
		StudentID:    sm.GetString(ctx, pKeyStudentID),
		ApplicantID:  sm.GetString(ctx, pKeyApplicantID),
		CredentialID: sm.GetString(ctx, pKeyCredentialID),
		Name:         sm.GetString(ctx, pKeyName),
		ExpiresAt:    time.Unix(sm.GetInt64(ctx, pKeyExpiresAt), 0),
		ReadOnly:     sm.GetBool(ctx, pKeyReadOnly),
	}
	switch {
	case p.SchoolID == "" || p.CredentialID == "":
		return Participant{}, false
	case p.Kind == RoleSiswa && p.StudentID != "" && p.ApplicantID == "":
	case p.Kind == RolePendaftar && p.ApplicantID != "" && p.StudentID == "":
	default:
		return Participant{}, false
	}
	if !now.Before(p.ExpiresAt) {
		return Participant{}, false
	}
	return p, true
}

type participantKey struct{}

// ContextWithParticipant menyimpan p ke ctx seperti RequireParticipant.
// Hanya untuk uji di paket lain.
func ContextWithParticipant(ctx context.Context, p Participant) context.Context {
	return context.WithValue(ctx, participantKey{}, p)
}

// CurrentParticipant mengembalikan peserta yang disimpan RequireParticipant.
func CurrentParticipant(ctx context.Context) (Participant, bool) {
	p, ok := ctx.Value(participantKey{}).(Participant)
	return p, ok
}

// RequireParticipant mengembalikan middleware untuk route peserta (/u/...).
// Tanpa sesi peserta yang sah, request dialihkan ke loginPath(r); sesi
// kedaluwarsa dihapus. Harus dipasang di dalam sm.LoadAndSave milik sesi
// peserta.
func RequireParticipant(sm *scs.SessionManager, loginPath func(*http.Request, string) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			code := sm.GetString(ctx, pKeySchoolCode)
			p, ok := participantFromSession(ctx, sm, time.Now())
			if !ok {
				if sm.Exists(ctx, pKeySchoolID) {
					_ = sm.Destroy(ctx)
				}
				http.Redirect(w, r, loginPath(r, code), http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r.WithContext(ContextWithParticipant(ctx, p)))
		})
	}
}

// ParticipantSchoolCode mengembalikan kode sekolah di sesi peserta (untuk
// tujuan keluar), atau "" bila tidak ada sesi.
func ParticipantSchoolCode(ctx context.Context, sm *scs.SessionManager) string {
	return sm.GetString(ctx, pKeySchoolCode)
}
