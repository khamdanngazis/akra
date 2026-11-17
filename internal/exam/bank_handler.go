package exam

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
)

// bankRoutes mendaftarkan route bank soal dan TP (T127). Dipanggil dari
// Routes di dalam /ujian (modul aktif).
func (h *Handler) bankRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		// Peran: admin_tu, koordinator ujian, guru (mapel yang diampu),
		// kepala_sekolah (baca). Cek per bank/soal di handler.
		r.Use(RequireAccess(h.db, h.render, h.logger, ActBank, Read))
		r.Get("/bank/", h.banks)
		r.Get("/bank/{id}", h.bankDetail)
		r.Get("/tp/", h.objectives)
		readers := r
		// Menulis: admin_tu dan guru (termasuk koordinator); kepala sekolah
		// hanya membaca.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleGuru))
			h.editorRoutes(readers, r)
			r.Get("/bank/baru", h.bankNew)
			r.Post("/bank/", h.bankCreate)
			r.Get("/bank/{id}/ubah", h.bankEdit)
			r.Post("/bank/{id}", h.bankUpdate)
			r.Post("/bank/{id}/arsip", h.bankArchive)
			r.Post("/bank/{id}/massal", h.bankBulk)
			r.Post("/soal/{id}/duplikat", h.questionDuplicate)
			r.Post("/soal/{id}/arsip", h.questionArchive)
			r.Post("/tp/", h.objectiveCreate)
			r.Post("/tp/tempel", h.objectivePaste)
			r.Post("/tp/{id}", h.objectiveUpdate)
			r.Post("/tp/{id}/arsip", h.objectiveArchive)
		})
	})
}

// staff mengembalikan pelaku dari request (setelah RequireAccess).
func (h *Handler) staff(r *http.Request) (auth.Identity, StaffActor) {
	id, _ := auth.CurrentUser(r.Context())
	a, _ := ActorFrom(r.Context())
	return id, StaffActor{Actor: a, SchoolID: id.SchoolID, IP: auth.ClientIP(r)}
}

// fail menerjemahkan error store ke halaman error. FieldError ditangani
// pemanggil (render ulang form).
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	switch {
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, "Data tidak ditemukan.")
	case errors.Is(err, ErrNotEditable):
		page.Forbidden(h.render, w, r, id, "Hanya pembuat dan admin/TU yang bisa mengubah ini. Anda tetap bisa memakai atau menduplikatnya.")
	default:
		page.ServerError(h.render, h.logger, w, r, id, op, err)
	}
}

func (h *Handler) parseForm(w http.ResponseWriter, r *http.Request, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil {
		id, _ := auth.CurrentUser(r.Context())
		page.BadRequest(h.render, w, r, id, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return false
	}
	return true
}

type subjectOption struct {
	ID, Name string
	Selected bool
}

// writableSubjects mengembalikan mapel aktif yang bank-nya boleh ditulis sa.
func (h *Handler) writableSubjects(r *http.Request, sa StaffActor, selected string) ([]subjectOption, error) {
	rows, err := h.db.Query(r.Context(), `SELECT id, name FROM subjects WHERE school_id = $1 AND is_active ORDER BY lower(name)`, sa.SchoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: list subjects: %w", err)
	}
	defer rows.Close()
	var out []subjectOption
	for rows.Next() {
		var o subjectOption
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, err
		}
		if sa.Can(ActBank, Resource{SubjectID: o.ID}, time.Now()) >= Write {
			o.Selected = o.ID == selected
			out = append(out, o)
		}
	}
	return out, rows.Err()
}

type banksView struct {
	User      auth.Identity
	Banks     []BankSummary
	Grade     int
	Grades    []int
	CanCreate bool
	Flash     string
	Error     string
}

func (h *Handler) banks(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	grade, _ := parseGrade(r.URL.Query().Get("tingkat"))
	list, err := ListBanks(r.Context(), h.db, sa.SchoolID, sa.Actor, grade, time.Now())
	if err != nil {
		h.fail(w, r, "list banks", err)
		return
	}
	subjects, err := h.writableSubjects(r, sa, "")
	if err != nil {
		h.fail(w, r, "list banks", err)
		return
	}
	v := banksView{User: id, Banks: list, Grade: grade, Flash: auth.PopFlash(r.Context(), h.sessions),
		CanCreate: len(subjects) > 0 || sa.Can(ActBank, Resource{}, time.Now()) >= Write}
	for g := 1; g <= 12; g++ {
		v.Grades = append(v.Grades, g)
	}
	h.render.Render(w, r, http.StatusOK, "exam/banks", v)
}

type bankFormView struct {
	User       auth.Identity
	IsNew      bool
	Bank       Bank
	Form       BankForm
	Subjects   []subjectOption
	AllowNoSub bool // bank umum (tes seleksi) tanpa mapel
	Grades     []int
	Errors     map[string]string
}

func (h *Handler) renderBankForm(w http.ResponseWriter, r *http.Request, status int, v bankFormView) {
	id, sa := h.staff(r)
	v.User = id
	if v.IsNew {
		subjects, err := h.writableSubjects(r, sa, v.Form.SubjectID)
		if err != nil {
			h.fail(w, r, "bank form", err)
			return
		}
		v.Subjects = subjects
		v.AllowNoSub = sa.Can(ActBank, Resource{}, time.Now()) >= Write
	}
	for g := 1; g <= 12; g++ {
		v.Grades = append(v.Grades, g)
	}
	h.render.Render(w, r, status, "exam/bank_form", v)
}

func (h *Handler) bankNew(w http.ResponseWriter, r *http.Request) {
	h.renderBankForm(w, r, http.StatusOK, bankFormView{IsNew: true, Form: BankForm{SubjectID: r.URL.Query().Get("mapel")}})
}

const maxBankForm = 4 << 10

func (h *Handler) bankCreate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBankForm) {
		return
	}
	_, sa := h.staff(r)
	f := BankForm{Name: r.PostForm.Get("name"), SubjectID: r.PostForm.Get("subject_id"), GradeLevel: r.PostForm.Get("grade_level")}
	b, err := CreateBank(r.Context(), h.db, sa, f, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderBankForm(w, r, http.StatusUnprocessableEntity, bankFormView{IsNew: true, Form: f, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "create bank", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Bank soal dibuat. Tambahkan soal pertama atau impor dari Word.")
	http.Redirect(w, r, "/ujian/bank/"+b.ID, http.StatusSeeOther)
}

// loadBank membaca bank dan memastikan pengguna boleh melihatnya.
func (h *Handler) loadBank(w http.ResponseWriter, r *http.Request, sa StaffActor) (Bank, bool) {
	b, err := GetBank(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if err == nil && sa.Can(ActBank, b.resource(), time.Now()) < Read {
		err = ErrNotFound
	}
	if err != nil {
		h.fail(w, r, "get bank", err)
		return Bank{}, false
	}
	return b, true
}

func (h *Handler) bankEdit(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	b, ok := h.loadBank(w, r, sa)
	if !ok {
		return
	}
	if sa.Can(ActQuestionEdit, b.resource(), time.Now()) < Write {
		h.fail(w, r, "edit bank", ErrNotEditable)
		return
	}
	f := BankForm{Name: b.Name}
	if b.GradeLevel > 0 {
		f.GradeLevel = strconv.Itoa(b.GradeLevel)
	}
	h.renderBankForm(w, r, http.StatusOK, bankFormView{Bank: b, Form: f})
}

func (h *Handler) bankUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBankForm) {
		return
	}
	_, sa := h.staff(r)
	b, ok := h.loadBank(w, r, sa)
	if !ok {
		return
	}
	f := BankForm{Name: r.PostForm.Get("name"), GradeLevel: r.PostForm.Get("grade_level")}
	err := UpdateBank(r.Context(), h.db, sa, b.ID, f, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.renderBankForm(w, r, http.StatusUnprocessableEntity, bankFormView{Bank: b, Form: f, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "update bank", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Bank soal disimpan.")
	http.Redirect(w, r, "/ujian/bank/"+b.ID, http.StatusSeeOther)
}

func (h *Handler) bankArchive(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	b, ok := h.loadBank(w, r, sa)
	if !ok {
		return
	}
	if err := ArchiveBank(r.Context(), h.db, sa, b.ID, time.Now()); err != nil {
		h.fail(w, r, "archive bank", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Bank \""+b.Name+"\" diarsipkan. Soalnya tidak dihapus.")
	http.Redirect(w, r, "/ujian/bank/", http.StatusSeeOther)
}

type optionItem struct {
	Value, Label string
	Selected     bool
}

func options(values []string, labels func(string) string, selected string) []optionItem {
	out := make([]optionItem, 0, len(values))
	for _, v := range values {
		out = append(out, optionItem{Value: v, Label: labels(v), Selected: v == selected})
	}
	return out
}

// questionGroup adalah soal-soal yang tampil berurutan: satu soal tunggal
// atau kelompok satu stimulus.
type questionGroup struct {
	StimulusID    string
	StimulusTitle string
	Rows          []QuestionRow
}

type bankDetailView struct {
	User        auth.Identity
	Bank        Bank
	CanWrite    bool // menambah soal di bank ini
	CanManage   bool // mengubah/mengarsipkan bank
	Filter      QuestionFilter
	HasFilter   bool
	Page        QuestionPage
	Groups      []questionGroup
	Kinds       []optionItem
	Diffs       []optionItem
	Statuses    []optionItem
	Objectives  []Objective
	Creators    []optionItem
	OtherBanks  []Bank
	PrevURL     string
	NextURL     string
	From, To    int
	Flash       string
	Error       string
	BulkErrors  map[string]string
	HasEditable bool
}

func (h *Handler) bankDetail(w http.ResponseWriter, r *http.Request) {
	h.showBankDetail(w, r, http.StatusOK, "", nil)
}

func (h *Handler) showBankDetail(w http.ResponseWriter, r *http.Request, status int, errMsg string, bulkErrs map[string]string) {
	id, sa := h.staff(r)
	ctx := r.Context()
	b, ok := h.loadBank(w, r, sa)
	if !ok {
		return
	}
	now := time.Now()
	q := r.URL.Query()
	f := QuestionFilter{Text: strings.TrimSpace(q.Get("q")), Kind: q.Get("jenis"), ObjectiveID: q.Get("tp"),
		Difficulty: q.Get("kesulitan"), Status: q.Get("status"), CreatedBy: q.Get("pembuat")}
	f.Page, _ = strconv.Atoi(q.Get("hal"))
	pg, err := ListQuestions(ctx, h.db, sa.Actor, b, f, now)
	if err != nil {
		h.fail(w, r, "list questions", err)
		return
	}
	v := bankDetailView{User: id, Bank: b, Filter: f, Page: pg, Error: errMsg, BulkErrors: bulkErrs,
		CanWrite:  b.ArchivedAt == nil && sa.Can(ActBank, b.resource(), now) >= Write,
		CanManage: b.ArchivedAt == nil && sa.Can(ActQuestionEdit, b.resource(), now) >= Write,
		HasFilter: f.Text != "" || f.Kind != "" || f.ObjectiveID != "" || f.Difficulty != "" || f.Status != "" || f.CreatedBy != ""}
	if errMsg == "" {
		v.Flash = auth.PopFlash(ctx, h.sessions)
	}
	kinds := make([]string, len(Kinds))
	for i, k := range Kinds {
		kinds[i] = string(k)
	}
	v.Kinds = options(kinds, func(s string) string { return Kind(s).Label() }, f.Kind)
	v.Diffs = options(Difficulties, strings.ToTitle, f.Difficulty)
	for i := range v.Diffs {
		v.Diffs[i].Label = strings.ToUpper(v.Diffs[i].Value[:1]) + v.Diffs[i].Value[1:]
	}
	v.Statuses = options([]string{StatusDraft, StatusInReview, StatusReady}, func(s string) string {
		return map[string]string{StatusDraft: "Draf", StatusInReview: "Ditelaah", StatusReady: "Siap"}[s]
	}, f.Status)
	if b.SubjectID != "" {
		if v.Objectives, err = ListObjectives(ctx, h.db, sa.SchoolID, b.SubjectID, b.GradeLevel); err != nil {
			h.fail(w, r, "list objectives", err)
			return
		}
	}
	creators, err := h.db.Query(ctx, `SELECT DISTINCT u.id, u.full_name FROM exam_questions q JOIN users u ON u.id = q.created_by AND u.school_id = q.school_id
		WHERE q.school_id = $1 AND q.bank_id = $2 AND q.archived_at IS NULL ORDER BY u.full_name`, sa.SchoolID, b.ID)
	if err != nil {
		h.fail(w, r, "list creators", err)
		return
	}
	for creators.Next() {
		var o optionItem
		if err := creators.Scan(&o.Value, &o.Label); err != nil {
			creators.Close()
			h.fail(w, r, "list creators", err)
			return
		}
		o.Selected = o.Value == f.CreatedBy
		v.Creators = append(v.Creators, o)
	}
	creators.Close()
	if v.CanWrite {
		banks, err := ListBanks(ctx, h.db, sa.SchoolID, sa.Actor, 0, now)
		if err != nil {
			h.fail(w, r, "list banks", err)
			return
		}
		for _, ob := range banks {
			if ob.ID != b.ID && sa.Can(ActBank, ob.resource(), now) >= Write {
				v.OtherBanks = append(v.OtherBanks, ob.Bank)
			}
		}
	}
	for _, row := range pg.Rows {
		v.HasEditable = v.HasEditable || row.CanEdit
		n := len(v.Groups)
		if row.StimulusID != "" && n > 0 && v.Groups[n-1].StimulusID == row.StimulusID {
			v.Groups[n-1].Rows = append(v.Groups[n-1].Rows, row)
			continue
		}
		v.Groups = append(v.Groups, questionGroup{StimulusID: row.StimulusID, StimulusTitle: row.StimulusTitle, Rows: []QuestionRow{row}})
	}
	if pg.Total > 0 {
		v.From = (pg.Page-1)*QuestionPageSize + 1
		v.To = v.From + len(pg.Rows) - 1
	}
	pageURL := func(p int) string {
		qq := url.Values{}
		for k, vs := range q {
			qq[k] = vs
		}
		qq.Set("hal", strconv.Itoa(p))
		return "/ujian/bank/" + b.ID + "?" + qq.Encode()
	}
	if pg.Page > 1 {
		v.PrevURL = pageURL(pg.Page - 1)
	}
	if pg.Page < pg.Pages {
		v.NextURL = pageURL(pg.Page + 1)
	}
	h.render.Render(w, r, status, "exam/bank_detail", v)
}

const maxBulkForm = 64 << 10

func (h *Handler) bankBulk(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBulkForm) {
		return
	}
	_, sa := h.staff(r)
	b, ok := h.loadBank(w, r, sa)
	if !ok {
		return
	}
	p := r.PostForm
	op := Bulk{Action: p.Get("action"), ObjectiveID: p.Get("objective_id"), Tags: ParseTags(p.Get("tags")),
		Difficulty: p.Get("difficulty"), TargetBank: p.Get("target_bank")}
	res, err := BulkUpdate(r.Context(), h.db, sa, b.ID, p["ids"], op, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.showBankDetail(w, r, http.StatusUnprocessableEntity, "Aksi massal belum dijalankan. Periksa pilihan Anda.", fe.Fields)
		return
	}
	if err != nil {
		h.fail(w, r, "bulk questions", err)
		return
	}
	msg := fmt.Sprintf("%d soal diperbarui.", res.Updated)
	if res.Skipped > 0 {
		msg += fmt.Sprintf(" %d soal dilewati karena milik guru lain (hanya pembuat dan admin/TU yang bisa mengubah).", res.Skipped)
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	back := "/ujian/bank/" + b.ID
	if op.Action == "pindah" && res.Updated > 0 {
		back = "/ujian/bank/" + op.TargetBank
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) questionDuplicate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBankForm) {
		return
	}
	_, sa := h.staff(r)
	q, err := DuplicateQuestion(r.Context(), h.db, sa, chi.URLParam(r, "id"), r.PostForm.Get("target_bank"), time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		err = ErrNotFound
	}
	if err != nil {
		h.fail(w, r, "duplicate question", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Soal diduplikat sebagai draf milik Anda. Ubah sesuai kebutuhan.")
	http.Redirect(w, r, "/ujian/bank/"+q.BankID+"?status=draf&pembuat="+sa.UserID, http.StatusSeeOther)
}

func (h *Handler) questionArchive(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	q, err := GetQuestion(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if err == nil {
		err = ArchiveQuestion(r.Context(), h.db, sa, q.ID, time.Now())
	}
	if err != nil {
		h.fail(w, r, "archive question", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Soal diarsipkan.")
	http.Redirect(w, r, "/ujian/bank/"+q.BankID, http.StatusSeeOther)
}

type objectivesView struct {
	User       auth.Identity
	Subjects   []subjectOption
	SubjectID  string
	Grade      int
	Grades     []int
	Objectives []Objective
	CanEdit    bool
	Form       ObjectiveForm
	Errors     map[string]string
	EditID     string
	Flash      string
	Error      string
	Back       string
}

// readableSubjects mengembalikan mapel yang bank-nya boleh dibaca sa.
func (h *Handler) readableSubjects(r *http.Request, sa StaffActor, selected string) ([]subjectOption, error) {
	rows, err := h.db.Query(r.Context(), `SELECT id, name FROM subjects WHERE school_id = $1 AND is_active ORDER BY lower(name)`, sa.SchoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: list subjects: %w", err)
	}
	defer rows.Close()
	var out []subjectOption
	for rows.Next() {
		var o subjectOption
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, err
		}
		if sa.Can(ActBank, Resource{SubjectID: o.ID}, time.Now()) >= Read {
			o.Selected = o.ID == selected
			out = append(out, o)
		}
	}
	return out, rows.Err()
}

func (h *Handler) objectives(w http.ResponseWriter, r *http.Request) {
	h.showObjectives(w, r, http.StatusOK, objectivesView{})
}

func (h *Handler) showObjectives(w http.ResponseWriter, r *http.Request, status int, v objectivesView) {
	id, sa := h.staff(r)
	ctx := r.Context()
	q := r.URL.Query()
	if v.SubjectID == "" {
		v.SubjectID = q.Get("mapel")
		v.Grade, _ = parseGrade(q.Get("tingkat"))
	}
	v.User = id
	subjects, err := h.readableSubjects(r, sa, v.SubjectID)
	if err != nil {
		h.fail(w, r, "objectives", err)
		return
	}
	v.Subjects = subjects
	if v.SubjectID == "" && len(subjects) > 0 {
		v.SubjectID = subjects[0].ID
		subjects[0].Selected = true
	}
	found := false
	for _, s := range subjects {
		found = found || s.ID == v.SubjectID
	}
	if v.SubjectID != "" && !found {
		h.fail(w, r, "objectives", ErrNotFound)
		return
	}
	for g := 1; g <= 12; g++ {
		v.Grades = append(v.Grades, g)
	}
	if v.SubjectID != "" {
		v.CanEdit = sa.Can(ActBank, Resource{SubjectID: v.SubjectID}, time.Now()) >= Write
		if v.Objectives, err = ListObjectives(ctx, h.db, sa.SchoolID, v.SubjectID, v.Grade); err != nil {
			h.fail(w, r, "objectives", err)
			return
		}
	}
	if v.Errors == nil {
		v.Flash = auth.PopFlash(ctx, h.sessions)
	}
	if b := q.Get("bank"); b != "" && len(b) <= 36 {
		v.Back = "/ujian/bank/" + url.PathEscape(b)
	}
	h.render.Render(w, r, status, "exam/objectives", v)
}

func objectivesURL(subjectID, grade string) string {
	return "/ujian/tp/?mapel=" + url.QueryEscape(subjectID) + "&tingkat=" + url.QueryEscape(grade)
}

func (h *Handler) objectiveCreate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBankForm) {
		return
	}
	_, sa := h.staff(r)
	p := r.PostForm
	f := ObjectiveForm{SubjectID: p.Get("subject_id"), GradeLevel: p.Get("grade_level"), Code: p.Get("code"), Description: p.Get("description")}
	_, err := CreateObjective(r.Context(), h.db, sa, f, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		g, _ := parseGrade(f.GradeLevel)
		h.showObjectives(w, r, http.StatusUnprocessableEntity, objectivesView{SubjectID: f.SubjectID, Grade: g, Form: f, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "create objective", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "TP ditambahkan.")
	http.Redirect(w, r, objectivesURL(f.SubjectID, f.GradeLevel), http.StatusSeeOther)
}

func (h *Handler) objectivePaste(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 64<<10) {
		return
	}
	_, sa := h.staff(r)
	p := r.PostForm
	subjectID, grade := p.Get("subject_id"), p.Get("grade_level")
	res, err := PasteObjectives(r.Context(), h.db, sa, subjectID, grade, p.Get("paste"), time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		g, _ := parseGrade(grade)
		h.showObjectives(w, r, http.StatusUnprocessableEntity, objectivesView{SubjectID: subjectID, Grade: g, Errors: fe.Fields})
		return
	}
	if err != nil {
		h.fail(w, r, "paste objectives", err)
		return
	}
	msg := fmt.Sprintf("%d TP ditambahkan.", res.Added)
	if len(res.Skipped) > 0 {
		msg += " Dilewati: " + strings.Join(res.Skipped, "; ") + "."
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, objectivesURL(subjectID, grade), http.StatusSeeOther)
}

func (h *Handler) objectiveUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, maxBankForm) {
		return
	}
	_, sa := h.staff(r)
	o, err := getObjective(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, "update objective", err)
		return
	}
	f := ObjectiveForm{Code: r.PostForm.Get("code"), Description: r.PostForm.Get("description")}
	err = UpdateObjective(r.Context(), h.db, sa, o.ID, f, time.Now())
	var fe *FieldError
	if errors.As(err, &fe) {
		h.showObjectives(w, r, http.StatusUnprocessableEntity, objectivesView{SubjectID: o.SubjectID, Grade: o.GradeLevel, Form: f, Errors: fe.Fields, EditID: o.ID})
		return
	}
	if err != nil {
		h.fail(w, r, "update objective", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "TP disimpan.")
	http.Redirect(w, r, objectivesURL(o.SubjectID, strconv.Itoa(o.GradeLevel)), http.StatusSeeOther)
}

func (h *Handler) objectiveArchive(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	o, err := getObjective(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if err == nil {
		err = ArchiveObjective(r.Context(), h.db, sa, o.ID, time.Now())
	}
	if err != nil {
		h.fail(w, r, "archive objective", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "TP "+o.Code+" diarsipkan. Soal yang memakainya tetap tersimpan.")
	http.Redirect(w, r, objectivesURL(o.SubjectID, strconv.Itoa(o.GradeLevel)), http.StatusSeeOther)
}
