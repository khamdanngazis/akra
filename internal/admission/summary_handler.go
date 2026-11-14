package admission

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/dapodik/ref"
	"akra/internal/platform/clock"
	"akra/internal/reports/export"
)

// ActionApplicantExport dicatat setiap daftar pendaftar diekspor (memuat
// NIK).
const ActionApplicantExport = "admission.export"

type dayRow struct {
	Label string
	N     int
	Pct   int
}

type summaryView struct {
	User     auth.Identity
	Periods  []option
	PeriodID string
	Summary  Summary
	Days     []dayRow
	Flash    string
	Error    string
}

// summary menampilkan rekap pendaftaran. Peran: admin_tu, kepala_sekolah.
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := r.URL.Query().Get("periode")
	periods, err := h.periods.List(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list periods", err)
		return
	}
	view := summaryView{User: id, Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, p := range periods {
		if p.ID == periodID {
			view.PeriodID = p.ID
		}
		view.Periods = append(view.Periods, option{Value: p.ID, Label: p.Name + " (" + p.AcademicYearName + ")", Selected: p.ID == periodID})
	}
	if view.Summary, err = h.applicants.Summarize(r.Context(), id.SchoolID, view.PeriodID, h.periods.Today()); err != nil {
		h.serverError(w, r, "summarize", err)
		return
	}
	top := 0
	for _, d := range view.Summary.ByDay {
		top = max(top, d.N)
	}
	for _, d := range view.Summary.ByDay {
		day, _ := time.Parse(dateLayout, d.Label)
		view.Days = append(view.Days, dayRow{Label: clock.FormatDateID(day), N: d.N, Pct: d.N * 100 / max(top, 1)})
	}
	h.render.Render(w, r, http.StatusOK, "admission/summary", view)
}

// exportApplicants mengunduh daftar pendaftar (termasuk NIK) sebagai Excel
// dan mencatatnya di audit. Peran: admin_tu, kepala_sekolah.
func (h *Handler) exportApplicants(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := uuidOrNil(r.URL.Query().Get("periode"))
	ids, err := h.db.Query(r.Context(), `SELECT a.id FROM applicants a WHERE a.school_id = $1 AND ($2::uuid IS NULL OR a.period_id = $2::uuid)
		AND a.status <> 'draf' ORDER BY a.registration_number LIMIT 5000`, id.SchoolID, periodID)
	if err != nil {
		h.serverError(w, r, "list applicants", err)
		return
	}
	list, err := pgx.CollectRows(ids, pgx.RowTo[string])
	if err != nil {
		h.serverError(w, r, "list applicants", err)
		return
	}
	t := export.Table{
		Title: "Daftar Pendaftar", SchoolName: id.SchoolName, PrintedAt: time.Now().In(clock.Jakarta),
		Columns: []export.Column{{Header: "Nomor"}, {Header: "Nama"}, {Header: "JK"}, {Header: "Tanggal lahir"}, {Header: "NIK"}, {Header: "NISN"},
			{Header: "Agama"}, {Header: "Sekolah asal"}, {Header: "Nama ibu kandung"}, {Header: "HP orang tua"}, {Header: "Periode"}, {Header: "Jalur"}, {Header: "Status"}},
	}
	periodNames, trackNames := map[string]string{}, map[string]string{}
	for _, aid := range list {
		a, err := h.applicants.Load(r.Context(), id.SchoolID, aid)
		if err != nil {
			h.serverError(w, r, "load applicant", err)
			return
		}
		if _, ok := periodNames[a.PeriodID]; !ok {
			p, err := h.periods.Get(r.Context(), id.SchoolID, a.PeriodID)
			if err != nil {
				h.serverError(w, r, "get period", err)
				return
			}
			periodNames[a.PeriodID] = p.Name
			for _, tr := range p.Tracks {
				trackNames[tr.ID] = tr.Name
			}
		}
		religion, _ := ref.Agama.Label(a.Values["religion"])
		t.Rows = append(t.Rows, []any{a.RegistrationNumber, a.FullName, a.Gender, a.BirthDate.Format(dateLayout), a.Values["nik"], a.Values["nisn"],
			religion, a.Values["previous_school"], a.Guardians["ibu"]["full_name"], a.ContactPhone, periodNames[a.PeriodID], trackNames[a.TrackID], StatusLabels[a.Status]})
	}
	actor := actorFrom(r, id)
	if err := pgx.BeginFunc(r.Context(), h.db, func(tx pgx.Tx) error {
		return audit.Record(r.Context(), tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionApplicantExport, EntityType: "school", EntityID: actor.SchoolID,
			After: map[string]any{"rows": len(t.Rows), "with_nik": true, "period_id": strings.TrimSpace(r.URL.Query().Get("periode"))}})
	}); err != nil {
		h.serverError(w, r, "audit export", err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="daftar-pendaftar.xlsx"`)
	w.Header().Set("Cache-Control", "private, no-store")
	if err := export.WriteExcel(w, t, "Pendaftar"); err != nil {
		h.logger.Error("export applicants", "error", err)
	}
}
