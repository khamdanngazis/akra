package admission

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/school"
)

// Langkah wizard gelombang pendaftaran (T117).
const (
	stepBasic    = "dasar"
	stepSchedule = "jadwal"
	stepTracks   = "jalur"
	stepDocs     = "dokumen"
)

var wizardSteps = []struct{ Key, Title string }{
	{stepBasic, "Dasar"}, {stepSchedule, "Jadwal & biaya"}, {stepTracks, "Jalur"}, {stepDocs, "Dokumen & buka"},
}

func validWizardStep(step string) bool {
	for _, s := range wizardSteps {
		if s.Key == step {
			return true
		}
	}
	return false
}

func nextWizardStep(step string) string {
	for i, s := range wizardSteps {
		if s.Key == step && i+1 < len(wizardSteps) {
			return wizardSteps[i+1].Key
		}
	}
	return ""
}

// wizardStepLink adalah satu langkah di kepala wizard.
type wizardStepLink struct {
	N          int
	Key, Title string
	URL        string // kosong untuk gelombang yang belum dibuat
	State      string // done, current, todo
}

type wizardView struct {
	User       auth.Identity
	IsNew      bool
	Step       string
	StepTitle  string
	Steps      []wizardStepLink
	Period     periodRow
	SchoolCode string
	Form       PeriodForm
	Years      []option
	Grades     []option
	Docs       []docOption
	Errors     map[string]string
	// Langkah jalur.
	Tracks         []trackRow
	NewTrack       TrackForm
	NewTrackErrors map[string]string
	EditTrack      string
	EditForm       TrackForm
	EditErrors     map[string]string
	// Langkah dokumen: daftar periksa sebelum dibuka.
	Checklist []checkItem
	Ready     bool
	Flash     string
	Error     string
}

// gradeLabels menjelaskan tingkat awal jenjang.
var gradeLabels = map[int]string{1: "Kelas 1 (awal SD/MI)", 7: "Kelas 7 (awal SMP/MTs)", 10: "Kelas 10 (awal SMA/SMK/MA)"}

func gradeOptionsFor(selected string) []option {
	var out []option
	for g := 1; g <= 12; g++ {
		label := gradeLabels[g]
		if label == "" {
			label = "Kelas " + strconv.Itoa(g)
		}
		v := strconv.Itoa(g)
		out = append(out, option{Value: v, Label: label, Selected: v == selected})
	}
	return out
}

// formOf mengubah periode tersimpan menjadi isian form.
func formOf(p Period) PeriodForm {
	f := PeriodForm{AcademicYearID: p.AcademicYearID, Name: p.Name, Kind: p.Kind, OpensOn: p.OpensOn.Format(dateLayout),
		ClosesOn: p.ClosesOn.Format(dateLayout), FeeRequired: p.FeeRequiredBeforeVerify, RequiredDocs: p.RequiredDocs}
	if p.TargetGrade > 0 {
		f.TargetGrade = strconv.Itoa(p.TargetGrade)
	}
	if p.FeeAmount > 0 {
		f.FeeAmount = money.Group(p.FeeAmount)
	}
	return f
}

// newPeriod menampilkan langkah pertama wizard untuk gelombang baru.
func (h *Handler) newPeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderWizard(w, r, http.StatusOK, wizardView{User: id, IsNew: true, Step: stepBasic, Form: PeriodForm{TargetGrade: "1", Kind: KindNew}})
}

// editPeriod mengarahkan tautan lama ke langkah pertama wizard.
func (h *Handler) editPeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	p, ok := h.loadPeriod(w, r, id, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	http.Redirect(w, r, periodsPath+"/"+p.ID+"/atur/"+stepBasic, http.StatusSeeOther)
}

// wizardStep menampilkan satu langkah wizard gelombang yang sudah dibuat.
func (h *Handler) wizardStep(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	step := chi.URLParam(r, "step")
	if !validWizardStep(step) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	p, ok := h.loadPeriod(w, r, id, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.renderWizard(w, r, http.StatusOK, wizardView{User: id, Step: step, Period: toRow(p), Form: formOf(p)})
}

func (h *Handler) loadPeriod(w http.ResponseWriter, r *http.Request, id auth.Identity, periodID string) (Period, bool) {
	p, err := h.periods.Get(r.Context(), id.SchoolID, periodID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return Period{}, false
	}
	if err != nil {
		h.serverError(w, r, "get period", err)
		return Period{}, false
	}
	return p, true
}

func (h *Handler) renderWizard(w http.ResponseWriter, r *http.Request, status int, view wizardView) {
	ctx := r.Context()
	if view.Flash == "" {
		view.Flash = auth.PopFlash(ctx, h.sessions)
	}
	reached := true
	for i, s := range wizardSteps {
		l := wizardStepLink{N: i + 1, Key: s.Key, Title: s.Title, State: "todo"}
		if s.Key == view.Step {
			l.State, view.StepTitle, reached = "current", s.Title, false
		} else if reached {
			l.State = "done"
		}
		if !view.IsNew {
			l.URL = periodsPath + "/" + view.Period.ID + "/atur/" + s.Key
		}
		view.Steps = append(view.Steps, l)
	}
	switch view.Step {
	case stepBasic:
		years, err := school.ListAcademicYears(ctx, h.db, view.User.SchoolID)
		if err != nil {
			h.serverError(w, r, "list academic years", err)
			return
		}
		for _, y := range years {
			label := y.Name
			if y.IsActive {
				label += " (aktif)"
			}
			sel := y.ID == view.Form.AcademicYearID || (view.Form.AcademicYearID == "" && y.IsActive)
			view.Years = append(view.Years, option{Value: y.ID, Label: label, Selected: sel})
		}
		view.Grades = gradeOptionsFor(view.Form.TargetGrade)
	case stepTracks:
		for _, t := range view.Period.Tracks {
			tr := trackRow{Track: t, Form: trackFormOf(t)}
			if t.ID == view.EditTrack {
				tr.Form, tr.Errors = view.EditForm, view.EditErrors
			}
			view.Tracks = append(view.Tracks, tr)
		}
	case stepDocs:
		for _, d := range Docs {
			if view.Period.Kind != KindTransfer && (d.Key == "surat_pindah" || d.Key == "rapor") {
				continue
			}
			checked := false
			for _, k := range view.Form.RequiredDocs {
				checked = checked || k == d.Key
			}
			view.Docs = append(view.Docs, docOption{Key: d.Key, Label: d.Label, Checked: checked})
		}
		tests, err := h.selection().Tests(ctx, view.User.SchoolID, view.Period.ID)
		if err != nil {
			h.serverError(w, r, "list tests", err)
			return
		}
		view.Checklist, view.Ready = periodChecklist(view.Period.Period, tests, h.periods.Today())
		if err := h.db.QueryRow(ctx, `SELECT code FROM schools WHERE id = $1`, view.User.SchoolID).Scan(&view.SchoolCode); err != nil {
			h.serverError(w, r, "school code", err)
			return
		}
	}
	h.render.Render(w, r, status, "admission/period_wizard", view)
}

// createPeriod membuat gelombang draf dari langkah pertama wizard. Jadwal
// diisi sementara (hari ini sampai 30 hari) dan diubah di langkah kedua.
func (h *Handler) createPeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := h.parsePeriod(w, r)
	if !ok {
		return
	}
	today := h.periods.Today()
	if f.OpensOn == "" && f.ClosesOn == "" {
		f.OpensOn, f.ClosesOn = today.Format(dateLayout), today.AddDate(0, 0, 30).Format(dateLayout)
	}
	if r.PostForm["required_docs"] == nil {
		f.RequiredDocs = []string{"kk", "akta", "foto"}
		if f.Kind == KindTransfer {
			f.RequiredDocs = []string{"kk", "akta", "surat_pindah", "rapor"}
		}
	}
	p, err := h.periods.Create(r.Context(), actorFrom(r, id), f)
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, p.Name+" dibuat sebagai draf. Lanjutkan mengatur jadwal dan biaya.")
		http.Redirect(w, r, periodsPath+"/"+p.ID+"/atur/"+stepSchedule, http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderWizard(w, r, http.StatusBadRequest, wizardView{User: id, IsNew: true, Step: stepBasic, Form: f, Errors: ve.Fields})
	default:
		h.serverError(w, r, "create period", err)
	}
}

// saveWizardStep menyimpan isian satu langkah (dasar, jadwal, dokumen)
// di atas data gelombang tersimpan, lalu ke langkah berikutnya. Di langkah
// dokumen, action=buka sekaligus membuka pendaftaran.
func (h *Handler) saveWizardStep(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	step := chi.URLParam(r, "step")
	if step != stepBasic && step != stepSchedule && step != stepDocs {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	posted, ok := h.parsePeriod(w, r)
	if !ok {
		return
	}
	p, ok := h.loadPeriod(w, r, id, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	f := formOf(p)
	switch step {
	case stepBasic:
		f.Name, f.AcademicYearID, f.TargetGrade = posted.Name, posted.AcademicYearID, posted.TargetGrade
	case stepSchedule:
		f.OpensOn, f.ClosesOn, f.FeeAmount, f.FeeRequired = posted.OpensOn, posted.ClosesOn, posted.FeeAmount, posted.FeeRequired
	case stepDocs:
		f.RequiredDocs = posted.RequiredDocs
	}
	updated, err := h.periods.Update(r.Context(), actorFrom(r, id), p.ID, f)
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		h.renderWizard(w, r, http.StatusBadRequest, wizardView{User: id, Step: step, Period: toRow(p), Form: f, Errors: ve.Fields})
		return
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	case err != nil:
		h.serverError(w, r, "save period step", err)
		return
	}
	detail := periodsPath + "/" + p.ID
	switch {
	case r.PostForm.Get("action") == "keluar":
		auth.SetFlash(r.Context(), h.sessions, updated.Name+" disimpan.")
		http.Redirect(w, r, detail, http.StatusSeeOther)
	case step == stepDocs && r.PostForm.Get("action") == "buka":
		h.openPeriod(w, r, id, updated, true)
	case step == stepDocs:
		msg := updated.Name + " disimpan."
		if updated.Status == StatusDraft {
			msg = updated.Name + " disimpan sebagai draf. Buka pendaftaran dari halaman ini jika sudah siap."
		}
		auth.SetFlash(r.Context(), h.sessions, msg)
		http.Redirect(w, r, detail, http.StatusSeeOther)
	default:
		http.Redirect(w, r, detail+"/atur/"+nextWizardStep(step), http.StatusSeeOther)
	}
}

// openPeriod membuka pendaftaran jika daftar periksa terpenuhi; inWizard
// menentukan halaman yang menampilkan kegagalan (wizard atau detail).
func (h *Handler) openPeriod(w http.ResponseWriter, r *http.Request, id auth.Identity, p Period, inWizard bool) {
	tests, err := h.selection().Tests(r.Context(), id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "list tests", err)
		return
	}
	if _, ready := periodChecklist(p, tests, h.periods.Today()); !ready {
		if !inWizard {
			h.renderDetail(w, r, id, p.ID, http.StatusConflict, "", TrackForm{}, nil, "Gelombang belum bisa dibuka. Lengkapi butir yang belum terpenuhi di daftar periksa.")
			return
		}
		h.renderWizard(w, r, http.StatusConflict, wizardView{User: id, Step: stepDocs, Period: toRow(p), Form: formOf(p),
			Error: "Gelombang belum bisa dibuka. Lengkapi butir yang belum terpenuhi di daftar periksa."})
		return
	}
	if _, err := h.periods.SetStatus(r.Context(), actorFrom(r, id), p.ID, StatusOpen); err != nil {
		h.serverError(w, r, "open period", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Pendaftaran "+p.Name+" dibuka. Bagikan tautan formulir ke orang tua.")
	http.Redirect(w, r, periodsPath+"/"+p.ID, http.StatusSeeOther)
}

// copyPeriod menyalin gelombang lalu membuka wizard salinannya.
func (h *Handler) copyPeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	p, err := h.periods.Copy(r.Context(), actorFrom(r, id), chi.URLParam(r, "id"))
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Salinan dibuat dengan jalur dan tes yang sama. Ubah nama dan jadwalnya.")
		http.Redirect(w, r, periodsPath+"/"+p.ID+"/atur/"+stepBasic, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	default:
		h.serverError(w, r, "copy period", err)
	}
}

func (h *Handler) parsePeriod(w http.ResponseWriter, r *http.Request) (PeriodForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return PeriodForm{}, false
	}
	p := r.PostForm
	return PeriodForm{
		AcademicYearID: p.Get("academic_year_id"), Name: p.Get("name"), Kind: p.Get("kind"), TargetGrade: p.Get("target_grade"),
		OpensOn: p.Get("opens_on"), ClosesOn: p.Get("closes_on"), FeeAmount: p.Get("fee_amount"),
		FeeRequired: p.Has("fee_required"), RequiredDocs: p["required_docs"],
	}, true
}

// fromWizard melaporkan apakah aksi jalur dikirim dari langkah jalur
// wizard (field kembali=atur), sehingga hasilnya kembali ke wizard.
func fromWizard(r *http.Request) bool { return r.PostForm.Get("kembali") == "atur" }

// renderTrackError menampilkan ulang langkah jalur wizard dengan error.
func (h *Handler) renderWizardTracks(w http.ResponseWriter, r *http.Request, id auth.Identity, periodID string, status int, view wizardView) {
	p, ok := h.loadPeriod(w, r, id, periodID)
	if !ok {
		return
	}
	view.User, view.Step, view.Period, view.Form = id, stepTracks, toRow(p), formOf(p)
	h.renderWizard(w, r, status, view)
}
