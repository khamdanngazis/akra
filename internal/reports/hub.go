package reports

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
)

type hubJob struct {
	ID, TypeLabel, Format, Status, Created string
}

type hubView struct {
	User          auth.Identity
	CanAttendance bool
	CanArrears    bool
	Jobs          []hubJob
	Flash         string
	Error         string
}

// hub menampilkan laporan yang boleh dibuka pengguna dan riwayat ekspor di
// server (milik sendiri; kepala sekolah melihat semua di sekolahnya).
func (h *Handler) hub(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	v := hubView{
		User:          id,
		CanAttendance: auth.HasAnyRole(id, auth.RoleAdminTU, auth.RoleKepalaSekolah),
		CanArrears:    auth.HasAnyRole(id, auth.RoleBendahara, auth.RoleAdminTU, auth.RoleKepalaSekolah),
	}
	rows, err := h.db.Query(r.Context(), `SELECT `+jobColumns+` FROM report_jobs
		WHERE school_id = $1 AND (requested_by = $2 OR $3) ORDER BY created_at DESC LIMIT 10`,
		id.SchoolID, id.UserID, id.HasRole(auth.RoleKepalaSekolah))
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list report jobs", err)
		return
	}
	jobs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReportJob, error) { return scanJob(row) })
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list report jobs", err)
		return
	}
	for _, j := range jobs {
		format := "PDF"
		if j.Format == "xlsx" {
			format = "Excel"
		}
		v.Jobs = append(v.Jobs, hubJob{ID: j.ID, TypeLabel: jobTypeLabels[j.Type], Format: format, Status: j.Status, Created: formatTime(j.CreatedAt)})
	}
	h.render.Render(w, r, http.StatusOK, "reports/hub", v)
}
