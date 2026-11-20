package exam

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
)

// essayRoutes mendaftarkan koreksi esai (T147). Peran: admin_tu dan guru;
// hak per ujian dari LoadGraderScope (pemegang ujian, koordinator,
// admin_tu penuh; guru yang ditugaskan hanya bagiannya; lainnya 404).
func (h *Handler) essayRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleGuru))
		r.Use(h.withActor)
		r.Get("/paket/{id}/esai", h.essayQueue)
		r.Get("/paket/{id}/esai/{qid}", h.essayGrade)
		r.Post("/paket/{id}/esai/{qid}/{attempt}", h.essaySave)
		r.Post("/paket/{id}/esai/tugas", h.essayAssign)
		r.Post("/paket/{id}/esai/tugas/{aid}/hapus", h.essayUnassign)
	})
}

func essayPath(examID string) string { return "/ujian/paket/" + examID + "/esai" }

// essayExam memuat ujian dan cakupan koreksi; 404 bila tidak berhak.
func (h *Handler) essayExam(w http.ResponseWriter, r *http.Request) (auth.Identity, StaffActor, Exam, GraderScope, bool) {
	id, sa := h.staff(r)
	e, err := GetExam(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Ujian tidak ditemukan.")
		return id, sa, e, GraderScope{}, false
	}
	if err != nil {
		h.fail(w, r, "get exam", err)
		return id, sa, e, GraderScope{}, false
	}
	scope, err := LoadGraderScope(r.Context(), h.db, sa.Actor, e, time.Now())
	if err != nil {
		h.fail(w, r, "grader scope", err)
		return id, sa, e, scope, false
	}
	if !scope.Any() {
		h.logger.WarnContext(r.Context(), "essay grading denied", "user_id", sa.UserID, "exam_id", e.ID)
		page.NotFound(h.render, w, r, id, "Ujian tidak ditemukan.")
		return id, sa, e, scope, false
	}
	return id, sa, e, scope, true
}

type essayQueueView struct {
	User        auth.Identity
	Exam        Exam
	Questions   []EssayQuestion
	Total       int
	Graded      int
	Full        bool
	Assignments []GraderAssignment
	Teachers    []optionItem
	Sessions    []optionItem
	Flash       string
	Error       string
}

func (h *Handler) essayQueue(w http.ResponseWriter, r *http.Request) {
	id, sa, e, scope, ok := h.essayExam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	qs, err := LoadEssayQueue(ctx, h.db, e, scope)
	if err != nil {
		h.fail(w, r, "essay queue", err)
		return
	}
	v := essayQueueView{User: id, Exam: e, Questions: qs, Full: scope.Full, Flash: auth.PopFlash(ctx, h.sessions)}
	for _, q := range qs {
		v.Total += q.Total
		v.Graded += q.Graded
	}
	if scope.Full {
		if v.Assignments, err = ListGraderAssignments(ctx, h.db, sa.SchoolID, e.ID); err != nil {
			h.fail(w, r, "grader assignments", err)
			return
		}
		rows, err := h.db.Query(ctx, `SELECT u.id, u.full_name FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles ro ON ro.id = ur.role_id
			WHERE u.school_id = $1 AND u.is_active AND ro.code = 'guru' AND u.id <> $2 ORDER BY lower(u.full_name)`, sa.SchoolID, e.OwnerID)
		if err != nil {
			h.fail(w, r, "teachers", err)
			return
		}
		for rows.Next() {
			var o optionItem
			if err := rows.Scan(&o.Value, &o.Label); err != nil {
				rows.Close()
				h.fail(w, r, "teachers", err)
				return
			}
			v.Teachers = append(v.Teachers, o)
		}
		rows.Close()
		sessions, err := ListSessions(ctx, h.db, sa.SchoolID, e.ID)
		if err != nil {
			h.fail(w, r, "sessions", err)
			return
		}
		for _, s := range sessions {
			v.Sessions = append(v.Sessions, optionItem{Value: s.ID, Label: s.Title()})
		}
	}
	h.render.Render(w, r, http.StatusOK, "exam/essay_queue", v)
}

type essayGradeView struct {
	User      auth.Identity
	Exam      Exam
	Q         EssayQuestion
	Body      template.HTML
	Rubric    template.HTML
	Answer    *EssayAnswer
	Graded    int
	Total     int
	Position  int // urutan jawaban ini dalam cakupan
	PrevID    string
	NextID    string
	Buttons   []int
	Anonymous bool
	Query     string // anonim=0 bila nama ditampilkan
	Score     string
	Comment   string
	Error     string
	Flash     string
}

// essayAnswers memuat soal esai dan jawaban dalam cakupan.
func (h *Handler) essayAnswers(w http.ResponseWriter, r *http.Request, e Exam, scope GraderScope) (EssayQuestion, []EssayAnswer, bool) {
	qid := chi.URLParam(r, "qid")
	qs, err := essaySnapshot(r.Context(), h.db, e)
	if err != nil {
		h.fail(w, r, "essay questions", err)
		return EssayQuestion{}, nil, false
	}
	var eq *EssayQuestion
	for i := range qs {
		if qs[i].Question.ID == qid {
			eq = &qs[i]
		}
	}
	id, _ := auth.CurrentUser(r.Context())
	if eq == nil || !scope.AllowsQuestion(qid) {
		page.NotFound(h.render, w, r, id, "Soal esai tidak ditemukan.")
		return EssayQuestion{}, nil, false
	}
	all, err := LoadEssayAnswers(r.Context(), h.db, e, qid, scope)
	if err != nil {
		h.fail(w, r, "essay answers", err)
		return EssayQuestion{}, nil, false
	}
	var mine []EssayAnswer
	for _, a := range all {
		if a.InScope {
			mine = append(mine, a)
			eq.Total++
			if a.Graded() {
				eq.Graded++
			}
		}
	}
	return *eq, mine, true
}

func (h *Handler) essayGradeView(r *http.Request, id auth.Identity, e Exam, eq EssayQuestion, answers []EssayAnswer, current string) essayGradeView {
	v := essayGradeView{User: id, Exam: e, Q: eq, Body: trusted(eq.Question.BodyHTML, staffAssetURL),
		Rubric: trusted(eq.Question.Key.RubricHTML, staffAssetURL), Graded: eq.Graded, Total: eq.Total,
		Anonymous: r.URL.Query().Get("anonim") != "0" && r.PostForm.Get("anonim") != "0"}
	if !v.Anonymous {
		v.Query = "?anonim=0"
	}
	for i := 0; i <= eq.Points && eq.Points <= 10; i++ {
		v.Buttons = append(v.Buttons, i)
	}
	pick := -1
	for i, a := range answers {
		if a.AttemptID == current {
			pick = i
		}
	}
	if pick < 0 {
		for i, a := range answers {
			if !a.Graded() {
				pick = i
				break
			}
		}
	}
	if pick < 0 && len(answers) > 0 {
		pick = 0
	}
	if pick >= 0 {
		a := answers[pick]
		v.Answer, v.Position = &a, pick+1
		v.Score, v.Comment = a.PointsLabel(), a.Comment
		if pick > 0 {
			v.PrevID = answers[pick-1].AttemptID
		}
		if pick+1 < len(answers) {
			v.NextID = answers[pick+1].AttemptID
		}
	}
	return v
}

func (h *Handler) essayGrade(w http.ResponseWriter, r *http.Request) {
	id, _, e, scope, ok := h.essayExam(w, r)
	if !ok {
		return
	}
	eq, answers, ok := h.essayAnswers(w, r, e, scope)
	if !ok {
		return
	}
	v := h.essayGradeView(r, id, e, eq, answers, r.URL.Query().Get("a"))
	v.Flash = auth.PopFlash(r.Context(), h.sessions)
	w.Header().Set("Cache-Control", "private, no-store")
	h.render.Render(w, r, http.StatusOK, "exam/essay_grade", v)
}

// essaySave menyimpan skor lalu membuka jawaban berikutnya yang belum
// dikoreksi (Simpan & berikutnya).
func (h *Handler) essaySave(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 16<<10) {
		return
	}
	id, sa, e, scope, ok := h.essayExam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	qid, attempt := chi.URLParam(r, "qid"), chi.URLParam(r, "attempt")
	_, err := GradeEssay(ctx, h.db, sa, e, scope, qid, attempt, r.PostForm.Get("skor"), r.PostForm.Get("komentar"), time.Now())
	if err != nil {
		var msg string
		switch {
		case errors.Is(err, ErrBadScore):
			msg = "Skor harus angka 0 sampai skor maksimal soal (boleh desimal, mis. 2,5)."
		case errors.Is(err, ErrBadComment):
			msg = "Komentar paling banyak 1.000 karakter."
		case errors.Is(err, ErrOutOfScope), errors.Is(err, ErrNoEssayText):
			page.NotFound(h.render, w, r, id, "Jawaban tidak ditemukan.")
			return
		default:
			h.fail(w, r, "grade essay", err)
			return
		}
		eq, answers, ok := h.essayAnswers(w, r, e, scope)
		if !ok {
			return
		}
		v := h.essayGradeView(r, id, e, eq, answers, attempt)
		v.Error, v.Score, v.Comment = msg, r.PostForm.Get("skor"), r.PostForm.Get("komentar")
		h.render.Render(w, r, http.StatusUnprocessableEntity, "exam/essay_grade", v)
		return
	}
	eq, answers, ok := h.essayAnswers(w, r, e, scope)
	if !ok {
		return
	}
	q := url.Values{}
	if r.PostForm.Get("anonim") == "0" {
		q.Set("anonim", "0")
	}
	// Berikutnya: jawaban belum dikoreksi setelah yang ini, lalu dari awal.
	cur := -1
	for i, a := range answers {
		if a.AttemptID == attempt {
			cur = i
		}
	}
	next := ""
	for k := 1; k <= len(answers) && next == ""; k++ {
		if a := answers[(cur+k+len(answers))%len(answers)]; !a.Graded() {
			next = a.AttemptID
		}
	}
	if next != "" {
		q.Set("a", next)
		h.flash(r, "Tersimpan. "+strconv.Itoa(eq.Graded)+"/"+strconv.Itoa(eq.Total)+" dikoreksi.")
		http.Redirect(w, r, essayPath(e.ID)+"/"+qid+"?"+q.Encode(), http.StatusSeeOther)
		return
	}
	pending, err := PendingEssays(ctx, h.db, sa.SchoolID, e.ID)
	if err != nil {
		h.fail(w, r, "pending essays", err)
		return
	}
	if pending == 0 {
		h.flash(r, "Semua esai ujian ini sudah dikoreksi. Nilai peserta final dan siap dikirim ke rapor.")
	} else {
		h.flash(r, "Semua jawaban soal ini sudah dikoreksi. Masih ada "+strconv.Itoa(pending)+" jawaban esai lain di ujian ini.")
	}
	http.Redirect(w, r, essayPath(e.ID), http.StatusSeeOther)
}

func (h *Handler) essayAssign(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 8<<10) {
		return
	}
	id, sa, e, scope, ok := h.essayExam(w, r)
	if !ok {
		return
	}
	if !scope.Full {
		page.Forbidden(h.render, w, r, id, "Hanya pemegang ujian, koordinator, dan admin TU yang membagi koreksi.")
		return
	}
	f := r.PostForm
	err := AssignGrader(r.Context(), h.db, sa, e, f.Get("user_id"), f.Get("question_id"), f.Get("session_id"))
	switch {
	case errors.Is(err, ErrBadAssignment):
		h.flash(r, "Pilih guru aktif, soal esai, dan sesi ujian ini.")
	case err != nil:
		h.fail(w, r, "assign grader", err)
		return
	default:
		h.flash(r, "Penugasan koreksi ditambahkan. Guru itu melihatnya di beranda.")
	}
	http.Redirect(w, r, essayPath(e.ID), http.StatusSeeOther)
}

func (h *Handler) essayUnassign(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 4<<10) {
		return
	}
	id, sa, e, scope, ok := h.essayExam(w, r)
	if !ok {
		return
	}
	if !scope.Full {
		page.Forbidden(h.render, w, r, id, "Hanya pemegang ujian, koordinator, dan admin TU yang membagi koreksi.")
		return
	}
	aid, _ := strconv.ParseInt(chi.URLParam(r, "aid"), 10, 64)
	if err := RemoveGrader(r.Context(), h.db, sa, e, aid); err != nil && !errors.Is(err, ErrNotFound) {
		h.fail(w, r, "remove grader", err)
		return
	}
	h.flash(r, "Penugasan koreksi dihapus.")
	http.Redirect(w, r, essayPath(e.ID), http.StatusSeeOther)
}
