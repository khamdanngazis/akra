package exam

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// examRoutes mendaftarkan route ujian (T133).
func (h *Handler) examRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		// Peran: admin_tu, kepala_sekolah (baca), guru (ujian yang ia
		// pegang; koordinator semua kecuali tes seleksi). Cek per ujian di
		// handler (Exam.Access).
		r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleGuru))
		r.Use(RequireAccess(h.db, h.render, h.logger, ActClassExam, Read))
		r.Get("/paket/", h.examList)
		r.Get("/paket/{id}", h.examDetail)
		readers := r
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleGuru))
			h.examWizardRoutes(readers, r)
			h.examDocsRoutes(readers, r)
			h.followUpRoutes(r)
			h.resultRoutes(readers, r)
			r.Post("/paket/{id}/soal/{qid}/batal", h.examVoidQuestion)
			r.Get("/paket/{id}/soal/{qid}/kunci", h.keyFixForm)
			r.Post("/paket/{id}/soal/{qid}/kunci", h.keyFixPost)
			r.Post("/paket/{id}/ralat", h.examCorrection)
			r.Post("/paket/{id}/salin", h.examCopy)
			r.Post("/paket/{id}/arsip", h.examArchive)
		})
	})
}

type examListItem struct {
	ExamListRow
	When string
	Href string
}

type examListView struct {
	User      auth.Identity
	Rows      []examListItem
	Archived  bool
	Purposes  []purposeOption
	CanCreate bool
	Flash     string
	Error     string
}

func (h *Handler) examList(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	archived := r.URL.Query().Get("arsip") == "1"
	rows, err := ListExams(r.Context(), h.db, sa.SchoolID, sa.Actor, archived)
	if err != nil {
		h.fail(w, r, "list exams", err)
		return
	}
	now := time.Now()
	v := examListView{User: id, Archived: archived, Flash: auth.PopFlash(r.Context(), h.sessions)}
	loc := id.Location()
	for _, row := range rows {
		it := examListItem{ExamListRow: row, Href: examPath(row.ID)}
		if row.Status == ExamDraft && row.Access(sa.Actor, now) >= Write {
			it.Href = examStepPath(row.ID, row.WizardStep)
		}
		if row.FirstStart != nil {
			it.When = clock.FormatDateTimeID(*row.FirstStart, loc)
		}
		v.Rows = append(v.Rows, it)
	}
	for _, p := range Purposes {
		if purposeOffered(sa.Actor, p, now) && ModuleFrom(r.Context()).Has(p.Release()) {
			v.Purposes = append(v.Purposes, purposeOption{Value: p, Label: p.Label(), Hint: purposeHints[p]})
		}
	}
	v.CanCreate = len(v.Purposes) > 0
	h.render.Render(w, r, http.StatusOK, "exam/exams", v)
}

// HasAnyRole melaporkan apakah a punya salah satu peran.
func (a Actor) HasAnyRole(roles ...string) bool {
	for _, r := range roles {
		if containsStr(a.Roles, r) {
			return true
		}
	}
	return false
}

// nextStep adalah kartu "langkah berikutnya" di detail ujian (pola T116).
type nextStep struct {
	Tone   string // info, warn, ok, muted
	Title  string
	Hint   string
	Label  string
	URL    string
	Flow   []flowPoint
	Method string // "post" bila tombol mengirim form
}

type flowPoint struct {
	Label string
	State string // done, current, todo
}

var examFlow = []string{"Disusun", "Dijadwalkan", "Berlangsung", "Dinilai"}

func examNextStep(e Exam, rd Readiness, sessions []Session, canWrite bool, now time.Time) nextStep {
	reached := map[string]int{ExamDraft: 0, ExamScheduled: 1, ExamRunning: 2, ExamDone: 3, ExamArchived: 3}[e.Status]
	var n nextStep
	for i, l := range examFlow {
		st := "todo"
		switch {
		case i < reached:
			st = "done"
		case i == reached:
			st = "current"
		}
		n.Flow = append(n.Flow, flowPoint{Label: l, State: st})
	}
	switch e.Status {
	case ExamDraft:
		n.Tone, n.Title = "info", "Lanjutkan menyusun ujian"
		n.Hint = "Ujian masih draf dan belum bisa dikerjakan peserta."
		if canWrite {
			n.Label, n.URL = "Lanjutkan", examStepPath(e.ID, e.WizardStep)
			if !rd.Blocked && len(rd.Items) > 0 {
				n.Title, n.Hint = "Siap dijadwalkan", "Semua butir merah di cek kesiapan sudah beres."
				n.Label, n.URL = "Cek & jadwalkan", examStepPath(e.ID, stepCheck)
			}
		}
	case ExamScheduled:
		n.Tone, n.Title = "ok", "Terjadwal"
		if len(sessions) > 0 {
			n.Hint = "Sesi pertama mulai " + clock.FormatDateID(sessions[0].StartsAt) + ". Soal dibekukan saat sesi pertama mulai."
		} else {
			n.Hint = "Soal sudah siap. Koordinator menjadwalkan sesi lewat agenda."
		}
		if canWrite {
			n.Label, n.URL = "Lihat sesi & peserta", examPath(e.ID)+"?tab=sesi"
		}
	case ExamRunning:
		n.Tone, n.Title = "warn", "Ujian berlangsung"
		active := 0
		for _, s := range sessions {
			if !now.Before(s.StartsAt) && now.Before(s.EndsAt) {
				active++
			}
		}
		n.Hint = "Soal sudah dibekukan. Menyunting soal di bank tidak mengubah ujian ini."
		if active > 0 {
			n.Hint = strconv.Itoa(active) + " sesi sedang berjalan. " + n.Hint
		}
	default:
		n.Tone, n.Title = "muted", "Ujian selesai"
		n.Hint = "Soal objektif sudah dinilai otomatis. Lihat tab Hasil untuk nilai, koreksi esai, analisis butir, dan analisis TP."
	}
	return n
}

type examDetailView struct {
	User         auth.Identity
	Exam         Exam
	Tab          string
	CanWrite     bool
	Next         nextStep
	Readiness    Readiness
	Comp         compView
	Variants     []Variant
	Variant      Variant
	Paper        []PaperItem
	Sections     map[string]Section
	Sessions     []sessionView
	Session      *sessionView
	Participants []ParticipantRow
	Rules        RulesForm
	DocSessions  []docSession
	Follow       followView
	DefaultDate  string
	Voided       map[string]VoidInfo // soal dibatalkan (T144)
	Overrides    map[string]bool     // soal dengan kunci dibetulkan (T146)
	Running      int                 // sesi berlangsung, untuk ralat
	Flash        string
	Error        string
	Zone         string
}

func (h *Handler) examDetail(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Read)
	if !ok {
		return
	}
	ctx := r.Context()
	now := time.Now()
	loc := id.Location()
	tab := r.URL.Query().Get("tab")
	if tab == "hasil" {
		// Hasil pindah ke halaman sendiri (T148).
		http.Redirect(w, r, resultsPath(e.ID), http.StatusSeeOther)
		return
	}
	switch tab {
	case "soal", "sesi", "dokumen", "lanjut":
	default:
		tab = "ringkasan"
	}
	v := examDetailView{User: id, Exam: e, Tab: tab, CanWrite: e.Access(sa.Actor, now) >= Write, Rules: RulesFormOf(e),
		Flash: auth.PopFlash(ctx, h.sessions), Zone: clock.ZoneLabel(loc), Sections: map[string]Section{}}
	plan, err := BuildPlan(ctx, h.db, e)
	if err != nil {
		h.fail(w, r, "build plan", err)
		return
	}
	sessions, err := ListSessions(ctx, h.db, sa.SchoolID, e.ID)
	if err != nil {
		h.fail(w, r, "list sessions", err)
		return
	}
	for _, s := range sessions {
		v.Sessions = append(v.Sessions, sessionView{Session: s, When: sessionWhen(s, loc), CanCancel: v.CanWrite && now.Before(s.StartsAt)})
	}
	st, err := LoadExamSettings(ctx, h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "load exam settings", err)
		return
	}
	if !e.Locked() {
		if v.Readiness, err = CheckReadiness(ctx, h.db, e, plan, sessions, st, now); err != nil {
			h.fail(w, r, "check readiness", err)
			return
		}
	}
	codes := map[string]string{}
	if e.SubjectID != "" {
		objs, _ := ListObjectives(ctx, h.db, sa.SchoolID, e.SubjectID, 0)
		for _, o := range objs {
			codes[o.ID] = o.Code
		}
	}
	v.Comp = newCompView(e, plan, codes)
	v.Variants = plan.Variants
	for _, s := range plan.Sections {
		v.Sections[s.ID] = s
	}
	v.Next = examNextStep(e, v.Readiness, sessions, v.CanWrite, now)
	switch tab {
	case "soal":
		if len(v.Variants) > 0 {
			v.Variant = v.Variants[0]
			for _, vr := range v.Variants {
				if vr.Label == r.URL.Query().Get("paket") {
					v.Variant = vr
				}
			}
			if v.Paper, err = LoadPaper(ctx, h.db, e, v.Variant.ID); err != nil {
				h.fail(w, r, "load paper", err)
				return
			}
		}
		if e.Locked() {
			if v.Voided, err = VoidedQuestions(ctx, h.db, sa.SchoolID, e.ID); err != nil {
				h.fail(w, r, "voided questions", err)
				return
			}
			running, err := RunningExamSessions(ctx, h.db, sa.SchoolID, e.ID, now)
			if err != nil {
				h.fail(w, r, "running sessions", err)
				return
			}
			v.Running = len(running)
			ov, err := keyOverrides(ctx, h.db, sa.SchoolID, e.ID)
			if err != nil {
				h.fail(w, r, "key overrides", err)
				return
			}
			v.Overrides = map[string]bool{}
			for id := range ov {
				v.Overrides[id] = true
			}
		}
	case "dokumen":
		v.DocSessions = h.examDocSessions(r, v.Sessions, loc, now)
	case "lanjut":
		if v.Follow, err = h.loadFollowView(r, e, codes, plan.Composition(), now); err != nil {
			h.fail(w, r, "followup view", err)
			return
		}
		v.DefaultDate = now.In(loc).AddDate(0, 0, 1).Format("2006-01-02")
	case "sesi":
		if sid := r.URL.Query().Get("sesi"); sid != "" {
			for i := range v.Sessions {
				if v.Sessions[i].ID == sid {
					v.Session = &v.Sessions[i]
				}
			}
			if v.Session != nil {
				if v.Participants, err = ListParticipants(ctx, h.db, sa.SchoolID, v.Session.ID); err != nil {
					h.fail(w, r, "list participants", err)
					return
				}
			}
		}
	}
	h.render.Render(w, r, http.StatusOK, "exam/exam_detail", v)
}

func (h *Handler) examCopy(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 16<<10) {
		return
	}
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Read)
	if !ok {
		return
	}
	cp, err := CopyExam(r.Context(), h.db, sa, e.ID, r.PostForm.Get("name"), time.Now())
	if errors.Is(err, ErrNotEditable) {
		page.Forbidden(h.render, w, r, id, "Anda tidak bisa membuat ujian dengan tujuan atau mapel ini.")
		return
	}
	if err != nil {
		h.examFail(w, r, e, examPath(e.ID), "copy exam", err)
		return
	}
	h.flash(r, "Ujian disalin. Atur jadwal untuk kelas atau tahun yang baru.")
	http.Redirect(w, r, examStepPath(cp.ID, stepBasics), http.StatusSeeOther)
}

func (h *Handler) examArchive(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	if err := ArchiveExam(r.Context(), h.db, sa, e.ID, time.Now()); err != nil {
		h.examFail(w, r, e, examPath(e.ID), "archive exam", err)
		return
	}
	h.flash(r, "Ujian diarsipkan. Sesi yang belum mulai dibatalkan.")
	http.Redirect(w, r, "/ujian/paket/", http.StatusSeeOther)
}
