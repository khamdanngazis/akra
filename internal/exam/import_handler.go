package exam

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/exam/richtext"
)

// WithJobs memasang antrean job untuk membaca impor besar. Tanpa antrean
// (uji), file dibaca langsung di request.
func (h *Handler) WithJobs(j ImportJobInserter) *Handler {
	h.jobs = j
	return h
}

// importRoutes mendaftarkan route impor soal (T129–T131). Semua untuk
// penulis bank (admin_tu, guru).
func (h *Handler) importRoutes(r chi.Router) {
	r.Get("/bank/{id}/impor", h.importPage)
	r.Post("/bank/{id}/impor", h.importUpload)
	r.Get("/impor/template-soal.docx", h.importWordTemplate)
	r.Get("/impor/{id}", h.importPreview)
	r.Get("/impor/{id}/butir/{seq}", h.importItemEdit)
	r.Post("/impor/{id}/butir/{seq}", h.importItemSave)
	r.Post("/impor/{id}/simpan", h.importCommit)
}

type importSource struct {
	Value, Title, Desc, Accept, Template string
}

var importSources = []importSource{
	{SourceWord, "Word (.docx)", "Soal bernomor dengan opsi A–E dan baris Kunci. Rumus Equation Word, gambar, tabel, dan teks Arab ikut terbaca.", ".docx", "/ujian/impor/template-soal.docx"},
}

type importPageView struct {
	User    auth.Identity
	Bank    Bank
	Sources []importSource
	Recent  []Import
	Error   string
	Flash   string
}

func (h *Handler) importPage(w http.ResponseWriter, r *http.Request) {
	h.showImportPage(w, r, http.StatusOK, "")
}

func (h *Handler) showImportPage(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	id, sa := h.staff(r)
	b, ok := h.writableBank(w, r, sa, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	v := importPageView{User: id, Bank: b, Sources: importSources, Error: errMsg}
	if errMsg == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	rows, err := h.db.Query(r.Context(), `SELECT id FROM exam_imports WHERE school_id = $1 AND bank_id = $2 AND created_by = $3
		ORDER BY created_at DESC LIMIT 10`, sa.SchoolID, b.ID, sa.UserID)
	if err != nil {
		h.fail(w, r, "list imports", err)
		return
	}
	var ids []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			ids = append(ids, s)
		}
	}
	rows.Close()
	for _, iid := range ids {
		if imp, err := GetImport(r.Context(), h.db, sa.SchoolID, iid); err == nil {
			v.Recent = append(v.Recent, imp)
		}
	}
	h.render.Render(w, r, status, "exam/import_upload", v)
}

func (h *Handler) importWordTemplate(w http.ResponseWriter, r *http.Request) {
	data, err := WordTemplate()
	if err != nil {
		h.fail(w, r, "word template", err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="template-soal-akra.docx"`)
	_, _ = w.Write(data)
}

func (h *Handler) importUpload(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	b, ok := h.writableBank(w, r, sa, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxImportBytes+(1<<20))
	file, hdr, err := r.FormFile("file")
	if err != nil {
		h.showImportPage(w, r, http.StatusBadRequest, "Pilih file (maks. 20 MB).")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxImportBytes+1))
	if err != nil {
		h.showImportPage(w, r, http.StatusBadRequest, "File tidak terbaca.")
		return
	}
	source := r.FormValue("source")
	imp, err := CreateImport(r.Context(), h.db, h.store, sa, b.ID, source, path.Base(hdr.Filename), data, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.showImportPage(w, r, http.StatusUnprocessableEntity, importMessage(err))
		return
	}
	if err != nil {
		h.fail(w, r, "create import", err)
		return
	}
	if h.jobs != nil {
		if _, err := h.jobs.Insert(r.Context(), ImportArgs{SchoolID: sa.SchoolID, ImportID: imp.ID}, nil); err != nil {
			h.fail(w, r, "enqueue import", err)
			return
		}
	} else if err := ProcessImport(r.Context(), h.db, h.store, h.math, sa.SchoolID, imp.ID); err != nil {
		h.fail(w, r, "process import", err)
		return
	}
	http.Redirect(w, r, "/ujian/impor/"+imp.ID, http.StatusSeeOther)
}

// loadImport membaca impor milik pembuat (atau admin_tu) yang bank-nya
// masih boleh ditulis.
func (h *Handler) loadImport(w http.ResponseWriter, r *http.Request, sa StaffActor) (Import, Bank, bool) {
	id, _ := auth.CurrentUser(r.Context())
	imp, err := GetImport(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if err == nil && imp.CreatedBy != sa.UserID && !id.HasRole(auth.RoleAdminTU) {
		err = ErrNotFound
	}
	var b Bank
	if err == nil {
		b, err = GetBank(r.Context(), h.db, sa.SchoolID, imp.BankID)
	}
	if err == nil && sa.Can(ActBank, b.resource(), time.Now()) < Write {
		err = ErrNotFound
	}
	if err != nil {
		h.fail(w, r, "load import", err)
		return Import{}, Bank{}, false
	}
	return imp, b, true
}

type importRow struct {
	ImportItem
	View     *QuestionView
	StimHTML template.HTML
	Key      []string
	Include  bool
}

type importPreviewView struct {
	User       auth.Identity
	Bank       Bank
	Import     Import
	Rows       []importRow
	Stimuli    []ImportItem
	Counts     map[string]int
	Objectives []Objective
	Diffs      []string
	Error      string
	Flash      string
}

func (h *Handler) importPreview(w http.ResponseWriter, r *http.Request) {
	h.showImportPreview(w, r, http.StatusOK, "")
}

func (h *Handler) showImportPreview(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	id, sa := h.staff(r)
	imp, b, ok := h.loadImport(w, r, sa)
	if !ok {
		return
	}
	v := importPreviewView{User: id, Bank: b, Import: imp, Counts: map[string]int{}, Diffs: Difficulties, Error: errMsg}
	if errMsg == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	if imp.Status == importReady {
		items, err := ListImportItems(r.Context(), h.db, sa.SchoolID, imp.ID)
		if err != nil {
			h.fail(w, r, "import items", err)
			return
		}
		for _, it := range items {
			row := importRow{ImportItem: it, Include: it.Status != ItemSkip}
			if it.Kind == "stimulus" {
				v.Stimuli = append(v.Stimuli, it)
				if s, _, err := h.buildStim(it.Stim); err == nil {
					row.StimHTML = trusted(s.BodyHTML, staffAssetURL)
				}
			} else {
				v.Counts[it.Status]++
				f := it.Form
				f.Status = StatusDraft
				if q, _, err := BuildQuestion(f, h.math); err == nil {
					n, _ := strconv.Atoi(it.Number)
					qv := NewQuestionView(ForParticipant(q), n, staffAssetURL)
					row.View, row.Key = &qv, keyLines(q)
				}
			}
			v.Rows = append(v.Rows, row)
		}
		if b.SubjectID != "" {
			v.Objectives, _ = ListObjectives(r.Context(), h.db, sa.SchoolID, b.SubjectID, b.GradeLevel)
		}
	}
	h.render.Render(w, r, status, "exam/import_preview", v)
}

// buildStim merender stimulus impor untuk pratinjau.
func (h *Handler) buildStim(s StimulusDraft) (Stimulus, []string, error) {
	doc, err := processRich(s.Body, h)
	if err != nil {
		return Stimulus{}, nil, err
	}
	return Stimulus{Title: s.Title, BodySrc: doc.Src, BodyHTML: doc.HTML, Numbered: s.Numbered}, doc.Assets, nil
}

func (h *Handler) importItem(w http.ResponseWriter, r *http.Request, imp Import) (ImportItem, bool) {
	seq, err := strconv.Atoi(chi.URLParam(r, "seq"))
	items, lerr := ListImportItems(r.Context(), h.db, imp.SchoolID, imp.ID)
	if err != nil || lerr != nil || seq < 0 || seq >= len(items) || imp.Status != importReady {
		h.fail(w, r, "import item", ErrNotFound)
		return ImportItem{}, false
	}
	return items[seq], true
}

func (h *Handler) importItemEdit(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	imp, b, ok := h.loadImport(w, r, sa)
	if !ok {
		return
	}
	it, ok := h.importItem(w, r, imp)
	if !ok {
		return
	}
	action := "/ujian/impor/" + imp.ID + "/butir/" + strconv.Itoa(it.Seq)
	if it.Kind == "stimulus" {
		h.renderStimulusForm(w, r, http.StatusOK, stimulusFormView{Bank: b, Title: it.Stim.Title, Body: safeSrc(it.Stim.Body), Numbered: it.Stim.Numbered,
			Action: action, CancelURL: "/ujian/impor/" + imp.ID})
		return
	}
	h.renderEditor(w, r, http.StatusOK, editorView{Bank: b, IsNew: true, Form: it.Form, Action: action, ImportMode: true,
		CancelURL: "/ujian/impor/" + imp.ID, Problems: it.Problems})
}

func (h *Handler) importItemSave(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxEditorForm) {
		return
	}
	_, sa := h.staff(r)
	imp, _, ok := h.loadImport(w, r, sa)
	if !ok {
		return
	}
	it, ok := h.importItem(w, r, imp)
	if !ok {
		return
	}
	f := QuestionForm{}
	st := StimulusDraft{}
	if it.Kind == "stimulus" {
		st = StimulusDraft{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), Numbered: r.PostForm.Get("numbered") == "on"}
	} else {
		f = mergeForm(emptyForm(""), ParseQuestionForm(r.PostForm))
	}
	saved, err := SaveImportItem(r.Context(), h.db, h.math, imp, it.Seq, f, st)
	if err != nil {
		h.fail(w, r, "save import item", err)
		return
	}
	msg := "Butir diperbarui dan siap diimpor."
	if saved.Status == ItemCheck {
		msg = "Butir diperbarui, tetapi masih perlu dicek: " + strings.Join(saved.Problems, "; ")
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, "/ujian/impor/"+imp.ID+"#butir-"+strconv.Itoa(it.Seq), http.StatusSeeOther)
}

func (h *Handler) importCommit(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 1<<20) {
		return
	}
	_, sa := h.staff(r)
	imp, _, ok := h.loadImport(w, r, sa)
	if !ok {
		return
	}
	items, err := ListImportItems(r.Context(), h.db, sa.SchoolID, imp.ID)
	if err != nil {
		h.fail(w, r, "commit import", err)
		return
	}
	p := r.PostForm
	opt := CommitOptions{IncludeCheck: p.Get("mode") == "semua", Exclude: map[int]bool{}, Stimulus: map[int]int{},
		ObjectiveID: p.Get("objective_id"), Difficulty: p.Get("difficulty"), Tags: p.Get("tags")}
	included := map[string]bool{}
	for _, s := range p["include"] {
		included[s] = true
	}
	stims := map[int]bool{}
	for _, it := range items {
		if it.Kind == "stimulus" {
			stims[it.Seq] = true
		}
	}
	for _, it := range items {
		k := strconv.Itoa(it.Seq)
		if it.Kind == "soal" && !included[k] {
			opt.Exclude[it.Seq] = true
		}
		if v, ok := p["stim_"+k]; ok && it.Kind == "soal" {
			n, err := strconv.Atoi(v[0])
			if err != nil || (n >= 0 && !stims[n]) {
				n = -1
			}
			opt.Stimulus[it.Seq] = n
		}
	}
	if opt.Difficulty != "" && !contains(Difficulties, opt.Difficulty) {
		opt.Difficulty = ""
	}
	res, err := CommitImport(r.Context(), h.db, h.math, sa, imp, opt, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) || errors.Is(err, ErrNotEditable) {
		h.showImportPreview(w, r, http.StatusUnprocessableEntity, "Impor belum disimpan: "+importMessage(err)+" Perbaiki butirnya lalu simpan lagi.")
		return
	}
	if err != nil {
		h.fail(w, r, "commit import", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, importSummary(res))
	http.Redirect(w, r, "/ujian/impor/"+imp.ID, http.StatusSeeOther)
}

// processRich memproses teks kaya dengan renderer rumus handler.
func processRich(src string, h *Handler) (richtext.Doc, error) { return richtext.Process(src, h.math) }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// importSummary menyusun laporan akhir, mis. "38 soal masuk (siap 35,
// draf 3), 2 dilewati."
func importSummary(res ImportResult) string {
	s := strconv.Itoa(res.Ready+res.Draft) + " soal masuk (siap " + strconv.Itoa(res.Ready) + ", draf " + strconv.Itoa(res.Draft) + ")"
	if res.Stimuli > 0 {
		s += ", " + strconv.Itoa(res.Stimuli) + " stimulus"
	}
	return s + ", " + strconv.Itoa(res.Skipped) + " dilewati."
}
