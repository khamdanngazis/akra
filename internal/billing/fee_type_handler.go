package billing

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	feeTypesPath    = "/keuangan/jenis-tagihan"
	maxFeeTypeForm  = 16 << 10
	msgFeeTypeGone  = "Jenis tagihan yang Anda cari tidak ada atau bukan milik sekolah Anda."
	pageFeeTypes    = "billing/fee_types"
	pageFeeTypeForm = "billing/fee_type_form"
)

// FeeTypeHandler melayani halaman jenis tagihan.
type FeeTypeHandler struct {
	svc      *FeeTypeService
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewFeeTypeHandler mengembalikan FeeTypeHandler. Route-nya harus berada di
// dalam grup RequireLogin.
func NewFeeTypeHandler(svc *FeeTypeService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *FeeTypeHandler {
	return &FeeTypeHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route jenis tagihan ke r.
func (h *FeeTypeHandler) Routes(r chi.Router) {
	r.Route(feeTypesPath, func(r chi.Router) {
		// Peran: bendahara, admin_tu, kepala_sekolah (hanya baca).
		r.With(auth.RequireRole(auth.RoleBendahara, auth.RoleAdminTU, auth.RoleKepalaSekolah)).Get("/", h.list)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleBendahara, auth.RoleAdminTU))
			// Peran: bendahara, admin_tu.
			r.Get("/baru", h.newForm)
			// Peran: bendahara, admin_tu.
			r.Post("/", h.create)
			// Peran: bendahara, admin_tu.
			r.Get("/{id}/ubah", h.editForm)
			// Peran: bendahara, admin_tu.
			r.Post("/{id}", h.update)
			// Peran: bendahara, admin_tu.
			r.Post("/{id}/nonaktifkan", h.deactivate)
			// Peran: bendahara, admin_tu.
			r.Post("/{id}/aktifkan", h.activate)
		})
	})
}

type feeTypeRow struct {
	ID       string
	Name     string
	Amount   string
	DueDay   int
	IsActive bool
}

type feeTypesView struct {
	User    auth.Identity
	Rows    []feeTypeRow
	CanEdit bool
	Flash   string
	Error   string
}

type feeTypeFormView struct {
	User   auth.Identity
	IsNew  bool
	ID     string
	Form   FeeTypeForm
	Errors map[string]string
	Error  string
	Flash  string
}

func (h *FeeTypeHandler) list(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	list, err := h.svc.ListAll(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list fee types", err)
		return
	}
	view := feeTypesView{
		User:    id,
		CanEdit: id.HasRole(auth.RoleBendahara) || id.HasRole(auth.RoleAdminTU),
		Flash:   auth.PopFlash(r.Context(), h.sessions),
	}
	for _, f := range list {
		view.Rows = append(view.Rows, feeTypeRow{ID: f.ID, Name: f.Name, Amount: money.FormatRupiah(f.DefaultAmount), DueDay: f.DefaultDueDay, IsActive: f.IsActive})
	}
	h.render.Render(w, r, http.StatusOK, pageFeeTypes, view)
}

func (h *FeeTypeHandler) newForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.render.Render(w, r, http.StatusOK, pageFeeTypeForm, feeTypeFormView{User: id, IsNew: true})
}

func (h *FeeTypeHandler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := parseFeeTypeForm(w, r)
	if !ok {
		return
	}
	ft, err := h.svc.Create(r.Context(), actorFrom(r, id), f)
	if h.formError(w, r, err, feeTypeFormView{User: id, IsNew: true, Form: f}) {
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Jenis tagihan "+ft.Name+" berhasil dibuat.")
	http.Redirect(w, r, feeTypesPath, http.StatusSeeOther)
}

func (h *FeeTypeHandler) editForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ft, err := h.svc.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrFeeTypeNotFound) {
		page.NotFound(h.render, w, r, id, msgFeeTypeGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get fee type", err)
		return
	}
	f := FeeTypeForm{Name: ft.Name, DefaultAmount: money.Group(ft.DefaultAmount)}
	if ft.DefaultDueDay > 0 {
		f.DefaultDueDay = strconv.Itoa(ft.DefaultDueDay)
	}
	h.render.Render(w, r, http.StatusOK, pageFeeTypeForm, feeTypeFormView{User: id, ID: ft.ID, Form: f})
}

func (h *FeeTypeHandler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	feeTypeID := chi.URLParam(r, "id")
	f, ok := parseFeeTypeForm(w, r)
	if !ok {
		return
	}
	ft, err := h.svc.Update(r.Context(), actorFrom(r, id), feeTypeID, f)
	if h.formError(w, r, err, feeTypeFormView{User: id, ID: feeTypeID, Form: f}) {
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Jenis tagihan "+ft.Name+" berhasil diubah.")
	http.Redirect(w, r, feeTypesPath, http.StatusSeeOther)
}

func (h *FeeTypeHandler) deactivate(w http.ResponseWriter, r *http.Request) { h.setActive(w, r, false) }
func (h *FeeTypeHandler) activate(w http.ResponseWriter, r *http.Request)   { h.setActive(w, r, true) }

func (h *FeeTypeHandler) setActive(w http.ResponseWriter, r *http.Request, active bool) {
	id, _ := auth.CurrentUser(r.Context())
	ft, changed, err := h.svc.SetActive(r.Context(), actorFrom(r, id), chi.URLParam(r, "id"), active)
	if errors.Is(err, ErrFeeTypeNotFound) {
		page.NotFound(h.render, w, r, id, msgFeeTypeGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "set fee type active", err)
		return
	}
	msg := "Jenis tagihan " + ft.Name + " dinonaktifkan. Tidak bisa dipakai untuk tagihan baru."
	switch {
	case active && changed:
		msg = "Jenis tagihan " + ft.Name + " diaktifkan kembali."
	case !changed:
		msg = "Status jenis tagihan " + ft.Name + " tidak berubah."
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, feeTypesPath, http.StatusSeeOther)
}

func parseFeeTypeForm(w http.ResponseWriter, r *http.Request) (FeeTypeForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFeeTypeForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return FeeTypeForm{}, false
	}
	return FeeTypeForm{
		Name:          r.PostForm.Get("name"),
		DefaultAmount: r.PostForm.Get("default_amount"),
		DefaultDueDay: r.PostForm.Get("default_due_day"),
	}, true
}

// formError menangani error Create/Update. Mengembalikan true jika respons
// sudah ditulis.
func (h *FeeTypeHandler) formError(w http.ResponseWriter, r *http.Request, err error, view feeTypeFormView) bool {
	if err == nil {
		return false
	}
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		view.Errors = ve.Fields
		h.render.Render(w, r, http.StatusBadRequest, pageFeeTypeForm, view)
	case errors.Is(err, ErrFeeTypeNotFound):
		page.NotFound(h.render, w, r, view.User, msgFeeTypeGone)
	default:
		h.serverError(w, r, "save fee type", err)
	}
	return true
}

func (h *FeeTypeHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func actorFrom(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
