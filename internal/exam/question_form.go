package exam

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"akra/internal/exam/mathrender"
	"akra/internal/exam/richtext"
)

// QuestionForm adalah isian editor soal apa adanya (T128). Teks kaya
// (Body, opsi, pembahasan, rubrik) berupa HTML dari editor dan dibersihkan
// richtext.Process.
type QuestionForm struct {
	Kind       string
	StimulusID string
	Body       string
	// pg / pg_kompleks
	Choices []string
	Correct []int // indeks opsi benar
	Scoring string
	// benar_salah: Truth[i] "B" atau "S"
	Statements []string
	Truth      []string
	// menjodohkan: Lefts[i] berpasangan dengan Rights[i]; Distractors
	// adalah pilihan kanan tambahan tanpa pasangan.
	Lefts       []string
	Rights      []string
	Distractors []string
	// isian
	Accepted        string // satu jawaban per baris
	Input           string // "teks" atau "angka"
	Tolerance       string
	ArabicNormalize bool
	// esai
	Rubric string
	// metadata
	Points         string
	Difficulty     string
	CognitiveLevel string
	ObjectiveID    string
	Indicator      string
	Tags           string
	Explanation    string
	Status         string
}

const maxRows = 10

func list(v url.Values, key string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = v.Get(fmt.Sprintf("%s_%d", key, i))
	}
	return out
}

// ParseQuestionForm membaca isian editor dari form POST.
func ParseQuestionForm(v url.Values) QuestionForm {
	f := QuestionForm{
		Kind: v.Get("kind"), StimulusID: v.Get("stimulus_id"), Body: v.Get("body"),
		Choices: list(v, "choice", maxChoices), Scoring: v.Get("scoring"),
		Statements: list(v, "statement", maxStatement), Truth: list(v, "truth", maxStatement),
		Lefts: list(v, "left", maxPairs), Rights: list(v, "right", maxPairs), Distractors: list(v, "distractor", 5),
		Accepted: v.Get("accepted"), Input: v.Get("input"), Tolerance: v.Get("tolerance"),
		ArabicNormalize: v.Get("arabic_normalize") == "on",
		Rubric:          v.Get("rubric"), Points: v.Get("points"), Difficulty: v.Get("difficulty"),
		CognitiveLevel: v.Get("cognitive_level"), ObjectiveID: v.Get("objective_id"), Indicator: v.Get("indicator"),
		Tags: v.Get("tags"), Explanation: v.Get("explanation"), Status: v.Get("status"),
	}
	for _, s := range v["correct"] {
		if i, err := strconv.Atoi(s); err == nil && i >= 0 && i < maxChoices {
			f.Correct = append(f.Correct, i)
		}
	}
	return f
}

// Values mengubah form kembali menjadi url.Values (untuk draf).
func (f QuestionForm) Values() url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	set("kind", f.Kind)
	set("stimulus_id", f.StimulusID)
	set("body", f.Body)
	for i, s := range f.Choices {
		set(fmt.Sprintf("choice_%d", i), s)
	}
	for _, c := range f.Correct {
		v.Add("correct", strconv.Itoa(c))
	}
	set("scoring", f.Scoring)
	for i := range f.Statements {
		set(fmt.Sprintf("statement_%d", i), f.Statements[i])
		if i < len(f.Truth) {
			set(fmt.Sprintf("truth_%d", i), f.Truth[i])
		}
	}
	for i := range f.Lefts {
		set(fmt.Sprintf("left_%d", i), f.Lefts[i])
		if i < len(f.Rights) {
			set(fmt.Sprintf("right_%d", i), f.Rights[i])
		}
	}
	for i, s := range f.Distractors {
		set(fmt.Sprintf("distractor_%d", i), s)
	}
	set("accepted", f.Accepted)
	set("input", f.Input)
	set("tolerance", f.Tolerance)
	if f.ArabicNormalize {
		v.Set("arabic_normalize", "on")
	}
	for k, s := range map[string]string{"rubric": f.Rubric, "points": f.Points, "difficulty": f.Difficulty, "cognitive_level": f.CognitiveLevel,
		"objective_id": f.ObjectiveID, "indicator": f.Indicator, "tags": f.Tags, "explanation": f.Explanation, "status": f.Status} {
		set(k, s)
	}
	return v
}

// IsCorrect melaporkan apakah opsi i ditandai benar (untuk template).
func (f QuestionForm) IsCorrect(i int) bool {
	for _, c := range f.Correct {
		if c == i {
			return true
		}
	}
	return false
}

// FormFromQuestion mengisi form editor dari soal yang ada.
func FormFromQuestion(q Question) QuestionForm {
	f := QuestionForm{Kind: string(q.Kind), StimulusID: q.StimulusID, Body: q.BodySrc,
		Choices: make([]string, maxChoices), Statements: make([]string, maxStatement), Truth: make([]string, maxStatement),
		Lefts: make([]string, maxPairs), Rights: make([]string, maxPairs), Distractors: make([]string, 5),
		Scoring: q.Key.Scoring, Input: q.Options.Input, ArabicNormalize: q.Key.ArabicNormalize, Rubric: q.Key.RubricSrc,
		Points: strconv.Itoa(q.Points), Difficulty: q.Difficulty, CognitiveLevel: q.CognitiveLevel, ObjectiveID: q.ObjectiveID,
		Indicator: q.Indicator, Tags: strings.Join(q.Tags, ", "), Explanation: q.ExplanationSrc, Status: q.Status}
	for i, c := range q.Options.Choices {
		if i < maxChoices {
			f.Choices[i] = c.Src
			for _, id := range q.Key.Correct {
				if id == c.ID {
					f.Correct = append(f.Correct, i)
				}
			}
		}
	}
	for i, s := range q.Options.Statements {
		if i < maxStatement {
			f.Statements[i] = s.Src
			if t, ok := q.Key.Truth[s.ID]; ok {
				f.Truth[i] = map[bool]string{true: "B", false: "S"}[t]
			}
		}
	}
	rightByID := map[string]Choice{}
	for _, r := range q.Options.Right {
		rightByID[r.ID] = r
	}
	used := map[string]bool{}
	for i, l := range q.Options.Left {
		if i < maxPairs {
			f.Lefts[i] = l.Src
			if rid, ok := q.Key.Pairs[l.ID]; ok {
				f.Rights[i] = rightByID[rid].Src
				used[rid] = true
			}
		}
	}
	d := 0
	for _, r := range q.Options.Right {
		if !used[r.ID] && d < len(f.Distractors) {
			f.Distractors[d] = r.Src
			d++
		}
	}
	f.Accepted = strings.Join(q.Key.Accepted, "\n")
	if q.Key.Tolerance > 0 {
		f.Tolerance = strconv.FormatFloat(q.Key.Tolerance, 'f', -1, 64)
	}
	return f
}

// builder memproses teks kaya dan mengumpulkan pesan kesalahan.
type builder struct {
	r      *mathrender.Renderer
	fields map[string]string
	assets []string
}

// rich memproses satu teks kaya. empty true bila tidak berisi teks, rumus,
// maupun gambar.
func (b *builder) rich(field, label, src string) (c Choice, empty bool) {
	doc, err := richtext.Process(src, b.r)
	if err != nil {
		b.fields[field] = label + ": isi terlalu panjang atau tidak terbaca."
		return Choice{}, true
	}
	switch {
	case len(doc.MathErrors) > 0:
		b.fields[field] = fmt.Sprintf("%s: rumus tidak valid (%s): %s", label, doc.MathErrors[0].TeX, doc.MathErrors[0].Message)
	case doc.MissingAlt > 0:
		b.fields[field] = label + ": isi teks alternatif untuk setiap gambar (untuk pembaca layar)."
	}
	b.assets = append(b.assets, doc.Assets...)
	return Choice{Src: doc.Src, HTML: doc.HTML}, doc.Empty()
}

func letter(i int) string { return string(rune('A' + i)) }

// trimTrailing membuang baris kosong di akhir daftar.
func trimTrailing(n int, empty func(i int) bool) int {
	for n > 0 && empty(n-1) {
		n--
	}
	return n
}

// BuildQuestion memeriksa form dan menyusun soal siap simpan. Status "siap"
// memerlukan isi lengkap per jenis; draf cukup teks soal dan rumus valid.
// Mengembalikan juga ID gambar yang dipakai (dicek pemanggil).
func BuildQuestion(f QuestionForm, r *mathrender.Renderer) (Question, []string, error) {
	b := &builder{r: r, fields: map[string]string{}}
	q := Question{Kind: Kind(f.Kind), StimulusID: f.StimulusID, Difficulty: f.Difficulty, CognitiveLevel: f.CognitiveLevel,
		ObjectiveID: f.ObjectiveID, Indicator: strings.TrimSpace(f.Indicator), Tags: ParseTags(f.Tags), Status: f.Status}
	if q.Status == "" {
		q.Status = StatusReady
	}
	strict := q.Status != StatusDraft
	if tags, msg := normalizeTags(q.Tags); msg != "" {
		b.fields["tags"] = msg
	} else {
		q.Tags = tags
	}
	if !q.Kind.Valid() {
		return q, nil, &FieldError{Fields: map[string]string{"kind": "Pilih jenis soal."}}
	}
	if p := strings.TrimSpace(f.Points); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			b.fields["points"] = "Poin harus bilangan bulat 1–100."
		}
		q.Points = n
	}
	body, empty := b.rich("body", "Teks soal", f.Body)
	q.BodySrc, q.BodyHTML = body.Src, body.HTML
	if empty && b.fields["body"] == "" {
		b.fields["body"] = "Tulis teks soal."
	}
	expl, _ := b.rich("explanation", "Pembahasan", f.Explanation)
	q.ExplanationSrc, q.ExplanationHTML = expl.Src, expl.HTML

	switch q.Kind {
	case KindMC, KindMulti:
		n := trimTrailing(len(f.Choices), func(i int) bool { return strings.TrimSpace(f.Choices[i]) == "" })
		seen := map[string]int{}
		for i := 0; i < n; i++ {
			c, empty := b.rich(fmt.Sprintf("choice_%d", i), "Opsi "+letter(i), f.Choices[i])
			if empty && b.fields[fmt.Sprintf("choice_%d", i)] == "" {
				b.fields[fmt.Sprintf("choice_%d", i)] = "Opsi " + letter(i) + " kosong. Isi atau hapus opsi ini."
			}
			key := NormalizeSearch(PlainText(c.Src)) + "|" + strings.Join(assetsOf(c.Src), ",")
			if j, dup := seen[key]; dup && !empty {
				b.fields[fmt.Sprintf("choice_%d", i)] = fmt.Sprintf("Opsi %s sama dengan opsi %s.", letter(i), letter(j))
			}
			seen[key] = i
			c.ID = strings.ToLower(letter(i))
			q.Options.Choices = append(q.Options.Choices, c)
		}
		for _, ci := range f.Correct {
			if ci < n {
				q.Key.Correct = append(q.Key.Correct, strings.ToLower(letter(ci)))
			}
		}
		if strict && n < minChoices {
			b.fields["choices"] = "Butuh minimal 2 opsi."
		}
		if q.Kind == KindMC && strict && len(q.Key.Correct) != 1 {
			b.fields["correct"] = "Tandai satu opsi yang benar."
		}
		if q.Kind == KindMulti {
			if strict && len(q.Key.Correct) < 1 {
				b.fields["correct"] = "Tandai minimal satu opsi yang benar."
			}
			q.Key.Scoring = f.Scoring
			if q.Key.Scoring != ScoringAll && q.Key.Scoring != ScoringProportional {
				q.Key.Scoring = ScoringProportional
			}
		}
	case KindTF:
		n := trimTrailing(len(f.Statements), func(i int) bool { return strings.TrimSpace(f.Statements[i]) == "" })
		q.Key.Truth = map[string]bool{}
		for i := 0; i < n; i++ {
			field := fmt.Sprintf("statement_%d", i)
			c, empty := b.rich(field, fmt.Sprintf("Pernyataan %d", i+1), f.Statements[i])
			if empty && b.fields[field] == "" {
				b.fields[field] = fmt.Sprintf("Pernyataan %d kosong.", i+1)
			}
			c.ID = fmt.Sprintf("s%d", i+1)
			q.Options.Statements = append(q.Options.Statements, c)
			t := ""
			if i < len(f.Truth) {
				t = f.Truth[i]
			}
			switch t {
			case "B":
				q.Key.Truth[c.ID] = true
			case "S":
				q.Key.Truth[c.ID] = false
			default:
				if strict {
					b.fields[field] = fmt.Sprintf("Pilih Benar atau Salah untuk pernyataan %d.", i+1)
				}
			}
		}
		if strict && n == 0 {
			b.fields["statements"] = "Tulis minimal satu pernyataan."
		}
	case KindShort:
		q.Options.Input = f.Input
		if q.Options.Input != "angka" {
			q.Options.Input = "teks"
		}
		q.Key.ArabicNormalize = f.ArabicNormalize
		for _, line := range strings.Split(f.Accepted, "\n") {
			for _, a := range strings.Split(line, "|") {
				if a = strings.TrimSpace(a); a != "" {
					q.Key.Accepted = append(q.Key.Accepted, a)
				}
			}
		}
		if strict && len(q.Key.Accepted) == 0 {
			b.fields["accepted"] = "Isian butuh minimal satu jawaban diterima."
		}
		if q.Options.Input == "angka" {
			for _, a := range q.Key.Accepted {
				if _, ok := parseNumber(a); !ok {
					b.fields["accepted"] = fmt.Sprintf("Jawaban \"%s\" bukan angka. Untuk jawaban teks, pilih jenis isian teks.", a)
				}
			}
			if t := strings.TrimSpace(f.Tolerance); t != "" {
				v, ok := parseNumber(t)
				if !ok || v < 0 {
					b.fields["tolerance"] = "Toleransi harus angka 0 atau lebih, mis. 0,5."
				}
				q.Key.Tolerance = v
			}
		}
	case KindMatch:
		n := trimTrailing(len(f.Lefts), func(i int) bool {
			return strings.TrimSpace(f.Lefts[i]) == "" && (i >= len(f.Rights) || strings.TrimSpace(f.Rights[i]) == "")
		})
		q.Key.Pairs = map[string]string{}
		seen := map[string]bool{}
		for i := 0; i < n; i++ {
			lf, rf := fmt.Sprintf("left_%d", i), fmt.Sprintf("right_%d", i)
			l, lEmpty := b.rich(lf, fmt.Sprintf("Pasangan %d kiri", i+1), f.Lefts[i])
			rsrc := ""
			if i < len(f.Rights) {
				rsrc = f.Rights[i]
			}
			rt, rEmpty := b.rich(rf, fmt.Sprintf("Pasangan %d kanan", i+1), rsrc)
			if (lEmpty || rEmpty) && b.fields[lf] == "" && b.fields[rf] == "" {
				b.fields[lf] = fmt.Sprintf("Lengkapi pasangan %d (kiri dan kanan).", i+1)
			}
			key := NormalizeSearch(PlainText(rt.Src))
			if seen[key] && !rEmpty {
				b.fields[rf] = fmt.Sprintf("Jawaban kanan pasangan %d sama dengan pasangan lain.", i+1)
			}
			seen[key] = true
			l.ID, rt.ID = fmt.Sprintf("l%d", i+1), fmt.Sprintf("r%d", i+1)
			q.Options.Left = append(q.Options.Left, l)
			q.Options.Right = append(q.Options.Right, rt)
			q.Key.Pairs[l.ID] = rt.ID
		}
		for i, d := range f.Distractors {
			if strings.TrimSpace(d) == "" {
				continue
			}
			c, _ := b.rich(fmt.Sprintf("distractor_%d", i), fmt.Sprintf("Pengecoh %d", i+1), d)
			c.ID = fmt.Sprintf("r%d", len(q.Options.Right)+1)
			q.Options.Right = append(q.Options.Right, c)
		}
		if strict && n < 2 {
			b.fields["pairs"] = "Menjodohkan butuh minimal 2 pasangan."
		}
	case KindEssay:
		rub, _ := b.rich("rubric", "Rubrik", f.Rubric)
		q.Key.RubricSrc, q.Key.RubricHTML = rub.Src, rub.HTML
	}
	if len(b.fields) > 0 {
		return q, nil, &FieldError{Fields: b.fields}
	}
	return q, uniq(b.assets), nil
}

func assetsOf(src string) []string {
	return assetIDRe.FindAllString(src, -1)
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
