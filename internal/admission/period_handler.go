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
	msgPeriodGone = "Gelombang pendaftaran yang Anda cari tidak ada atau bukan milik sekolah Anda."
)

// Handler melayani halaman pendaftaran untuk staf sekolah.
type Handler struct {
	db         database.DB
	periods    *Service
	applicants *ApplicantService
	documents  *DocumentService
	logos      *LogoService
	sessions   *scs.SessionManager
	render     page.Renderer
	logger     *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DB, periods *Service, applicants *ApplicantService, documents *DocumentService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, periods: periods, applicants: applicants, documents: documents, logos: logoServiceFor(db, documents),
		sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route pendaftaran staf ke r.
func (h *Handler) Routes(r chi.Router) {
	r.Route(applicantsPath, func(r chi.Router) {
		r.Route("/periode", h.periodRoutes)
		// Biaya formulir (T94).
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleBendahara, auth.RoleAdminTU, auth.RoleKepalaSekolah))
			// Peran: bendahara, admin_tu, kepala_sekolah (baca).
			r.Get("/biaya", h.listFees)
			// Peran: bendahara, admin_tu, kepala_sekolah.
			r.Get("/{id}/biaya/{paymentID}/kuitansi", h.feeReceipt)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleBendahara))
			// Peran: bendahara.
			r.Post("/{id}/biaya", h.recordFee)
			// Peran: bendahara.
			r.Post("/{id}/biaya/batal", h.voidFee)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah))
			// Peran: admin_tu, kepala_sekolah (hanya baca).
			r.Get("/", h.listApplicants)
			// Peran: admin_tu, kepala_sekolah (hanya baca). Penerimaan (T96).
			r.Get("/terima", h.enrollPage)
			// Peran: admin_tu, kepala_sekolah (hanya baca). Pengaturan pendaftaran (T120).
			r.Get("/pengaturan", h.settingsPage)
			// Peran: admin_tu, kepala_sekolah. Rekap (T97).
			r.Get("/rekap", h.summary)
			// Peran: admin_tu, kepala_sekolah. Memuat NIK; dicatat di audit.
			r.Get("/rekap/pendaftar.xlsx", h.exportApplicants)
			// Peran: admin_tu, kepala_sekolah (hanya baca). Memuat NIK utuh.
			r.Get("/{id}", h.applicantDetail)
			// Peran: admin_tu, kepala_sekolah. Dokumen didekripsi dan diaudit.
			r.Get("/{id}/dokumen/{docID}", h.openDocument)
			// Peran: admin_tu, kepala_sekolah. Bukti pendaftaran (T95).
			r.Get("/{id}/bukti.pdf", h.staffProof)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU))
			// Peran: admin_tu.
			r.Post("/{id}/status", h.setApplicantStatus)
			// Peran: admin_tu. Hapus data pribadi atas permintaan (T113).
			r.Post("/{id}/hapus-data", h.eraseApplicant)
			// Peran: admin_tu.
			r.Post("/terima", h.enroll)
			// Peran: admin_tu. Pengaturan pendaftaran (T120).
			r.Post("/pengaturan", h.saveSettings)
			// Peran: admin_tu.
			r.Post("/{id}/jadikan-siswa", h.enrollOne)
			// Peran: admin_tu.
			r.Post("/{id}/daftar-ulang", h.markReRegistered)
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
		// Peran: admin_tu, kepala_sekolah (hanya baca). Peringkat (T93b).
		r.Get("/{id}/hasil", h.results)
		// Peran: admin_tu, kepala_sekolah.
		r.Get("/{id}/hasil.xlsx", h.exportResults)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU))
		// Peran: admin_tu.
		r.Get("/baru", h.newPeriod)
		// Peran: admin_tu.
		r.Post("/", h.createPeriod)
		// Peran: admin_tu. Logo sekolah untuk halaman pendaftaran (T111).
		r.Post("/logo", h.uploadLogo)
		// Peran: admin_tu.
		r.Post("/logo/hapus", h.removeLogo)
		// Peran: admin_tu. Tautan lama ke form ubah diarahkan ke wizard.
		r.Get("/{id}/ubah", h.editPeriod)
		// Peran: admin_tu. Wizard gelombang bertahap (T117).
		r.Get("/{id}/atur/{step}", h.wizardStep)
		// Peran: admin_tu.
		r.Post("/{id}/atur/{step}", h.saveWizardStep)
		// Peran: admin_tu. Salin gelombang (T117).
		r.Post("/{id}/salin", h.copyPeriod)
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
		// Peran: admin_tu.
		r.Post("/{id}/hasil", h.applyResults)
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
	// FormPath adalah alamat formulir publik sesuai jenis periode, tanpa
	// awalan /s/{kode}.
	FormPath  string
	KindLabel string
}

type periodListView struct {
	User       auth.Identity
	CanEdit    bool
	Periods    []periodRow
	SchoolCode string
	LogoURL    string
	Flash      string
	Error      string
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
	User       auth.Identity
	SchoolCode string
	CanEdit    bool
	Period     periodRow
	Docs       []string
	Tracks     []trackRow
	NewTrack   TrackForm
	// TrackErrors berisi error form jalur baru (EditTrack kosong) atau
	// jalur yang diubah.
	TrackErrors map[string]string
	EditTrack   string
	// NewTrackErrors adalah error form tambah jalur.
	NewTrackErrors map[string]string
	// Checklist dan Ready: daftar periksa sebelum dibuka (T117).
	Checklist []checkItem
	Ready     bool
	TestCount int
	Flash     string
	Error     string
}

// publicFormPath mengembalikan alamat formulir publik untuk jenis periode.
func publicFormPath(kind string) string {
	if kind == KindTransfer {
		return "/pindah"
	}
	return "/daftar"
}

func toRow(p Period) periodRow {
	r := periodRow{Period: p, Opens: clock.FormatDateID(p.OpensOn), Closes: clock.FormatDateID(p.ClosesOn), Fee: "Gratis",
		FormPath: publicFormPath(p.Kind), KindLabel: KindLabel(p.Kind)}
	if p.FeeAmount > 0 {
		r.Fee = money.FormatRupiah(p.FeeAmount)
	}
	return r
}

func (h *Handler) listPeriods(w http.ResponseWriter, r *http.Request) {
	h.renderPeriods(w, r, http.StatusOK, "")
}

func (h *Handler) renderPeriods(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	id, _ := auth.CurrentUser(r.Context())
	list, err := h.periods.List(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list periods", err)
		return
	}
	view := periodListView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Flash: auth.PopFlash(r.Context(), h.sessions), Error: errMsg}
	if err := h.db.QueryRow(r.Context(), `SELECT code FROM schools WHERE id = $1`, id.SchoolID).Scan(&view.SchoolCode); err != nil {
		h.serverError(w, r, "school code", err)
		return
	}
	logo, ok, err := h.logos.Get(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "get logo", err)
		return
	}
	if ok {
		view.LogoURL = "/s/" + view.SchoolCode + "/logo?v=" + logoVersion(logo.Key)
	}
	for _, p := range list {
		row := toRow(p)
		row.OpenNow = p.IsOpen(h.periods.Today())
		view.Periods = append(view.Periods, row)
	}
	h.render.Render(w, r, status, "admission/periods", view)
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
	if err := h.db.QueryRow(r.Context(), `SELECT code FROM schools WHERE id = $1`, id.SchoolID).Scan(&view.SchoolCode); err != nil {
		h.serverError(w, r, "school code", err)
		return
	}
	tests, err := h.selection().Tests(r.Context(), id.SchoolID, p.ID)
	if err != nil {
		h.serverError(w, r, "list tests", err)
		return
	}
	view.TestCount = len(tests)
	view.Checklist, view.Ready = periodChecklist(p, tests, h.periods.Today())
	h.render.Render(w, r, status, "admission/period_detail", view)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("status") == StatusOpen {
		cur, ok := h.loadPeriod(w, r, id, periodID)
		if !ok {
			return
		}
		if cur.Status != StatusOpen {
			h.openPeriod(w, r, id, cur, false)
			return
		}
	}
	p, err := h.periods.SetStatus(r.Context(), actorFrom(r, id), periodID, r.PostForm.Get("status"))
	var ve *ValidationError
	switch {
	case err == nil:
		msg := map[string]string{StatusOpen: "Pendaftaran " + p.Name + " dibuka.", StatusClosed: "Pendaftaran " + p.Name + " ditutup.", StatusDraft: p.Name + " dikembalikan ke draf."}[p.Status]
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
		auth.SetFlash(r.Context(), h.sessions, "Gelombang dihapus.")
		http.Redirect(w, r, periodsPath, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.Is(err, ErrInUse):
		h.renderDetail(w, r, id, periodID, http.StatusConflict, "", TrackForm{}, nil, "Hanya gelombang berstatus draf dan belum punya pendaftar yang bisa dihapus. Tutup gelombang ini jika tidak dipakai lagi.")
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
		http.Redirect(w, r, h.trackReturn(r, periodID), http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
	case errors.As(err, &ve):
		// Form jalur hanya ada di langkah jalur wizard.
		view := wizardView{NewTrack: f, NewTrackErrors: ve.Fields}
		if trackID != "" {
			view = wizardView{EditTrack: trackID, EditForm: f, EditErrors: ve.Fields}
		}
		h.renderWizardTracks(w, r, id, periodID, http.StatusBadRequest, view)
	default:
		h.serverError(w, r, "save track", err)
	}
}

func (h *Handler) deleteTrack(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	_ = r.ParseForm()
	err := h.periods.DeleteTrack(r.Context(), actorFrom(r, id), periodID, chi.URLParam(r, "trackID"))
	msg := ""
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Jalur dihapus.")
		http.Redirect(w, r, h.trackReturn(r, periodID), http.StatusSeeOther)
		return
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	case errors.Is(err, ErrNoTracks):
		msg = "Gelombang yang sedang dibuka harus punya minimal satu jalur."
	case errors.Is(err, ErrInUse):
		msg = "Jalur yang sudah punya pendaftar tidak bisa dihapus."
	default:
		h.serverError(w, r, "delete track", err)
		return
	}
	h.renderWizardTracks(w, r, id, periodID, http.StatusConflict, wizardView{Error: msg})
}

// trackReturn adalah tujuan setelah aksi jalur: langkah jalur wizard atau
// detail gelombang.
func (h *Handler) trackReturn(r *http.Request, periodID string) string {
	if fromWizard(r) {
		return periodsPath + "/" + periodID + "/atur/" + stepTracks
	}
	return periodsPath + "/" + periodID + "#jalur"
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func actorFrom(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
