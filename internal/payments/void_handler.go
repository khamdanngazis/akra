package payments

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/school"
)

const pageVoids = "payments/voids"

// voidErrors adalah pesan untuk pengajuan atau keputusan yang ditolak aturan.
var voidErrors = map[error]string{
	ErrVoidReasonRequired: "Alasan pembatalan wajib diisi.",
	ErrVoidReasonTooLong:  "Alasan paling banyak 500 karakter.",
	ErrVoidPeriodClosed:   "Pembayaran hanya bisa dibatalkan di bulan yang sama dengan tanggal bayarnya.",
	ErrAlreadyVoided:      "Pembayaran ini sudah dibatalkan atau pembatalannya sedang menunggu persetujuan.",
	ErrVoidNotPending:     "Pengajuan ini sudah diputuskan.",
	ErrSelfApproval:       "Anda tidak bisa memutuskan pembatalan yang Anda ajukan sendiri.",
}

func voidMessage(err error) (string, bool) {
	for target, msg := range voidErrors {
		if errors.Is(err, target) {
			return msg, true
		}
	}
	return "", false
}

func actor(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}

func (h *Handler) requestVoid(w http.ResponseWriter, r *http.Request) {
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
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		h.renderInvoice(w, r, id, invoiceID, http.StatusBadRequest, "Form tidak bisa dibaca. Coba lagi.")
		return
	}
	v, err := h.voids.Request(r.Context(), actor(r, id), paymentID, r.PostForm.Get("reason"))
	if msg, ok := voidMessage(err); ok {
		h.renderInvoice(w, r, id, invoiceID, http.StatusBadRequest, msg)
		return
	}
	if err != nil {
		h.serverError(w, r, "request void", err)
		return
	}
	msg := "Pembayaran dibatalkan. Status tagihan sudah dihitung ulang."
	if v.Status == VoidPending {
		msg = "Pengajuan pembatalan tersimpan dan menunggu persetujuan kepala sekolah."
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, invoicesPath+"/"+invoiceID, http.StatusSeeOther)
}

type pendingVoidRow struct {
	ID, InvoiceID, Student, NIS, Period, Amount, PaidOn, Method, Reason, Requester, RequestedAt string
	Own                                                                                         bool
}

type voidsView struct {
	User    auth.Identity
	Pending []pendingVoidRow
	Error   string
	Flash   string
}

func (h *Handler) pendingVoids(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderVoids(w, r, id, http.StatusOK, "")
}

func (h *Handler) renderVoids(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, errMsg string) {
	list, err := h.voids.ListPending(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list pending voids", err)
		return
	}
	view := voidsView{User: id, Error: errMsg, Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, v := range list {
		view.Pending = append(view.Pending, pendingVoidRow{
			ID: v.ID, InvoiceID: v.InvoiceID, Student: v.StudentName, NIS: v.NIS, Period: billing.PeriodLabel(v.Period),
			Amount: money.FormatRupiah(v.Amount), PaidOn: clock.FormatDateID(v.PaidOn), Method: methodLabels[v.Method],
			Reason: v.Reason, Requester: v.RequesterName, RequestedAt: clock.FormatDateID(clock.DateOf(v.CreatedAt)),
			Own: v.RequestedBy == id.UserID,
		})
	}
	h.render.Render(w, r, status, pageVoids, view)
}

func (h *Handler) approveVoid(w http.ResponseWriter, r *http.Request) {
	h.decideVoid(w, r, true)
}

func (h *Handler) rejectVoid(w http.ResponseWriter, r *http.Request) {
	h.decideVoid(w, r, false)
}

func (h *Handler) decideVoid(w http.ResponseWriter, r *http.Request, approve bool) {
	id, _ := auth.CurrentUser(r.Context())
	decide, msg := h.voids.Reject, "Pengajuan pembatalan ditolak. Pembayaran tetap berlaku."
	if approve {
		decide, msg = h.voids.Approve, "Pembatalan disetujui. Status tagihan sudah dihitung ulang."
	}
	_, err := decide(r.Context(), actor(r, id), chi.URLParam(r, "id"))
	if errors.Is(err, ErrVoidNotFound) {
		h.renderNotFound(w, r, id, "Pengajuan pembatalan tidak ditemukan.")
		return
	}
	if m, ok := voidMessage(err); ok {
		status := http.StatusConflict
		if errors.Is(err, ErrSelfApproval) {
			status = http.StatusForbidden
		}
		h.renderVoids(w, r, id, status, m)
		return
	}
	if err != nil {
		h.serverError(w, r, "decide void", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, voidsPath, http.StatusSeeOther)
}
