package exam

import (
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/exam/richtext"
)

type stimulusFormView struct {
	User     auth.Identity
	Bank     Bank
	Stimulus Stimulus
	IsNew    bool
	Title    string
	Body     template.HTML
	Numbered bool
	Errors   map[string]string
}

func (h *Handler) renderStimulusForm(w http.ResponseWriter, r *http.Request, status int, v stimulusFormView) {
	v.User, _ = auth.CurrentUser(r.Context())
	h.render.Render(w, r, status, "exam/stimulus_form", v)
}

func (h *Handler) stimulusNew(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	b, ok := h.writableBank(w, r, sa, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.renderStimulusForm(w, r, http.StatusOK, stimulusFormView{Bank: b, IsNew: true})
}

// buildStimulus memproses isian form stimulus.
func (h *Handler) buildStimulus(r *http.Request) (Stimulus, []string, error) {
	s := Stimulus{Title: r.PostForm.Get("title"), Numbered: r.PostForm.Get("numbered") == "on"}
	doc, err := richtext.Process(r.PostForm.Get("body"), h.math)
	if err != nil {
		return s, nil, &FieldError{Fields: map[string]string{"body": "Isi stimulus terlalu panjang atau tidak terbaca."}}
	}
	s.BodySrc, s.BodyHTML = doc.Src, doc.HTML
	fields := map[string]string{}
	if len(doc.MathErrors) > 0 {
		fields["body"] = "Rumus tidak valid (" + doc.MathErrors[0].TeX + "): " + doc.MathErrors[0].Message
	} else if doc.MissingAlt > 0 {
		fields["body"] = "Isi teks alternatif untuk setiap gambar."
	}
	if len(fields) > 0 {
		return s, nil, &FieldError{Fields: fields}
	}
	return s, doc.Assets, nil
}

func (h *Handler) stimulusCreate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxEditorForm) {
		return
	}
	_, sa := h.staff(r)
	b, ok := h.writableBank(w, r, sa, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	s, assets, err := h.buildStimulus(r)
	if err == nil {
		err = checkAssets(r.Context(), h.db, sa.SchoolID, assets)
	}
	var saved Stimulus
	if err == nil {
		saved, err = CreateStimulus(r.Context(), h.db, sa, b.ID, s, time.Now())
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderStimulusForm(w, r, http.StatusUnprocessableEntity, stimulusFormView{Bank: b, IsNew: true, Title: s.Title,
			Body: safeSrc(r.PostForm.Get("body")), Numbered: s.Numbered, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "create stimulus", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Stimulus disimpan. Tambahkan soal-soal untuk stimulus ini.")
	http.Redirect(w, r, "/ujian/stimulus/"+saved.ID, http.StatusSeeOther)
}

// loadStimulus membaca stimulus dan banknya dengan cek baca.
func (h *Handler) loadStimulus(w http.ResponseWriter, r *http.Request, sa StaffActor) (Stimulus, Bank, bool) {
	s, err := GetStimulus(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	var b Bank
	if err == nil {
		b, err = GetBank(r.Context(), h.db, sa.SchoolID, s.BankID)
	}
	if err == nil && sa.Can(ActBank, b.resource(), time.Now()) < Read {
		err = ErrNotFound
	}
	if err != nil {
		h.fail(w, r, "load stimulus", err)
		return Stimulus{}, Bank{}, false
	}
	return s, b, true
}

func (h *Handler) stimulusEdit(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	s, b, ok := h.loadStimulus(w, r, sa)
	if !ok {
		return
	}
	if !CanEditStimulus(sa.Actor, b, s, time.Now()) {
		h.fail(w, r, "edit stimulus", ErrNotEditable)
		return
	}
	h.renderStimulusForm(w, r, http.StatusOK, stimulusFormView{Bank: b, Stimulus: s, Title: s.Title, Body: safeSrc(s.BodySrc), Numbered: s.Numbered})
}

func (h *Handler) stimulusUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxEditorForm) {
		return
	}
	_, sa := h.staff(r)
	cur, b, ok := h.loadStimulus(w, r, sa)
	if !ok {
		return
	}
	s, assets, err := h.buildStimulus(r)
	if err == nil {
		err = checkAssets(r.Context(), h.db, sa.SchoolID, assets)
	}
	if err == nil {
		_, err = UpdateStimulus(r.Context(), h.db, sa, cur.ID, s, time.Now())
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderStimulusForm(w, r, http.StatusUnprocessableEntity, stimulusFormView{Bank: b, Stimulus: cur, Title: s.Title,
			Body: safeSrc(r.PostForm.Get("body")), Numbered: s.Numbered, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "update stimulus", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Stimulus disimpan sebagai versi baru.")
	http.Redirect(w, r, "/ujian/stimulus/"+cur.ID, http.StatusSeeOther)
}

type stimulusPageView struct {
	User      auth.Identity
	Bank      Bank
	Stimulus  Stimulus
	Body      template.HTML
	Questions []QuestionView
	Rows      []Question
	CanEdit   bool
	CanWrite  bool
	Flash     string
	Error     string
}

func (h *Handler) stimulusView(w http.ResponseWriter, r *http.Request) {
	h.showStimulus(w, r, http.StatusOK, "")
}

func (h *Handler) showStimulus(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	id, sa := h.staff(r)
	s, b, ok := h.loadStimulus(w, r, sa)
	if !ok {
		return
	}
	qs, err := StimulusQuestions(r.Context(), h.db, sa.SchoolID, s.ID)
	if err != nil {
		h.fail(w, r, "stimulus questions", err)
		return
	}
	now := time.Now()
	v := stimulusPageView{User: id, Bank: b, Stimulus: s, Body: trusted(s.BodyHTML, staffAssetURL), Rows: qs, Error: errMsg,
		CanEdit:  CanEditStimulus(sa.Actor, b, s, now),
		CanWrite: b.ArchivedAt == nil && sa.Can(ActBank, b.resource(), now) >= Write}
	if errMsg == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	for i, q := range qs {
		v.Questions = append(v.Questions, NewQuestionView(ForParticipant(q), i+1, staffAssetURL))
	}
	h.render.Render(w, r, status, "exam/stimulus_view", v)
}

func (h *Handler) stimulusReorder(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBankForm) {
		return
	}
	_, sa := h.staff(r)
	s, _, ok := h.loadStimulus(w, r, sa)
	if !ok {
		return
	}
	err := ReorderStimulus(r.Context(), h.db, sa, s.ID, r.PostForm["order"], time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.showStimulus(w, r, http.StatusUnprocessableEntity, fe.Fields["order"])
		return
	}
	if err != nil {
		h.fail(w, r, "reorder stimulus", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Urutan soal disimpan.")
	http.Redirect(w, r, "/ujian/stimulus/"+s.ID, http.StatusSeeOther)
}

func (h *Handler) stimulusArchive(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	s, b, ok := h.loadStimulus(w, r, sa)
	if !ok {
		return
	}
	err := ArchiveStimulus(r.Context(), h.db, sa, s.ID, time.Now())
	if errors.Is(err, ErrStimulusInUse) {
		h.showStimulus(w, r, http.StatusConflict, "Stimulus masih punya soal. Arsipkan atau pindahkan soalnya dulu.")
		return
	}
	if err != nil {
		h.fail(w, r, "archive stimulus", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Stimulus diarsipkan.")
	http.Redirect(w, r, "/ujian/bank/"+b.ID, http.StatusSeeOther)
}
