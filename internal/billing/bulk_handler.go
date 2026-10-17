package billing

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
)

const (
	bulkPath     = "/keuangan/tagihan-massal"
	maxBulkForm  = 16 << 10
	pageBulkForm = "billing/bulk_form"
	pageBulkPrev = "billing/bulk_preview"
	pageBulkRun  = "billing/bulk_run"
)

// BulkHandler melayani halaman tagihan massal.
type BulkHandler struct {
	svc      *BulkService
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewBulkHandler mengembalikan BulkHandler. Route-nya harus berada di dalam
// grup RequireLogin.
func NewBulkHandler(svc *BulkService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *BulkHandler {
	return &BulkHandler{svc: svc, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route tagihan massal.
func (h *BulkHandler) Routes(r chi.Router) {
	r.Route(bulkPath, func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleBendahara))
		// Peran: bendahara.
		r.Get("/", h.form)
		// Peran: bendahara.
		r.Post("/pratinjau", h.preview)
		// Peran: bendahara.
		r.Post("/", h.run)
		// Peran: bendahara.
		r.Get("/{id}", h.show)
	})
}

type option struct {
	Value, Label string
	Selected     bool
}

type runRow struct {
	ID, FeeType, Period, ClassName, Status, StatusLabel, CreatedAt string
	Created, Skipped                                               int
}

type bulkFormView struct {
	User     auth.Identity
	YearName string
	FeeTypes []option
	Classes  []option
	Form     BulkForm
	Recent   []runRow
	Errors   map[string]string
	Error    string
	Flash    string
}

type bulkPreviewView struct {
	User    auth.Identity
	Form    BulkForm
	Preview BulkPreview
	Amount  string
	Total   string
	DueDate string
	Period  string
	Error   string
	Flash   string
}

type bulkRunView struct {
	User        auth.Identity
	Run         BulkRun
	Period      string
	DueDate     string
	StatusLabel string
	Percent     int
	Error       string
	Flash       string
}

var runStatusLabels = map[string]string{RunQueued: "Menunggu antrean", RunRunning: "Sedang diproses", RunDone: "Selesai", RunFailed: "Gagal"}

func (h *BulkHandler) form(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderForm(w, r, id, http.StatusOK, BulkForm{}, nil)
}

func (h *BulkHandler) renderForm(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, f BulkForm, errs map[string]string) {
	fees, year, classes, err := h.svc.FormOptions(r.Context(), id.SchoolID)
	view := bulkFormView{User: id, Form: f, Errors: errs, Flash: auth.PopFlash(r.Context(), h.sessions)}
	switch {
	case errors.Is(err, ErrNoActiveYear):
		view.Error = "Belum ada tahun ajaran aktif. Aktifkan tahun ajaran sebelum membuat tagihan."
	case err != nil:
		h.serverError(w, r, "bulk form options", err)
		return
	}
	view.YearName = year.Name
	for _, ft := range fees {
		view.FeeTypes = append(view.FeeTypes, option{Value: ft.ID, Label: ft.Name + " · " + money.FormatRupiah(ft.DefaultAmount), Selected: ft.ID == f.FeeTypeID})
	}
	view.Classes = append(view.Classes, option{Value: "", Label: "Semua kelas", Selected: f.ClassID == ""})
	for _, c := range classes {
		view.Classes = append(view.Classes, option{Value: c.ID, Label: c.Name, Selected: c.ID == f.ClassID})
	}
	recent, err := h.svc.ListRecent(r.Context(), id.SchoolID, 10)
	if err != nil {
		h.serverError(w, r, "list bulk runs", err)
		return
	}
	for _, run := range recent {
		view.Recent = append(view.Recent, runRow{
			ID: run.ID, FeeType: run.FeeTypeName, Period: periodLabel(run.Period), ClassName: run.ClassName,
			Status: run.Status, StatusLabel: runStatusLabels[run.Status], Created: run.CreatedCount, Skipped: run.SkippedCount,
			CreatedAt: clock.FormatDateID(clock.DateOf(run.CreatedAt)),
		})
	}
	h.render.Render(w, r, status, pageBulkForm, view)
}

func (h *BulkHandler) preview(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := parseBulkForm(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Preview(r.Context(), id.SchoolID, f)
	if h.bulkError(w, r, id, f, err) {
		return
	}
	h.render.Render(w, r, http.StatusOK, pageBulkPrev, bulkPreviewView{
		User: id, Form: f, Preview: p, Amount: money.FormatRupiah(p.Amount), Total: money.FormatRupiah(p.Total),
		DueDate: clock.FormatDateID(p.DueDate), Period: periodLabel(p.Period),
	})
}

func (h *BulkHandler) run(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := parseBulkForm(w, r)
	if !ok {
		return
	}
	run, err := h.svc.Run(r.Context(), actorFrom(r, id), f)
	if h.bulkError(w, r, id, f, err) {
		return
	}
	http.Redirect(w, r, bulkPath+"/"+run.ID, http.StatusSeeOther)
}

func (h *BulkHandler) show(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	run, err := h.svc.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrRunNotFound) {
		page.NotFound(h.render, w, r, id, "Proses tagihan massal tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "get bulk run", err)
		return
	}
	view := bulkRunView{User: id, Run: run, Period: periodLabel(run.Period), DueDate: clock.FormatDateID(run.DueDate), StatusLabel: runStatusLabels[run.Status]}
	if run.TotalCount > 0 {
		view.Percent = min(100, run.ProcessedCount*100/run.TotalCount)
	} else if run.Finished() {
		view.Percent = 100
	}
	h.render.Render(w, r, http.StatusOK, pageBulkRun, view)
}

// bulkError menangani error pratinjau/eksekusi. true jika respons sudah ditulis.
func (h *BulkHandler) bulkError(w http.ResponseWriter, r *http.Request, id auth.Identity, f BulkForm, err error) bool {
	var ve *ValidationError
	switch {
	case err == nil:
		return false
	case errors.As(err, &ve):
		h.renderForm(w, r, id, http.StatusBadRequest, f, ve.Fields)
	case errors.Is(err, ErrClassNotFound):
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan di tahun ajaran aktif sekolah Anda.")
	case errors.Is(err, ErrNoActiveYear):
		h.renderForm(w, r, id, http.StatusBadRequest, f, nil)
	default:
		h.serverError(w, r, "bulk invoice", err)
	}
	return true
}

func parseBulkForm(w http.ResponseWriter, r *http.Request) (BulkForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBulkForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return BulkForm{}, false
	}
	p := r.PostForm
	return BulkForm{FeeTypeID: p.Get("fee_type_id"), Period: p.Get("period"), ClassID: p.Get("class_id"), DueDate: p.Get("due_date"), Amount: p.Get("amount")}, true
}

func (h *BulkHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

var monthNames = [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

// periodLabel mengubah "2026-09" menjadi "September 2026".
func periodLabel(period string) string {
	if !periodPattern.MatchString(period) {
		return period
	}
	m := int(period[5]-'0')*10 + int(period[6]-'0')
	return monthNames[m-1] + " " + period[:4]
}
