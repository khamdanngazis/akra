package payments

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/reports/export"
)

const (
	recapPath  = "/keuangan/rekap-pembayaran"
	pageRecap  = "payments/rekap"
	modeDaily  = "harian"
	modeMonth  = "bulanan"
	recapSheet = "Rekap"
)

type recapRow struct {
	Label                          string
	Cash, Transfer, Total, Pending string
	Count, PendingCount            int
	Empty                          bool
}

type recapView struct {
	User     auth.Identity
	Mode     string
	From, To string // YYYY-MM-DD (harian)
	Year     int    // bulanan
	Years    []int
	Range    string
	Rows     []recapRow
	Total    recapRow
	ExcelURL string
	Flash    string
	Error    string
}

// recapQuery adalah isian filter rekap yang sudah dibaca.
type recapQuery struct {
	mode     string
	from, to time.Time
	year     int
}

// parseRecap membaca ?mode=harian|bulanan&dari=&sampai=&tahun=. Default:
// harian dari tanggal 1 bulan ini sampai hari ini; bulanan tahun ini.
func parseRecap(q url.Values, today time.Time) (recapQuery, string) {
	rq := recapQuery{mode: q.Get("mode"), year: today.Year()}
	if rq.mode != modeMonth {
		rq.mode = modeDaily
	}
	rq.from = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	rq.to = today
	if rq.mode == modeMonth {
		if raw := q.Get("tahun"); raw != "" {
			y, err := strconv.Atoi(raw)
			if err != nil || y < 2000 || y > 2100 {
				return rq, "Tahun tidak valid."
			}
			rq.year = y
		}
		return rq, ""
	}
	for _, f := range []struct {
		key string
		dst *time.Time
	}{{"dari", &rq.from}, {"sampai", &rq.to}} {
		if raw := q.Get(f.key); raw != "" {
			d, err := time.Parse(time.DateOnly, raw)
			if err != nil {
				return rq, "Tanggal tidak valid. Gunakan format YYYY-MM-DD."
			}
			*f.dst = d
		}
	}
	return rq, ""
}

func (rq recapQuery) values() url.Values {
	if rq.mode == modeMonth {
		return url.Values{"mode": {modeMonth}, "tahun": {strconv.Itoa(rq.year)}}
	}
	return url.Values{"mode": {modeDaily}, "dari": {rq.from.Format(time.DateOnly)}, "sampai": {rq.to.Format(time.DateOnly)}}
}

// load menjalankan rekap sesuai mode.
func (h *Handler) loadRecap(r *http.Request, schoolID string, rq recapQuery) ([]PeriodTotal, error) {
	if rq.mode == modeMonth {
		return MonthlyTotals(r.Context(), h.svc.db, schoolID, rq.year)
	}
	return DailyTotals(r.Context(), h.svc.db, schoolID, rq.from, rq.to)
}

func (rq recapQuery) label(t PeriodTotal) string {
	if rq.mode == modeMonth {
		return billing.PeriodLabel(t.Date.Format("2006-01"))
	}
	return clock.FormatDateID(t.Date)
}

func (rq recapQuery) rangeLabel() string {
	if rq.mode == modeMonth {
		return "Tahun " + strconv.Itoa(rq.year)
	}
	return clock.FormatDateID(rq.from) + " – " + clock.FormatDateID(rq.to)
}

func toRow(label string, t PeriodTotal) recapRow {
	return recapRow{Label: label, Cash: money.FormatRupiah(t.Cash), Transfer: money.FormatRupiah(t.Transfer), Total: money.FormatRupiah(t.Total),
		Pending: money.FormatRupiah(t.Pending), Count: t.Count, PendingCount: t.PendingCount, Empty: t.Count == 0}
}

func recapError(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrInvalidRecapRange):
		return "Tanggal awal harus sebelum atau sama dengan tanggal akhir.", true
	case errors.Is(err, ErrRecapRangeTooLong):
		return fmt.Sprintf("Rekap harian maksimal %d hari. Untuk setahun, pakai rekap bulanan.", MaxDailyRecapDays), true
	}
	return "", false
}

// recap menampilkan rekap pembayaran harian atau bulanan (T80).
func (h *Handler) recap(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	today := clock.Today()
	rq, bad := parseRecap(r.URL.Query(), today)
	v := recapView{User: id, Mode: rq.mode, From: rq.from.Format(time.DateOnly), To: rq.to.Format(time.DateOnly), Year: rq.year, Range: rq.rangeLabel()}
	for y := today.Year() + 1; y >= today.Year()-5; y-- {
		v.Years = append(v.Years, y)
	}
	if bad != "" {
		v.Error = bad
		h.render.Render(w, r, http.StatusBadRequest, pageRecap, v)
		return
	}
	rows, err := h.loadRecap(r, id.SchoolID, rq)
	if msg, ok := recapError(err); ok {
		v.Error = msg
		h.render.Render(w, r, http.StatusBadRequest, pageRecap, v)
		return
	}
	if err != nil {
		h.serverError(w, r, "payment recap", err)
		return
	}
	for _, t := range rows {
		v.Rows = append(v.Rows, toRow(rq.label(t), t))
	}
	v.Total = toRow("Total", RecapTotals(rows))
	v.ExcelURL = recapPath + ".xlsx?" + rq.values().Encode()
	h.render.Render(w, r, http.StatusOK, pageRecap, v)
}

// recapExcel mengunduh rekap sebagai .xlsx dengan pola T47 (export.Table).
func (h *Handler) recapExcel(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	today := clock.Today()
	rq, bad := parseRecap(r.URL.Query(), today)
	if bad != "" {
		page.BadRequest(h.render, w, r, id, "Filter tidak valid", bad)
		return
	}
	rows, err := h.loadRecap(r, id.SchoolID, rq)
	if msg, ok := recapError(err); ok {
		page.BadRequest(h.render, w, r, id, "Filter tidak valid", msg)
		return
	}
	if err != nil {
		h.serverError(w, r, "payment recap", err)
		return
	}
	t := recapTable(rows, rq, id.SchoolName, today)
	var buf bytes.Buffer
	if err := export.WriteExcel(&buf, t, recapSheet); err != nil {
		h.serverError(w, r, "payment recap excel", err)
		return
	}
	name := "rekap-pembayaran-" + rq.mode + "-" + rq.from.Format(time.DateOnly) + "-" + rq.to.Format(time.DateOnly) + ".xlsx"
	if rq.mode == modeMonth {
		name = "rekap-pembayaran-bulanan-" + strconv.Itoa(rq.year) + ".xlsx"
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	hdr.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	hdr.Set("Cache-Control", "private, no-store")
	_, _ = w.Write(buf.Bytes())
}

// recapTable menyusun tabel ekspor rekap. Nominal sebagai export.Money
// (angka di Excel).
func recapTable(rows []PeriodTotal, rq recapQuery, schoolName string, printed time.Time) export.Table {
	title := "Rekap Pembayaran Harian"
	first := "Tanggal"
	if rq.mode == modeMonth {
		title, first = "Rekap Pembayaran Bulanan", "Bulan"
	}
	t := export.Table{Title: title, SchoolName: schoolName, PrintedAt: printed,
		Meta: []string{"Periode: " + rq.rangeLabel(), "Berdasarkan tanggal bayar. Pembayaran yang pembatalannya disetujui tidak dihitung; yang masih menunggu persetujuan tetap dihitung dan ditampilkan di kolom tersendiri."},
		Columns: []export.Column{{Header: first, Width: 40}, {Header: "Tunai", Width: 30, Numeric: true}, {Header: "Transfer", Width: 30, Numeric: true},
			{Header: "Total", Width: 30, Numeric: true}, {Header: "Jumlah pembayaran", Width: 26, Numeric: true},
			{Header: "Menunggu pembatalan", Width: 30, Numeric: true}, {Header: "Jumlah menunggu", Width: 24, Numeric: true}}}
	cells := func(label string, p PeriodTotal) []any {
		return []any{label, export.Money(p.Cash), export.Money(p.Transfer), export.Money(p.Total), p.Count, export.Money(p.Pending), p.PendingCount}
	}
	for _, p := range rows {
		t.Rows = append(t.Rows, cells(rq.label(p), p))
	}
	t.Total = cells("Total", RecapTotals(rows))
	return t
}
