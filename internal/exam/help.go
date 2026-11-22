package exam

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// Pusat bantuan saat ujian (T154): langkah pemecahan masalah umum untuk
// pengawas dan koordinator, plus kontak dukungan AKRA. Jam layanan musim
// ujian tampil selama sekolah punya agenda PTS/PAS yang dekat atau sedang
// berjalan.

// Support adalah kontak dukungan AKRA (dari konfigurasi server).
type Support struct {
	WhatsApp  string // nomor, mis. 0812xxxx atau +62812xxxx
	Email     string
	Hours     string // jam layanan biasa
	PeakHours string // jam layanan musim PTS/PAS
}

// WhatsAppURL mengembalikan tautan wa.me untuk nomor WhatsApp, atau "".
func (s Support) WhatsAppURL() string {
	var b strings.Builder
	for _, r := range s.WhatsApp {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	n := b.String()
	if strings.HasPrefix(n, "0") {
		n = "62" + n[1:]
	}
	if len(n) < 8 {
		return ""
	}
	return "https://wa.me/" + n
}

// Configured melaporkan apakah ada kontak dukungan.
func (s Support) Configured() bool { return s.WhatsAppURL() != "" || s.Email != "" }

// WithSupport mengatur kontak dukungan AKRA di pusat bantuan.
func (h *Handler) WithSupport(s Support) *Handler {
	h.support = s
	return h
}

// peakWindow: agenda dianggap musim ujian sejak 7 hari sebelum mulai.
const peakWindow = 7

// inExamSeason melaporkan apakah sekolah punya agenda PTS/PAS yang mulai
// dalam peakWindow hari atau sedang berjalan pada today.
func inExamSeason(ctx context.Context, q database.DBTX, schoolID string, today time.Time) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_events WHERE school_id = $1 AND archived_at IS NULL
		AND starts_on - $3::int <= $2::date AND ends_on >= $2::date)`, schoolID, today, peakWindow).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("exam: exam season: %w", err)
	}
	return ok, nil
}

type helpView struct {
	User    auth.Identity
	Support Support
	Peak    bool
	Code    string // kode sekolah untuk alamat peserta /u/<kode>
}

// help menampilkan pusat bantuan. Peran: admin_tu, kepala_sekolah, guru.
func (h *Handler) help(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	peak, err := inExamSeason(r.Context(), h.db, id.SchoolID, clock.TodayIn(id.Location()))
	if err != nil {
		h.fail(w, r, "exam season", err)
		return
	}
	var code string
	if err := h.db.QueryRow(r.Context(), `SELECT code FROM schools WHERE id = $1`, id.SchoolID).Scan(&code); err != nil {
		h.fail(w, r, "school code", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/help", helpView{User: id, Support: h.support, Peak: peak, Code: code})
}
