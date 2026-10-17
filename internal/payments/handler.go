package payments

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	paymentsPath = "/keuangan/pembayaran"
	invoicesPath = "/keuangan/tagihan"
	maxForm      = 64 << 10
	pageForm     = "payments/form"
	pageResults  = "payments/search_results"
	pageInvoice  = "payments/invoice"
)

// Renderer merender halaman penuh dan potongan halaman (hasil pencarian).
type Renderer interface {
	page.Renderer
	RenderFragment(w http.ResponseWriter, r *http.Request, status int, name, block string, data any)
}

// Handler melayani form catat pembayaran dan halaman detail tagihan.
type Handler struct {
	svc      *Service
	sessions *scs.SessionManager
	render   Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, sessions *scs.SessionManager, render Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route pembayaran dan detail tagihan.
func (h *Handler) Routes(r chi.Router) {
	r.Route(paymentsPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleBendahara))
		// Peran: bendahara.
		r.Get("/baru", h.form)
		// Peran: bendahara. Potongan HTML untuk pencarian Alpine.js.
		r.Get("/cari", h.search)
		// Peran: bendahara.
		r.Post("/", h.create)
	})
	// Peran: bendahara, admin_tu, kepala_sekolah (hanya baca).
	r.With(auth.RequireRole(auth.RoleBendahara, auth.RoleAdminTU, auth.RoleKepalaSekolah)).
		Get(invoicesPath+"/{id}", h.invoice)
}

type studentRow struct {
	ID, NIS, Name, Status string
}

type searchView struct {
	Query    string
	Searched bool
	Results  []studentRow
}

type invoiceRow struct {
	ID, Label, Period, ClassName, DueDate string
	Total, Paid, Remaining                string
	Overdue, Partial, Checked             bool
	Amount, Error                         string
}

type formView struct {
	User         auth.Identity
	Search       searchView
	Student      *studentRow
	Invoices     []invoiceRow
	Form         Form
	Today        string
	BankAccounts []string
	Errors       map[string]string
	Error        string
	Flash        string
}

type paymentRow struct {
	PaidOn, Method, BankAccount, Reference, Note, Amount, Receipt string
}

type invoiceView struct {
	User      auth.Identity
	Invoice   InvoiceDetail
	Period    string
	DueDate   string
	Status    string
	Overdue   bool
	Items     []itemRow
	Payments  []paymentRow
	Total     string
	Paid      string
	Remaining string
	CanPay    bool
	Error     string
	Flash     string
}

type itemRow struct {
	Name, Description, Amount string
}

func (h *Handler) form(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	studentID := r.URL.Query().Get("siswa")
	if studentID == "" {
		view := formView{User: id, Flash: auth.PopFlash(r.Context(), h.sessions)}
		var ok bool
		if view.Search, ok = h.searchResults(w, r, id, r.URL.Query().Get("q")); !ok {
			return
		}
		h.render.Render(w, r, http.StatusOK, pageForm, view)
		return
	}
	h.renderStudentForm(w, r, id, http.StatusOK, Form{StudentID: studentID, Method: string(MethodCash), PaidOn: clock.Today().Format("2006-01-02")}, nil, true)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	view, ok := h.searchResults(w, r, id, r.URL.Query().Get("q"))
	if !ok {
		return
	}
	h.render.RenderFragment(w, r, http.StatusOK, pageResults, "content", view)
}

func (h *Handler) searchResults(w http.ResponseWriter, r *http.Request, id auth.Identity, q string) (searchView, bool) {
	q = strings.TrimSpace(q)
	view := searchView{Query: q, Searched: len([]rune(q)) >= MinSearchLen}
	list, err := h.svc.SearchStudents(r.Context(), id.SchoolID, q)
	if err != nil {
		h.serverError(w, r, "search students", err)
		return view, false
	}
	for _, s := range list {
		view.Results = append(view.Results, studentRow{ID: s.ID, NIS: s.NIS, Name: s.FullName, Status: s.Status})
	}
	return view, true
}

// renderStudentForm merender form untuk satu siswa. defaults: nominal
// tagihan diisi sisa tagihan (bukan dari isian sebelumnya).
func (h *Handler) renderStudentForm(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, f Form, errs map[string]string, defaults bool) {
	st, open, err := h.svc.StudentInvoices(r.Context(), id.SchoolID, f.StudentID)
	if errors.Is(err, ErrStudentNotFound) {
		page.NotFound(h.render, w, r, id, "Siswa tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "student invoices", err)
		return
	}
	banks, err := h.svc.BankAccounts(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "bank accounts", err)
		return
	}
	view := formView{
		User: id, Student: &studentRow{ID: st.ID, NIS: st.NIS, Name: st.FullName, Status: st.Status},
		Form: f, Today: clock.Today().Format("2006-01-02"), BankAccounts: banks, Errors: errs,
		Flash: auth.PopFlash(r.Context(), h.sessions),
	}
	if errs["invoice"] != "" {
		view.Error = errs["invoice"]
	}
	checked := map[string]bool{}
	for _, inv := range f.InvoiceIDs {
		checked[inv] = true
	}
	today := clock.Today()
	for _, o := range open {
		row := invoiceRow{
			ID: o.ID, Label: o.FeeNames, Period: billing.PeriodLabel(o.Period), ClassName: o.ClassName + " · " + o.YearName,
			DueDate: clock.FormatDateID(o.DueDate), Total: money.FormatRupiah(o.Total), Paid: money.FormatRupiah(o.Paid),
			Remaining: money.FormatRupiah(o.Remaining()), Overdue: billing.IsOverdue(o.Status, o.DueDate, today),
			Partial: o.Status == billing.StatusPartial, Checked: checked[o.ID] || len(open) == 1,
			Amount: money.Group(o.Remaining()), Error: errs[AmountField(o.ID)],
		}
		if !defaults {
			row.Amount = f.Amounts[o.ID]
		}
		view.Invoices = append(view.Invoices, row)
	}
	h.render.Render(w, r, status, pageForm, view)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	p := r.PostForm
	f := Form{
		StudentID: p.Get("student_id"), InvoiceIDs: p["invoice_id"], Amounts: map[string]string{},
		Method: p.Get("method"), BankAccount: p.Get("bank_account"), Reference: p.Get("reference"),
		PaidOn: p.Get("paid_on"), Note: p.Get("note"),
	}
	for key, vals := range p {
		if inv, ok := strings.CutPrefix(key, "amount_"); ok && len(vals) > 0 {
			f.Amounts[inv] = vals[0]
		}
	}

	paid, err := h.svc.RecordForm(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, f)
	var ve *ValidationError
	switch {
	case err == nil:
	case errors.As(err, &ve):
		h.renderStudentForm(w, r, id, http.StatusBadRequest, f, ve.Fields, false)
		return
	case errors.Is(err, ErrStudentNotFound):
		page.NotFound(h.render, w, r, id, "Siswa tidak ditemukan.")
		return
	case errors.Is(err, billing.ErrInvoiceNotFound):
		page.NotFound(h.render, w, r, id, "Tagihan tidak ditemukan untuk siswa ini.")
		return
	default:
		h.serverError(w, r, "record payments", err)
		return
	}

	var total int64
	for _, pay := range paid {
		total += pay.Amount
	}
	if len(paid) == 1 {
		auth.SetFlash(r.Context(), h.sessions, fmt.Sprintf("Pembayaran %s tercatat.", money.FormatRupiah(total)))
		http.Redirect(w, r, invoicesPath+"/"+paid[0].InvoiceID, http.StatusSeeOther)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, fmt.Sprintf("%d pembayaran tercatat, total %s.", len(paid), money.FormatRupiah(total)))
	http.Redirect(w, r, paymentsPath+"/baru?siswa="+f.StudentID, http.StatusSeeOther)
}

var statusLabels = map[billing.Status]string{billing.StatusUnpaid: "Belum bayar", billing.StatusPartial: "Sebagian", billing.StatusPaid: "Lunas"}

var methodLabels = map[Method]string{MethodCash: "Tunai", MethodTransfer: "Transfer"}

func (h *Handler) invoice(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	d, list, err := h.svc.InvoiceDetail(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, billing.ErrInvoiceNotFound) {
		page.NotFound(h.render, w, r, id, "Tagihan tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "invoice detail", err)
		return
	}
	view := invoiceView{
		User: id, Invoice: d, Period: billing.PeriodLabel(d.Period), DueDate: clock.FormatDateID(d.DueDate),
		Status: statusLabels[d.Status], Overdue: d.VoidedAt == nil && billing.IsOverdue(d.Status, d.DueDate, clock.Today()),
		Total: money.FormatRupiah(d.TotalAmount), Paid: money.FormatRupiah(d.Paid), Remaining: money.FormatRupiah(d.Remaining()),
		CanPay: id.HasRole(auth.RoleBendahara) && d.VoidedAt == nil && d.Status != billing.StatusPaid,
		Flash:  auth.PopFlash(r.Context(), h.sessions),
	}
	for _, it := range d.Items {
		view.Items = append(view.Items, itemRow{Name: it.FeeTypeName, Description: it.Description, Amount: money.FormatRupiah(it.Amount)})
	}
	for _, p := range list {
		view.Payments = append(view.Payments, paymentRow{
			PaidOn: clock.FormatDateID(p.PaidOn), Method: methodLabels[p.Method], BankAccount: p.BankAccount,
			Reference: p.Reference, Note: p.Note, Amount: money.FormatRupiah(p.Amount), Receipt: p.ReceiptNumber,
		})
	}
	h.render.Render(w, r, http.StatusOK, pageInvoice, view)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}
