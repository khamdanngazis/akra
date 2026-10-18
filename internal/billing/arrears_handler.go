package billing

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	arrearsPath = "/keuangan/tunggakan"
	pageArrears = "billing/arrears"
)

// ArrearsHandler melayani halaman daftar tunggakan.
type ArrearsHandler struct {
	db     database.DBTX
	render page.Renderer
	logger *slog.Logger
}

// NewArrearsHandler mengembalikan ArrearsHandler. Route-nya harus berada di
// dalam grup RequireLogin.
func NewArrearsHandler(db database.DBTX, render page.Renderer, logger *slog.Logger) *ArrearsHandler {
	return &ArrearsHandler{db: db, render: render, logger: logger}
}

// Routes mendaftarkan route daftar tunggakan.
func (h *ArrearsHandler) Routes(r chi.Router) {
	// Peran: bendahara, admin_tu, kepala_sekolah (hanya baca).
	r.With(auth.RequireRole(auth.RoleBendahara, auth.RoleAdminTU, auth.RoleKepalaSekolah)).Get(arrearsPath, h.index)
}

type arrearRow struct {
	InvoiceID, Student, NIS, Class, Fees, Period, DueDate, Days, Total, Paid, Remaining string
}

type arrearsView struct {
	User           auth.Identity
	Classes        []option
	FeeTypes       []option
	From, To       string
	Today          string
	Rows           []arrearRow
	TotalRemaining string
	StudentCount   int
	InvoiceCount   int
	Filtered       bool
	Error          string
	Flash          string
}

func (h *ArrearsHandler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	today := clock.Today()
	f := ArrearsFilter{ClassID: q.Get("kelas"), FeeTypeID: q.Get("jenis")}
	v := arrearsView{User: id, From: q.Get("dari"), To: q.Get("sampai"), Today: clock.FormatDateID(today)}
	if d, ok := parseDate(v.From); ok {
		f.DueFrom = d
	} else if v.From != "" {
		v.Error, v.From = "Tanggal tidak valid.", ""
	}
	if d, ok := parseDate(v.To); ok {
		f.DueTo = d
	} else if v.To != "" {
		v.Error, v.To = "Tanggal tidak valid.", ""
	}
	v.Filtered = f.ClassID != "" || f.FeeTypeID != "" || !f.DueFrom.IsZero() || !f.DueTo.IsZero()

	// Pilihan kelas dari tahun ajaran aktif; tunggakan tetap mencakup semua
	// tahun ajaran.
	v.Classes = append(v.Classes, option{Value: "", Label: "Semua kelas", Selected: f.ClassID == ""})
	year, err := school.GetActiveAcademicYear(r.Context(), h.db, id.SchoolID)
	switch {
	case err == nil:
		classes, err := listClassOptions(r.Context(), h.db, id.SchoolID, year.ID)
		if err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "list classes", err)
			return
		}
		for _, c := range classes {
			v.Classes = append(v.Classes, option{Value: c.ID, Label: c.Name, Selected: c.ID == f.ClassID})
		}
	case !errors.Is(err, school.ErrNotFound):
		page.ServerError(h.render, h.logger, w, r, id, "active academic year", err)
		return
	}
	fees, err := ListAllFeeTypes(r.Context(), h.db, id.SchoolID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list fee types", err)
		return
	}
	v.FeeTypes = append(v.FeeTypes, option{Value: "", Label: "Semua jenis", Selected: f.FeeTypeID == ""})
	for _, ft := range fees {
		label := ft.Name
		if !ft.IsActive {
			label += " (nonaktif)"
		}
		v.FeeTypes = append(v.FeeTypes, option{Value: ft.ID, Label: label, Selected: ft.ID == f.FeeTypeID})
	}

	a, err := ListArrears(r.Context(), h.db, id.SchoolID, f, today)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list arrears", err)
		return
	}
	v.TotalRemaining, v.StudentCount, v.InvoiceCount = money.FormatRupiah(a.TotalRemaining), a.StudentCount, len(a.Items)
	for _, it := range a.Items {
		v.Rows = append(v.Rows, arrearRow{
			InvoiceID: it.InvoiceID, Student: it.StudentName, NIS: it.NIS, Class: it.ClassName + " · " + it.YearName,
			Fees: it.FeeNames, Period: PeriodLabel(it.Period), DueDate: clock.FormatDateID(it.DueDate),
			Days: strconv.Itoa(it.DaysOverdue) + " hari", Total: money.FormatRupiah(it.Total),
			Paid: money.FormatRupiah(it.Paid), Remaining: money.FormatRupiah(it.Remaining),
		})
	}
	h.render.Render(w, r, http.StatusOK, pageArrears, v)
}

func parseDate(s string) (time.Time, bool) {
	d, err := time.Parse("2006-01-02", s)
	return d, err == nil
}
