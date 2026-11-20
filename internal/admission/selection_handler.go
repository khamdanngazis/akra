package admission

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/exam"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

type testRow struct {
	SelectionTest
	Schedule string
	Form     TestForm
	Errors   map[string]string
	// Online: ringkasan ujian online (T149).
	Online *onlineTestRow
}

type testsView struct {
	User         auth.Identity
	CanEdit      bool
	Period       Period
	UsesTest     bool
	Tests        []testRow
	Total        int
	NewTest      TestForm
	NewErrors    map[string]string
	Flash, Error string
	// Zone adalah singkatan zona waktu sekolah untuk label jadwal (T126).
	Zone string
	// Exams: ujian seleksi yang bisa dipilih untuk tes online (T149).
	Exams []exam.AdmissionExam
	// Practice: sesi latihan pendaftar yang masih buka.
	Practice  *sessionRow
	HasOnline bool
}

func (h *Handler) selection() *SelectionService { return NewSelectionService(h.db) }

// listTests menampilkan tes seleksi periode. Peran: admin_tu (ubah),
// kepala_sekolah (baca).
func (h *Handler) listTests(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderTests(w, r, id, chi.URLParam(r, "id"), http.StatusOK, "", TestForm{}, nil, "")
}

func (h *Handler) renderTests(w http.ResponseWriter, r *http.Request, id auth.Identity, periodID string, status int, editID string, f TestForm, errs map[string]string, msg string) {
	p, err := h.periods.Get(r.Context(), id.SchoolID, periodID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	tests, err := h.selection().Tests(r.Context(), id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "list tests", err)
		return
	}
	view := testsView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Period: p, Total: WeightsTotal(tests), Error: msg, Zone: clock.ZoneLabel(id.Location()),
		Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, t := range p.Tracks {
		view.UsesTest = view.UsesTest || t.UsesTest
	}
	if view.Exams, err = exam.AdmissionExams(r.Context(), h.db, id.SchoolID); err != nil {
		h.serverError(w, r, "admission exams", err)
		return
	}
	for _, t := range tests {
		row := testRow{SelectionTest: t, Form: TestForm{Name: t.Name, Weight: strconv.Itoa(t.Weight), MaxScore: strconv.Itoa(t.MaxScore), Location: t.Location, Rubric: t.Rubric,
			Method: TestMethodPaper, ExamID: t.ExamID}}
		if t.Online() {
			row.Form.Method = TestMethodOnline
			if row.Online, err = h.loadOnlineTest(r.Context(), id.SchoolID, t.ExamID, p.ID, id.Location()); err != nil {
				h.serverError(w, r, "online test", err)
				return
			}
			view.HasOnline = true
			if pr := row.Online.Practice; pr != nil && view.Practice == nil {
				view.Practice = &sessionRow{AdmissionSession: *pr, When: sessionWhen(*pr, id.Location())}
			}
		}
		if t.PassScore > 0 {
			row.Form.PassScore = strconv.Itoa(t.PassScore)
		}
		if t.ScheduledAt != nil {
			row.Schedule = clock.FormatDateTimeID(*t.ScheduledAt, id.Location())
			row.Form.ScheduledAt = t.ScheduledAt.In(id.Location()).Format(scheduleLayout)
		}
		if editID == t.ID {
			row.Form, row.Errors = f, errs
		}
		view.Tests = append(view.Tests, row)
	}
	if editID == "" {
		view.NewTest, view.NewErrors = f, errs
	}
	h.render.Render(w, r, status, "admission/tests", view)
}

func parseTestForm(w http.ResponseWriter, r *http.Request) (TestForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return TestForm{}, false
	}
	p := r.PostForm
	return TestForm{Name: p.Get("test_name"), Weight: p.Get("weight"), MaxScore: p.Get("max_score"), ScheduledAt: p.Get("scheduled_at"), Location: p.Get("location"),
		Rubric: p.Get("rubric"), PassScore: p.Get("pass_score"), Method: p.Get("method"), ExamID: p.Get("exam_id")}, true
}

// saveTest menambah atau mengubah tes. Peran: admin_tu.
func (h *Handler) saveTest(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID, testID := chi.URLParam(r, "id"), chi.URLParam(r, "testID")
	f, ok := parseTestForm(w, r)
	if !ok {
		return
	}
	var err error
	if testID == "" {
		_, err = h.selection().AddTest(r.Context(), actorFrom(r, id), periodID, f)
	} else {
		err = h.selection().UpdateTest(r.Context(), actorFrom(r, id), periodID, testID, f)
	}
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Tes "+strings.TrimSpace(f.Name)+" disimpan.")
		http.Redirect(w, r, periodsPath+"/"+periodID+"/tes", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.As(err, &ve):
		h.renderTests(w, r, id, periodID, http.StatusBadRequest, testID, f, ve.Fields, "")
	default:
		h.serverError(w, r, "save test", err)
	}
}

// deleteTest menghapus tes yang belum punya nilai. Peran: admin_tu.
func (h *Handler) deleteTest(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	err := h.selection().DeleteTest(r.Context(), actorFrom(r, id), periodID, chi.URLParam(r, "testID"))
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Tes dihapus.")
		http.Redirect(w, r, periodsPath+"/"+periodID+"/tes", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.Is(err, ErrInUse):
		h.renderTests(w, r, id, periodID, http.StatusConflict, "", TestForm{}, nil, "Tes yang sudah punya nilai tidak bisa dihapus.")
	default:
		h.serverError(w, r, "delete test", err)
	}
}

type scoresView struct {
	User         auth.Identity
	CanEdit      bool
	Period       Period
	Test         SelectionTest
	Rows         []ScoreRow
	Errors       map[string]string
	Flash, Error string
}

// scores menampilkan grid nilai satu tes. Peran: admin_tu (ubah),
// kepala_sekolah (baca).
func (h *Handler) scores(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderScores(w, r, id, http.StatusOK, nil, nil)
}

func (h *Handler) renderScores(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, submitted, errs map[string]string) {
	periodID, testID := chi.URLParam(r, "id"), chi.URLParam(r, "testID")
	p, err := h.periods.Get(r.Context(), id.SchoolID, periodID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	tests, err := h.selection().Tests(r.Context(), id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "list tests", err)
		return
	}
	view := scoresView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Period: p, Errors: errs, Flash: auth.PopFlash(r.Context(), h.sessions)}
	found := false
	for _, t := range tests {
		if t.ID == testID {
			view.Test, found = t, true
		}
	}
	if !found {
		page.NotFound(h.render, w, r, id, "Tes yang Anda cari tidak ada.")
		return
	}
	view.Rows, err = h.selection().Grid(r.Context(), id.SchoolID, p.ID, testID)
	if err != nil {
		h.serverError(w, r, "score grid", err)
		return
	}
	for i := range view.Rows {
		if v, ok := submitted[view.Rows[i].ApplicantID]; ok {
			view.Rows[i].Score = v
		}
	}
	if errs != nil {
		view.Error = "Nilai belum disimpan. Periksa baris yang ditandai."
	}
	h.render.Render(w, r, status, "admission/scores", view)
}

// saveScores menyimpan grid nilai. Peran: admin_tu.
func (h *Handler) saveScores(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID, testID := chi.URLParam(r, "id"), chi.URLParam(r, "testID")
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	scores := map[string]string{}
	for k, v := range r.PostForm {
		if aid, ok := strings.CutPrefix(k, "score_"); ok && len(v) > 0 {
			scores[aid] = v[0]
		}
	}
	err := h.selection().SaveScores(r.Context(), actorFrom(r, id), periodID, testID, scores)
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Nilai disimpan.")
		http.Redirect(w, r, periodsPath+"/"+periodID+"/tes/"+testID+"/nilai", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, "Tes atau pendaftar tidak ditemukan.")
	case errors.Is(err, ErrOnlineScores):
		auth.SetFlash(r.Context(), h.sessions, "Nilai tes online masuk otomatis dari ujian dan tidak bisa diketik. Koreksi lewat nilai ulang di hasil ujian.")
		http.Redirect(w, r, periodsPath+"/"+periodID+"/tes/"+testID+"/nilai", http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderScores(w, r, id, http.StatusBadRequest, scores, ve.Fields)
	default:
		h.serverError(w, r, "save scores", err)
	}
}

// testSheet mengunduh lembar penilaian satu tes (T119). Peran: admin_tu,
// kepala_sekolah.
func (h *Handler) testSheet(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	p, err := h.periods.Get(ctx, id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	tests, err := h.selection().Tests(ctx, id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "list tests", err)
		return
	}
	d := TestSheetData{SchoolName: id.SchoolName, PeriodName: p.Name, PrintedAt: time.Now().In(id.Location()), Loc: id.Location()}
	found := false
	for _, t := range tests {
		if t.ID == chi.URLParam(r, "testID") {
			d.Test, found = t, true
		}
	}
	if !found {
		page.NotFound(h.render, w, r, id, "Tes tidak ditemukan.")
		return
	}
	if d.Rows, err = testParticipants(ctx, h.db, id.SchoolID, p.ID); err != nil {
		h.serverError(w, r, "test participants", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="lembar-penilaian.pdf"`)
	w.Header().Set("Cache-Control", "private, no-store")
	if err := WriteTestSheetPDF(w, d, true); err != nil {
		h.logger.Error("write test sheet", "error", err)
	}
}
