package exam

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/grades"
	"akra/internal/platform/clock"
)

// syncRoutes mendaftarkan kirim nilai ke rapor (T150). Peran: pemegang
// ujian (hak ubah ujian); baris yang dikirim dibatasi kelas yang diampu
// pengirim (hak input nilai T70).
func (h *Handler) syncRoutes(writers chi.Router) {
	writers.Get("/paket/{id}/rapor", h.gradeSyncPage)
	writers.Post("/paket/{id}/rapor", h.gradeSyncPost)
}

func gradeSyncPath(examID string) string { return "/ujian/paket/" + examID + "/rapor" }

type gradeSyncView struct {
	Frame      examFrame // kerangka halaman ujian (T160)
	User       auth.Identity
	Exam       Exam
	Plan       GradeSyncPlan
	Components []grades.Component
	NoTarget   string // alasan pratinjau tidak tersedia
	// WrongPurpose: ujian ini memang tidak dikirim ke rapor.
	WrongPurpose bool
	Policy       string
	Flash        string
	Error        string
	Zone         string
}

func (h *Handler) gradeSyncPage(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Write)
	if !ok {
		return
	}
	h.renderGradeSync(w, r, id, sa, e, r.URL.Query().Get("aturan"), http.StatusOK, "")
}

func (h *Handler) renderGradeSync(w http.ResponseWriter, r *http.Request, id auth.Identity, sa StaffActor, e Exam, policy string, status int, msg string) {
	ctx := r.Context()
	loc := id.Location()
	v := gradeSyncView{User: id, Exam: e, Policy: policy, Flash: auth.PopFlash(ctx, h.sessions), Error: msg, Zone: clock.ZoneLabel(loc)}
	base, _, _, err := gradeTarget(ctx, h.db, e, policy)
	if err != nil {
		h.fail(w, r, "grade target", err)
		return
	}
	if v.Components, err = GradeComponentOptions(ctx, h.db, base); err != nil {
		h.fail(w, r, "grade components", err)
		return
	}
	v.Plan, err = PlanGradeSync(ctx, h.db, sa, e, policy)
	switch {
	case errors.Is(err, ErrGradePurpose):
		v.WrongPurpose = true
		v.NoTarget = "Hanya ulangan, PTS, dan PAS bermapel (serta remedial/pengayaannya) yang nilainya dikirim ke rapor."
	case errors.Is(err, ErrNoGradeComponent):
		v.NoTarget = "Pilih dulu komponen nilai rapor tujuan ujian ini."
	case errors.As(err, new(*FieldError)):
		v.NoTarget = "Komponen tujuan tidak aktif lagi atau bukan mapel ujian ini. Pilih komponen lain."
	case err != nil:
		h.fail(w, r, "plan grade sync", err)
		return
	default:
		for i := range v.Plan.History {
			v.Plan.History[i].At = v.Plan.History[i].At.In(loc)
		}
		v.Policy = v.Plan.Policy
		// Saran deskripsi capaian dari analisis TP (T148).
		a, err := LoadExamAnalysis(ctx, h.db, e, ResultFilter{})
		if err != nil {
			h.fail(w, r, "analysis", err)
			return
		}
		if len(a.Objectives.Objectives) > 0 {
			v.Plan.HasObjectives = true
			sug := map[string]string{}
			for _, s := range a.Objectives.Students {
				if s.StudentID != "" {
					sug[s.StudentID] = a.Objectives.AchievementSuggestion(s)
				}
			}
			for i := range v.Plan.Rows {
				v.Plan.Rows[i].Suggestion = sug[v.Plan.Rows[i].StudentID]
			}
		}
	}
	v.Frame = h.frameOrLog(ctx, sa, e, "hasil", "rapor", "Kirim ke rapor")
	h.render.Render(w, r, status, "exam/sync_grades", v)
}

func (h *Handler) gradeSyncPost(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	pf := r.PostForm
	back := gradeSyncPath(e.ID)
	switch pf.Get("aksi") {
	case "komponen":
		err := SetGradeComponent(r.Context(), h.db, sa, e, pf.Get("grade_component_id"))
		switch {
		case errors.Is(err, ErrGradePurpose), errors.As(err, new(*FieldError)):
			h.flash(r, "Pilih komponen nilai aktif mapel ujian ini.")
		case err != nil:
			h.fail(w, r, "set grade component", err)
			return
		default:
			h.flash(r, "Komponen nilai tujuan disimpan. Periksa pratinjau lalu kirim.")
		}
	case "kirim":
		policy := pf.Get("aturan")
		res, err := SendGrades(r.Context(), h.db, sa, e, policy, time.Now())
		var se *grades.ScoreError
		switch {
		case errors.Is(err, ErrNotYourClasses):
			h.renderGradeSync(w, r, id, sa, e, policy, http.StatusForbidden, "Anda tidak mengampu mapel ini di kelas peserta mana pun, jadi tidak bisa mengirim nilainya. Hanya guru pengampu kelas yang bisa (sama dengan input nilai).")
			return
		case errors.Is(err, grades.ErrComponentInactive), errors.Is(err, ErrBadGradeComponent), errors.Is(err, ErrNoGradeComponent):
			h.flash(r, "Komponen tujuan tidak aktif. Pilih komponen lain.")
		case errors.As(err, &se):
			h.renderGradeSync(w, r, id, sa, e, policy, http.StatusUnprocessableEntity, "Ada nilai yang tidak valid; tidak ada nilai yang dikirim.")
			return
		case err != nil:
			h.fail(w, r, "send grades", err)
			return
		case res.Changed == 0:
			h.flash(r, fmt.Sprintf("Tidak ada nilai yang berubah. %d siswa tercatat sudah dikirim.", res.Students))
		default:
			h.flash(r, fmt.Sprintf("%d nilai rapor diperbarui. Nilai tampil di input nilai, rekap, dan rapor.", res.Changed))
		}
		if policy != "" {
			back += "?aturan=" + policy
		}
	default:
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
