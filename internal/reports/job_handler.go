package reports

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/module"
	"akra/internal/platform/page"
	"akra/internal/platform/storage"
	"akra/internal/reports/builder"
)

const (
	pageJob = "reports/job"
	// DownloadURLTTL adalah masa berlaku URL unduhan hasil job (sama dengan
	// bukti transfer, T37).
	DownloadURLTTL = 5 * time.Minute
)

var jobTypeLabels = map[string]string{TypeAttendance: "Rekap kehadiran", TypeArrears: "Daftar tunggakan", TypeCustom: "Laporan kustom"}

type jobView struct {
	User      auth.Identity
	Job       ReportJob
	TypeLabel string
	Created   string
	Finished  string
	Pending   bool
	Error     string
	Flash     string
}

// jobModule mengembalikan modul pemilik data job: kehadiran → absensi,
// tunggakan → keuangan, laporan kustom → modul sumbernya (T165).
func jobModule(j ReportJob) string {
	switch j.Type {
	case TypeAttendance:
		return string(module.Attendance)
	case TypeArrears:
		return string(module.Billing)
	case TypeCustom:
		if j.Params.Config != nil {
			if src, ok := builder.Default.Source(j.Params.Config.Source); ok {
				return src.Module
			}
		}
	}
	return ""
}

func (h *Handler) loadJob(w http.ResponseWriter, r *http.Request) (auth.Identity, ReportJob, bool) {
	id, _ := auth.CurrentUser(r.Context())
	j, err := GetJob(r.Context(), h.db, id.SchoolID, id.UserID, id.HasRole(auth.RoleKepalaSekolah), chi.URLParam(r, "id"))
	if errors.Is(err, ErrJobNotFound) {
		page.NotFound(h.render, w, r, id, "Laporan tidak ditemukan.")
		return id, j, false
	}
	// Hasil ekspor modul yang tidak aktif tidak bisa dibuka atau diunduh.
	if err == nil && !module.From(r.Context()).HasName(jobModule(j)) {
		info, _ := module.Get(module.ID(jobModule(j)))
		page.NotFoundTitled(h.render, w, r, id, "Modul tidak aktif", module.Inactive(info))
		return id, j, false
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "get report job", err)
		return id, j, false
	}
	return id, j, true
}

func (h *Handler) jobStatus(w http.ResponseWriter, r *http.Request) {
	id, j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	v := jobView{User: id, Job: j, TypeLabel: jobTypeLabels[j.Type], Created: formatTime(j.CreatedAt),
		Pending: j.Status == JobQueued || j.Status == JobRunning}
	if j.FinishedAt != nil {
		v.Finished = formatTime(*j.FinishedAt)
	}
	w.Header().Set("Cache-Control", "no-store")
	h.render.Render(w, r, http.StatusOK, pageJob, v)
}

func (h *Handler) jobDownload(w http.ResponseWriter, r *http.Request) {
	id, j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	if j.Status != JobDone || j.FileKey == "" || h.store == nil {
		page.NotFound(h.render, w, r, id, "File laporan tidak tersedia.")
		return
	}
	ct := "application/pdf"
	if j.Format == "xlsx" {
		ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	u, err := h.store.Presign(r.Context(), j.FileKey, DownloadURLTTL, storage.PresignOptions{
		ContentDisposition: `attachment; filename="` + j.FileName + `"`, ContentType: ct,
	})
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "presign report", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, u, http.StatusFound)
}

func formatTime(t time.Time) string {
	t = t.In(clock.Jakarta)
	return clock.FormatDateID(t) + " " + t.Format("15.04") + " WIB"
}
