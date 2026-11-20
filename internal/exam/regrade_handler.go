package exam

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
)

// Halaman "Betulkan kunci" (T146): form kunci per jenis soal, pratinjau
// dampak, lalu terapkan dengan alasan. Hak: penulis ujian (pembuat,
// koordinator, admin_tu).

type keyOption struct {
	ID, Label string
	On        bool
}

type keyRow struct {
	ID, Label string
	Value     string // benar_salah: "B"/"S"; menjodohkan: id kanan
}

type keyFixView struct {
	User       auth.Identity
	Exam       Exam
	Question   Question
	Excerpt    string
	Overridden bool
	Kind       Kind
	Choices    []keyOption // pg, pg_kompleks
	Scoring    string
	Rows       []keyRow    // benar_salah, menjodohkan
	Right      []keyOption // menjodohkan
	Accepted   string      // isian: satu per baris
	Tolerance  string
	Numeric    bool
	Reason     string
	Impact     *RegradeImpact
	Error      string
	Flash      string
}

func choiceLabel(c Choice, i int) string {
	return string(rune('A'+i)) + ". " + Snippet(PlainText(c.Src), 80)
}

// keyFixViewOf mengisi form dari kunci k.
func keyFixViewOf(id auth.Identity, e Exam, q Question, k AnswerKey, overridden bool) keyFixView {
	v := keyFixView{User: id, Exam: e, Question: q, Excerpt: q.Excerpt(), Overridden: overridden, Kind: q.Kind, Scoring: k.Scoring,
		Numeric: q.Options.Input == "angka"}
	correct := setOf(k.Correct)
	for i, c := range q.Options.Choices {
		v.Choices = append(v.Choices, keyOption{ID: c.ID, Label: choiceLabel(c, i), On: correct[c.ID]})
	}
	for i, s := range q.Options.Statements {
		val := ""
		if t, ok := k.Truth[s.ID]; ok {
			val = map[bool]string{true: "B", false: "S"}[t]
		}
		v.Rows = append(v.Rows, keyRow{ID: s.ID, Label: strconv.Itoa(i+1) + ". " + Snippet(PlainText(s.Src), 100), Value: val})
	}
	for i, l := range q.Options.Left {
		v.Rows = append(v.Rows, keyRow{ID: l.ID, Label: strconv.Itoa(i+1) + ". " + Snippet(PlainText(l.Src), 80), Value: k.Pairs[l.ID]})
	}
	for i, r := range q.Options.Right {
		v.Right = append(v.Right, keyOption{ID: r.ID, Label: choiceLabel(r, i)})
	}
	v.Accepted = strings.Join(k.Accepted, "\n")
	if k.Tolerance > 0 {
		v.Tolerance = strings.ReplaceAll(strconv.FormatFloat(k.Tolerance, 'f', -1, 64), ".", ",")
	}
	return v
}

// keyFromForm membaca kunci baru dari form; bagian lain kunci (rubrik,
// normalisasi Arab) dipertahankan dari kunci lama.
func keyFromForm(q Question, cur AnswerKey, r *http.Request) AnswerKey {
	f := r.PostForm
	k := cur
	switch q.Kind {
	case KindMC:
		k.Correct = nil
		if c := f.Get("correct"); c != "" {
			k.Correct = []string{c}
		}
	case KindMulti:
		k.Correct = f["correct"]
		if s := f.Get("scoring"); s != "" {
			k.Scoring = s
		}
	case KindTF:
		k.Truth = map[string]bool{}
		for _, s := range q.Options.Statements {
			switch f.Get("truth_" + s.ID) {
			case "B":
				k.Truth[s.ID] = true
			case "S":
				k.Truth[s.ID] = false
			}
		}
	case KindShort:
		k.Accepted = nil
		for _, line := range strings.Split(f.Get("accepted"), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				k.Accepted = append(k.Accepted, line)
			}
		}
		k.Tolerance = 0
		if t := strings.TrimSpace(f.Get("tolerance")); t != "" {
			if v, ok := parseNumber(t); ok {
				k.Tolerance = v
			} else {
				k.Tolerance = -1 // ditolak ValidateKey
			}
		}
	case KindMatch:
		k.Pairs = map[string]string{}
		for _, l := range q.Options.Left {
			if v := f.Get("pair_" + l.ID); v != "" {
				k.Pairs[l.ID] = v
			}
		}
	}
	return k
}

func keyFixPath(examID, qid string) string {
	return "/ujian/paket/" + examID + "/soal/" + qid + "/kunci"
}

func (h *Handler) keyFixLoad(w http.ResponseWriter, r *http.Request) (auth.Identity, StaffActor, Exam, Question, AnswerKey, bool, bool) {
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Write)
	if !ok {
		return id, sa, e, Question{}, AnswerKey{}, false, false
	}
	q, k, over, err := QuestionKey(r.Context(), h.db, e, chi.URLParam(r, "qid"))
	switch {
	case errors.Is(err, ErrNotLocked):
		h.flash(r, "Kunci bisa dibetulkan setelah ujian dimulai. Sebelum itu, sunting soal di bank.")
		http.Redirect(w, r, examSoalPath(e.ID), http.StatusSeeOther)
		return id, sa, e, q, k, over, false
	case err != nil:
		h.fail(w, r, "question key", err)
		return id, sa, e, q, k, over, false
	case q.Kind == KindEssay:
		h.flash(r, "Esai dinilai manual; tidak ada kunci untuk dibetulkan.")
		http.Redirect(w, r, examSoalPath(e.ID), http.StatusSeeOther)
		return id, sa, e, q, k, over, false
	}
	return id, sa, e, q, k, over, true
}

func (h *Handler) keyFixForm(w http.ResponseWriter, r *http.Request) {
	id, _, e, q, k, over, ok := h.keyFixLoad(w, r)
	if !ok {
		return
	}
	v := keyFixViewOf(id, e, q, k, over)
	v.Flash = auth.PopFlash(r.Context(), h.sessions)
	h.render.Render(w, r, http.StatusOK, "exam/key_fix", v)
}

// keyFixPost: aksi=pratinjau menghitung dampak; aksi=terapkan menyimpan
// kunci dan menilai ulang.
func (h *Handler) keyFixPost(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 32<<10) {
		return
	}
	id, sa, e, q, cur, over, ok := h.keyFixLoad(w, r)
	if !ok {
		return
	}
	ctx, now := r.Context(), time.Now()
	key := keyFromForm(q, cur, r)
	v := keyFixViewOf(id, e, q, key, over)
	v.Reason = strings.TrimSpace(r.PostForm.Get("alasan"))
	var imp RegradeImpact
	var err error
	apply := r.PostForm.Get("aksi") == "terapkan"
	if apply {
		imp, err = ApplyKeyFix(ctx, h.db, sa, e, q.ID, key, v.Reason, now)
	} else {
		imp, err = PreviewKeyFix(ctx, h.db, e, q.ID, key, now)
	}
	switch {
	case errors.Is(err, ErrBadKey):
		v.Error = "Kunci belum lengkap atau tidak valid. Periksa pilihan di bawah."
	case errors.Is(err, ErrKeyUnchanged):
		v.Error = "Kunci sama dengan yang berlaku sekarang."
	case errors.Is(err, ErrNeedReason):
		v.Error = "Tulis alasan pembetulan kunci (paling banyak 300 karakter); tercatat di audit."
		if p, perr := PreviewKeyFix(ctx, h.db, e, q.ID, key, now); perr == nil {
			v.Impact = &p
		}
	case err != nil:
		h.fail(w, r, "regrade", err)
		return
	case apply:
		h.flash(r, "Kunci dibetulkan. "+strconv.Itoa(imp.Attempts)+" attempt dinilai ulang: "+strconv.Itoa(imp.Raised)+" naik, "+
			strconv.Itoa(imp.Lowered)+" turun.")
		http.Redirect(w, r, resultsPath(e.ID), http.StatusSeeOther)
		return
	default:
		v.Impact = &imp
	}
	status := http.StatusOK
	if v.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	h.render.Render(w, r, status, "exam/key_fix", v)
}
