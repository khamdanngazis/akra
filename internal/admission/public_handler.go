package admission

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

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
	limiter    *Limiter
	sessions   *scs.SessionManager
	render     page.Renderer
	logger     *slog.Logger
}

// NewPublicHandler mengembalikan PublicHandler. Route-nya publik (di luar
// RequireLogin) tetapi tetap memakai sesi dan CSRF.
func NewPublicHandler(db database.DB, periods *Service, applicants *ApplicantService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *PublicHandler {
	return &PublicHandler{db: db, periods: periods, applicants: applicants, limiter: NewLimiter(db), sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route publik pendaftaran ke r.
func (h *PublicHandler) Routes(r chi.Router) {
	r.Route("/s/{code}/daftar", func(r chi.Router) {
		// Publik: halaman awal dan pembuatan draf.
		r.Get("/", h.start)
		r.Post("/", h.createDraft)
		// Publik: melanjutkan draf dengan nomor HP + kode lanjut.
		r.Get("/lanjut", h.resumeForm)
		r.Post("/lanjut", h.resume)
		// Publik, hanya pemilik draf (sesi): isian per langkah dan kirim.
		r.Get("/isi/{step}", h.stepForm)
		r.Post("/isi/{step}", h.saveStep)
		r.Post("/kirim", h.submit)
		// Publik: konfirmasi setelah kirim (nomor dari sesi).
		r.Get("/selesai", h.done)
	})
}

type schoolInfo struct {
	ID, Code, Name string
}

func (h *PublicHandler) school(ctx context.Context, code string) (schoolInfo, bool, error) {
	code = auth.NormalizeSchoolCode(code)
	var s schoolInfo
	err := h.db.QueryRow(ctx, `SELECT id, code, name FROM schools WHERE code = $1`, code).Scan(&s.ID, &s.Code, &s.Name)
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
	Selected                 bool
}

type startView struct {
	School   schoolInfo
	Open     bool
	Tracks   []trackOption
	Form     DraftForm
	Choice   string // "<period_id>|<track_id>"
	Errors   map[string]string
	Error    string
	Flash    string
	Steps    []stepNav
	StepName string
}

func (h *PublicHandler) start(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	h.renderStart(w, r, s, http.StatusOK, startView{})
}

func (h *PublicHandler) renderStart(w http.ResponseWriter, r *http.Request, s schoolInfo, status int, view startView) {
	open, err := h.periods.OpenPeriods(r.Context(), s.ID)
	if err != nil {
		h.serverError(w, r, "list open periods", err)
		return
	}
	view.School, view.Open = s, len(open) > 0
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

func (h *PublicHandler) createDraft(w http.ResponseWriter, r *http.Request) {
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
	choice := r.PostForm.Get("choice")
	periodID, trackID, _ := strings.Cut(choice, "|")
	f := DraftForm{PeriodID: periodID, TrackID: trackID, FullName: r.PostForm.Get("full_name"), Gender: r.PostForm.Get("gender"),
		BirthDate: r.PostForm.Get("birth_date"), Phone: r.PostForm.Get("phone"), Consent: r.PostForm.Has("consent")}

	// Honeypot: manusia tidak melihat field ini. Bot yang mengisinya
	// mendapat tampilan sukses palsu tanpa draf dibuat.
	if r.PostForm.Get("website") != "" {
		_ = h.limiter.Record(r.Context(), s.ID, attemptDraft, "", ip, false)
		auth.SetFlash(r.Context(), h.sessions, "Terima kasih. Data Anda sedang diproses.")
		http.Redirect(w, r, "/s/"+s.Code+"/daftar", http.StatusSeeOther)
		return
	}
	phoneKey, _ := students.NormalizePhone(f.Phone)
	allowed, err := h.limiter.Allow(r.Context(), attemptDraft, phoneKey, ip)
	if err != nil {
		h.serverError(w, r, "check draft limit", err)
		return
	}
	if !allowed {
		h.renderStart(w, r, s, http.StatusTooManyRequests, startView{Form: f, Choice: choice, Error: "Terlalu banyak percobaan. Coba lagi dalam satu jam, atau hubungi sekolah."})
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
		h.renderStart(w, r, s, http.StatusBadRequest, startView{Form: f, Choice: choice, Errors: ve.Fields})
	case errors.Is(err, ErrClosed):
		h.renderStart(w, r, s, http.StatusConflict, startView{Form: f, Error: "Periode pendaftaran ini sudah ditutup."})
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
	Current    bool
}

type publicSection struct {
	Key, Title, Hint string
	Fields           []students.FieldView
}

type stepView struct {
	School     schoolInfo
	Applicant  Applicant
	Step       string
	StepName   string
	Steps      []stepNav
	Sections   []publicSection
	Missing    []students.RequiredField
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
	h.renderStep(w, r, s, a, step, http.StatusOK, nil, nil)
}

func (h *PublicHandler) renderStep(w http.ResponseWriter, r *http.Request, s schoolInfo, a Applicant, step string, status int, submitted, errs map[string]string) {
	view := stepView{School: s, Applicant: a, Step: step, Flash: auth.PopFlash(r.Context(), h.sessions),
		ResumeCode: h.sessions.PopString(r.Context(), sessionCode)}
	if errs != nil {
		view.Error = "Data belum disimpan. Periksa isian yang ditandai."
	}
	if p, err := h.periods.Get(r.Context(), s.ID, a.PeriodID); err == nil {
		view.Period = p.Name
		for _, t := range p.Tracks {
			if t.ID == a.TrackID {
				view.Track = t.Name
			}
		}
	}
	view.Steps, view.StepName = navFor(step)
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
		view.Sections = append(view.Sections, publicSection{Key: students.SectionIdentity, Title: "Data pribadi",
			Hint: "NIK dan No KK tercantum di Kartu Keluarga. Kosongkan NISN jika belum punya.", Fields: students.ProfileFieldViews(students.SectionIdentity, values, errs)})
	case StepAddress:
		view.Sections = append(view.Sections,
			publicSection{Key: students.SectionAddress, Title: "Alamat tempat tinggal", Fields: students.ProfileFieldViews(students.SectionAddress, values, errs)},
			publicSection{Key: students.SectionFamily, Title: "Keluarga dan bantuan", Fields: students.ProfileFieldViews(students.SectionFamily, values, errs)})
	case StepParents:
		titles := map[string]string{students.RelationFather: "Ayah kandung", students.RelationMother: "Ibu kandung", students.RelationGuardian: "Wali (jika ada)"}
		hints := map[string]string{students.RelationMother: "Nama ibu kandung wajib diisi. Hindari gelar (Almh., Hj., Dra.).", students.RelationFather: "Kosongkan jika tidak ada.", students.RelationGuardian: "Isi jika calon siswa tinggal bersama wali."}
		for _, role := range students.ParentRoles {
			gv := map[string]string{}
			ge := map[string]string{}
			for _, k := range guardianFields {
				gv[k] = values[role+"."+k]
				ge[k] = errs[role+"."+k]
			}
			fields := []students.FieldView{
				{Key: "full_name", Label: "Nama lengkap", Input: "text", Max: maxNameLen, Value: gv["full_name"], Error: ge["full_name"], Required: role == students.RelationMother},
				{Key: "phone", Label: "Nomor HP", Input: "text", InputMode: "tel", Value: gv["phone"], Error: ge["phone"]},
			}
			fields = append(fields, students.GuardianFieldViews(gv, ge)...)
			for i := range fields {
				fields[i].Key = role + "." + fields[i].Key
			}
			view.Sections = append(view.Sections, publicSection{Key: role, Title: titles[role], Hint: hints[role], Fields: fields})
		}
	case StepSchool:
		reg := students.ProfileFieldViews(students.SectionRegistration, values, errs)
		reg = slices.DeleteFunc(reg, func(f students.FieldView) bool { return slices.Contains(schoolManagedFields, f.Key) })
		view.Sections = append(view.Sections,
			publicSection{Key: students.SectionRegistration, Title: "Sekolah asal", Hint: "Untuk siswa pindahan, isi sekolah sebelumnya.", Fields: reg},
			publicSection{Key: students.SectionContact, Title: "Kontak calon siswa", Fields: students.ProfileFieldViews(students.SectionContact, values, errs)})
	case StepReview:
		view.Missing = Missing(a)
	}
	h.render.Render(w, r, status, "admission/public_step", view)
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
	}
	_, err := h.applicants.SaveStep(r.Context(), s.ID, a.ID, step, values)
	var ve *ValidationError
	switch {
	case err == nil:
		http.Redirect(w, r, "/s/"+s.Code+"/daftar/isi/"+nextStep(step), http.StatusSeeOther)
	case errors.As(err, &ve):
		h.renderStep(w, r, s, a, step, http.StatusBadRequest, values, ve.Fields)
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
	h.render.Render(w, r, http.StatusOK, "admission/public_done", doneView{School: s, Number: number})
}

func (h *PublicHandler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	page.ServerError(h.render, h.logger, w, r, auth.Identity{}, op, err)
}
