package exam

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// followUpRoutes mendaftarkan susulan, remedial, dan pengayaan (T137).
func (h *Handler) followUpRoutes(writers chi.Router) {
	writers.Post("/paket/{id}/susulan", h.examMakeup)
	writers.Post("/paket/{id}/tindak-lanjut", h.examFollowUp)
}

// followView adalah isi tab Tindak lanjut di detail ujian.
type followView struct {
	Candidates []MakeupCandidate
	Absent     int
	Students   []optionItem
	Objectives []optionItem
	FollowUps  []Exam
	History    []StudentHistory
	Parent     *Exam
}

func (h *Handler) loadFollowView(r *http.Request, e Exam, codes map[string]string, comp Composition, now time.Time) (followView, error) {
	ctx := r.Context()
	var v followView
	var err error
	if v.Candidates, err = MakeupCandidates(ctx, h.db, e, now); err != nil {
		return v, err
	}
	for _, c := range v.Candidates {
		if c.Absent {
			v.Absent++
		}
	}
	rows, err := h.db.Query(ctx, `SELECT DISTINCT s.id, s.full_name FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
		JOIN students s ON s.id = p.student_id WHERE x.exam_id = $1 AND x.cancelled_at IS NULL ORDER BY s.full_name`, e.ID)
	if err != nil {
		return v, fmt.Errorf("exam: followup students: %w", err)
	}
	for rows.Next() {
		var o optionItem
		if err := rows.Scan(&o.Value, &o.Label); err != nil {
			rows.Close()
			return v, err
		}
		v.Students = append(v.Students, o)
	}
	rows.Close()
	for id := range comp.ByObjective {
		if id != "" {
			v.Objectives = append(v.Objectives, optionItem{Value: id, Label: codes[id]})
		}
	}
	if v.FollowUps, err = ListFollowUps(ctx, h.db, e.SchoolID, e.ID); err != nil {
		return v, err
	}
	root := e.ID
	if e.ParentExamID != "" {
		root = e.ParentExamID
		if p, err := GetExam(ctx, h.db, e.SchoolID, e.ParentExamID); err == nil {
			v.Parent = &p
		}
	}
	v.History, err = FollowUpHistory(ctx, h.db, e.SchoolID, root)
	return v, err
}

func (h *Handler) examMakeup(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	back := examPath(e.ID) + "?tab=lanjut"
	pf := r.PostForm
	loc := id.Location()
	start, ok1 := parseLocal(pf.Get("date"), pf.Get("start"), loc)
	end, ok2 := parseLocal(pf.Get("end_date"), pf.Get("end"), loc)
	if !ok1 || !ok2 {
		start, end = time.Time{}, time.Time{}
	}
	s, err := CreateMakeupSession(r.Context(), h.db, sa, e.ID, MakeupForm{Participants: pf["participant"], Start: start, End: end, Location: pf.Get("location")}, time.Now())
	if err != nil {
		h.examFail(w, r, e, back, "create makeup", err)
		return
	}
	h.flash(r, fmt.Sprintf("Sesi susulan dibuat untuk %d peserta. Nilainya masuk ke ujian ini. Cetak kartu di tab Dokumen.", s.Participants))
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) examFollowUp(w http.ResponseWriter, r *http.Request) {
	id, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	back := examPath(e.ID) + "?tab=lanjut"
	pf := r.PostForm
	loc := id.Location()
	start, ok1 := parseLocal(pf.Get("date"), pf.Get("start"), loc)
	end, ok2 := parseLocal(pf.Get("end_date"), pf.Get("end"), loc)
	if !ok1 || !ok2 {
		start, end = time.Time{}, time.Time{}
	}
	f := FollowUpForm{Kind: pf.Get("kind"), Students: pf["student"], Objectives: pf["objective"], Start: start, End: end, Location: pf.Get("location")}
	fu, err := CreateFollowUp(r.Context(), h.db, sa, e.ID, f, time.Now())
	if errors.Is(err, ErrNoObjectives) {
		h.flash(r, "Remedial terfokus butuh soal bertanda TP di ujian ini. Tandai TP soal di bank, atau buat pengayaan.")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if err != nil {
		h.examFail(w, r, e, back, "create followup", err)
		return
	}
	h.flash(r, "Ujian "+f.Kind+" dibuat sebagai draf. Periksa soalnya lalu jadwalkan.")
	http.Redirect(w, r, examStepPath(fu.ID, stepQuestions), http.StatusSeeOther)
}
