package exam

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// resultRoutes mendaftarkan hasil dan analisis ujian (T148). Baca: semua
// yang boleh melihat ujian (termasuk kepala sekolah); nilai manual: yang
// boleh mengubah ujian.
func (h *Handler) resultRoutes(readers, writers chi.Router) {
	readers.Get("/paket/{id}/hasil", h.resultsPage)
	readers.Get("/paket/{id}/hasil/butir", h.resultsPage)
	readers.Get("/paket/{id}/hasil/tp", h.resultsPage)
	readers.Get("/paket/{id}/hasil/peserta/{attempt}", h.resultDetail)
	readers.Get("/paket/{id}/hasil/analisis.xlsx", h.resultsXLSX)
	readers.Get("/paket/{id}/hasil/cetak", h.resultsPrint)
	writers.Post("/paket/{id}/hasil/peserta/{attempt}/nilai", h.resultManualScore)
}

func resultsPath(examID string) string { return "/ujian/paket/" + examID + "/hasil" }

// resultsView adalah isi halaman hasil.
type resultsView struct {
	User     auth.Identity
	Exam     Exam
	Tab      string // nilai, butir, tp
	A        ExamAnalysis
	CanWrite bool
	Regrades []Regrade
	Query    string // saringan aktif, untuk tautan
	KKTPLine int    // posisi garis KKTP di histogram (0–100)
	Flash    string
	Error    string
	// Rapor (T150).
	ToReport    bool
	GradesStale int
}

// filterOf membaca saringan dari query string.
func filterOf(r *http.Request) ResultFilter {
	q := r.URL.Query()
	return ResultFilter{ClassID: q.Get("kelas"), SessionID: q.Get("sesi"), VariantID: q.Get("paket")}
}

// loadAnalysis memuat ujian (akses baca) dan analisisnya.
func (h *Handler) loadAnalysis(w http.ResponseWriter, r *http.Request) (auth.Identity, StaffActor, Exam, ExamAnalysis, bool) {
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Read)
	if !ok {
		return id, sa, e, ExamAnalysis{}, false
	}
	a, err := LoadExamAnalysis(r.Context(), h.db, e, filterOf(r))
	if err != nil {
		h.fail(w, r, "load analysis", err)
		return id, sa, e, a, false
	}
	return id, sa, e, a, true
}

func (h *Handler) resultsPage(w http.ResponseWriter, r *http.Request) {
	id, sa, e, a, ok := h.loadAnalysis(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	now := time.Now()
	v := resultsView{User: id, Exam: e, Tab: "nilai", A: a, CanWrite: e.Access(sa.Actor, now) >= Write, Query: a.Filter.Query(),
		KKTPLine: a.KKTP.Value, Flash: auth.PopFlash(ctx, h.sessions)}
	if v.CanWrite && (e.FeedsGrades() || e.FollowUp != "") {
		v.ToReport = true
		var err error
		if v.GradesStale, err = GradesStale(ctx, h.db, sa.SchoolID, e.ID); err != nil {
			h.fail(w, r, "grades stale", err)
			return
		}
	}
	switch path := r.URL.Path; {
	case strings.HasSuffix(path, "/butir"):
		v.Tab = "butir"
		// Riwayat butir di bank soal (T127): disimpan dari analisis lengkap
		// (tanpa saringan) dan diperbarui setiap kali dibuka ulang, mis.
		// setelah susulan atau koreksi esai.
		if a.Filter.Empty() && a.Summary.Final > 0 {
			if err := StoreQuestionStats(ctx, h.db, e, a.Items, now); err != nil {
				h.logger.ErrorContext(ctx, "store question stats", "exam_id", e.ID, "error", err)
			}
		}
	case strings.HasSuffix(path, "/tp"):
		v.Tab = "tp"
	default:
		regs, err := ListRegrades(ctx, h.db, sa.SchoolID, e.ID)
		if err != nil {
			h.fail(w, r, "regrades", err)
			return
		}
		loc := id.Location()
		for i := range regs {
			regs[i].At = regs[i].At.In(loc)
		}
		v.Regrades = regs
	}
	tpl := map[string]string{"nilai": "exam/results", "butir": "exam/item_analysis", "tp": "exam/objective_analysis"}[v.Tab]
	h.render.Render(w, r, http.StatusOK, tpl, v)
}

// resultDetailView adalah detail hasil satu peserta.
type resultDetailView struct {
	User     auth.Identity
	Exam     Exam
	P        ParticipantResult
	Answers  []AnswerDetail
	Events   []AttemptEvent
	CanWrite bool
	Edit     string // soal yang formulir nilai manualnya dibuka (error)
	Errors   map[string]string
	Form     url.Values
	Flash    string
	Error    string
	Zone     string
}

func (h *Handler) resultDetail(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Read)
	if !ok {
		return
	}
	h.renderResultDetail(w, r, id, sa, e, http.StatusOK, "", nil, nil)
}

func (h *Handler) renderResultDetail(w http.ResponseWriter, r *http.Request, id auth.Identity, sa StaffActor, e Exam, status int, edit string, errs map[string]string, form url.Values) {
	ctx := r.Context()
	p, answers, err := LoadAttemptDetail(ctx, h.db, e, chi.URLParam(r, "attempt"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Hasil peserta tidak ditemukan.")
		return
	}
	if err != nil {
		h.fail(w, r, "attempt detail", err)
		return
	}
	events, err := ListAttemptEvents(ctx, h.db, sa.SchoolID, p.AttemptID, 200)
	if err != nil {
		h.fail(w, r, "attempt events", err)
		return
	}
	loc := id.Location()
	for i := range events {
		events[i].OccurredAt = events[i].OccurredAt.In(loc)
	}
	for i := range answers {
		answers[i].OverrideAt = answers[i].OverrideAt.In(loc)
	}
	v := resultDetailView{User: id, Exam: e, P: p, Answers: answers, Events: events, CanWrite: e.Access(sa.Actor, time.Now()) >= Write,
		Edit: edit, Errors: errs, Form: form, Flash: auth.PopFlash(ctx, h.sessions), Zone: clock.ZoneLabel(loc)}
	h.render.Render(w, r, status, "exam/result_detail", v)
}

// resultManualScore memberi atau menghapus nilai manual satu soal.
func (h *Handler) resultManualScore(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	pf := r.PostForm
	attemptID, qid := chi.URLParam(r, "attempt"), pf.Get("qid")
	var units *int
	if pf.Get("aksi") != "hapus" {
		// Poin maksimum diperiksa SetManualPoints; batas sementara 100.
		u, err := ParseEssayScore(pf.Get("skor"), 100)
		if err != nil {
			h.renderResultDetail(w, r, id, sa, e, http.StatusUnprocessableEntity, qid,
				map[string]string{"skor": "Isi poin berupa angka, paling banyak dua desimal."}, pf)
			return
		}
		units = &u
	}
	g, err := SetManualPoints(r.Context(), h.db, sa, e, attemptID, qid, units, pf.Get("alasan"), time.Now())
	var fe *FieldError
	switch {
	case errors.As(err, &fe):
		h.renderResultDetail(w, r, id, sa, e, http.StatusUnprocessableEntity, qid, fe.Fields, pf)
		return
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, "Soal atau hasil peserta tidak ditemukan.")
		return
	case errors.Is(err, ErrNotSubmitted):
		h.flash(r, "Peserta belum mengumpulkan; nilai manual hanya untuk hasil yang sudah dikumpulkan.")
	case err != nil:
		h.fail(w, r, "manual score", err)
		return
	case units == nil:
		h.flash(r, "Nilai manual dihapus; soal dinilai otomatis lagi. Nilai sekarang "+ScoreLabel(g.ScoreX100)+".")
	default:
		h.flash(r, "Nilai manual disimpan dan tercatat di audit. Nilai sekarang "+ScoreLabel(g.ScoreX100)+".")
	}
	http.Redirect(w, r, resultsPath(e.ID)+"/peserta/"+attemptID+"#soal-"+qid, http.StatusSeeOther)
}

// resultsPrintView adalah halaman cetak "Analisis Hasil Ulangan".
type resultsPrintView struct {
	resultsView
	Head      Letterhead
	PrintedAt string
	Teacher   string
	Scope     string // saringan aktif, mis. "Kelas 7A · Paket B"
}

func (h *Handler) resultsPrint(w http.ResponseWriter, r *http.Request) {
	id, sa, e, a, ok := h.loadAnalysis(w, r)
	if !ok {
		return
	}
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return
	}
	v := resultsPrintView{resultsView: resultsView{User: id, Exam: e, A: a, Query: a.Filter.Query(), KKTPLine: a.KKTP.Value},
		Head: lh, PrintedAt: clock.FormatDateID(time.Now().In(lh.Loc)), Teacher: e.OwnerName, Scope: "Semua peserta"}
	var scope []string
	for _, opts := range [][]optionItem{a.Classes, a.Sessions, a.Variants} {
		for _, o := range opts {
			if o.Selected {
				scope = append(scope, o.Label)
			}
		}
	}
	if len(scope) > 0 {
		v.Scope = strings.Join(scope, " · ")
	}
	h.render.Render(w, r, http.StatusOK, "exam/analysis_print", v)
}

func (h *Handler) resultsXLSX(w http.ResponseWriter, r *http.Request) {
	_, _, e, a, ok := h.loadAnalysis(w, r)
	if !ok {
		return
	}
	data, err := AnalysisXLSX(a)
	if err != nil {
		h.fail(w, r, "analysis xlsx", err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFileName("analisis-"+e.Name)+`.xlsx"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
