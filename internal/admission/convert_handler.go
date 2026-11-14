package admission

import (
	"errors"
	"net/http"
	"strings"

	"akra/internal/auth"
	"akra/internal/classes"
)

type enrollRow struct {
	ApplicantID        string
	RegistrationNumber string
	FullName           string
	PeriodName         string
	TrackName          string
	YearID             string
	Classes            []option
}

type enrollResultRow struct {
	EnrollResult
	ErrText string
}

type enrollView struct {
	User      auth.Identity
	CanEdit   bool
	Format    string
	Preview   string
	Rows      []enrollRow
	Results   []enrollResultRow
	Succeeded int
	Errors    map[string]string
	Flash     string
	Error     string
}

func (h *Handler) enroller() *Enroller { return NewEnroller(h.db, h.applicants.keys, h.applicants) }

// enrollPage menampilkan pendaftar diterima yang belum menjadi siswa.
// Peran: admin_tu (proses), kepala_sekolah (baca).
func (h *Handler) enrollPage(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderEnroll(w, r, id, http.StatusOK, enrollView{})
}

func (h *Handler) renderEnroll(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, view enrollView) {
	ctx := r.Context()
	view.User, view.CanEdit = id, id.HasRole(auth.RoleAdminTU)
	if view.Flash == "" {
		view.Flash = auth.PopFlash(ctx, h.sessions)
	}
	if view.Format == "" {
		f, err := NISFormat(ctx, h.db, id.SchoolID)
		if err != nil {
			h.serverError(w, r, "nis format", err)
			return
		}
		view.Format = f
	}
	if ValidateNISFormat(view.Format) == nil {
		view.Preview = RenderNIS(view.Format, h.periods.Today().Year()+1, 1)
	}
	rows, err := h.db.Query(ctx, `
		SELECT a.id, coalesce(a.registration_number, ''), a.full_name, p.name, t.name, p.academic_year_id
		FROM applicants a JOIN admission_periods p ON p.id = a.period_id JOIN admission_tracks t ON t.id = a.track_id
		WHERE a.school_id = $1 AND a.status = 'diterima' AND a.student_id IS NULL
		ORDER BY p.opens_on, a.registration_number LIMIT 500`, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list accepted", err)
		return
	}
	for rows.Next() {
		var x enrollRow
		if err := rows.Scan(&x.ApplicantID, &x.RegistrationNumber, &x.FullName, &x.PeriodName, &x.TrackName, &x.YearID); err != nil {
			rows.Close()
			h.serverError(w, r, "scan accepted", err)
			return
		}
		view.Rows = append(view.Rows, x)
	}
	rows.Close()
	byYear := map[string][]option{}
	for i, x := range view.Rows {
		if _, ok := byYear[x.YearID]; !ok {
			list, err := classes.ListByYear(ctx, h.db, id.SchoolID, x.YearID)
			if err != nil {
				h.serverError(w, r, "list classes", err)
				return
			}
			var opts []option
			for _, c := range list {
				opts = append(opts, option{Value: c.ID, Label: c.Name})
			}
			byYear[x.YearID] = opts
		}
		view.Rows[i].Classes = byYear[x.YearID]
	}
	h.render.Render(w, r, status, "admission/enroll", view)
}

// saveNISFormat menyimpan format NIS otomatis. Peran: admin_tu.
func (h *Handler) saveNISFormat(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	format := r.PostForm.Get("nis_format")
	err := SetNISFormat(r.Context(), h.db, actorFrom(r, id), format)
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Format NIS disimpan.")
		http.Redirect(w, r, applicantsPath+"/terima", http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderEnroll(w, r, id, http.StatusBadRequest, enrollView{Format: format, Errors: ve.Fields})
	default:
		h.serverError(w, r, "save nis format", err)
	}
}

// enroll menjadikan pendaftar terpilih sebagai siswa. Peran: admin_tu.
func (h *Handler) enroll(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	opts := map[string]EnrollOptions{}
	for _, aid := range r.PostForm["pick"] {
		opts[aid] = EnrollOptions{NIS: strings.TrimSpace(r.PostForm.Get("nis_" + aid)), ClassID: r.PostForm.Get("class_" + aid)}
	}
	if len(opts) == 0 {
		h.renderEnroll(w, r, id, http.StatusBadRequest, enrollView{Error: "Pilih minimal satu pendaftar."})
		return
	}
	// Semua pilihan harus pendaftar diterima milik sekolah ini; ID lain
	// (sekolah lain, sudah menjadi siswa) membuat seluruh permintaan ditolak.
	ids := make([]string, 0, len(opts))
	for aid := range opts {
		ids = append(ids, aid)
	}
	var valid int
	if err := h.db.QueryRow(r.Context(), `SELECT count(*) FROM applicants WHERE school_id = $1 AND id::text = ANY ($2)
		AND status = 'diterima' AND student_id IS NULL`, id.SchoolID, ids).Scan(&valid); err != nil {
		h.serverError(w, r, "check picks", err)
		return
	}
	if valid != len(ids) {
		h.renderEnroll(w, r, id, http.StatusBadRequest, enrollView{Error: "Sebagian pilihan tidak valid atau sudah diproses. Muat ulang halaman lalu coba lagi."})
		return
	}
	results := h.enroller().EnrollMany(r.Context(), actorFrom(r, id), opts)
	view := enrollView{}
	for _, res := range results {
		if res.Err == nil {
			view.Succeeded++
		}
		view.Results = append(view.Results, enrollResultRow{EnrollResult: res, ErrText: EnrollErrorText(res.Err)})
	}
	h.renderEnroll(w, r, id, http.StatusOK, view)
}

// EnrollErrorText mengubah error penerimaan menjadi pesan untuk TU.
func EnrollErrorText(err error) string {
	var ve *ValidationError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ve):
		for _, m := range ve.Fields {
			return m
		}
	case errors.Is(err, ErrNotAccepted):
		return "Belum berstatus diterima atau sudah menjadi siswa."
	case errors.Is(err, ErrNotFound):
		return "Pendaftar tidak ditemukan."
	}
	return "Gagal diproses. Coba lagi atau hubungi admin."
}
