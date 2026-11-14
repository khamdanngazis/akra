package admission

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/students"
)

const (
	sessionApplicant = "admission_applicant" // "<school_id>|<applicant_id>"
	sessionCode      = "admission_code"      // kode lanjut, ditampilkan sekali
	sessionDone      = "admission_done"      // nomor pendaftaran setelah kirim
	maxPublicForm    = 32 << 10
	msgSchoolMissing = "Halaman pendaftaran tidak ditemukan. Periksa kembali tautan dari sekolah."
)

// PublicHandler melayani formulir pendaftaran tanpa login (T90).
type PublicHandler struct {
	db         database.DB
	periods    *Service
	applicants *ApplicantService
	documents  *DocumentService
	logos      *LogoService
	limiter    *Limiter
	sessions   *scs.SessionManager
	render     page.Renderer
	logger     *slog.Logger
}

// NewPublicHandler mengembalikan PublicHandler. Route-nya publik (di luar
// RequireLogin) tetapi tetap memakai sesi dan CSRF.
func NewPublicHandler(db database.DB, periods *Service, applicants *ApplicantService, documents *DocumentService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *PublicHandler {
	return &PublicHandler{db: db, periods: periods, applicants: applicants, documents: documents, logos: logoServiceFor(db, documents), limiter: NewLimiter(db), sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route publik pendaftaran ke r.
func (h *PublicHandler) Routes(r chi.Router) {
	r.Route("/s/{code}/daftar", func(r chi.Router) {
		// Publik: halaman awal dan pembuatan draf.
		r.Get("/", h.start(KindNew))
		r.Post("/", h.createDraft(KindNew))
		// Publik: melanjutkan draf dengan nomor HP + kode lanjut.
		r.Get("/lanjut", h.resumeForm)
		r.Post("/lanjut", h.resume)
		// Publik, hanya pemilik draf (sesi): isian per langkah dan kirim.
		r.Get("/isi/{step}", h.stepForm)
		r.Post("/isi/{step}", h.saveStep)
		r.Post("/dokumen/{type}", h.uploadDocument)
		r.Post("/kirim", h.submit)
		// Publik: konfirmasi setelah kirim (nomor dari sesi).
		r.Get("/selesai", h.done)
	})
	r.Route("/s/{code}/pindah", func(r chi.Router) {
		// Publik: halaman awal dan pembuatan draf siswa pindahan (T109).
		// Langkah berikutnya memakai /s/{kode}/daftar/isi/... yang sama.
		r.Get("/", h.start(KindTransfer))
		r.Post("/", h.createDraft(KindTransfer))
	})
	r.Route("/s/{code}/status", func(r chi.Router) {
		// Publik: cek status dengan nomor pendaftaran + tanggal lahir (T95).
		r.Get("/", h.statusPage)
		r.Post("/", h.checkStatus)
		// Publik, hanya sesi yang sudah lolos cek: bukti pendaftaran.
		r.Get("/bukti.pdf", h.statusProof)
		r.Post("/keluar", h.statusLogout)
	})
	// Publik: logo sekolah untuk kepala halaman pendaftaran (T111).
	r.Get("/s/{code}/logo", h.logo)
}

// schoolInfo adalah identitas sekolah di kepala halaman publik.
type schoolInfo struct {
	ID, Code, Name string
	Address        string
	// LogoURL kosong jika sekolah belum mengunggah logo (T111).
	LogoURL string
}

// Initials mengembalikan inisial nama sekolah sebagai pengganti logo,
// tanpa awalan jenjang (SD, SMP, MI, ...).
func (s schoolInfo) Initials() string {
	words := strings.Fields(s.Name)
	skip := map[string]bool{"SD": true, "SDN": true, "SDIT": true, "MI": true, "MIN": true, "SMP": true, "SMPN": true, "MTS": true, "SMA": true, "SMAN": true, "SMK": true, "SMKN": true, "MA": true, "TK": true}
	var out []rune
	for _, w := range words {
		if len(words) > 1 && skip[strings.ToUpper(w)] {
			continue
		}
		for _, r := range w {
			out = append(out, unicode.ToUpper(r))
			break
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "S"
	}
	return string(out)
}

func (h *PublicHandler) school(ctx context.Context, code string) (schoolInfo, bool, error) {
	code = auth.NormalizeSchoolCode(code)
	var s schoolInfo
	var address, logo *string
	err := h.db.QueryRow(ctx, `SELECT id, code, name, address, logo_key FROM schools WHERE code = $1`, code).Scan(&s.ID, &s.Code, &s.Name, &address, &logo)
	if address != nil {
		s.Address = *address
	}
	if logo != nil {
		s.LogoURL = "/s/" + s.Code + "/logo?v=" + logoVersion(*logo)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return schoolInfo{}, false, nil
	}
	if err != nil {
		return schoolInfo{}, false, fmt.Errorf("admission: find school: %w", err)
	}
	return s, true, nil
}

// resolveSchool mengembalikan sekolah dari URL, atau menulis 404.
func (h *PublicHandler) resolveSchool(w http.ResponseWriter, r *http.Request) (schoolInfo, bool) {
	s, ok, err := h.school(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		h.serverError(w, r, "find school", err)
		return schoolInfo{}, false
	}
	if !ok {
		page.NotFound(h.render, w, r, auth.Identity{}, msgSchoolMissing)
		return schoolInfo{}, false
	}
	return s, true
}

type trackOption struct {
	PeriodID, TrackID, Label string
	Kind                     string
	Selected                 bool
}

type startView struct {
	School   schoolInfo
	Kind     string
	FormPath string // /s/{kode}/daftar atau /s/{kode}/pindah
	// OtherPath adalah formulir jenis lain jika sedang dibuka (mis. tautan
	// ke formulir pindahan dari halaman siswa baru).
	OtherPath string
	Open      bool
	Year      string
	Periods   []periodCard
	Docs      []docCard
	Tracks    []trackOption
	Form      DraftForm
	Choice    string // "<period_id>|<track_id>"
	Errors    map[string]string
	Error     string
	Flash     string
	Steps     []stepNav
	StepName  string
}

// start menampilkan halaman awal formulir untuk jenis kind.
func (h *PublicHandler) start(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := h.resolveSchool(w, r)
		if !ok {
			return
		}
		h.renderStart(w, r, s, http.StatusOK, startView{Kind: kind})
	}
}

func (h *PublicHandler) renderStart(w http.ResponseWriter, r *http.Request, s schoolInfo, status int, view startView) {
	all, err := h.periods.OpenPeriods(r.Context(), s.ID)
	if err != nil {
		h.serverError(w, r, "list open periods", err)
		return
	}
	var open []Period
	for _, p := range all {
		if p.Kind == view.Kind {
			open = append(open, p)
		} else if view.OtherPath == "" {
			view.OtherPath = "/s/" + s.Code + publicFormPath(p.Kind)
		}
	}
	view.School, view.Open = s, len(open) > 0
	view.FormPath = "/s/" + s.Code + publicFormPath(view.Kind)
	view.Periods, view.Docs, view.Year = startInfo(open)
	if view.Flash == "" {
		view.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	for _, p := range open {
		for _, t := range p.Tracks {
			label := p.Name + " · Jalur " + t.Name
			if p.FeeAmount > 0 {
				label += " · biaya formulir " + money.FormatRupiah(p.FeeAmount)
			}
			choice := p.ID + "|" + t.ID
			view.Tracks = append(view.Tracks, trackOption{PeriodID: p.ID, TrackID: t.ID, Label: label, Selected: choice == view.Choice})
		}
	}
	h.render.Render(w, r, status, "admission/public_start", view)
}

// createDraft membuat draf dari halaman awal formulir jenis kind.
func (h *PublicHandler) createDraft(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { h.createDraftKind(w, r, kind) }
}

func (h *PublicHandler) createDraftKind(w http.ResponseWriter, r *http.Request, kind string) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	formPath := "/s/" + s.Code + publicFormPath(kind)
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ip := auth.ClientIP(r)
	choice := r.PostForm.Get("choice")
	periodID, trackID, _ := strings.Cut(choice, "|")
	f := DraftForm{Kind: kind, PeriodID: periodID, TrackID: trackID, FullName: r.PostForm.Get("full_name"), Gender: r.PostForm.Get("gender"),
		BirthDate: r.PostForm.Get("birth_date"), Phone: r.PostForm.Get("phone"), Consent: r.PostForm.Has("consent")}

	// Honeypot: manusia tidak melihat field ini. Bot yang mengisinya
	// mendapat tampilan sukses palsu tanpa draf dibuat.
	if r.PostForm.Get("website") != "" {
		_ = h.limiter.Record(r.Context(), s.ID, attemptDraft, "", ip, false)
		auth.SetFlash(r.Context(), h.sessions, "Terima kasih. Data Anda sedang diproses.")
		http.Redirect(w, r, formPath, http.StatusSeeOther)
		return
	}
	phoneKey, _ := students.NormalizePhone(f.Phone)
	allowed, err := h.limiter.Allow(r.Context(), attemptDraft, phoneKey, ip)
	if err != nil {
		h.serverError(w, r, "check draft limit", err)
		return
	}
	if !allowed {
		h.renderStart(w, r, s, http.StatusTooManyRequests, startView{Kind: kind, Form: f, Choice: choice, Error: "Terlalu banyak percobaan. Coba lagi dalam satu jam, atau hubungi sekolah."})
		return
	}

	a, code, err := h.applicants.CreateDraft(r.Context(), s.ID, f, "")
	_ = h.limiter.Record(r.Context(), s.ID, attemptDraft, phoneKey, ip, err == nil)
	var ve *ValidationError
	switch {
	case err == nil:
		if err := h.sessions.RenewToken(r.Context()); err != nil {
			h.serverError(w, r, "renew session", err)
			return
		}
		h.sessions.Put(r.Context(), sessionApplicant, s.ID+"|"+a.ID)
		h.sessions.Put(r.Context(), sessionCode, code)
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/isi/"+StepChild, http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderStart(w, r, s, http.StatusBadRequest, startView{Kind: kind, Form: f, Choice: choice, Errors: ve.Fields})
	case errors.Is(err, ErrClosed):
		h.renderStart(w, r, s, http.StatusConflict, startView{Kind: kind, Form: f, Error: "Periode pendaftaran ini sudah ditutup."})
	default:
		h.serverError(w, r, "create draft", err)
	}
}

type resumeView struct {
	School   schoolInfo
	Phone    string
	Error    string
	Flash    string
	Steps    []stepNav
	StepName string
}

func (h *PublicHandler) resumeForm(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	h.render.Render(w, r, http.StatusOK, "admission/public_resume", resumeView{School: s})
}

func (h *PublicHandler) resume(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ip := auth.ClientIP(r)
	phone := r.PostForm.Get("phone")
	phoneKey, _ := students.NormalizePhone(phone)
	view := resumeView{School: s, Phone: phone}
	allowed, err := h.limiter.Allow(r.Context(), attemptResume, phoneKey, ip)
	if err != nil {
		h.serverError(w, r, "check resume limit", err)
		return
	}
	if !allowed {
		view.Error = "Terlalu banyak percobaan. Coba lagi dalam 15 menit."
		h.render.Render(w, r, http.StatusTooManyRequests, "admission/public_resume", view)
		return
	}
	a, err := h.applicants.Resume(r.Context(), s.ID, phone, r.PostForm.Get("code"))
	_ = h.limiter.Record(r.Context(), s.ID, attemptResume, phoneKey, ip, err == nil)
	switch {
	case err == nil:
		if err := h.sessions.RenewToken(r.Context()); err != nil {
			h.serverError(w, r, "renew session", err)
			return
		}
		h.sessions.Put(r.Context(), sessionApplicant, s.ID+"|"+a.ID)
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/isi/"+StepChild, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrResumeLocked):
		// Pesan sama untuk HP tidak dikenal, kode salah, dan terkunci.
		view.Error = "Nomor HP atau kode lanjut tidak cocok. Setelah 5 kali salah, draf dikunci 15 menit."
		h.render.Render(w, r, http.StatusUnauthorized, "admission/public_resume", view)
	default:
		h.serverError(w, r, "resume draft", err)
	}
}

type stepNav struct {
	Key, Title string
	Number     int
	Current    bool
	Done       bool
	Missing    int // isian wajib yang belum diisi (hanya langkah yang sudah dibuka)
}

type publicSection struct {
	Key, Title, Hint string
	Fields           []students.FieldView
	// XData adalah objek Alpine.js untuk isian kondisional di bagian ini;
	// ShowIf menyembunyikan seluruh bagian (mis. wali).
	XData, ShowIf string
}

type stepView struct {
	// Base adalah alamat dasar form: /s/{kode}/daftar (publik) atau
	// /pendaftaran/{id} (TU, T92).
	Base      string
	Staff     bool
	User      auth.Identity
	School    schoolInfo
	Applicant Applicant
	Step      string
	StepName  string
	Steps     []stepNav
	Sections  []publicSection
	Missing   []students.RequiredField
	// Kind adalah jenis pendaftar; FormData variabel Alpine.js tingkat form
	// (mis. showWali).
	Kind     string
	FormData string
	Groups   []missingGroup
	// Highlight: halaman dibuka dari tombol "Lengkapi" dan isian yang
	// kurang ditandai (T107). Discard adalah langkah tujuan untuk tombol
	// "Kembali tanpa menyimpan" saat isian halaman ini belum valid.
	Highlight  bool
	Discard    string
	Summary    []summaryCard
	StepNumber int
	Prev       string
	Documents  []docRow
	DocErrors  map[string]string
	ResumeCode string
	NextLabel  string
	Error      string
	Flash      string
	Track      string
	Period     string
}

// owned mengembalikan pendaftar milik sesi ini, atau mengarahkan ke halaman
// awal jika sesi tidak punya draf di sekolah ini.
func (h *PublicHandler) owned(w http.ResponseWriter, r *http.Request, s schoolInfo) (Applicant, bool) {
	school, id, _ := strings.Cut(h.sessions.GetString(r.Context(), sessionApplicant), "|")
	if school != s.ID || id == "" {
		auth.SetFlash(r.Context(), h.sessions, "Mulai pendaftaran baru, atau lanjutkan draf dengan nomor HP dan kode lanjut.")
		http.Redirect(w, r, "/s/"+s.Code+"/daftar", http.StatusSeeOther)
		return Applicant{}, false
	}
	a, err := h.applicants.Load(r.Context(), s.ID, id)
	if errors.Is(err, ErrNotFound) {
		h.sessions.Remove(r.Context(), sessionApplicant)
		http.Redirect(w, r, "/s/"+s.Code+"/daftar", http.StatusSeeOther)
		return Applicant{}, false
	}
	if err != nil {
		h.serverError(w, r, "load applicant", err)
		return Applicant{}, false
	}
	if !a.Editable() {
		h.sessions.Remove(r.Context(), sessionApplicant)
		auth.SetFlash(r.Context(), h.sessions, editableMessage)
		http.Redirect(w, r, "/s/"+s.Code+"/daftar", http.StatusSeeOther)
		return Applicant{}, false
	}
	return a, true
}

func validStep(step string) bool {
	if step == StepReview {
		return true
	}
	for _, s := range Steps {
		if s.Key == step {
			return true
		}
	}
	return false
}

func (h *PublicHandler) stepForm(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	step := chi.URLParam(r, "step")
	if !validStep(step) {
		page.NotFound(h.render, w, r, auth.Identity{}, msgSchoolMissing)
		return
	}
	a, ok := h.owned(w, r, s)
	if !ok {
		return
	}
	errs, opts := highlightMissing(r)
	h.renderStep(w, r, s, a, step, http.StatusOK, nil, errs, opts...)
}

// buildStepView menyusun halaman satu langkah formulir. Dipakai formulir
// publik (T90) dan input oleh TU (T92) agar isian dan aturannya sama.
func buildStepView(ctx context.Context, periods *Service, documents *DocumentService, s schoolInfo, a Applicant, step string, submitted, errs map[string]string) (stepView, error) {
	view := stepView{School: s, Applicant: a, Step: step, Kind: a.Kind}
	if errs != nil && step != StepDocument {
		view.Error = "Data belum disimpan. Periksa isian yang ditandai."
	}
	p, err := periods.Get(ctx, s.ID, a.PeriodID)
	if err != nil {
		return stepView{}, err
	}
	view.Period = p.Name
	for _, t := range p.Tracks {
		if t.ID == a.TrackID {
			view.Track = t.Name
		}
	}
	docs, err := documents.List(ctx, s.ID, a.ID)
	if err != nil {
		return stepView{}, err
	}
	missing := Missing(a)
	for _, k := range MissingDocs(p, docs) {
		missing = append(missing, students.RequiredField{Key: "doc_" + k, Label: DocLabel(k)})
	}
	view.Steps, view.StepName = navFor(step)
	view.Steps = markSteps(view.Steps, a, missing, docs)
	for _, n := range view.Steps {
		if n.Current {
			view.StepNumber = n.Number
		}
	}
	view.Prev = prevStep(step)
	values := map[string]string{}
	for k, v := range a.Values {
		values[k] = v
	}
	values["full_name"], values["gender"], values["birth_date"] = a.FullName, a.Gender, a.BirthDate.Format(dateLayout)
	for role, g := range a.Guardians {
		for k, v := range g {
			values[role+"."+k] = v
		}
	}
	for k, v := range a.Transfer {
		values[k] = v
	}
	for k, v := range submitted {
		values[k] = v
	}
	view.NextLabel = "Simpan dan lanjut"

	switch step {
	case StepChild:
		basics := []students.FieldView{
			{Key: "full_name", Label: "Nama lengkap (sesuai akta)", Input: "text", Max: maxNameLen, Value: values["full_name"], Error: errs["full_name"], Required: true},
			{Key: "gender", Label: "Jenis kelamin", Input: "select", Value: values["gender"], Error: errs["gender"], Required: true,
				Options: []students.Option{{Value: "L", Label: "Laki-laki", Selected: values["gender"] == "L"}, {Value: "P", Label: "Perempuan", Selected: values["gender"] == "P"}}},
			{Key: "birth_date", Label: "Tanggal lahir", Input: "date", Value: values["birth_date"], Error: errs["birth_date"], Required: true},
		}
		view.Sections = append(view.Sections, publicSection{Key: "dasar", Title: "Identitas", Fields: basics})
		identity := publicSection{Key: students.SectionIdentity, Title: "Data pribadi",
			Hint: "NIK dan No KK tercantum di Kartu Keluarga. Kosongkan NISN jika belum punya.", Fields: students.ProfileFieldViews(students.SectionIdentity, values, errs)}
		if a.Kind == KindTransfer {
			identity.Hint = "NIK dan No KK tercantum di Kartu Keluarga. NISN wajib diisi jika sudah punya (lihat rapor atau nisn.data.kemdikbud.go.id)."
		}
		view.Sections = append(view.Sections, identity)
	case StepAddress:
		if err := students.SuggestWilayah(ctx, periods.db, values); err != nil {
			return stepView{}, err
		}
		view.Sections = addressSections(values, errs)
	case StepParents:
		view.Sections = parentSections(values, errs)
		view.FormData = "showWali: " + jsBool(showGuardian(a, values, errs))
	case StepSchool:
		view.Sections = schoolSections(a.Kind, p.TargetGrade, values, errs)
	case StepDocument, StepReview:
		view.Documents, view.DocErrors = docRows(p, docs), errs
		if step == StepReview {
			view.Missing, view.Groups = missing, groupMissing(missing)
			view.Summary = reviewSummary(a, missing, view.Documents)
		}
	}
	return view, nil
}

func (h *PublicHandler) renderStep(w http.ResponseWriter, r *http.Request, s schoolInfo, a Applicant, step string, status int, submitted, errs map[string]string, opts ...stepOpt) {
	view, err := buildStepView(r.Context(), h.periods, h.documents, s, a, step, submitted, errs)
	if err != nil {
		h.serverError(w, r, "build step view", err)
		return
	}
	for _, o := range opts {
		o(&view)
	}
	view.Base = "/s/" + s.Code + "/daftar"
	view.Flash = auth.PopFlash(r.Context(), h.sessions)
	view.ResumeCode = h.sessions.PopString(r.Context(), sessionCode)
	h.render.Render(w, r, status, "admission/public_step", view)
}

// stepValues mengambil isian satu langkah dari form yang sudah di-parse.
func stepValues(r *http.Request, step string) map[string]string {
	values := map[string]string{}
	if step == StepParents {
		for _, role := range students.ParentRoles {
			for _, k := range guardianFields {
				key := role + "." + k
				if k == "special_needs" {
					values[key] = strings.Join(r.PostForm[key], ",")
				} else {
					values[key] = r.PostForm.Get(key)
				}
			}
		}
	} else {
		if step == StepChild {
			for _, k := range []string{"full_name", "gender", "birth_date"} {
				values[k] = r.PostForm.Get(k)
			}
		}
		for _, sec := range stepSections[step] {
			for _, f := range students.ProfileFields {
				if f.Section != sec {
					continue
				}
				switch {
				case f.Key == "has_kip" || f.Key == "pip_eligible":
					if r.PostForm.Has(f.Key) {
						values[f.Key] = "true"
					} else {
						values[f.Key] = "false"
					}
				case f.Key == "special_needs":
					values[f.Key] = strings.Join(r.PostForm[f.Key], ",")
				default:
					values[f.Key] = r.PostForm.Get(f.Key)
				}
			}
		}
		if step == StepAddress && !r.PostForm.Has("has_kks") {
			values["kks_number"] = "" // "Punya KKS/PKH" tidak dicentang
		}
		if step == StepSchool {
			for _, k := range transferKeys {
				values[k] = r.PostForm.Get(k)
			}
		}
	}
	return values
}

type docRow struct {
	Key, Label string
	Hint       string
	Required   bool
	Uploaded   *Document
}

// docHints adalah petunjuk foto per jenis dokumen (T108).
var docHints = map[string]string{
	"kk":           "Seluruh halaman KK terlihat, tidak terpotong dan tidak buram.",
	"akta":         "Halaman yang memuat nama dan tanggal lahir.",
	"ijazah":       "Halaman depan ijazah atau surat keterangan lulus.",
	"foto":         "Foto wajah calon siswa, latar polos, tampak jelas.",
	"kip":          "Sisi kartu yang memuat nomor KIP.",
	"surat_pindah": "Surat keterangan pindah bercap sekolah asal.",
	"rapor":        "Halaman identitas dan nilai semester terakhir.",
}

// docRows menampilkan dokumen wajib periode dulu, lalu dokumen opsional.
func docRows(p Period, docs []Document) []docRow {
	byType := map[string]*Document{}
	for i := range docs {
		byType[docs[i].DocType] = &docs[i]
	}
	var rows []docRow
	for _, d := range Docs {
		if p.Requires(d.Key) {
			rows = append(rows, docRow{Key: d.Key, Label: d.Label, Hint: docHints[d.Key], Required: true, Uploaded: byType[d.Key]})
		}
	}
	for _, d := range Docs {
		// Dokumen khusus pindahan tidak ditawarkan di formulir siswa baru
		// kecuali diwajibkan periode.
		if (d.Key == "surat_pindah" || d.Key == "rapor") && p.Kind != KindTransfer {
			continue
		}
		if !p.Requires(d.Key) {
			rows = append(rows, docRow{Key: d.Key, Label: d.Label, Hint: docHints[d.Key], Uploaded: byType[d.Key]})
		}
	}
	return rows
}

// uploadDocument menerima satu dokumen (multipart, field "file").
func (h *PublicHandler) uploadDocument(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	a, ok := h.owned(w, r, s)
	if !ok {
		return
	}
	docType := chi.URLParam(r, "type")
	if !validDocType(docType) || docType == "lainnya" {
		page.NotFound(h.render, w, r, auth.Identity{}, msgSchoolMissing)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxDocumentSize+64<<10)
	js := wantsJSON(r)
	file, _, err := r.FormFile("file")
	if err != nil {
		if js {
			writeUploadJSON(w, http.StatusBadRequest, false, msgPickFile)
			return
		}
		h.renderStep(w, r, s, a, StepDocument, http.StatusBadRequest, nil, map[string]string{docType: msgPickFile})
		return
	}
	defer file.Close()
	_, err = h.documents.Upload(r.Context(), s.ID, a.ID, docType, file)
	switch {
	case err == nil && js:
		writeUploadJSON(w, http.StatusOK, true, DocLabel(docType)+" berhasil diunggah.")
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, DocLabel(docType)+" berhasil diunggah.")
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/isi/"+uploadReturn(r), http.StatusSeeOther)
	case docErrorMessage(err) != "" && js:
		writeUploadJSON(w, http.StatusBadRequest, false, docErrorMessage(err))
	case docErrorMessage(err) != "":
		h.renderStep(w, r, s, a, StepDocument, http.StatusBadRequest, nil, map[string]string{docType: docErrorMessage(err)})
	case errors.Is(err, ErrNotEditable) && js:
		writeUploadJSON(w, http.StatusConflict, false, editableMessage)
	case errors.Is(err, ErrNotEditable):
		auth.SetFlash(r.Context(), h.sessions, editableMessage)
		http.Redirect(w, r, "/s/"+s.Code+"/daftar", http.StatusSeeOther)
	case js:
		h.logger.ErrorContext(r.Context(), "upload document", "error", err)
		writeUploadJSON(w, http.StatusInternalServerError, false, msgUploadError)
	default:
		h.serverError(w, r, "upload document", err)
	}
}

// docErrorMessage mengubah error unggah dokumen menjadi pesan untuk
// pengguna, atau "" jika bukan kesalahan isian.
func docErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrDocTooLarge):
		return "Ukuran file maksimal 2 MB. Untuk PDF besar, foto dokumennya dengan kamera HP."
	case errors.Is(err, ErrDocType), errors.Is(err, ErrDocEmpty):
		return "File harus berupa foto (JPG/PNG) atau PDF."
	}
	return ""
}

func navFor(step string) ([]stepNav, string) {
	var nav []stepNav
	name := ""
	for _, s := range Steps {
		nav = append(nav, stepNav{Key: s.Key, Title: s.Title, Current: s.Key == step})
		if s.Key == step {
			name = s.Title
		}
	}
	nav = append(nav, stepNav{Key: StepReview, Title: "Periksa dan kirim", Current: step == StepReview})
	if step == StepReview {
		name = "Periksa dan kirim"
	}
	return nav, name
}

func nextStep(step string) string {
	for i, s := range Steps {
		if s.Key == step && i+1 < len(Steps) {
			return Steps[i+1].Key
		}
	}
	return StepReview
}

func (h *PublicHandler) saveStep(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	step := chi.URLParam(r, "step")
	if !validStep(step) || step == StepReview {
		page.NotFound(h.render, w, r, auth.Identity{}, msgSchoolMissing)
		return
	}
	a, ok := h.owned(w, r, s)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	goTo := r.PostForm.Get("go")
	target := targetStep(step, goTo)
	if goTo == goDraft {
		auth.SetFlash(r.Context(), h.sessions, draftSavedPublic)
	}
	if step == StepDocument {
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/isi/"+target, http.StatusSeeOther)
		return
	}
	values := stepValues(r, step)
	_, err := h.applicants.SaveStep(r.Context(), s.ID, a.ID, step, values)
	var ve *ValidationError
	switch {
	case err == nil:
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/isi/"+target, http.StatusSeeOther)
	case errors.As(err, &ve):
		_ = auth.PopFlash(r.Context(), h.sessions) // belum tersimpan
		h.renderStep(w, r, s, a, step, http.StatusBadRequest, values, ve.Fields, withDiscard(step, goTo))
	case errors.Is(err, ErrNotEditable):
		auth.SetFlash(r.Context(), h.sessions, editableMessage)
		http.Redirect(w, r, "/s/"+s.Code+"/daftar", http.StatusSeeOther)
	default:
		h.serverError(w, r, "save applicant step", err)
	}
}

func (h *PublicHandler) submit(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	a, ok := h.owned(w, r, s)
	if !ok {
		return
	}
	done, err := h.applicants.Submit(r.Context(), s.ID, a.ID)
	var ve *ValidationError
	switch {
	case err == nil:
		h.sessions.Remove(r.Context(), sessionApplicant)
		h.sessions.Put(r.Context(), sessionDone, done.RegistrationNumber)
		// Akses cek status langsung, agar bukti pendaftaran bisa diunduh dari
		// halaman selesai tanpa mengetik nomor dan tanggal lahir lagi.
		h.sessions.Put(r.Context(), sessionStatus, s.ID+"|"+done.ID)
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/selesai", http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderStep(w, r, s, a, StepReview, http.StatusBadRequest, nil, nil)
	case errors.Is(err, ErrDuplicateNIK):
		auth.SetFlash(r.Context(), h.sessions, "Calon siswa dengan NIK ini sudah terdaftar di periode ini. Hubungi sekolah jika ini keliru.")
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/isi/"+StepReview, http.StatusSeeOther)
	default:
		h.serverError(w, r, "submit applicant", err)
	}
}

type doneView struct {
	School   schoolInfo
	Number   string
	Name     string
	Timeline []timelineItem
	Steps    []stepNav
	StepName string
	Error    string
	Flash    string
}

func (h *PublicHandler) done(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	number := h.sessions.GetString(r.Context(), sessionDone)
	if number == "" {
		http.Redirect(w, r, "/s/"+s.Code+"/daftar", http.StatusSeeOther)
		return
	}
	view := doneView{School: s, Number: number}
	school, id, _ := strings.Cut(h.sessions.GetString(r.Context(), sessionStatus), "|")
	if school == s.ID && id != "" {
		a, err := h.applicants.Load(r.Context(), s.ID, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			h.serverError(w, r, "load applicant", err)
			return
		}
		if err == nil && a.RegistrationNumber == number {
			if view, err = h.doneDetails(r.Context(), s, a, view); err != nil {
				h.serverError(w, r, "done details", err)
				return
			}
		}
	}
	if view.Timeline == nil {
		view.Timeline = nextSteps(nil, "", false)
	}
	h.render.Render(w, r, http.StatusOK, "admission/public_done", view)
}

// doneDetails melengkapi halaman selesai dengan nama, biaya, dan tes.
func (h *PublicHandler) doneDetails(ctx context.Context, s schoolInfo, a Applicant, view doneView) (doneView, error) {
	p, err := h.periods.Get(ctx, s.ID, a.PeriodID)
	if err != nil {
		return view, err
	}
	usesTest := false
	for _, t := range p.Tracks {
		if t.ID == a.TrackID {
			usesTest = t.UsesTest
		}
	}
	var fee int64
	if err := h.db.QueryRow(ctx, `SELECT fee_amount FROM applicants WHERE school_id = $1 AND id = $2`, s.ID, a.ID).Scan(&fee); err != nil {
		return view, err
	}
	feeText := ""
	if fee > 0 {
		feeText = money.FormatRupiah(fee)
	}
	view.Name, view.Timeline = a.FullName, nextSteps(a.SubmittedAt, feeText, usesTest)
	return view, nil
}

func (h *PublicHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	page.ServerError(h.render, h.logger, w, r, auth.Identity{}, op, err)
}
