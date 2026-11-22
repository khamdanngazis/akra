package exam

import (
	"bytes"
	"net/http"
	"time"

	"akra/internal/auth"
)

// guideFile adalah nama berkas unduhan panduan.
var guideFile = map[Guide]string{
	GuideProctor:     "panduan-pengawas",
	GuideParticipant: "panduan-peserta",
	GuideParent:      "panduan-orang-tua",
}

// writeGuide mengirim panduan sebagai PDF (dibuka di browser untuk dicetak).
func writeGuide(w http.ResponseWriter, kind Guide, info GuideInfo) error {
	var buf bytes.Buffer
	if err := WriteGuidePDF(&buf, kind, info, time.Now()); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+guideFile[kind]+`.pdf"`)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, err := w.Write(buf.Bytes())
	return err
}

// guidePDF mengunduh panduan cetak dengan nama dan alamat sekolah. Peran:
// admin_tu, kepala_sekolah, guru.
func (h *Handler) guidePDF(kind Guide) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.CurrentUser(r.Context())
		var name, code string
		if err := h.db.QueryRow(r.Context(), `SELECT name, code FROM schools WHERE id = $1`, id.SchoolID).Scan(&name, &code); err != nil {
			h.fail(w, r, "guide school", err)
			return
		}
		info := GuideInfo{School: name, Support: h.support}
		if h.baseURL != "" {
			info.LoginURL = h.baseURL + "/u/" + code
			info.CheckURL = h.baseURL + "/u/cek"
		}
		if err := writeGuide(w, kind, info); err != nil {
			h.fail(w, r, "guide pdf", err)
		}
	}
}

// WithBaseURL mengatur URL publik untuk alamat di panduan cetak.
func (h *ParticipantHandler) WithBaseURL(u string) *ParticipantHandler {
	h.baseURL = u
	return h
}

// guidePDF mengunduh panduan umum tanpa data sekolah. Publik.
func (h *ParticipantHandler) guidePDF(kind Guide) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := GuideInfo{}
		if h.baseURL != "" {
			info.CheckURL = h.baseURL + "/u/cek"
		}
		if err := writeGuide(w, kind, info); err != nil {
			h.logger.ErrorContext(r.Context(), "guide pdf", "error", err)
			http.Error(w, "Panduan tidak bisa dibuat.", http.StatusInternalServerError)
		}
	}
}
