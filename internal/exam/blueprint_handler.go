package exam

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/settings"
)

// blueprintRoutes mendaftarkan kisi-kisi, kartu soal, cek mutu, dan telaah
// (T132).
func (h *Handler) blueprintRoutes(readers, writers chi.Router) {
	readers.Get("/bank/{id}/kisi-kisi", h.blueprintPrint)
	readers.Get("/bank/{id}/kisi-kisi.xlsx", h.blueprintXLSX)
	readers.Get("/bank/{id}/kartu-soal", h.questionCards)
	readers.Get("/bank/{id}/mutu", h.quality)
	writers.Get("/telaah", h.reviewQueue)
	writers.Post("/soal/{id}/ajukan", h.reviewSubmit)
	writers.Post("/soal/{id}/telaah", h.reviewDecide)
}

// settingsRoutes: pengaturan modul ujian (admin_tu, koordinator).
func (h *Handler) settingsRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(RequireAccess(h.db, h.render, h.logger, ActAgenda, Write))
		r.Get("/pengaturan", h.examSettings)
		r.Post("/pengaturan", h.examSettingsSave)
	})
}

// letterhead adalah kop dokumen cetak (dari pengaturan rapor).
type letterhead struct {
	School      string
	Address     string
	Principal   string
	SignerTitle string
}

func (h *Handler) letterhead(r *http.Request, schoolID string) letterhead {
	var l letterhead
	_ = h.db.QueryRow(r.Context(), `SELECT name, coalesce(address, '') FROM schools WHERE id = $1`, schoolID).Scan(&l.School, &l.Address)
	l.Principal, _, _ = settings.Get(r.Context(), h.db, schoolID, settings.ReportPrincipalName)
	l.SignerTitle, _, _ = settings.Get(r.Context(), h.db, schoolID, settings.ReportSignerTitle)
	if l.SignerTitle == "" {
		l.SignerTitle = "Kepala Sekolah"
	}
	return l
}

// docQuestions mengembalikan soal untuk dokumen: soal terpilih (?ids=)
// atau semua soal sesuai filter daftar bank.
func (h *Handler) docQuestions(w http.ResponseWriter, r *http.Request) (Bank, []Question, bool) {
	_, sa := h.staff(r)
	b, ok := h.loadBank(w, r, sa)
	if !ok {
		return Bank{}, nil, false
	}
	q := r.URL.Query()
	f := QuestionFilter{Text: q.Get("q"), Kind: q.Get("jenis"), ObjectiveID: q.Get("tp"), Difficulty: q.Get("kesulitan"),
		Status: q.Get("status"), CreatedBy: q.Get("pembuat")}
	var ids []string
	for _, id := range q["ids"] {
		if len(ids) < 500 {
			ids = append(ids, id)
		}
	}
	rows, err := AllQuestions(r.Context(), h.db, sa.Actor, b, f, ids, time.Now())
	if err != nil {
		h.fail(w, r, "document questions", err)
		return Bank{}, nil, false
	}
	qs := make([]Question, len(rows))
	for i, row := range rows {
		qs[i] = row.Question
	}
	return b, qs, true
}

func (h *Handler) objectiveMap(r *http.Request, b Bank) map[string]Objective {
	out := map[string]Objective{}
	if b.SubjectID == "" {
		return out
	}
	objs, _ := ListObjectives(r.Context(), h.db, b.SchoolID, b.SubjectID, 0)
	for _, o := range objs {
		out[o.ID] = o
	}
	return out
}

type blueprintView struct {
	User       auth.Identity
	Bank       Bank
	Head       letterhead
	Blueprint  Blueprint
	Settings   ExamSettings
	Query      string
	PrintedAt  string
	CanSetting bool
}

func (h *Handler) blueprintPrint(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	b, qs, ok := h.docQuestions(w, r)
	if !ok {
		return
	}
	s, err := LoadExamSettings(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "blueprint", err)
		return
	}
	v := blueprintView{User: id, Bank: b, Head: h.letterhead(r, sa.SchoolID), Settings: s,
		Blueprint: BuildBlueprint(qs, h.objectiveMap(r, b), s.Targets), Query: r.URL.RawQuery,
		PrintedAt: clockDate(id), CanSetting: sa.CanAny(ActAgenda, Write)}
	h.render.Render(w, r, http.StatusOK, "exam/blueprint_print", v)
}

func (h *Handler) blueprintXLSX(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	b, qs, ok := h.docQuestions(w, r)
	if !ok {
		return
	}
	s, err := LoadExamSettings(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "blueprint xlsx", err)
		return
	}
	data, err := BlueprintXLSX("Kisi-kisi "+b.Name, BuildBlueprint(qs, h.objectiveMap(r, b), s.Targets))
	if err != nil {
		h.fail(w, r, "blueprint xlsx", err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="kisi-kisi-`+safeFileName(b.Name)+`.xlsx"`)
	_, _ = w.Write(data)
}

// questionCard adalah satu kartu soal.
type questionCard struct {
	No          int
	Question    Question
	View        QuestionView
	Objective   string
	Key         []string
	Explanation template.HTML
	Rubric      template.HTML
	Stats       string
	Creator     string
}

type cardsView struct {
	User      auth.Identity
	Bank      Bank
	Head      letterhead
	Cards     []questionCard
	PrintedAt string
}

func (h *Handler) questionCards(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	b, qs, ok := h.docQuestions(w, r)
	if !ok {
		return
	}
	objs := h.objectiveMap(r, b)
	names := map[string]string{}
	v := cardsView{User: id, Bank: b, Head: h.letterhead(r, sa.SchoolID), PrintedAt: clockDate(id)}
	stims := map[string]Stimulus{}
	for i, q := range qs {
		c := questionCard{No: i + 1, Question: q, View: NewQuestionView(ForParticipant(q), i+1, staffAssetURL), Key: keyLines(q),
			Explanation: trusted(q.ExplanationHTML, staffAssetURL), Rubric: trusted(q.Key.RubricHTML, staffAssetURL)}
		if q.StimulusID != "" {
			s, ok := stims[q.StimulusID]
			if !ok {
				if s, _ = GetStimulus(r.Context(), h.db, sa.SchoolID, q.StimulusID); s.ID != "" {
					stims[q.StimulusID] = s
				}
			}
			if s.ID != "" {
				c.View = c.View.WithStimulus(s, staffAssetURL)
			}
		}
		if o, ok := objs[q.ObjectiveID]; ok {
			c.Objective = o.Code + " — " + o.Description
		}
		if _, ok := names[q.CreatedBy]; !ok {
			var n string
			_ = h.db.QueryRow(r.Context(), `SELECT full_name FROM users WHERE school_id = $1 AND id = $2`, sa.SchoolID, q.CreatedBy).Scan(&n)
			names[q.CreatedBy] = n
		}
		c.Creator = names[q.CreatedBy]
		var p, d *float64
		var n int
		if err := h.db.QueryRow(r.Context(), `SELECT difficulty_index::float8, discrimination::float8, participants FROM exam_question_stats
			WHERE question_id = $1 ORDER BY computed_at DESC LIMIT 1`, q.ID).Scan(&p, &d, &n); err == nil && p != nil {
			c.Stats = "p = " + strconv.FormatFloat(*p, 'f', 2, 64)
			if d != nil {
				c.Stats += ", D = " + strconv.FormatFloat(*d, 'f', 2, 64)
			}
			c.Stats += " (" + strconv.Itoa(n) + " peserta)"
		}
		v.Cards = append(v.Cards, c)
	}
	h.render.Render(w, r, http.StatusOK, "exam/question_card_print", v)
}

func clockDate(id auth.Identity) string {
	return clock.FormatDateID(time.Now().In(id.Location()))
}

type qualityView struct {
	User   auth.Identity
	Bank   Bank
	Report QualityReport
}

func (h *Handler) quality(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	b, qs, ok := h.docQuestions(w, r)
	if !ok {
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/quality", qualityView{User: id, Bank: b, Report: CheckQuality(qs)})
}

type reviewQueueView struct {
	User   auth.Identity
	Rows   []QuestionRow
	Review bool
	Flash  string
	Error  string
}

func (h *Handler) reviewQueue(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	s, err := LoadExamSettings(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "review queue", err)
		return
	}
	v := reviewQueueView{User: id, Review: s.Review, Flash: auth.PopFlash(r.Context(), h.sessions)}
	if s.Review {
		if v.Rows, err = ReviewQueue(r.Context(), h.db, sa, time.Now()); err != nil {
			h.fail(w, r, "review queue", err)
			return
		}
	}
	h.render.Render(w, r, http.StatusOK, "exam/review_queue", v)
}

func reviewMessage(err error) string {
	switch {
	case errors.Is(err, ErrReviewOff):
		return "Telaah soal tidak aktif di sekolah ini."
	case errors.Is(err, ErrReviewOwn):
		return "Soal buatan sendiri tidak bisa Anda telaah. Minta guru mapel lain atau koordinator."
	case errors.Is(err, ErrReviewState):
		return "Status soal sudah berubah. Muat ulang halaman."
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		var parts []string
		for _, m := range fe.Fields {
			parts = append(parts, m)
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func (h *Handler) reviewSubmit(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	q, _, ok := h.loadQuestion(w, r, sa)
	if !ok {
		return
	}
	qid := q.ID
	err := SubmitForReview(r.Context(), h.db, sa, qid, time.Now())
	if msg := reviewMessage(err); msg != "" {
		auth.SetFlash(r.Context(), h.sessions, msg)
	} else if err != nil {
		h.fail(w, r, "submit review", err)
		return
	} else {
		auth.SetFlash(r.Context(), h.sessions, "Soal diajukan untuk ditelaah.")
	}
	http.Redirect(w, r, "/ujian/soal/"+qid, http.StatusSeeOther)
}

func (h *Handler) reviewDecide(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 16<<10) {
		return
	}
	_, sa := h.staff(r)
	q, _, ok := h.loadQuestion(w, r, sa)
	if !ok {
		return
	}
	qid := q.ID
	err := ReviewQuestion(r.Context(), h.db, sa, qid, r.PostForm.Get("decision"), r.PostForm.Get("note"), time.Now())
	if msg := reviewMessage(err); msg != "" {
		auth.SetFlash(r.Context(), h.sessions, msg)
		http.Redirect(w, r, "/ujian/soal/"+qid, http.StatusSeeOther)
		return
	}
	if err != nil {
		h.fail(w, r, "review question", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, map[string]string{"setuju": "Soal disetujui dan siap dipakai.", "perbaiki": "Catatan perbaikan dikirim ke pembuat soal."}[r.PostForm.Get("decision")])
	http.Redirect(w, r, "/ujian/telaah", http.StatusSeeOther)
}

type examSettingsView struct {
	User       auth.Identity
	Review     bool
	Cognitive  string
	Difficulty string
	Errors     map[string]string
	Flash      string
	Error      string
}

func (h *Handler) examSettings(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	s, err := LoadExamSettings(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "exam settings", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/settings", examSettingsView{User: id, Review: s.Review,
		Cognitive: FormatTarget(s.Targets.Cognitive), Difficulty: FormatTarget(s.Targets.Difficulty), Flash: auth.PopFlash(r.Context(), h.sessions)})
}

func (h *Handler) examSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 4<<10) {
		return
	}
	id, sa := h.staff(r)
	p := r.PostForm
	review := p.Get("review") == "on"
	err := SaveExamSettings(r.Context(), h.db, sa, review, p.Get("cognitive"), p.Get("difficulty"))
	var fe *FieldError
	if errors.As(err, &fe) {
		h.render.Render(w, r, http.StatusUnprocessableEntity, "exam/settings", examSettingsView{User: id, Review: review,
			Cognitive: p.Get("cognitive"), Difficulty: p.Get("difficulty"), Errors: fe.Fields, Error: "Pengaturan belum disimpan."})
		return
	}
	if err != nil {
		h.fail(w, r, "save exam settings", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Pengaturan ujian disimpan.")
	http.Redirect(w, r, "/ujian/pengaturan", http.StatusSeeOther)
}
