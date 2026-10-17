package payments

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/school"
)

var proofKinds = map[string]string{"image/jpeg": "Foto JPG", "image/png": "Foto PNG", "application/pdf": "PDF"}

// proofErrors adalah pesan untuk unggahan yang ditolak karena isinya.
var proofErrors = map[error]string{
	ErrProofEmpty:       "Pilih file bukti transfer.",
	ErrProofTooLarge:    "File terlalu besar. Maksimal 2 MB.",
	ErrProofType:        "File harus berupa foto (JPG atau PNG) atau PDF.",
	ErrProofDuplicate:   "Bukti ini sudah dipakai untuk pembayaran lain.",
	ErrProofNotTransfer: "Bukti hanya bisa dilampirkan ke pembayaran transfer.",
}

func (h *Handler) uploadProof(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	paymentID := chi.URLParam(r, "id")
	invoiceID, err := h.proofs.InvoiceIDOfPayment(r.Context(), id.SchoolID, paymentID)
	if errors.Is(err, ErrPaymentNotFound) {
		h.notFoundPayment(w, r, id)
		return
	}
	if err != nil {
		h.serverError(w, r, "get payment", err)
		return
	}

	file, hdr, err := r.FormFile("file")
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &mbe):
		err = ErrProofTooLarge
	case errors.Is(err, http.ErrMissingFile):
		err = ErrProofEmpty
	case err != nil:
		h.renderInvoice(w, r, id, invoiceID, http.StatusBadRequest, "Unggahan tidak bisa dibaca. Coba lagi.")
		return
	case hdr.Size > MaxProofSize:
		_ = file.Close()
		err = ErrProofTooLarge
	default:
		defer file.Close()
		_, err = h.proofs.Upload(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, paymentID, file)
	}
	if err != nil {
		for target, msg := range proofErrors {
			if errors.Is(err, target) {
				h.renderInvoice(w, r, id, invoiceID, http.StatusBadRequest, msg)
				return
			}
		}
		if errors.Is(err, ErrPaymentNotFound) {
			h.notFoundPayment(w, r, id)
			return
		}
		h.serverError(w, r, "upload proof", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Bukti transfer tersimpan.")
	http.Redirect(w, r, invoicesPath+"/"+invoiceID, http.StatusSeeOther)
}

func (h *Handler) notFoundPayment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	h.renderNotFound(w, r, id, "Pembayaran tidak ditemukan.")
}

// formatSize memformat ukuran file, mis. "180 KB" atau "1,4 MB".
func formatSize(n int) string {
	if n < 1<<20 {
		return strconv.Itoa((n+1023)/1024) + " KB"
	}
	tenths := (n*10 + 1<<19) >> 20
	return strconv.Itoa(tenths/10) + "," + strconv.Itoa(tenths%10) + " MB"
}

func (h *Handler) openProof(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	u, err := h.proofs.OpenURL(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, chi.URLParam(r, "id"))
	if errors.Is(err, ErrProofNotFound) {
		h.renderNotFound(w, r, id, "Bukti transfer tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "open proof", err)
		return
	}
	// URL bertanda tangan tidak boleh di-cache atau bocor lewat Referer.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, u, http.StatusFound)
}
