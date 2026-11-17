package exam

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// Langkah wizard buat ujian (T133), pola wizard gelombang (T117).
const (
	stepBasics    = "dasar"
	stepQuestions = "soal"
	stepRules     = "aturan"
	stepSchedule  = "jadwal"
	stepCheck     = "cek"
)

var examWizardSteps = []struct{ Key, Title string }{
	{stepBasics, "Dasar"}, {stepQuestions, "Soal"}, {stepRules, "Aturan"}, {stepSchedule, "Jadwal & peserta"}, {stepCheck, "Cek & jadwalkan"},
}

func validExamStep(s string) bool {
	for _, st := range examWizardSteps {
		if st.Key == s {
			return true
		}
	}
	return false
}

func nextExamStep(s string) string {
	for i, st := range examWizardSteps {
		if st.Key == s && i+1 < len(examWizardSteps) {
			return examWizardSteps[i+1].Key
		}
	}
	return stepCheck
}

type stepLink struct {
	N          int
	Key, Title string
	URL        string
	State      string
}

type purposeOption struct {
	Value    Purpose
	Label    string
	Hint     string
	Selected bool
}

var purposeHints = map[Purpose]string{
	PurposeQuiz:      "Ulangan harian untuk kelas yang Anda ampu. Nilai bisa dikirim ke rapor.",
	PurposePractice:  "Siswa mencoba antarmuka ujian. Hasil dan pembahasan langsung, boleh mengulang.",
	PurposeMidterm:   "Penilaian tengah semester. Jadwal, ruang, dan pengawas diatur di agenda.",
	PurposeFinal:     "Penilaian akhir semester. Jadwal, ruang, dan pengawas diatur di agenda.",
	PurposeAdmission: "Tes calon siswa. Hasil tidak ditampilkan ke peserta; nilai masuk ke peringkat.",
}

// countRow adalah satu baris sebaran di panel ringkas.
type countRow struct {
	Label   string
	N       int
	Percent int
}

// compView adalah panel ringkas wizard.
type compView struct {
	Composition
	Duration     int
	Kinds        []countRow
	Difficulty   []countRow
	Cognitive    []countRow
	Objectives   []countRow
	Untagged     int
	Variants     int
	Shared       int
	OverTime     bool
	ShortRules   int
	HasQuestions bool
}

func newCompView(e Exam, p Plan, objCodes map[string]string) compView {
	c := p.Composition()
	v := compView{Composition: c, Duration: e.DurationMinutes, Variants: len(p.Variants), HasQuestions: c.Count > 0}
	pct := func(n int) int {
		if c.Count == 0 {
			return 0
		}
		return (n*100 + c.Count/2) / c.Count
	}
	for _, k := range []Kind{KindMC, KindMulti, KindTF, KindShort, KindMatch, KindEssay} {
		if n := c.ByKind[k]; n > 0 {
			v.Kinds = append(v.Kinds, countRow{Label: k.Label(), N: n, Percent: pct(n)})
		}
	}
	for _, d := range Difficulties {
		v.Difficulty = append(v.Difficulty, countRow{Label: d, N: c.ByDifficulty[d], Percent: pct(c.ByDifficulty[d])})
	}
	if n := c.ByDifficulty[""]; n > 0 {
		v.Difficulty = append(v.Difficulty, countRow{Label: "belum diisi", N: n, Percent: pct(n)})
	}
	for _, g := range []string{"LOTS", "MOTS", "HOTS"} {
		v.Cognitive = append(v.Cognitive, countRow{Label: g, N: c.ByCognitive[g], Percent: pct(c.ByCognitive[g])})
	}
	if n := c.ByCognitive[""]; n > 0 {
		v.Cognitive = append(v.Cognitive, countRow{Label: "belum diisi", N: n, Percent: pct(n)})
	}
	for id, n := range c.ByObjective {
		if id == "" {
			v.Untagged = n
			continue
		}
		code := objCodes[id]
		if code == "" {
			code = "TP lain"
		}
		v.Objectives = append(v.Objectives, countRow{Label: code, N: n, Percent: pct(n)})
	}
	sort.Slice(v.Objectives, func(i, j int) bool { return v.Objectives[i].Label < v.Objectives[j].Label })
	for _, a := range p.Assembled[min(1, len(p.Assembled)):] {
		v.Shared += a.Shared
	}
	if len(p.Assembled) > 0 {
		v.ShortRules = len(p.Assembled[0].Short)
	}
	v.OverTime = e.DurationMinutes > 0 && c.Minutes > e.DurationMinutes
	return v
}

// classRow adalah satu kelas di langkah Jadwal ulangan.
type classRow struct {
	ClassOption
	Scheduled bool // sudah punya sesi
}

type sessionView struct {
	Session
	When      string
	CanCancel bool
}

type admissionPeriodOption struct {
	ID, Name string
	Tracks   []optionItem
	Verified int
}

type examWizardView struct {
	User      auth.Identity
	IsNew     bool
	Exam      Exam
	Step      string
	StepTitle string
	Steps     []stepLink
	ReadOnly  bool
	Flash     string
	Error     string
	Errors    map[string]string
	Zone      string
	// Dasar.
	Form     BasicsForm
	Purposes []purposeOption
	Subjects []subjectOption
	Grades   []int
	Owners   []optionItem
	// Soal.
	Plan       Plan
	Comp       compView
	Banks      []Bank
	Rule       RuleForm
	RuleErrors map[string]string
	Kinds      []optionItem
	Diffs      []optionItem
	Objectives []Objective
	VariantOps []optionItem
	// Aturan.
	Rules RulesForm
	// Jadwal.
	Classes     []classRow
	Sessions    []sessionView
	Periods     []admissionPeriodOption
	DefaultDate string
	DefaultTime string
	// Cek.
	Readiness Readiness
}

func (h *Handler) examWizardRoutes(readers, writers chi.Router) {
	readers.Get("/paket/{id}/atur/{step}", h.examWizardStep)
	writers.Get("/paket/baru", h.examNew)
	writers.Post("/paket/", h.examCreate)
	writers.Post("/paket/{id}/atur/dasar", h.examSaveBasics)
	writers.Post("/paket/{id}/atur/aturan", h.examSaveRules)
	writers.Get("/paket/{id}/pilih", h.examPicker)
	writers.Post("/paket/{id}/soal", h.examAddQuestions)
	writers.Post("/paket/{id}/acak", h.examAddRule)
	writers.Post("/paket/{id}/isi/{item}/hapus", h.examRemoveItem)
	writers.Post("/paket/{id}/isi/{item}/geser", h.examMoveItem)
	writers.Post("/paket/{id}/urutan", h.examReorder)
	writers.Post("/paket/{id}/bagian", h.examAddSection)
	writers.Post("/paket/{id}/bagian/{sid}", h.examUpdateSection)
	writers.Post("/paket/{id}/bagian/{sid}/hapus", h.examRemoveSection)
	writers.Post("/paket/{id}/varian", h.examSetVariants)
	writers.Post("/paket/{id}/sesi", h.examCreateSessions)
	writers.Post("/paket/{id}/sesi/{sid}/batal", h.examCancelSession)
	writers.Post("/paket/{id}/jadwalkan", h.examSchedule)
}

func examPath(id string) string                      { return "/ujian/paket/" + id }
func examStepPath(id, step string) string            { return examPath(id) + "/atur/" + step }
func (h *Handler) flash(r *http.Request, msg string) { auth.SetFlash(r.Context(), h.sessions, msg) }

// purposeOffered melaporkan apakah tujuan p ditawarkan ke a saat membuat
// ujian: ulangan dan latihan untuk guru dan admin, lainnya sesuai hak.
func purposeOffered(a Actor, p Purpose, now time.Time) bool {
	if p == PurposeQuiz || p == PurposePractice {
		return a.HasAnyRole(roleTeacher, roleAdmin)
	}
	return CanCreate(a, p, "", now)
}

// examNew menampilkan langkah Dasar untuk ujian baru.
func (h *Handler) examNew(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	p := Purpose(r.URL.Query().Get("tujuan"))
	if !p.Valid() || !ModuleFrom(r.Context()).Has(p.Release()) || !purposeOffered(sa.Actor, p, time.Now()) {
		p = ""
		for _, c := range Purposes {
			if ModuleFrom(r.Context()).Has(c.Release()) && purposeOffered(sa.Actor, c, time.Now()) {
				p = c
				break
			}
		}
	}
	d := defaultsFor(p)
	f := BasicsForm{Purpose: string(p), Duration: strconv.Itoa(d.DurationMinutes), SubjectID: r.URL.Query().Get("mapel")}
	h.renderExamWizard(w, r, http.StatusOK, examWizardView{User: id, IsNew: true, Step: stepBasics, Form: f, Exam: d})
}

func basicsFormFrom(r *http.Request) BasicsForm {
	return BasicsForm{Name: r.PostForm.Get("name"), Purpose: r.PostForm.Get("purpose"), SubjectID: r.PostForm.Get("subject_id"),
		GradeLevel: r.PostForm.Get("grade_level"), Duration: r.PostForm.Get("duration"), OwnerID: r.PostForm.Get("owner_id")}
}

func basicsFormOf(e Exam) BasicsForm {
	f := BasicsForm{Name: e.Name, Purpose: string(e.Purpose), SubjectID: e.SubjectID, Duration: strconv.Itoa(e.DurationMinutes), OwnerID: e.OwnerID}
	if e.GradeLevel > 0 {
		f.GradeLevel = strconv.Itoa(e.GradeLevel)
	}
	return f
}

func (h *Handler) examCreate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 64<<10) {
		return
	}
	id, sa := h.staff(r)
	f := basicsFormFrom(r)
	if p := Purpose(f.Purpose); p.Valid() && !ModuleFrom(r.Context()).Has(p.Release()) {
		page.NotFound(h.render, w, r, id, "Jenis ujian ini belum dibuka untuk sekolah Anda.")
		return
	}
	e, err := CreateExam(r.Context(), h.db, sa, f, time.Now())
	var fe *FieldError
	switch {
	case errors.As(err, &fe):
		d := defaultsFor(Purpose(f.Purpose))
		h.renderExamWizard(w, r, http.StatusUnprocessableEntity, examWizardView{User: id, IsNew: true, Step: stepBasics, Form: f, Errors: fe.Fields, Exam: d})
		return
	case errors.Is(err, ErrForbidden):
		page.Forbidden(h.render, w, r, id, "Anda tidak bisa membuat ujian dengan tujuan atau mapel ini.")
		return
	case err != nil:
		h.fail(w, r, "create exam", err)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions), http.StatusSeeOther)
}

// loadExam memuat ujian dan memeriksa hak minimal want.
func (h *Handler) loadExam(w http.ResponseWriter, r *http.Request, sa StaffActor, want Level) (Exam, bool) {
	id, _ := auth.CurrentUser(r.Context())
	e, err := GetExam(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Ujian tidak ditemukan.")
		return Exam{}, false
	}
	if err != nil {
		h.fail(w, r, "get exam", err)
		return Exam{}, false
	}
	now := time.Now()
	if lv := e.Access(sa.Actor, now); lv < want {
		if lv == None {
			page.NotFound(h.render, w, r, id, "Ujian tidak ditemukan.")
		} else {
			page.Forbidden(h.render, w, r, id, "Anda hanya bisa melihat ujian ini.")
		}
		return Exam{}, false
	}
	if e, err = EnsureLocked(r.Context(), h.db, e, now); err != nil {
		h.fail(w, r, "lock exam", err)
		return Exam{}, false
	}
	return e, true
}

func (h *Handler) examWizardStep(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	step := chi.URLParam(r, "step")
	if !validExamStep(step) {
		page.NotFound(h.render, w, r, id, "Langkah tidak ditemukan.")
		return
	}
	e, ok := h.loadExam(w, r, sa, Read)
	if !ok {
		return
	}
	v := examWizardView{User: id, Exam: e, Step: step, Form: basicsFormOf(e), Rules: RulesFormOf(e)}
	if e.Access(sa.Actor, time.Now()) >= Write && e.Status == ExamDraft {
		_ = SetWizardStep(r.Context(), h.db, sa.SchoolID, e.ID, step)
	}
	h.renderExamWizard(w, r, http.StatusOK, v)
}

func (h *Handler) renderExamWizard(w http.ResponseWriter, r *http.Request, status int, v examWizardView) {
	ctx := r.Context()
	_, sa := h.staff(r)
	now := time.Now()
	loc := v.User.Location()
	v.Zone = clock.ZoneLabel(loc)
	if v.Flash == "" {
		v.Flash = auth.PopFlash(ctx, h.sessions)
	}
	if !v.IsNew {
		v.ReadOnly = v.Exam.Access(sa.Actor, now) < Write
	}
	reached := true
	for i, s := range examWizardSteps {
		l := stepLink{N: i + 1, Key: s.Key, Title: s.Title, State: "todo"}
		if s.Key == v.Step {
			l.State, v.StepTitle, reached = "current", s.Title, false
		} else if reached {
			l.State = "done"
		}
		if !v.IsNew {
			l.URL = examStepPath(v.Exam.ID, s.Key)
		}
		v.Steps = append(v.Steps, l)
	}
	fail := func(op string, err error) { h.fail(w, r, op, err) }
	switch v.Step {
	case stepBasics:
		for _, p := range Purposes {
			if v.IsNew && (!purposeOffered(sa.Actor, p, now) || !ModuleFrom(ctx).Has(p.Release())) {
				continue
			}
			v.Purposes = append(v.Purposes, purposeOption{Value: p, Label: p.Label(), Hint: purposeHints[p], Selected: string(p) == v.Form.Purpose})
		}
		subs, err := h.readableSubjects(r, sa, v.Form.SubjectID)
		if err != nil {
			fail("list subjects", err)
			return
		}
		for _, s := range subs {
			if a := sa.Actor; a.Can(ActClassExam, Resource{SubjectID: s.ID, ClassID: "*"}, now) >= Write || containsStr(a.Subjects, s.ID) ||
				CanCreate(a, PurposeAdmission, s.ID, now) {
				v.Subjects = append(v.Subjects, s)
			}
		}
		v.Grades = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
		if sa.CanAny(ActAgenda, Write) {
			owners, err := h.teacherOptions(r, sa.SchoolID, v.Form.OwnerID)
			if err != nil {
				fail("list teachers", err)
				return
			}
			v.Owners = owners
		}
	case stepQuestions, stepCheck:
		p, err := BuildPlan(ctx, h.db, v.Exam)
		if err != nil {
			fail("build plan", err)
			return
		}
		v.Plan = p
		codes := map[string]string{}
		if v.Exam.SubjectID != "" {
			objs, err := ListObjectives(ctx, h.db, sa.SchoolID, v.Exam.SubjectID, 0)
			if err != nil {
				fail("list objectives", err)
				return
			}
			for _, o := range objs {
				codes[o.ID] = o.Code
				if v.Exam.GradeLevel == 0 || o.GradeLevel == v.Exam.GradeLevel {
					v.Objectives = append(v.Objectives, o)
				}
			}
		}
		v.Comp = newCompView(v.Exam, p, codes)
		if v.Step == stepQuestions {
			banks, err := h.examBanks(r, sa, v.Exam)
			if err != nil {
				fail("list banks", err)
				return
			}
			v.Banks = banks
			v.Kinds = options(kindValues(), func(s string) string { return Kind(s).Label() }, v.Rule.Kind)
			v.Diffs = options(Difficulties, func(s string) string { return s }, v.Rule.Difficulty)
			var counts []string
			for i := 1; i <= len(variantLabels); i++ {
				counts = append(counts, strconv.Itoa(i))
			}
			v.VariantOps = options(counts, func(s string) string {
				if s == "1" {
					return "1 paket"
				}
				n, _ := strconv.Atoi(s)
				return s + " paket (" + strings.Join(variantLabels[:n], "/") + ")"
			}, strconv.Itoa(v.Exam.VariantCount))
			break
		}
		sessions, err := ListSessions(ctx, h.db, sa.SchoolID, v.Exam.ID)
		if err != nil {
			fail("list sessions", err)
			return
		}
		st, err := LoadExamSettings(ctx, h.db, sa.SchoolID)
		if err != nil {
			fail("load exam settings", err)
			return
		}
		if v.Readiness, err = CheckReadiness(ctx, h.db, v.Exam, p, sessions, st, now); err != nil {
			fail("check readiness", err)
			return
		}
	case stepSchedule:
		sessions, err := ListSessions(ctx, h.db, sa.SchoolID, v.Exam.ID)
		if err != nil {
			fail("list sessions", err)
			return
		}
		scheduled := map[string]bool{}
		for _, s := range sessions {
			scheduled[s.ClassID] = true
			v.Sessions = append(v.Sessions, sessionView{Session: s, When: sessionWhen(s, loc), CanCancel: !v.ReadOnly && now.Before(s.StartsAt)})
		}
		tomorrow := now.In(loc).AddDate(0, 0, 1)
		v.DefaultDate, v.DefaultTime = tomorrow.Format("2006-01-02"), "07:30"
		switch {
		case v.Exam.Purpose == PurposeAdmission:
			periods, err := h.admissionPeriods(r, sa.SchoolID)
			if err != nil {
				fail("list periods", err)
				return
			}
			v.Periods = periods
		case !v.Exam.Purpose.SchoolWide():
			classes, err := ClassesFor(ctx, h.db, sa.Actor, v.Exam, now)
			if err != nil {
				fail("list classes", err)
				return
			}
			for _, c := range classes {
				v.Classes = append(v.Classes, classRow{ClassOption: c, Scheduled: scheduled[c.ID]})
			}
		}
	}
	if v.Errors == nil {
		v.Errors = map[string]string{}
	}
	if v.RuleErrors == nil {
		v.RuleErrors = map[string]string{}
	}
	h.render.Render(w, r, status, "exam/exam_wizard", v)
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func kindValues() []string {
	return []string{string(KindMC), string(KindMulti), string(KindTF), string(KindShort), string(KindMatch), string(KindEssay)}
}

// sessionWhen memformat jendela sesi di zona sekolah.
func sessionWhen(s Session, loc *time.Location) string {
	a, b := s.StartsAt.In(loc), s.EndsAt.In(loc)
	out := clock.FormatDateID(a) + " " + a.Format("15.04") + "–"
	if a.Format("2006-01-02") != b.Format("2006-01-02") {
		out += clock.FormatDateID(b) + " "
	}
	return out + b.Format("15.04") + " " + clock.ZoneLabel(loc)
}

// examBanks mengembalikan bank yang boleh dipakai ujian e.
func (h *Handler) examBanks(r *http.Request, sa StaffActor, e Exam) ([]Bank, error) {
	sums, err := ListBanks(r.Context(), h.db, sa.SchoolID, sa.Actor, 0, time.Now())
	if err != nil {
		return nil, err
	}
	var out []Bank
	for _, s := range sums {
		if bankUsable(sa.Actor, e, s.Bank, time.Now()) {
			out = append(out, s.Bank)
		}
	}
	return out, nil
}

func (h *Handler) teacherOptions(r *http.Request, schoolID, selected string) ([]optionItem, error) {
	rows, err := h.db.Query(r.Context(), `SELECT DISTINCT u.id, u.full_name FROM users u
		JOIN user_roles ur ON ur.user_id = u.id JOIN roles ro ON ro.id = ur.role_id AND ro.code = 'guru'
		WHERE u.school_id = $1 AND u.is_active ORDER BY u.full_name`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: list teachers: %w", err)
	}
	defer rows.Close()
	var out []optionItem
	for rows.Next() {
		var o optionItem
		if err := rows.Scan(&o.Value, &o.Label); err != nil {
			return nil, err
		}
		o.Selected = o.Value == selected
		out = append(out, o)
	}
	return out, rows.Err()
}

func (h *Handler) admissionPeriods(r *http.Request, schoolID string) ([]admissionPeriodOption, error) {
	rows, err := h.db.Query(r.Context(), `SELECT p.id, p.name, t.id, t.name,
		(SELECT count(*) FROM applicants a WHERE a.period_id = p.id AND a.status = 'diverifikasi')
		FROM admission_periods p LEFT JOIN admission_tracks t ON t.period_id = p.id
		WHERE p.school_id = $1 ORDER BY p.opens_on DESC, p.name, t.name LIMIT 200`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: list periods: %w", err)
	}
	defer rows.Close()
	var out []admissionPeriodOption
	idx := map[string]int{}
	for rows.Next() {
		var pid, pname string
		var tid, tname *string
		var verified int
		if err := rows.Scan(&pid, &pname, &tid, &tname, &verified); err != nil {
			return nil, err
		}
		i, ok := idx[pid]
		if !ok {
			i = len(out)
			idx[pid] = i
			out = append(out, admissionPeriodOption{ID: pid, Name: pname, Verified: verified})
		}
		if tid != nil {
			out[i].Tracks = append(out[i].Tracks, optionItem{Value: *tid, Label: *tname})
		}
	}
	return out, rows.Err()
}

// examWrite memuat ujian untuk aksi tulis dari form POST.
func (h *Handler) examWrite(w http.ResponseWriter, r *http.Request) (auth.Identity, StaffActor, Exam, bool) {
	if !h.parseForm(w, r, 256<<10) {
		return auth.Identity{}, StaffActor{}, Exam{}, false
	}
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Write)
	return id, sa, e, ok
}

// examFail menangani error umum aksi ujian: terkunci, tidak berhak, dan
// error input ditampilkan sebagai flash di langkah back.
func (h *Handler) examFail(w http.ResponseWriter, r *http.Request, e Exam, back, op string, err error) {
	var fe *FieldError
	switch {
	case errors.Is(err, ErrExamLocked):
		h.flash(r, "Soal ujian sudah dibekukan karena sesi sudah mulai. Salin ujian untuk membuat versi baru.")
	case errors.Is(err, ErrNotYourClass):
		h.flash(r, "Anda hanya bisa memilih kelas yang Anda ampu untuk mapel ini.")
	case errors.Is(err, ErrSectionNotEmpty):
		h.flash(r, "Bagian masih berisi soal, atau tinggal satu-satunya bagian.")
	case errors.Is(err, ErrSessionStarted):
		h.flash(r, "Sesi sudah mulai sehingga tidak bisa dibatalkan.")
	case errors.Is(err, ErrExamRunning):
		h.flash(r, "Ujian sedang berlangsung.")
	case errors.As(err, &fe):
		var msgs []string
		for _, m := range fe.Fields {
			msgs = append(msgs, m)
		}
		sort.Strings(msgs)
		h.flash(r, strings.Join(msgs, " "))
	default:
		h.fail(w, r, op, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) examSaveBasics(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	f := basicsFormFrom(r)
	err := UpdateBasics(r.Context(), h.db, sa, e.ID, f, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		f.Purpose = string(e.Purpose)
		h.renderExamWizard(w, r, http.StatusUnprocessableEntity, examWizardView{User: id, Exam: e, Step: stepBasics, Form: f, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepBasics), "update basics", err)
		return
	}
	h.examAfterSave(w, r, e, stepBasics)
}

// afterSave mengarahkan ke langkah berikutnya, atau ke detail bila
// "Simpan dan keluar".
func (h *Handler) examAfterSave(w http.ResponseWriter, r *http.Request, e Exam, step string) {
	if r.PostForm.Get("action") == "keluar" {
		h.flash(r, "Tersimpan.")
		http.Redirect(w, r, examPath(e.ID), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, nextExamStep(step)), http.StatusSeeOther)
}

func (h *Handler) examSaveRules(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	pf := r.PostForm
	f := RulesForm{ShuffleQuestions: pf.Get("shuffle_questions") != "", ShuffleOptions: pf.Get("shuffle_options") != "",
		ShuffleInStimulus: pf.Get("shuffle_in_stimulus") != "", ResultsMode: pf.Get("results_mode"), ResultsAfterAll: pf.Get("results_after_all") != "",
		MaxAttempts: pf.Get("max_attempts"), PassScore: pf.Get("pass_score"), IntegrityMode: pf.Get("integrity_mode"),
		IntegrityLimit: pf.Get("integrity_limit"), AllowBack: pf.Get("allow_back") != ""}
	err := UpdateRules(r.Context(), h.db, sa, e.ID, f, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderExamWizard(w, r, http.StatusUnprocessableEntity, examWizardView{User: id, Exam: e, Step: stepRules, Rules: f, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepRules), "update rules", err)
		return
	}
	h.examAfterSave(w, r, e, stepRules)
}

func (h *Handler) examAddRule(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	pf := r.PostForm
	f := RuleForm{SectionID: pf.Get("section_id"), BankID: pf.Get("bank_id"), Kind: pf.Get("kind"), Difficulty: pf.Get("difficulty"),
		ObjectiveID: pf.Get("objective_id"), Count: pf.Get("count"), Stimulus: pf.Get("stimulus") != ""}
	err := AddRule(r.Context(), h.db, sa, e.ID, f, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderExamWizard(w, r, http.StatusUnprocessableEntity, examWizardView{User: id, Exam: e, Step: stepQuestions, Rule: f, RuleErrors: fe.Fields,
			Error: "Aturan acak belum bisa ditambahkan. Periksa isian yang ditandai."})
		return
	}
	if err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "add rule", err)
		return
	}
	h.flash(r, "Aturan acak ditambahkan.")
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions)+"#bagian-"+f.SectionID, http.StatusSeeOther)
}

func (h *Handler) examAddQuestions(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	ids := r.PostForm["ids"]
	if len(ids) > 500 {
		ids = ids[:500]
	}
	n, err := AddQuestions(r.Context(), h.db, sa, e.ID, r.PostForm.Get("section_id"), ids, time.Now())
	if err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "add questions", err)
		return
	}
	switch {
	case n == 0:
		h.flash(r, "Tidak ada soal baru yang ditambahkan (soal sudah ada di ujian atau tidak dipilih).")
	default:
		h.flash(r, fmt.Sprintf("%d soal ditambahkan.", n))
	}
	if back := r.PostForm.Get("kembali"); back == "pilih" {
		http.Redirect(w, r, examPath(e.ID)+"/pilih?"+r.PostForm.Get("query"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions), http.StatusSeeOther)
}

func (h *Handler) examRemoveItem(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	if err := RemoveItem(r.Context(), h.db, sa, e.ID, chi.URLParam(r, "item"), time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "remove item", err)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions), http.StatusSeeOther)
}

func (h *Handler) examMoveItem(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	dir := 1
	if r.PostForm.Get("arah") == "naik" {
		dir = -1
	}
	if err := MoveItem(r.Context(), h.db, sa, e.ID, chi.URLParam(r, "item"), dir, time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "move item", err)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions)+"#isi-"+chi.URLParam(r, "item"), http.StatusSeeOther)
}

func (h *Handler) examReorder(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	ids := r.PostForm["ids"]
	if len(ids) > 1000 {
		ids = ids[:1000]
	}
	if err := ReorderItems(r.Context(), h.db, sa, e.ID, r.PostForm.Get("section_id"), ids, time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "reorder", err)
		return
	}
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions), http.StatusSeeOther)
}

func (h *Handler) examAddSection(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	f := SectionForm{Title: r.PostForm.Get("title"), Instructions: r.PostForm.Get("instructions")}
	if err := AddSection(r.Context(), h.db, sa, e.ID, f, time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "add section", err)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions), http.StatusSeeOther)
}

func (h *Handler) examUpdateSection(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	f := SectionForm{Title: r.PostForm.Get("title"), Instructions: r.PostForm.Get("instructions")}
	sid := chi.URLParam(r, "sid")
	if err := UpdateSection(r.Context(), h.db, sa, e.ID, sid, f, time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "update section", err)
		return
	}
	h.flash(r, "Bagian disimpan.")
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions)+"#bagian-"+sid, http.StatusSeeOther)
}

func (h *Handler) examRemoveSection(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	if err := RemoveSection(r.Context(), h.db, sa, e.ID, chi.URLParam(r, "sid"), time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "remove section", err)
		return
	}
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions), http.StatusSeeOther)
}

func (h *Handler) examSetVariants(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	n, _ := strconv.Atoi(r.PostForm.Get("variants"))
	if err := SetVariantCount(r.Context(), h.db, sa, e.ID, n, time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepQuestions), "set variants", err)
		return
	}
	h.flash(r, "Jumlah paket disimpan.")
	http.Redirect(w, r, examStepPath(e.ID, stepQuestions), http.StatusSeeOther)
}

// parseLocal membaca tanggal (YYYY-MM-DD) dan jam (HH:MM) di zona loc.
func parseLocal(date, hm string, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(date)+" "+strings.TrimSpace(hm), loc)
	return t, err == nil
}

func (h *Handler) examCreateSessions(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	loc := id.Location()
	pf := r.PostForm
	back := examStepPath(e.ID, stepSchedule)
	now := time.Now()
	if e.Purpose == PurposeAdmission {
		start, ok1 := parseLocal(pf.Get("date"), pf.Get("start"), loc)
		end, ok2 := parseLocal(pf.Get("end_date"), pf.Get("end"), loc)
		if pf.Get("end_date") == "" {
			end, ok2 = parseLocal(pf.Get("date"), pf.Get("end"), loc)
		}
		if !ok1 || !ok2 {
			start, end = time.Time{}, time.Time{}
		}
		s, err := CreateAdmissionSession(r.Context(), h.db, sa, e.ID, AdmissionSlot{PeriodID: pf.Get("period_id"), TrackID: pf.Get("track_id"),
			Start: start, End: end, Location: pf.Get("location")}, now)
		if err != nil {
			h.examFail(w, r, e, back, "create admission session", err)
			return
		}
		h.flash(r, fmt.Sprintf("Sesi dibuat dengan %d pendaftar terverifikasi.", s.Participants))
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	var slots []ClassSlot
	for _, cid := range pf["class_id"] {
		start, ok1 := parseLocal(pf.Get("date_"+cid), pf.Get("start_"+cid), loc)
		end, ok2 := parseLocal(pf.Get("date_"+cid), pf.Get("end_"+cid), loc)
		if !ok1 || !ok2 {
			start, end = time.Time{}, time.Time{}
		}
		slots = append(slots, ClassSlot{ClassID: cid, Start: start, End: end, Location: pf.Get("location_" + cid)})
	}
	n, err := CreateClassSessions(r.Context(), h.db, sa, e.ID, slots, now)
	if err != nil {
		h.examFail(w, r, e, back, "create class sessions", err)
		return
	}
	h.flash(r, fmt.Sprintf("%d sesi dibuat.", n))
	if pf.Get("action") == "lanjut" {
		back = examStepPath(e.ID, stepCheck)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) examCancelSession(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	if err := CancelSession(r.Context(), h.db, sa, e.ID, chi.URLParam(r, "sid"), time.Now()); err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepSchedule), "cancel session", err)
		return
	}
	h.flash(r, "Sesi dibatalkan. Kode ujian sesi itu tidak berlaku lagi.")
	http.Redirect(w, r, examStepPath(e.ID, stepSchedule), http.StatusSeeOther)
}

func (h *Handler) examSchedule(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	err := ScheduleExam(r.Context(), h.db, sa, e.ID, time.Now())
	if errors.Is(err, ErrNotReady) {
		h.flash(r, "Masih ada butir merah di cek kesiapan.")
		http.Redirect(w, r, examStepPath(e.ID, stepCheck), http.StatusSeeOther)
		return
	}
	if err != nil {
		h.examFail(w, r, e, examStepPath(e.ID, stepCheck), "schedule exam", err)
		return
	}
	msg := "Ujian dijadwalkan."
	if e.Purpose.SchoolWide() {
		msg = "Ujian ditandai siap. Koordinator menjadwalkannya lewat agenda."
	}
	h.flash(r, msg)
	http.Redirect(w, r, examPath(e.ID), http.StatusSeeOther)
}

// pickerView adalah halaman pilih soal dari bank.
type pickerView struct {
	User       auth.Identity
	Exam       Exam
	Banks      []Bank
	Bank       Bank
	Filter     QuestionFilter
	Page       QuestionPage
	Groups     []questionGroup
	InExam     map[string]bool
	Sections   []Section
	SectionID  string
	Kinds      []optionItem
	Diffs      []optionItem
	Objectives []Objective
	Query      string
	PrevURL    string
	NextURL    string
	Flash      string
	Error      string
}

// examPicker menampilkan daftar soal bank dengan filter dan centang.
func (h *Handler) examPicker(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Write)
	if !ok {
		return
	}
	if !e.Editable() {
		h.examFail(w, r, e, examPath(e.ID), "picker", ErrExamLocked)
		return
	}
	ctx := r.Context()
	banks, err := h.examBanks(r, sa, e)
	if err != nil {
		h.fail(w, r, "list banks", err)
		return
	}
	q := r.URL.Query()
	v := pickerView{User: id, Exam: e, Banks: banks, InExam: map[string]bool{}, SectionID: q.Get("bagian"), Flash: auth.PopFlash(ctx, h.sessions)}
	for _, b := range banks {
		if b.ID == q.Get("bank") || (q.Get("bank") == "" && v.Bank.ID == "") {
			v.Bank = b
		}
	}
	secs, err := LoadContent(ctx, h.db, e)
	if err != nil {
		h.fail(w, r, "load content", err)
		return
	}
	v.Sections = secs
	for _, s := range secs {
		for _, it := range s.Items {
			if it.Question != nil {
				v.InExam[it.Question.ID] = true
			}
		}
	}
	if v.SectionID == "" && len(secs) > 0 {
		v.SectionID = secs[len(secs)-1].ID
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("hal")); err == nil {
		page = p
	}
	v.Filter = QuestionFilter{Text: q.Get("q"), Kind: q.Get("jenis"), ObjectiveID: q.Get("tp"), Difficulty: q.Get("kesulitan"), Status: q.Get("status"), Page: page}
	v.Kinds = options(kindValues(), func(s string) string { return Kind(s).Label() }, v.Filter.Kind)
	v.Diffs = options(Difficulties, func(s string) string { return s }, v.Filter.Difficulty)
	if v.Bank.ID != "" {
		if v.Page, err = ListQuestions(ctx, h.db, sa.Actor, v.Bank, v.Filter, time.Now()); err != nil {
			h.fail(w, r, "list questions", err)
			return
		}
		v.Groups = groupRows(v.Page.Rows)
		if v.Bank.SubjectID != "" {
			v.Objectives, _ = ListObjectives(ctx, h.db, sa.SchoolID, v.Bank.SubjectID, 0)
		}
	}
	vals := url.Values{}
	for k, val := range map[string]string{"bank": v.Bank.ID, "q": v.Filter.Text, "jenis": v.Filter.Kind, "tp": v.Filter.ObjectiveID,
		"kesulitan": v.Filter.Difficulty, "status": v.Filter.Status, "bagian": v.SectionID} {
		if val != "" {
			vals.Set(k, val)
		}
	}
	v.Query = vals.Encode()
	if v.Page.Page > 1 {
		vals.Set("hal", strconv.Itoa(v.Page.Page-1))
		v.PrevURL = examPath(e.ID) + "/pilih?" + vals.Encode()
	}
	if v.Page.Page < v.Page.Pages {
		vals.Set("hal", strconv.Itoa(v.Page.Page+1))
		v.NextURL = examPath(e.ID) + "/pilih?" + vals.Encode()
	}
	h.render.Render(w, r, http.StatusOK, "exam/exam_picker", v)
}

// groupRows mengelompokkan baris soal per stimulus (berurutan).
func groupRows(rows []QuestionRow) []questionGroup {
	var out []questionGroup
	for _, row := range rows {
		n := len(out)
		if row.StimulusID != "" && n > 0 && out[n-1].StimulusID == row.StimulusID {
			out[n-1].Rows = append(out[n-1].Rows, row)
			continue
		}
		out = append(out, questionGroup{StimulusID: row.StimulusID, StimulusTitle: row.StimulusTitle, Rows: []QuestionRow{row}})
	}
	return out
}
