package attendance

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

// ActionSettingsUpdate adalah aksi audit perubahan pengaturan absensi.
// Sengaja terpisah dari settings.update (pengaturan keuangan) agar tidak
// masuk audit keuangan.
const ActionSettingsUpdate = "attendance.settings_update"

const (
	settingsPath = "/pengaturan/absensi"
	// subjectsPage adalah halaman tempat pengaturan ini ditampilkan.
	subjectsPage = "/pengaturan/mapel"
)

// PerSubjectEnabled melaporkan apakah sekolah mengaktifkan absensi per mapel.
// Belum diatur berarti nonaktif.
func PerSubjectEnabled(ctx context.Context, q database.DBTX, schoolID string) (bool, error) {
	return settings.Bool(ctx, q, schoolID, settings.AttendancePerSubject, false)
}

// SetPerSubject menyimpan pengaturan absensi per mapel beserta audit dalam
// satu transaksi. Menyimpan nilai yang sama tidak menambah audit. Sesi
// per mapel yang sudah ada tidak dihapus saat pengaturan dimatikan.
func (s *Service) SetPerSubject(ctx context.Context, actor school.Actor, enabled bool) (changed bool, err error) {
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := PerSubjectEnabled(ctx, tx, actor.SchoolID)
		if err != nil {
			return err
		}
		if before == enabled {
			return nil
		}
		value := "false"
		if enabled {
			value = "true"
		}
		if err := settings.Set(ctx, tx, actor.SchoolID, settings.AttendancePerSubject, value); err != nil {
			return err
		}
		changed = true
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionSettingsUpdate, EntityType: "school_settings", EntityID: actor.SchoolID,
			Before: map[string]any{settings.AttendancePerSubject: before}, After: map[string]any{settings.AttendancePerSubject: enabled},
		})
	})
	return changed, err
}

// saveSettings menyimpan isian per_mapel ("aktif" atau "nonaktif") dari
// kartu pengaturan di halaman mata pelajaran.
func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Data tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	var enabled bool
	switch r.PostForm.Get("per_mapel") {
	case "aktif":
		enabled = true
	case "nonaktif":
	default:
		page.BadRequest(h.render, w, r, id, "Data tidak valid", "Pilih aktif atau nonaktif untuk absensi per mapel.")
		return
	}
	changed, err := h.svc.SetPerSubject(r.Context(), actorFrom(r, id), enabled)
	if err != nil {
		h.serverError(w, r, "save attendance settings", err)
		return
	}
	msg := "Pengaturan absensi tidak berubah."
	switch {
	case changed && enabled:
		msg = "Absensi per mapel diaktifkan. Guru bisa memilih mapel di form absensi."
	case changed:
		msg = "Absensi per mapel dinonaktifkan. Data absensi per mapel yang sudah ada tetap tersimpan."
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, subjectsPage, http.StatusSeeOther)
}
