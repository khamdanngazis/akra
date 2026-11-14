package admission

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/page"
)

// ActionApplicantCreate dicatat saat TU menginput pendaftar (T92).
const ActionApplicantCreate = "admission.applicant_create"

type staffNewView struct {
	User   auth.Identity
	Tracks []trackOption
	Form   DraftForm
	Choice string
	Errors map[string]string
	Error  string
	Flash  string
}

// staffNew menampilkan form pendaftar baru oleh TU. Semua periode bisa
// dipilih, termasuk yang sudah ditutup (pendaftar susulan). Peran: admin_tu.
func (h *Handler) staffNew(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderStaffNew(w, r, http.StatusOK, staffNewView{User: id})
}

func (h *Handler) renderStaffNew(w http.ResponseWriter, r *http.Request, status int, view staffNewView) {
	periods, err := h.periods.List(r.Context(), view.User.SchoolID)
	if err != nil {
		h.serverError(w, r, "list periods", err)
		return
	}
	today := h.periods.Today()
	for _, p := range periods {
		full, err := h.periods.Get(r.Context(), view.User.SchoolID, p.ID)
		if err != nil {
			h.serverError(w, r, "get period", err)
			return
		}
		for _, t := range full.Tracks {
			label := p.Name + " (" + p.AcademicYearName + ") · Jalur " + t.Name
			if !p.IsOpen(today) {
				label += " · di luar masa pendaftaran"
			}
			choice := p.ID + "|" + t.ID
			view.Tracks = append(view.Tracks, trackOption{PeriodID: p.ID, TrackID: t.ID, Label: label, Selected: choice == view.Choice})
		}
	}
	h.render.Render(w, r, status, "admission/staff_new", view)
}

// staffCreate membuat draf pendaftar atas nama orang tua. Peran: admin_tu.
func (h *Handler) staffCreate(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	choice := r.PostForm.Get("choice")
	periodID, trackID, _ := strings.Cut(choice, "|")
	f := DraftForm{PeriodID: periodID, TrackID: trackID, FullName: r.PostForm.Get("full_name"), Gender: r.PostForm.Get("gender"),
		BirthDate: r.PostForm.Get("birth_date"), Phone: r.PostForm.Get("phone"), Consent: r.PostForm.Has("consent")}
	a, _, err := h.applicants.CreateDraft(r.Context(), id.SchoolID, f, id.UserID)
	var ve *ValidationError
	switch {
	case err == nil:
		actor := actorFrom(r, id)
		if err := pgx.BeginFunc(r.Context(), h.db, func(tx pgx.Tx) error {
			return audit.Record(r.Context(), tx, audit.Entry{
				SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
				Action: ActionApplicantCreate, EntityType: "applicant", EntityID: a.ID,
				After: map[string]any{"period_id": a.PeriodID, "track_id": a.TrackID, "entered_by_staff": true},
			})
		}); err != nil {
			h.serverError(w, r, "audit applicant create", err)
			return
		}
		auth.SetFlash(r.Context(), h.sessions, "Draf pendaftar dibuat. Lengkapi data per langkah, lalu kirim.")
		http.Redirect(w, r, applicantsPath+"/"+a.ID+"/isi/"+StepChild, http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderStaffNew(w, r, http.StatusBadRequest, staffNewView{User: id, Form: f, Choice: choice, Errors: ve.Fields})
	default:
		h.serverError(w, r, "create applicant", err)
	}
}

// editable memuat pendaftar milik sekolah yang masih bisa diubah, atau
// menulis respons yang sesuai.
func (h *Handler) editable(w http.ResponseWriter, r *http.Request, id auth.Identity) (Applicant, schoolInfo, bool) {
	a, err := h.applicants.Load(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgApplicantGone)
		return Applicant{}, schoolInfo{}, false
	}
	if err != nil {
		h.serverError(w, r, "load applicant", err)
		return Applicant{}, schoolInfo{}, false
	}
	if !a.Editable() {
		auth.SetFlash(r.Context(), h.sessions, "Data pendaftar hanya bisa diubah saat berstatus draf atau perlu perbaikan.")
		http.Redirect(w, r, applicantsPath+"/"+a.ID, http.StatusSeeOther)
		return Applicant{}, schoolInfo{}, false
	}
	return a, schoolInfo{ID: id.SchoolID, Name: id.SchoolName}, true
}

func (h *Handler) renderStaffStep(w http.ResponseWriter, r *http.Request, id auth.Identity, s schoolInfo, a Applicant, step string, status int, submitted, errs map[string]string) {
	view, err := buildStepView(r.Context(), h.periods, h.documents, s, a, step, submitted, errs)
	if err != nil {
		h.serverError(w, r, "build step view", err)
		return
	}
	view.Base, view.Staff, view.User = applicantsPath+"/"+a.ID, true, id
	view.Flash = auth.PopFlash(r.Context(), h.sessions)
	h.render.Render(w, r, status, "admission/public_step", view)
}

// staffStep menampilkan satu langkah formulir untuk TU. Peran: admin_tu.
func (h *Handler) staffStep(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	step := chi.URLParam(r, "step")
	if !validStep(step) {
		page.NotFound(h.render, w, r, id, msgApplicantGone)
		return
	}
	a, s, ok := h.editable(w, r, id)
	if !ok {
		return
	}
	h.renderStaffStep(w, r, id, s, a, step, http.StatusOK, nil, nil)
}

// staffSaveStep menyimpan satu langkah formulir. Peran: admin_tu.
func (h *Handler) staffSaveStep(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	step := chi.URLParam(r, "step")
	if !validStep(step) || step == StepReview {
		page.NotFound(h.render, w, r, id, msgApplicantGone)
		return
	}
	a, s, ok := h.editable(w, r, id)
	if !ok {
		return
	}
	base := applicantsPath + "/" + a.ID
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	target := targetStep(step, r.PostForm.Get("go"))
	if step == StepDocument {
		http.Redirect(w, r, base+"/isi/"+target, http.StatusSeeOther)
		return
	}
	values := stepValues(r, step)
	_, err := h.applicants.SaveStep(r.Context(), id.SchoolID, a.ID, step, values)
	var ve *ValidationError
	switch {
	case err == nil:
		http.Redirect(w, r, base+"/isi/"+target, http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderStaffStep(w, r, id, s, a, step, http.StatusBadRequest, values, ve.Fields)
	case errors.Is(err, ErrNotEditable):
		http.Redirect(w, r, base, http.StatusSeeOther)
	default:
		h.serverError(w, r, "save applicant step", err)
	}
}

// staffUpload menerima satu dokumen pendaftar. Peran: admin_tu.
func (h *Handler) staffUpload(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	a, s, ok := h.editable(w, r, id)
	if !ok {
		return
	}
	docType := chi.URLParam(r, "type")
	if !validDocType(docType) || docType == "lainnya" {
		page.NotFound(h.render, w, r, id, "Jenis dokumen tidak dikenal.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxDocumentSize+64<<10)
	file, _, err := r.FormFile("file")
	if err != nil {
		h.renderStaffStep(w, r, id, s, a, StepDocument, http.StatusBadRequest, nil, map[string]string{docType: "Pilih file yang akan diunggah (maksimal 2 MB)."})
		return
	}
	defer file.Close()
	_, err = h.documents.Upload(r.Context(), id.SchoolID, a.ID, docType, file)
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, DocLabel(docType)+" berhasil diunggah.")
		http.Redirect(w, r, applicantsPath+"/"+a.ID+"/isi/"+StepDocument, http.StatusSeeOther)
	case docErrorMessage(err) != "":
		h.renderStaffStep(w, r, id, s, a, StepDocument, http.StatusBadRequest, nil, map[string]string{docType: docErrorMessage(err)})
	case errors.Is(err, ErrNotEditable):
		http.Redirect(w, r, applicantsPath+"/"+a.ID, http.StatusSeeOther)
	default:
		h.serverError(w, r, "upload document", err)
	}
}

// staffSubmit mengirim pendaftaran atas nama orang tua. Peran: admin_tu.
func (h *Handler) staffSubmit(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	a, s, ok := h.editable(w, r, id)
	if !ok {
		return
	}
	done, err := h.applicants.SubmitBy(r.Context(), id.SchoolID, a.ID, id.UserID)
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Pendaftaran dikirim dengan nomor "+done.RegistrationNumber+".")
		http.Redirect(w, r, applicantsPath+"/"+a.ID, http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderStaffStep(w, r, id, s, a, StepReview, http.StatusBadRequest, nil, nil)
	case errors.Is(err, ErrDuplicateNIK):
		auth.SetFlash(r.Context(), h.sessions, "Calon siswa dengan NIK ini sudah terdaftar di periode ini.")
		http.Redirect(w, r, applicantsPath+"/"+a.ID+"/isi/"+StepReview, http.StatusSeeOther)
	default:
		h.serverError(w, r, "submit applicant", err)
	}
}
