package admission

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
	"akra/internal/reports/export"
)

type resultsView struct {
	User         auth.Identity
	CanEdit      bool
	Period       Period
	Tracks       []TrackRanking
	TestsOK      bool
	HasTestTrack bool
	Flash, Error string
}

// results menampilkan peringkat per jalur dan usulan hasil. Peran:
// admin_tu (tetapkan), kepala_sekolah (baca).
func (h *Handler) results(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderResults(w, r, id, http.StatusOK, "")
}

func (h *Handler) renderResults(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, msg string) {
	periodID := chi.URLParam(r, "id")
	p, err := h.periods.Get(r.Context(), id.SchoolID, periodID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	ranking, err := h.applicants.Ranking(r.Context(), id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "ranking", err)
		return
	}
	tests, err := h.selection().Tests(r.Context(), id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "list tests", err)
		return
	}
	view := resultsView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Period: p, Tracks: ranking, TestsOK: WeightsTotal(tests) == 100,
		Error: msg, Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, t := range p.Tracks {
		view.HasTestTrack = view.HasTestTrack || t.Method == MethodTest
	}
	h.render.Render(w, r, status, "admission/results", view)
}

// applyResults menetapkan hasil sesuai pilihan per baris. Peran: admin_tu.
func (h *Handler) applyResults(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	// Simpan urutan manual (T118), lalu kembali ke peringkat dengan usulan
	// yang dihitung ulang.
	if r.PostForm.Get("action") == "urutan" {
		ranks := map[string]string{}
		for k, v := range r.PostForm {
			if aid, ok := strings.CutPrefix(k, "rank_"); ok && len(v) > 0 {
				ranks[aid] = v[0]
			}
		}
		n, err := h.applicants.SetManualRanks(r.Context(), actorFrom(r, id), periodID, ranks)
		var ve *ValidationError
		switch {
		case err == nil:
			auth.SetFlash(r.Context(), h.sessions, fmt.Sprintf("Urutan disimpan untuk %d pendaftar. Periksa usulan lalu tetapkan hasil.", n))
			http.Redirect(w, r, periodsPath+"/"+periodID+"/hasil", http.StatusSeeOther)
		case errors.Is(err, ErrNotFound):
			page.NotFound(h.render, w, r, id, msgPeriodGone)
		case errors.As(err, &ve):
			h.renderResults(w, r, id, http.StatusBadRequest, "Urutan harus angka 1–9999. Tidak ada yang disimpan.")
		default:
			h.serverError(w, r, "save manual ranks", err)
		}
		return
	}
	results := map[string]string{}
	for k, v := range r.PostForm {
		if aid, ok := strings.CutPrefix(k, "result_"); ok && len(v) > 0 {
			results[aid] = v[0]
		}
	}
	changed, over, err := h.applicants.ApplyResults(r.Context(), actorFrom(r, id), periodID, results)
	var ve *ValidationError
	switch {
	case err == nil:
		msg := fmt.Sprintf("Hasil ditetapkan untuk %d pendaftar.", changed)
		if changed == 0 {
			msg = "Tidak ada perubahan status."
		}
		if over {
			msg += " Perhatian: jumlah diterima di salah satu jalur melebihi kuota."
		}
		auth.SetFlash(r.Context(), h.sessions, msg)
		http.Redirect(w, r, periodsPath+"/"+periodID+"/hasil", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.As(err, &ve):
		h.renderResults(w, r, id, http.StatusBadRequest, "Tidak ada yang disimpan: sebagian pendaftar gagal dijadikan siswa otomatis. "+ve.Fields["nis"])
	case errors.Is(err, ErrBadTransition):
		h.renderResults(w, r, id, http.StatusConflict, "Sebagian status tidak bisa diubah ke pilihan itu (mungkin sudah diubah petugas lain). Tidak ada yang disimpan; periksa lalu coba lagi.")
	default:
		h.serverError(w, r, "apply results", err)
	}
}

// exportResults mengunduh peringkat sebagai Excel. Peran: admin_tu,
// kepala_sekolah.
func (h *Handler) exportResults(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	p, err := h.periods.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	ranking, err := h.applicants.Ranking(r.Context(), id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "ranking", err)
		return
	}
	t := export.Table{
		Title: "Peringkat Pendaftar " + p.Name, SchoolName: id.SchoolName, PrintedAt: time.Now().In(clock.Jakarta),
		Meta: []string{"Tahun ajaran: " + p.AcademicYearName},
		Columns: []export.Column{{Header: "Jalur"}, {Header: "Peringkat", Numeric: true}, {Header: "Nomor"}, {Header: "Nama"},
			{Header: "Skor"}, {Header: "Status"}, {Header: "Usulan"}},
	}
	for _, tr := range ranking {
		for _, x := range tr.Rows {
			rank, score := "", ""
			if x.Rank > 0 {
				rank = strconv.Itoa(x.Rank)
			}
			switch tr.Track.Method {
			case MethodTest:
				score = strconv.FormatFloat(x.Score, 'f', 2, 64)
				if !x.Complete {
					score += " (belum lengkap)"
				}
			case MethodScore:
				score = x.ScoreText()
				if !x.Complete {
					score = "(belum diisi)"
				}
			case MethodManual:
				if x.ManualRank > 0 {
					score = "Urutan " + strconv.Itoa(x.ManualRank)
				}
			}
			t.Rows = append(t.Rows, []any{tr.Track.Name, rank, x.RegistrationNumber, x.FullName, score, StatusLabels[x.Status], StatusLabels[x.Proposal]})
		}
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="peringkat-pendaftar.xlsx"`)
	w.Header().Set("Cache-Control", "private, no-store")
	if err := export.WriteExcel(w, t, "Peringkat"); err != nil {
		h.logger.Error("export ranking", "error", err)
	}
}
