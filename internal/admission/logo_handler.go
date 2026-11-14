package admission

import (
	"errors"
	"io"
	"net/http"

	"akra/internal/auth"
)

// uploadLogo mengganti logo sekolah (multipart, field "logo"). Peran: admin_tu.
func (h *Handler) uploadLogo(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, MaxLogoSize+64<<10)
	file, _, err := r.FormFile("logo")
	if err != nil {
		h.renderPeriods(w, r, http.StatusBadRequest, "Pilih file logo (PNG atau JPG, maksimal 512 KB).")
		return
	}
	defer file.Close()
	switch err := h.logos.Save(r.Context(), id.SchoolID, file); {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Logo sekolah disimpan. Logo tampil di halaman pendaftaran.")
		http.Redirect(w, r, periodsPath, http.StatusSeeOther)
	case errors.Is(err, ErrLogoTooLarge):
		h.renderPeriods(w, r, http.StatusBadRequest, "Ukuran logo maksimal 512 KB.")
	case errors.Is(err, ErrLogoType):
		h.renderPeriods(w, r, http.StatusBadRequest, "Logo harus berupa gambar PNG atau JPG.")
	default:
		h.serverError(w, r, "save logo", err)
	}
}

// removeLogo menghapus logo sekolah. Peran: admin_tu.
func (h *Handler) removeLogo(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	if err := h.logos.Remove(r.Context(), id.SchoolID); err != nil {
		h.serverError(w, r, "remove logo", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Logo sekolah dihapus.")
	http.Redirect(w, r, periodsPath, http.StatusSeeOther)
}

// logo menyajikan logo sekolah untuk halaman publik.
func (h *PublicHandler) logo(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	l, found, err := h.logos.Get(r.Context(), s.ID)
	if err != nil {
		h.serverError(w, r, "get logo", err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	body, err := h.logos.Open(r.Context(), l)
	if err != nil {
		h.serverError(w, r, "open logo", err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", l.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// URL memuat versi logo (?v=), jadi aman di-cache lama.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, body)
}
