package exam

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/exam/richtext"
	"akra/internal/platform/clock"
	"akra/internal/platform/storage"
)

// Batas form editor: teks kaya berisi rumus bisa panjang.
const maxEditorForm = 2 << 20

// editorRoutes mendaftarkan route editor soal dan stimulus (T128).
// readers: semua yang membuka bank; writers: admin_tu dan guru.
func (h *Handler) editorRoutes(readers, writers chi.Router) {
	readers.Get("/soal/{id}", h.questionView)
	readers.Get("/stimulus/{id}", h.stimulusView)
	readers.Get("/aset/{id}", h.asset)

	writers.Get("/bank/{id}/soal/baru", h.questionNew)
	writers.Post("/bank/{id}/soal", h.questionCreate)
	writers.Get("/soal/{id}/ubah", h.questionEdit)
	writers.Post("/soal/{id}", h.questionUpdate)
	writers.Post("/bank/{id}/gambar", h.assetUpload)
	writers.Post("/draf", h.draftSave)
	writers.Post("/draf/buang", h.draftDiscard)
	writers.Post("/pratinjau", h.preview)
	writers.Get("/bank/{id}/stimulus/baru", h.stimulusNew)
	writers.Post("/bank/{id}/stimulus", h.stimulusCreate)
	writers.Get("/stimulus/{id}/ubah", h.stimulusEdit)
	writers.Post("/stimulus/{id}", h.stimulusUpdate)
	writers.Post("/stimulus/{id}/urutan", h.stimulusReorder)
	writers.Post("/stimulus/{id}/arsip", h.stimulusArchive)
}

// safeSrc menyiapkan sumber teks kaya untuk dimuat ulang ke editor:
// dibersihkan dulu (isian form bisa berisi apa saja), gambar diberi URL.
func safeSrc(src string) template.HTML {
	clean, _, err := richtext.Sanitize(src)
	if err != nil {
		return ""
	}
	return template.HTML(richtext.ResolveAssets(clean, staffAssetURL)) //nolint:gosec // sudah disanitasi
}

type labeled struct {
	Value, Label string
}

var cognitiveLabels = map[string]string{
	"C1": "C1 Mengingat (LOTS)", "C2": "C2 Memahami (LOTS)", "C3": "C3 Menerapkan (MOTS)",
	"C4": "C4 Menganalisis (HOTS)", "C5": "C5 Mengevaluasi (HOTS)", "C6": "C6 Mencipta (HOTS)",
}

type editorView struct {
	User         auth.Identity
	Bank         Bank
	Question     Question
	IsNew        bool
	Form         QuestionForm
	Errors       map[string]string
	Objectives   []Objective
	Stimuli      []Stimulus
	Stimulus     *Stimulus
	Kinds        []labeled
	Levels       []labeled
	Difficulties []string
	DraftAt      string // draf server lebih baru dari soal
	Restored     bool
	SavedAt      int64 // unix detik saat halaman dibuat (pembanding draf lokal)
	Flash        string
	Error        string
	// Mode perbaikan butir impor: form dikirim ke Action, tanpa draf
	// otomatis dan tanpa "simpan & tambah".
	Action     string
	ImportMode bool
	CancelURL  string
	Problems   []string
	// Teks kaya siap dimuat ke editor.
	Body, Explanation, Rubric template.HTML
	Choices, Statements       []template.HTML
	Lefts, Rights, Distract   []template.HTML
}

// editorLoad memuat bank dan memastikan pengguna boleh menulis.
func (h *Handler) writableBank(w http.ResponseWriter, r *http.Request, sa StaffActor, bankID string) (Bank, bool) {
	b, err := GetBank(r.Context(), h.db, sa.SchoolID, bankID)
	if err == nil && sa.Can(ActBank, b.resource(), time.Now()) < Read {
		err = ErrNotFound
	}
	if err == nil && (b.ArchivedAt != nil || sa.Can(ActBank, b.resource(), time.Now()) < Write) {
		err = ErrNotEditable
	}
	if err != nil {
		h.fail(w, r, "load bank", err)
		return Bank{}, false
	}
	return b, true
}

func (h *Handler) renderEditor(w http.ResponseWriter, r *http.Request, status int, v editorView) {
	id, sa := h.staff(r)
	ctx := r.Context()
	v.User, v.SavedAt, v.Difficulties = id, time.Now().Unix(), Difficulties
	for _, k := range Kinds {
		v.Kinds = append(v.Kinds, labeled{string(k), k.Label()})
	}
	for _, c := range CognitiveLevels {
		v.Levels = append(v.Levels, labeled{c, cognitiveLabels[c]})
	}
	var err error
	if v.Bank.SubjectID != "" {
		if v.Objectives, err = ListObjectives(ctx, h.db, sa.SchoolID, v.Bank.SubjectID, v.Bank.GradeLevel); err != nil {
			h.fail(w, r, "editor objectives", err)
			return
		}
	}
	if v.Stimuli, err = ListStimuli(ctx, h.db, sa.SchoolID, v.Bank.ID); err != nil {
		h.fail(w, r, "editor stimuli", err)
		return
	}
	stimID := v.Form.StimulusID
	if !v.IsNew {
		stimID = v.Question.StimulusID
	}
	for i := range v.Stimuli {
		if v.Stimuli[i].ID == stimID {
			v.Stimulus = &v.Stimuli[i]
		}
	}
	f := v.Form
	pad := func(in []string, n int) []template.HTML {
		out := make([]template.HTML, n)
		for i := 0; i < n && i < len(in); i++ {
			out[i] = safeSrc(in[i])
		}
		return out
	}
	v.Body, v.Explanation, v.Rubric = safeSrc(f.Body), safeSrc(f.Explanation), safeSrc(f.Rubric)
	v.Choices, v.Statements = pad(f.Choices, maxChoices), pad(f.Statements, maxStatement)
	v.Lefts, v.Rights, v.Distract = pad(f.Lefts, maxPairs), pad(f.Rights, maxPairs), pad(f.Distractors, 5)
	if len(v.Form.Truth) < maxStatement {
		v.Form.Truth = append(v.Form.Truth, make([]string, maxStatement-len(v.Form.Truth))...)
	}
	h.render.Render(w, r, status, "exam/question_form", v)
}

// emptyForm mengisi daftar opsi kosong sesuai kapasitas form.
func emptyForm(kind string) QuestionForm {
	if !Kind(kind).Valid() {
		kind = string(KindMC)
	}
	return QuestionForm{Kind: kind, Choices: make([]string, maxChoices), Statements: make([]string, maxStatement),
		Truth: make([]string, maxStatement), Lefts: make([]string, maxPairs), Rights: make([]string, maxPairs),
		Distractors: make([]string, 5), Points: "1", Status: StatusReady, Scoring: ScoringProportional, Input: "teks"}
}

func (h *Handler) draftNote(r *http.Request, sa StaffActor, bankID, questionID string, since time.Time) (Draft, string) {
	d, ok, err := GetDraft(r.Context(), h.db, sa.SchoolID, sa.UserID, bankID, questionID)
	if err != nil || !ok || !d.UpdatedAt.After(since) {
		return Draft{}, ""
	}
	id, _ := auth.CurrentUser(r.Context())
	return d, clock.FormatDateTimeID(d.UpdatedAt, id.Location())
}

func (h *Handler) questionNew(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	b, ok := h.writableBank(w, r, sa, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	q := r.URL.Query()
	f := emptyForm(q.Get("jenis"))
	f.ObjectiveID, f.Difficulty, f.CognitiveLevel, f.StimulusID = q.Get("tp"), q.Get("kesulitan"), q.Get("level"), q.Get("stimulus")
	v := editorView{Bank: b, IsNew: true, Form: f, Flash: auth.PopFlash(r.Context(), h.sessions)}
	d, at := h.draftNote(r, sa, b.ID, "", time.Time{})
	if q.Get("draf") == "1" && at != "" {
		v.Form, v.Restored = mergeForm(emptyForm(""), ParseQuestionForm(d.Values)), true
	} else {
		v.DraftAt = at
	}
	h.renderEditor(w, r, http.StatusOK, v)
}

// mergeForm melengkapi panjang daftar form hasil parse dengan form kosong.
func mergeForm(base, f QuestionForm) QuestionForm {
	fill := func(dst, src []string) []string {
		out := append([]string{}, dst...)
		copy(out, src)
		return out
	}
	f.Choices, f.Statements, f.Truth = fill(base.Choices, f.Choices), fill(base.Statements, f.Statements), fill(base.Truth, f.Truth)
	f.Lefts, f.Rights, f.Distractors = fill(base.Lefts, f.Lefts), fill(base.Rights, f.Rights), fill(base.Distractors, f.Distractors)
	if f.Kind == "" {
		f.Kind = base.Kind
	}
	return f
}

func (h *Handler) questionCreate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxEditorForm) {
		return
	}
	_, sa := h.staff(r)
	b, ok := h.writableBank(w, r, sa, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	f := ParseQuestionForm(r.PostForm)
	q, assets, err := BuildQuestion(f, h.math)
	if err == nil {
		err = checkAssets(r.Context(), h.db, sa.SchoolID, assets)
	}
	if err == nil {
		err = h.applyReview(r, sa, &q)
	}
	var saved Question
	if err == nil {
		saved, err = CreateQuestion(r.Context(), h.db, sa, b.ID, q, time.Now())
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderEditor(w, r, http.StatusUnprocessableEntity, editorView{Bank: b, IsNew: true, Form: mergeForm(emptyForm(""), f), Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "create question", err)
		return
	}
	_ = DeleteDraft(r.Context(), h.db, sa.SchoolID, sa.UserID, b.ID, "")
	h.afterSave(w, r, b, saved, "Soal disimpan.")
}

// afterSave mengarahkan ke soal, atau ke form baru dengan jenis, TP, dan
// kesulitan yang sama untuk "Simpan & tambah berikutnya".
func (h *Handler) afterSave(w http.ResponseWriter, r *http.Request, b Bank, q Question, msg string) {
	if r.PostForm.Get("next") == "1" {
		v := url.Values{"jenis": {string(q.Kind)}}
		for k, s := range map[string]string{"tp": q.ObjectiveID, "kesulitan": q.Difficulty, "level": q.CognitiveLevel, "stimulus": q.StimulusID} {
			if s != "" {
				v.Set(k, s)
			}
		}
		auth.SetFlash(r.Context(), h.sessions, msg+" Lanjutkan soal berikutnya.")
		http.Redirect(w, r, "/ujian/bank/"+b.ID+"/soal/baru?"+v.Encode(), http.StatusSeeOther)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, "/ujian/soal/"+q.ID, http.StatusSeeOther)
}

// loadQuestion membaca soal dan banknya, dengan cek baca.
func (h *Handler) loadQuestion(w http.ResponseWriter, r *http.Request, sa StaffActor) (Question, Bank, bool) {
	q, err := GetQuestion(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	var b Bank
	if err == nil {
		b, err = GetBank(r.Context(), h.db, sa.SchoolID, q.BankID)
	}
	if err == nil && sa.Can(ActBank, b.resource(), time.Now()) < Read {
		err = ErrNotFound
	}
	if err != nil {
		h.fail(w, r, "load question", err)
		return Question{}, Bank{}, false
	}
	return q, b, true
}

func (h *Handler) questionEdit(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	q, b, ok := h.loadQuestion(w, r, sa)
	if !ok {
		return
	}
	if !CanEditQuestion(sa.Actor, b, q, time.Now()) {
		h.fail(w, r, "edit question", ErrNotEditable)
		return
	}
	v := editorView{Bank: b, Question: q, Form: mergeForm(emptyForm(""), FormFromQuestion(q)), Flash: auth.PopFlash(r.Context(), h.sessions)}
	d, at := h.draftNote(r, sa, b.ID, q.ID, q.UpdatedAt)
	if r.URL.Query().Get("draf") == "1" && at != "" {
		v.Form, v.Restored = mergeForm(emptyForm(""), ParseQuestionForm(d.Values)), true
	} else {
		v.DraftAt = at
	}
	h.renderEditor(w, r, http.StatusOK, v)
}

func (h *Handler) questionUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxEditorForm) {
		return
	}
	_, sa := h.staff(r)
	cur, b, ok := h.loadQuestion(w, r, sa)
	if !ok {
		return
	}
	f := ParseQuestionForm(r.PostForm)
	q, assets, err := BuildQuestion(f, h.math)
	if err == nil {
		err = checkAssets(r.Context(), h.db, sa.SchoolID, assets)
	}
	if err == nil {
		err = h.applyReview(r, sa, &q)
	}
	var saved Question
	if err == nil {
		saved, err = UpdateQuestion(r.Context(), h.db, sa, cur.ID, q, time.Now())
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderEditor(w, r, http.StatusUnprocessableEntity, editorView{Bank: b, Question: cur, Form: mergeForm(emptyForm(""), f), Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "update question", err)
		return
	}
	_ = DeleteDraft(r.Context(), h.db, sa.SchoolID, sa.UserID, b.ID, cur.ID)
	h.afterSave(w, r, b, saved, "Soal disimpan sebagai versi "+strconv.Itoa(saved.Version)+".")
}

type questionPageView struct {
	User          auth.Identity
	Bank          Bank
	Question      Question
	View          QuestionView
	CanEdit       bool
	CanWrite      bool
	Objective     string
	Explanation   template.HTML
	Rubric        template.HTML
	Key           []string // kunci jawaban yang mudah dibaca (hanya staf)
	Stimulus      *Stimulus
	Flash, Error  string
	CognitiveText string
	// Telaah (T132).
	ReviewOn  bool
	Reviews   []Review
	CanSubmit bool
	CanReview bool
}

// keyLines menjelaskan kunci jawaban untuk staf.
func keyLines(q Question) []string {
	var out []string
	switch q.Kind {
	case KindMC, KindMulti:
		var ls []string
		for i, c := range q.Options.Choices {
			for _, k := range q.Key.Correct {
				if k == c.ID {
					ls = append(ls, letter(i))
				}
			}
		}
		out = append(out, "Kunci: "+strings.Join(ls, ", "))
		if q.Kind == KindMulti {
			out = append(out, map[string]string{ScoringAll: "Nilai penuh hanya bila semua benar", ScoringProportional: "Nilai proporsional"}[q.Key.Scoring])
		}
	case KindTF:
		for i, s := range q.Options.Statements {
			out = append(out, strconv.Itoa(i+1)+". "+map[bool]string{true: "Benar", false: "Salah"}[q.Key.Truth[s.ID]])
		}
	case KindShort:
		out = append(out, "Diterima: "+strings.Join(q.Key.Accepted, " | "))
		if q.Options.Input == "angka" && q.Key.Tolerance > 0 {
			out = append(out, "Toleransi ±"+strconv.FormatFloat(q.Key.Tolerance, 'f', -1, 64))
		}
		if q.Key.ArabicNormalize {
			out = append(out, "Harakat dan variasi huruf Arab diabaikan")
		}
	case KindMatch:
		right := map[string]int{}
		for i, rc := range q.Options.Right {
			right[rc.ID] = i
		}
		for i, l := range q.Options.Left {
			out = append(out, strconv.Itoa(i+1)+" → "+letter(right[q.Key.Pairs[l.ID]]))
		}
	case KindEssay:
		out = append(out, "Dinilai manual dengan rubrik.")
	}
	return out
}

func (h *Handler) questionView(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	q, b, ok := h.loadQuestion(w, r, sa)
	if !ok {
		return
	}
	now := time.Now()
	v := questionPageView{User: id, Bank: b, Question: q, CanEdit: CanEditQuestion(sa.Actor, b, q, now),
		CanWrite:    b.ArchivedAt == nil && sa.Can(ActBank, b.resource(), now) >= Write,
		View:        NewQuestionView(ForParticipant(q), 1, staffAssetURL),
		Explanation: trusted(q.ExplanationHTML, staffAssetURL), Rubric: trusted(q.Key.RubricHTML, staffAssetURL),
		Key: keyLines(q), Flash: auth.PopFlash(r.Context(), h.sessions), CognitiveText: cognitiveLabels[q.CognitiveLevel]}
	if s, err := LoadExamSettings(r.Context(), h.db, sa.SchoolID); err == nil && s.Review {
		v.ReviewOn = true
		v.Reviews, _ = ListReviews(r.Context(), h.db, sa.SchoolID, q.ID)
		v.CanSubmit = v.CanEdit && q.Status == StatusDraft
		v.CanReview = q.Status == StatusInReview && q.CreatedBy != sa.UserID && v.CanWrite && q.ArchivedAt == nil
	}
	if q.ObjectiveID != "" {
		if o, err := getObjective(r.Context(), h.db, sa.SchoolID, q.ObjectiveID); err == nil {
			v.Objective = o.Code + " — " + o.Description
		}
	}
	if q.StimulusID != "" {
		if s, err := GetStimulus(r.Context(), h.db, sa.SchoolID, q.StimulusID); err == nil {
			v.Stimulus = &s
			v.View = v.View.WithStimulus(s, staffAssetURL)
		}
	}
	h.render.Render(w, r, http.StatusOK, "exam/question_view", v)
}

// asset mengalihkan ke URL bertanda tangan gambar (5 menit) setelah cek
// akses bank.
func (h *Handler) asset(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	a, err := GetAsset(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	var b Bank
	if err == nil {
		b, err = GetBank(r.Context(), h.db, sa.SchoolID, a.BankID)
	}
	if err == nil && sa.Can(ActBank, b.resource(), time.Now()) < Read {
		err = ErrNotFound
	}
	if err != nil {
		h.fail(w, r, "asset", err)
		return
	}
	u, err := h.store.Presign(r.Context(), a.StorageKey, 5*time.Minute, storage.PresignOptions{ContentDisposition: "inline", ContentType: a.ContentType})
	if err != nil {
		h.fail(w, r, "presign asset", err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=240")
	http.Redirect(w, r, u, http.StatusFound)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// assetUpload menerima satu gambar (multipart "file") dan mengembalikan
// JSON {id, url}.
func (h *Handler) assetUpload(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	b, err := GetBank(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if err == nil && (b.ArchivedAt != nil || sa.Can(ActBank, b.resource(), time.Now()) < Write) {
		err = ErrNotFound
	}
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Bank tidak ditemukan."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxAssetBytes*4+(64<<10))
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Gambar tidak terbaca atau terlalu besar (maks. 20 MB)."})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxAssetBytes*4+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Gambar tidak terbaca."})
		return
	}
	a, err := SaveAsset(r.Context(), h.db, h.store, sa, b.ID, data)
	if errors.Is(err, ErrAssetType) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Pakai gambar PNG, JPEG, WebP, atau GIF."})
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "save asset", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Gambar gagal disimpan. Coba lagi."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": a.ID, "url": staffAssetURL(a.ID), "width": a.Width, "height": a.Height})
}

// draftSave menyimpan draf editor (dipanggil tiap 5 detik). Body berisi
// field editor ditambah bank_id dan question_id.
func (h *Handler) draftSave(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxEditorForm) {
		return
	}
	_, sa := h.staff(r)
	bankID, qid := r.PostForm.Get("bank_id"), r.PostForm.Get("question_id")
	b, err := GetBank(r.Context(), h.db, sa.SchoolID, bankID)
	if err == nil && (b.ArchivedAt != nil || sa.Can(ActBank, b.resource(), time.Now()) < Write) {
		err = ErrNotFound
	}
	if err == nil && qid != "" {
		var q Question
		q, err = GetQuestion(r.Context(), h.db, sa.SchoolID, qid)
		if err == nil && (q.BankID != b.ID || !CanEditQuestion(sa.Actor, b, q, time.Now())) {
			err = ErrNotFound
		}
	}
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Soal tidak ditemukan."})
		return
	}
	v := ParseQuestionForm(r.PostForm).Values()
	if err := SaveDraft(r.Context(), h.db, sa, b.ID, qid, v); err != nil {
		var fe *FieldError
		if errors.As(err, &fe) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fe.Fields["draft"]})
			return
		}
		h.logger.ErrorContext(r.Context(), "save draft", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Draf gagal disimpan."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"saved": time.Now().Format(time.RFC3339)})
}

func (h *Handler) draftDiscard(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBankForm) {
		return
	}
	_, sa := h.staff(r)
	bankID, qid := r.PostForm.Get("bank_id"), r.PostForm.Get("question_id")
	if _, err := GetBank(r.Context(), h.db, sa.SchoolID, bankID); err != nil {
		h.fail(w, r, "discard draft", err)
		return
	}
	if err := DeleteDraft(r.Context(), h.db, sa.SchoolID, sa.UserID, bankID, qid); err != nil {
		h.fail(w, r, "discard draft", err)
		return
	}
	back := "/ujian/bank/" + bankID + "/soal/baru"
	if qid != "" {
		back = "/ujian/soal/" + url.PathEscape(qid) + "/ubah"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// fragmentRenderer merender satu blok tanpa layout (render.Renderer).
type fragmentRenderer interface {
	RenderFragment(w http.ResponseWriter, r *http.Request, status int, name, block string, data any)
}

type previewView struct {
	View   QuestionView
	Errors map[string]string
}

// preview merender isi editor sebagai tampilan peserta (komponen yang sama
// dengan halaman ujian). Validasi longgar seperti draf, agar pratinjau
// tetap tampil saat soal belum lengkap.
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxEditorForm) {
		return
	}
	_, sa := h.staff(r)
	f := ParseQuestionForm(r.PostForm)
	f.Status = StatusDraft
	q, _, err := BuildQuestion(f, h.math)
	v := previewView{}
	var fe *FieldError
	if errors.As(err, &fe) {
		v.Errors = fe.Fields
	} else if err != nil {
		h.fail(w, r, "preview", err)
		return
	}
	v.View = NewQuestionView(ForParticipant(q), 1, staffAssetURL)
	if f.StimulusID != "" {
		if s, err := GetStimulus(r.Context(), h.db, sa.SchoolID, f.StimulusID); err == nil {
			v.View = v.View.WithStimulus(s, staffAssetURL)
		}
	}
	if fr, ok := h.render.(fragmentRenderer); ok {
		fr.RenderFragment(w, r, http.StatusOK, "exam/preview_fragment", "content", v)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/preview_fragment", v)
}

// applyReview mengajukan soal "siap" untuk ditelaah bila telaah aktif.
func (h *Handler) applyReview(r *http.Request, sa StaffActor, q *Question) error {
	s, err := LoadExamSettings(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		return err
	}
	q.Status = reviewStatus(q.Status, s)
	return nil
}
