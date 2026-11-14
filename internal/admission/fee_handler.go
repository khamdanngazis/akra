package admission

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/payments"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
)

type feeRowView struct {
	FeeRow
	AmountText string
	PaidOnText string
}

type feesView struct {
	User       auth.Identity
	CanRecord  bool
	Rows       []feeRowView
	UnpaidOnly bool
	Today      string
	// Form dan Errors untuk baris yang gagal disimpan.
	FailedID string
	Form     FeePaymentForm
	Errors   map[string]string
	Flash    string
	Error    string
}

func (h *Handler) fees() *FeeService { return NewFeeService(h.db) }

// listFees menampilkan biaya formulir pendaftar. Peran: bendahara (catat),
// admin_tu dan kepala_sekolah (baca).
func (h *Handler) listFees(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderFees(w, r, id, http.StatusOK, "", FeePaymentForm{}, nil, "")
}

func (h *Handler) renderFees(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, failedID string, f FeePaymentForm, errs map[string]string, msg string) {
	unpaid := r.URL.Query().Get("status") != "semua"
	rows, err := h.fees().List(r.Context(), id.SchoolID, unpaid)
	if err != nil {
		h.serverError(w, r, "list fees", err)
		return
	}
	view := feesView{User: id, CanRecord: id.HasRole(auth.RoleBendahara), UnpaidOnly: unpaid, Today: clock.Today().Format(dateLayout),
		FailedID: failedID, Form: f, Errors: errs, Error: msg, Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, x := range rows {
		v := feeRowView{FeeRow: x, AmountText: money.FormatRupiah(x.Amount)}
		if x.Payment != nil {
			v.PaidOnText = clock.FormatDateID(x.Payment.PaidOn)
		}
		view.Rows = append(view.Rows, v)
	}
	h.render.Render(w, r, status, "admission/fees", view)
}

// recordFee mencatat pembayaran biaya formulir. Peran: bendahara.
func (h *Handler) recordFee(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	applicantID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	f := FeePaymentForm{PaidOn: r.PostForm.Get("paid_on"), Method: r.PostForm.Get("method"), BankAccount: r.PostForm.Get("bank_account"), Reference: r.PostForm.Get("reference")}
	p, err := h.fees().Record(r.Context(), actorFrom(r, id), applicantID, f)
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Pembayaran dicatat dengan kuitansi "+p.ReceiptNumber+".")
		http.Redirect(w, r, applicantsPath+"/biaya", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgApplicantGone)
	case errors.As(err, &ve):
		h.renderFees(w, r, id, http.StatusBadRequest, applicantID, f, ve.Fields, "")
	case errors.Is(err, ErrFeePaid):
		h.renderFees(w, r, id, http.StatusConflict, "", FeePaymentForm{}, nil, "Biaya formulir pendaftar ini sudah lunas.")
	case errors.Is(err, ErrNoFee):
		h.renderFees(w, r, id, http.StatusConflict, "", FeePaymentForm{}, nil, "Pendaftar ini tidak punya biaya formulir.")
	default:
		h.serverError(w, r, "record fee", err)
	}
}

// voidFee membatalkan pembayaran biaya formulir dengan alasan. Peran:
// bendahara.
func (h *Handler) voidFee(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	applicantID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	err := h.fees().Void(r.Context(), actorFrom(r, id), applicantID, r.PostForm.Get("void_reason"))
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Pembayaran biaya formulir dibatalkan.")
		http.Redirect(w, r, applicantsPath+"/biaya?status=semua", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgApplicantGone)
	case errors.As(err, &ve):
		h.renderFees(w, r, id, http.StatusBadRequest, applicantID, FeePaymentForm{}, ve.Fields, "")
	case errors.Is(err, ErrNoPayment):
		h.renderFees(w, r, id, http.StatusConflict, "", FeePaymentForm{}, nil, "Tidak ada pembayaran aktif untuk dibatalkan.")
	default:
		h.serverError(w, r, "void fee", err)
	}
}

// feeReceipt mengunduh kuitansi biaya formulir. Peran: bendahara, admin_tu,
// kepala_sekolah.
func (h *Handler) feeReceipt(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	rec, err := h.fees().Receipt(r.Context(), id.SchoolID, chi.URLParam(r, "id"), chi.URLParam(r, "paymentID"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Kuitansi tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "fee receipt", err)
		return
	}
	rec.PrintedAt = time.Now().In(clock.Jakarta)
	var buf bytes.Buffer
	if err := payments.WriteReceiptPDF(&buf, rec, true); err != nil {
		h.serverError(w, r, "write receipt pdf", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+payments.ReceiptFilename(rec.Number)+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(buf.Bytes())
}
