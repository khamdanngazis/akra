// Package portal menyediakan halaman untuk orang tua: daftar anak (T43),
// detail siswa (T44), tagihan dan riwayat (T45), kuitansi (T46).
package portal

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/attendance"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/payments"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	pageChildren    = "portal/children"
	pageChildDetail = "portal/child_detail"
	pageBilling     = "portal/billing"
)

// Handler melayani route /portal.
type Handler struct {
	svc      *Service
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route portal. Hanya akun orang tua yang boleh masuk;
// akun staf mendapat 403.
func (h *Handler) Routes(r chi.Router) {
	r.Route(auth.PortalPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleOrangTua))
		// Peran: orang_tua.
		r.Get("/", h.children)
		// Peran: orang_tua (hanya anak sendiri; selain itu 404).
		r.Get("/siswa/{id}", h.childDetail)
		// Peran: orang_tua (hanya anak sendiri; selain itu 404).
		r.Get("/siswa/{id}/tagihan", h.childBilling)
		// Peran: orang_tua (hanya pembayaran anak sendiri; selain itu 404).
		r.Get("/pembayaran/{id}/kuitansi", h.receipt)
		// Peran: orang_tua.
		r.Get("/pengumuman", h.announcementsPage)
	})
}

var relationshipLabels = map[string]string{"ayah": "Ayah", "ibu": "Ibu", "wali": "Wali", "lainnya": "Keluarga"}

// childCard sengaja tanpa status siswa (status internal sekolah).
type childCard struct {
	ID, Name, NIS, Relationship, ClassName, YearName string
}

type childrenView struct {
	User     auth.Identity
	Children []childCard
	Single   bool
	Flash    string
	Error    string
}

func (h *Handler) children(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	list, err := h.svc.ListChildren(r.Context(), id.SchoolID, id.UserID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list children", err)
		return
	}
	v := childrenView{User: id, Single: len(list) == 1, Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, c := range list {
		v.Children = append(v.Children, childCard{
			ID: c.ID, Name: c.FullName, NIS: c.NIS, Relationship: relationshipLabels[c.Relationship],
			ClassName: c.ClassName, YearName: c.YearName,
		})
	}
	h.render.Render(w, r, http.StatusOK, pageChildren, v)
}

var genderLabels = map[string]string{"L": "Laki-laki", "P": "Perempuan"}

type childDetailView struct {
	User      auth.Identity
	Child     ChildDetail
	BirthDate string
	Gender    string
	Range     string
	Percent   string
	Flash     string
	Error     string
}

func (h *Handler) childDetail(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	d, err := h.svc.GetChildDetail(r.Context(), id.SchoolID, id.UserID, chi.URLParam(r, "id"), clock.Today())
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Data siswa tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "child detail", err)
		return
	}
	v := childDetailView{User: id, Child: d, Gender: genderLabels[d.Gender], Percent: attendance.FormatPercent(d.Attendance.PresentPercent())}
	if d.BirthDate != nil {
		v.BirthDate = clock.FormatDateID(*d.BirthDate)
	}
	if d.HasRecap {
		v.Range = clock.FormatDateID(d.RecapFrom) + " – " + clock.FormatDateID(d.RecapTo)
	}
	h.render.Render(w, r, http.StatusOK, pageChildDetail, v)
}

var statusLabels = map[billing.Status]string{billing.StatusUnpaid: "Belum bayar", billing.StatusPartial: "Sebagian", billing.StatusPaid: "Lunas"}

var methodLabels = map[payments.Method]string{payments.MethodCash: "Tunai", payments.MethodTransfer: "Transfer"}

type invoiceRow struct {
	Fees, Period, Class, DueDate, Total, Paid, Remaining, Status string
	Paid0, Overdue, IsPaid                                       bool
}

type paymentRow struct {
	ID, PaidOn, Amount, Method, Receipt, Fees, Period, VoidReason string
	Voided                                                        bool
}

type billingView struct {
	User             auth.Identity
	Child            ChildDetail
	Invoices         []invoiceRow
	Payments         []paymentRow
	TotalRemaining   string
	OverdueRemaining string
	HasOverdue       bool
	Flash            string
	Error            string
}

func (h *Handler) childBilling(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	b, err := h.svc.GetChildBilling(r.Context(), id.SchoolID, id.UserID, chi.URLParam(r, "id"), clock.Today())
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Data siswa tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "child billing", err)
		return
	}
	v := billingView{
		User: id, Child: b.Child, TotalRemaining: money.FormatRupiah(b.TotalRemaining),
		OverdueRemaining: money.FormatRupiah(b.OverdueRemaining), HasOverdue: b.OverdueRemaining > 0,
	}
	for _, inv := range b.Invoices {
		v.Invoices = append(v.Invoices, invoiceRow{
			Fees: inv.FeeNames, Period: billing.PeriodLabel(inv.Period), Class: inv.ClassName + " · " + inv.YearName,
			DueDate: clock.FormatDateID(inv.DueDate), Total: money.FormatRupiah(inv.Total), Paid: money.FormatRupiah(inv.Paid),
			Remaining: money.FormatRupiah(inv.Remaining), Status: statusLabels[inv.Status],
			Paid0: inv.Paid == 0, Overdue: inv.Overdue, IsPaid: inv.Status == billing.StatusPaid,
		})
	}
	for _, p := range b.Payments {
		v.Payments = append(v.Payments, paymentRow{
			ID: p.ID, PaidOn: clock.FormatDateID(p.PaidOn), Amount: money.FormatRupiah(p.Amount), Method: methodLabels[p.Method],
			Receipt: p.ReceiptNumber, Fees: p.FeeNames, Period: billing.PeriodLabel(p.Period), Voided: p.Voided, VoidReason: p.VoidReason,
		})
	}
	h.render.Render(w, r, http.StatusOK, pageBilling, v)
}

func (h *Handler) receipt(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	actor := school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
	rc, err := h.svc.ParentReceipt(r.Context(), actor, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Kuitansi tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "parent receipt", err)
		return
	}
	rc.PrintedAt = clock.Today()
	var buf bytes.Buffer
	if err := payments.WriteReceiptPDF(&buf, rc, true); err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "render receipt", err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/pdf")
	hdr.Set("Content-Disposition", `attachment; filename="`+payments.ReceiptFilename(rc.Number)+`"`)
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = buf.WriteTo(w)
}
