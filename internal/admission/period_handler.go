package admission

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	periodsPath   = "/pendaftaran/periode"
	maxPeriodForm = 16 << 10
	msgPeriodGone = "Periode pendaftaran yang Anda cari tidak ada atau bukan milik sekolah Anda."
)

// Handler melayani halaman pendaftaran untuk staf sekolah.
type Handler struct {
	db         database.DB
	periods    *Service
	applicants *ApplicantService
	documents  *DocumentService
	sessions   *scs.SessionManager
	render     page.Renderer
	logger     *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DB, periods *Service, applicants *ApplicantService, documents *DocumentService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, periods: periods, applicants: applicants, documents: documents, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route pendaftaran staf ke r.
func (h *Handler) Routes(r chi.Router) {
	r.Route(applicantsPath, func(r chi.Router) {
		r.Route("/periode", h.periodRoutes)
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah))
			// Peran: admin_tu, kepala_sekolah (hanya baca).
			r.Get("/", h.listApplicants)
			// Peran: admin_tu, kepala_sekolah (hanya baca). Memuat NIK utuh.
			r.Get("/{id}", h.applicantDetail)
			// Peran: admin_tu, kepala_sekolah. Dokumen didekripsi dan diaudit.
			r.Get("/{id}/dokumen/{docID}", h.openDocument)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU))
			// Peran: admin_tu.
			r.Post("/{id}/status", h.setApplicantStatus)
			// Peran: admin_tu. Input pendaftar oleh TU (T92).
			r.Get("/baru", h.staffNew)
			// Peran: admin_tu.
			r.Post("/baru", h.staffCreate)
			// Peran: admin_tu.
			r.Get("/{id}/isi/{step}", h.staffStep)
			// Peran: admin_tu.
			r.Post("/{id}/isi/{step}", h.staffSaveStep)
			// Peran: admin_tu.
			r.Post("/{id}/dokumen/{type}", h.staffUpload)
			// Peran: admin_tu.
			r.Post("/{id}/kirim", h.staffSubmit)
		})
	})
}

// periodRoutes mendaftarkan route periode dan jalur (T89).
func (h *Handler) periodRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah))
		// Peran: admin_tu, kepala_sekolah (hanya baca).
		r.Get("/", h.listPeriods)
		// Peran: admin_tu, kepala_sekolah (hanya baca).
		r.Get("/{id}", h.periodDetail)
		// Peran: admin_tu, kepala_sekolah (hanya baca). Tes seleksi (T93a).
		r.Get("/{id}/tes", h.listTests)
		// Peran: admin_tu, kepala_sekolah (hanya baca).
		r.Get("/{id}/tes/{testID}/nilai", h.scores)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU))
		// Peran: admin_tu.
		r.Get("/baru", h.newPeriod)
		// Peran: admin_tu.
		r.Post("/", h.createPeriod)
		// Peran: admin_tu.
		r.Get("/{id}/ubah", h.editPeriod)
		// Peran: admin_tu.
		r.Post("/{id}", h.updatePeriod)
		// Peran: admin_tu.
		r.Post("/{id}/status", h.setStatus)
		// Peran: admin_tu.
		r.Post("/{id}/hapus", h.deletePeriod)
		// Peran: admin_tu.
		r.Post("/{id}/jalur", h.addTrack)
		// Peran: admin_tu.
		r.Post("/{id}/jalur/{trackID}", h.updateTrack)
		// Peran: admin_tu.
		r.Post("/{id}/jalur/{trackID}/hapus", h.deleteTrack)
		// Peran: admin_tu.
		r.Post("/{id}/tes", h.saveTest)
		// Peran: admin_tu.
		r.Post("/{id}/tes/{testID}", h.saveTest)
		// Peran: admin_tu.
		r.Post("/{id}/tes/{testID}/hapus", h.deleteTest)
		// Peran: admin_tu.
		r.Post("/{id}/tes/{testID}/nilai", h.saveScores)
	})
}

type option struct {
	Value, Label string
	Selected     bool
}

type periodRow struct {
	Period
	Opens, Closes string
	Fee           string
	OpenNow       bool
}

type periodListView struct {
	User    auth.Identity
	CanEdit bool
	Periods []periodRow
	Flash   string
	Error   string
}

type periodFormView struct {
	User   auth.Identity
	IsNew  bool
	ID     string
	Form   PeriodForm
	Years  []option
	Docs   []docOption
	Errors map[string]string
}

type docOption struct {
	Key, Label string
	Checked    bool
}

type trackRow struct {
	Track
	Form   TrackForm
	Errors map[string]string
}

type periodDetailView struct {
	User     auth.Identity
	CanEdit  bool
	Period   periodRow
	Docs     []string
	Tracks   []trackRow
	NewTrack TrackForm
	// TrackErrors berisi error form jalur baru (EditTrack kosong) atau
	// jalur yang diubah.
	TrackErrors map[string]string
	EditTrack   string
	// NewTrackErrors adalah error form tambah jalur.
	NewTrackErrors map[string]string
	Flash          string
	Error          string
}

func toRow(p Period) periodRow {
	r := periodRow{Period: p, Opens: clock.FormatDateID(p.OpensOn), Closes: clock.FormatDateID(p.ClosesOn), Fee: "Gratis"}
	if p.FeeAmount > 0 {
		r.Fee = money.FormatRupiah(p.FeeAmount)
	}
	return r
}

func (h *Handler) listPeriods(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	list, err := h.periods.List(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list periods", err)
		return
	}
	view := periodListView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, p := range list {
		row := toRow(p)
		row.OpenNow = p.IsOpen(h.periods.Today())
		view.Periods = append(view.Periods, row)
	}
	h.render.Render(w, r, http.StatusOK, "admission/periods", view)
}

func (h *Handler) periodDetail(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderDetail(w, r, id, chi.URLParam(r, "id"), http.StatusOK, "", TrackForm{}, nil, "")
}

func (h *Handler) renderDetail(w http.ResponseWriter, r *http.Request, id auth.Identity, periodID string, status int, editTrack string, tf TrackForm, errs map[string]string, msg string) {
	p, err := h.periods.Get(r.Context(), id.SchoolID, periodID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	row := toRow(p)
	row.OpenNow = p.IsOpen(h.periods.Today())
	view := periodDetailView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Period: row, EditTrack: editTrack,
		TrackErrors: errs, Error: msg, Flash: auth.PopFlash(r.Context(), h.sessions)}
	for _, d := range Docs {
		if p.Requires(d.Key) {
			view.Docs = append(view.Docs, d.Label)
		}
	}
	for _, t := range p.Tracks {
		tr := trackRow{Track: t, Form: TrackForm{Name: t.Name, UsesTest: t.UsesTest}}
		if t.Quota > 0 {
			tr.Form.Quota = strconv.Itoa(t.Quota)
		}
		if editTrack == t.ID {
			tr.Form, tr.Errors = tf, errs
		}
		view.Tracks = append(view.Tracks, tr)
	}
	if editTrack == "" {
		view.NewTrack, view.NewTrackErrors = tf, errs
	}
	h.render.Render(w, r, status, "admission/period_detail", view)
}

func (h *Handler) newPeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderForm(w, r, periodFormView{User: id, IsNew: true, Form: PeriodForm{TargetGrade: "1"}}, http.StatusOK)
}

func (h *Handler) editPeriod(w http.ResponseWriter, r *http.Request) {
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
	f := PeriodForm{
		AcademicYearID: p.AcademicYearID, Name: p.Name, TargetGrade: strconv.Itoa(p.TargetGrade),
		OpensOn: p.OpensOn.Format(dateLayout), ClosesOn: p.ClosesOn.Format(dateLayout),
		FeeRequired: p.FeeRequiredBeforeVerify, RequiredDocs: p.RequiredDocs,
	}
	if p.FeeAmount > 0 {
		f.FeeAmount = money.Group(p.FeeAmount)
	}
	h.renderForm(w, r, periodFormView{User: id, ID: p.ID, Form: f}, http.StatusOK)
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, view periodFormView, status int) {
	years, err := school.ListAcademicYears(r.Context(), h.db, view.User.SchoolID)
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
	for _, d := range Docs {
		checked := false
		for _, k := range view.Form.RequiredDocs {
			checked = checked || k == d.Key
		}
		view.Docs = append(view.Docs, docOption{Key: d.Key, Label: d.Label, Checked: checked})
	}
	h.render.Render(w, r, status, "admission/period_form", view)
}

func (h *Handler) parsePeriod(w http.ResponseWriter, r *http.Request) (PeriodForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return PeriodForm{}, false
	}
	p := r.PostForm
	return PeriodForm{
		AcademicYearID: p.Get("academic_year_id"), Name: p.Get("name"), TargetGrade: p.Get("target_grade"),
		OpensOn: p.Get("opens_on"), ClosesOn: p.Get("closes_on"), FeeAmount: p.Get("fee_amount"),
		FeeRequired: p.Has("fee_required"), RequiredDocs: p["required_docs"],
	}, true
}

func (h *Handler) createPeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := h.parsePeriod(w, r)
	if !ok {
		return
	}
	p, err := h.periods.Create(r.Context(), actorFrom(r, id), f)
	if h.periodFormError(w, r, err, periodFormView{User: id, IsNew: true, Form: f}) {
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Periode "+p.Name+" dibuat. Tambahkan jalur pendaftaran sebelum membukanya.")
	http.Redirect(w, r, periodsPath+"/"+p.ID, http.StatusSeeOther)
}

func (h *Handler) updatePeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	f, ok := h.parsePeriod(w, r)
	if !ok {
		return
	}
	p, err := h.periods.Update(r.Context(), actorFrom(r, id), periodID, f)
	if h.periodFormError(w, r, err, periodFormView{User: id, ID: periodID, Form: f}) {
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Periode "+p.Name+" diubah.")
	http.Redirect(w, r, periodsPath+"/"+p.ID, http.StatusSeeOther)
}

func (h *Handler) periodFormError(w http.ResponseWriter, r *http.Request, err error, view periodFormView) bool {
	if err == nil {
		return false
	}
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		view.Errors = ve.Fields
		h.renderForm(w, r, view, http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, view.User, msgPeriodGone)
	default:
		h.serverError(w, r, "save period", err)
	}
	return true
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	p, err := h.periods.SetStatus(r.Context(), actorFrom(r, id), periodID, r.PostForm.Get("status"))
	var ve *ValidationError
	switch {
	case err == nil:
		msg := map[string]string{StatusOpen: "Pendaftaran " + p.Name + " dibuka.", StatusClosed: "Pendaftaran " + p.Name + " ditutup.", StatusDraft: "Periode " + p.Name + " dikembalikan ke draf."}[p.Status]
		auth.SetFlash(r.Context(), h.sessions, msg)
		http.Redirect(w, r, periodsPath+"/"+periodID, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.Is(err, ErrNoTracks):
		h.renderDetail(w, r, id, periodID, http.StatusConflict, "", TrackForm{}, nil, "Tambahkan minimal satu jalur sebelum membuka pendaftaran.")
	case errors.As(err, &ve):
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	default:
		h.serverError(w, r, "set period status", err)
	}
}

func (h *Handler) deletePeriod(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	err := h.periods.Delete(r.Context(), actorFrom(r, id), periodID)
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Periode dihapus.")
		http.Redirect(w, r, periodsPath, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.Is(err, ErrInUse):
		h.renderDetail(w, r, id, periodID, http.StatusConflict, "", TrackForm{}, nil, "Hanya periode berstatus draf dan belum punya pendaftar yang bisa dihapus. Tutup periode ini jika tidak dipakai lagi.")
	default:
		h.serverError(w, r, "delete period", err)
	}
}

func parseTrack(w http.ResponseWriter, r *http.Request) (TrackForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return TrackForm{}, false
	}
	return TrackForm{Name: r.PostForm.Get("track_name"), Quota: r.PostForm.Get("quota"), UsesTest: r.PostForm.Has("uses_test")}, true
}

func (h *Handler) addTrack(w http.ResponseWriter, r *http.Request) {
	h.saveTrack(w, r, "")
}

func (h *Handler) updateTrack(w http.ResponseWriter, r *http.Request) {
	h.saveTrack(w, r, chi.URLParam(r, "trackID"))
}

func (h *Handler) saveTrack(w http.ResponseWriter, r *http.Request, trackID string) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	f, ok := parseTrack(w, r)
	if !ok {
		return
	}
	var t Track
	var err error
	if trackID == "" {
		t, err = h.periods.AddTrack(r.Context(), actorFrom(r, id), periodID, f)
	} else {
		t, err = h.periods.UpdateTrack(r.Context(), actorFrom(r, id), periodID, trackID, f)
	}
	var ve *ValidationError
	switch {
	case err == nil:
		verb := "ditambahkan"
		if trackID != "" {
			verb = "diubah"
		}
		auth.SetFlash(r.Context(), h.sessions, "Jalur "+t.Name+" "+verb+".")
		http.Redirect(w, r, periodsPath+"/"+periodID+"#jalur", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.As(err, &ve):
		h.renderDetail(w, r, id, periodID, http.StatusBadRequest, trackID, f, ve.Fields, "")
	default:
		h.serverError(w, r, "save track", err)
	}
}

func (h *Handler) deleteTrack(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	err := h.periods.DeleteTrack(r.Context(), actorFrom(r, id), periodID, chi.URLParam(r, "trackID"))
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Jalur dihapus.")
		http.Redirect(w, r, periodsPath+"/"+periodID+"#jalur", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.Is(err, ErrNoTracks):
		h.renderDetail(w, r, id, periodID, http.StatusConflict, "", TrackForm{}, nil, "Periode yang sedang dibuka harus punya minimal satu jalur.")
	case errors.Is(err, ErrInUse):
		h.renderDetail(w, r, id, periodID, http.StatusConflict, "", TrackForm{}, nil, "Jalur yang sudah punya pendaftar tidak bisa dihapus.")
	default:
		h.serverError(w, r, "delete track", err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func actorFrom(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
