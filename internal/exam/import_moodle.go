package exam

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// MaxMoodleQuestions membatasi jumlah soal satu file Moodle/GIFT (T130).
const MaxMoodleQuestions = 2000

func init() {
	parsers[SourceMoodle] = ParseMoodleXML
	parsers[SourceGIFT] = ParseGIFT
}

type mText struct {
	Text  string  `xml:"text"`
	Files []mFile `xml:"file"`
}

type mFile struct {
	Name     string `xml:"name,attr"`
	Encoding string `xml:"encoding,attr"`
	Data     string `xml:",chardata"`
}

type mAnswer struct {
	Fraction  string  `xml:"fraction,attr"`
	Text      string  `xml:"text"`
	Files     []mFile `xml:"file"`
	Tolerance string  `xml:"tolerance"`
}

type mSubQ struct {
	Text   string  `xml:"text"`
	Files  []mFile `xml:"file"`
	Answer mText   `xml:"answer"`
}

type mQuestion struct {
	Type            string    `xml:"type,attr"`
	Name            mText     `xml:"name"`
	Category        mText     `xml:"category"`
	QuestionText    mText     `xml:"questiontext"`
	GeneralFeedback mText     `xml:"generalfeedback"`
	DefaultGrade    string    `xml:"defaultgrade"`
	Single          string    `xml:"single"`
	Answers         []mAnswer `xml:"answer"`
	Subs            []mSubQ   `xml:"subquestion"`
	GraderInfo      mText     `xml:"graderinfo"`
}

type mQuiz struct {
	Questions []mQuestion `xml:"question"`
}

var (
	rePluginImg = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc\s*=\s*"@@PLUGINFILE@@/([^"]+)"[^>]*>`)
	reImgAlt    = regexp.MustCompile(`(?i)\balt\s*=\s*"([^"]*)"`)
	reDollarTeX = regexp.MustCompile(`\$\$(.+?)\$\$`)
)

// moodleHTML mengubah HTML Moodle: gambar @@PLUGINFILE@@ disimpan sebagai
// aset, rumus $$...$$ menjadi \(...\) agar dirender ulang AKRA.
func moodleHTML(h string, files []mFile, save ImageSaver, problems *[]string) string {
	byName := map[string]mFile{}
	for _, f := range files {
		byName[f.Name] = f
	}
	h = rePluginImg.ReplaceAllStringFunc(h, func(tag string) string {
		name := rePluginImg.FindStringSubmatch(tag)[1]
		if un, err := urlUnescape(name); err == nil {
			name = un
		}
		alt := ""
		if m := reImgAlt.FindStringSubmatch(tag); m != nil {
			alt = html.UnescapeString(m[1])
		}
		if strings.TrimSpace(alt) == "" {
			alt = "Gambar soal"
		}
		f, ok := byName[name]
		if !ok || f.Encoding != "base64" {
			*problems = append(*problems, "gambar "+name+" tidak ada di file")
			return ""
		}
		data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(f.Data), ""))
		if err != nil {
			*problems = append(*problems, "gambar "+name+" rusak")
			return ""
		}
		id, err := save(data, name)
		if err != nil {
			*problems = append(*problems, "gambar "+name+" tidak bisa dibaca")
			return ""
		}
		return imgTag(id, alt)
	})
	return reDollarTeX.ReplaceAllString(h, `\($1\)`)
}

func urlUnescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(v))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func fraction(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// ParseMoodleXML membaca ekspor Moodle XML (T130).
func ParseMoodleXML(data []byte, save ImageSaver) ([]ParsedItem, error) {
	if len(data) > MaxImportBytes {
		return nil, ErrImportFile
	}
	if bytes.Contains(data[:min(len(data), 2048)], []byte("<!DOCTYPE")) {
		return nil, ErrImportFile
	}
	var quiz mQuiz
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	if err := d.Decode(&quiz); err != nil {
		return nil, ErrImportFile
	}
	var items []ParsedItem
	category := ""
	stim := -1
	n := 0
	for _, q := range quiz.Questions {
		if q.Type == "category" {
			parts := strings.Split(strings.Trim(q.Category.Text, "/ "), "/")
			category = strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
			category = strings.TrimPrefix(category, "$course$")
			stim = -1
			continue
		}
		n++
		if n > MaxMoodleQuestions {
			return nil, ErrTooManyItems
		}
		var problems []string
		body := moodleHTML(q.QuestionText.Text, q.QuestionText.Files, save, &problems)
		it := ParsedItem{Kind: "soal", Number: strconv.Itoa(n), StimIdx: stim, Origin: Snippet(strings.TrimSpace(q.Name.Text+": "+PlainText(body)), 400)}
		if q.Type == "description" {
			title := strings.TrimSpace(q.Name.Text)
			if title == "" {
				title = Snippet(PlainText(body), 80)
			}
			items = append(items, ParsedItem{Kind: "stimulus", Number: strconv.Itoa(n), StimIdx: -1, Stim: StimulusDraft{Title: title, Body: body},
				Problems: problems, Origin: it.Origin})
			stim = len(items) - 1
			continue
		}
		f := emptyForm("")
		f.Body = body
		f.Explanation = moodleHTML(q.GeneralFeedback.Text, q.GeneralFeedback.Files, save, &problems)
		if g := fraction(q.DefaultGrade); g >= 1 && g <= 100 {
			f.Points = strconv.Itoa(int(g))
		}
		f.Tags = category
		switch q.Type {
		case "multichoice":
			multi := strings.TrimSpace(q.Single) == "false" || strings.TrimSpace(q.Single) == "0"
			f.Kind = string(KindMC)
			if multi {
				f.Kind = string(KindMulti)
			}
			for i, a := range q.Answers {
				if i >= maxChoices {
					problems = append(problems, "lebih dari 6 opsi; opsi selebihnya dibuang")
					break
				}
				f.Choices[i] = moodleHTML(a.Text, a.Files, save, &problems)
				if fr := fraction(a.Fraction); (multi && fr > 0) || (!multi && fr >= 100) {
					f.Correct = append(f.Correct, i)
				}
			}
		case "truefalse":
			f.Kind = string(KindTF)
			f.Body = "<p>Tentukan apakah pernyataan berikut benar atau salah.</p>"
			f.Statements[0] = body
			for _, a := range q.Answers {
				if fraction(a.Fraction) >= 100 {
					f.Truth[0] = map[bool]string{true: "B", false: "S"}[strings.EqualFold(strings.TrimSpace(PlainText(a.Text)), "true")]
				}
			}
		case "shortanswer", "numerical":
			f.Kind = string(KindShort)
			var acc []string
			for _, a := range q.Answers {
				if fraction(a.Fraction) >= 100 {
					acc = append(acc, strings.TrimSpace(html.UnescapeString(PlainText(a.Text))))
					if q.Type == "numerical" {
						f.Tolerance = strings.TrimSpace(a.Tolerance)
					}
				}
			}
			f.Accepted = strings.Join(acc, "\n")
			if q.Type == "numerical" {
				f.Input = "angka"
			}
		case "matching":
			f.Kind = string(KindMatch)
			p, dIdx := 0, 0
			for _, s := range q.Subs {
				right := html.EscapeString(strings.TrimSpace(s.Answer.Text))
				if strings.TrimSpace(PlainText(s.Text)) == "" {
					if dIdx < len(f.Distractors) {
						f.Distractors[dIdx] = right
						dIdx++
					}
					continue
				}
				if p < maxPairs {
					f.Lefts[p] = moodleHTML(s.Text, s.Files, save, &problems)
					f.Rights[p] = right
					p++
				}
			}
		case "essay":
			f.Kind = string(KindEssay)
			f.Rubric = moodleHTML(q.GraderInfo.Text, q.GraderInfo.Files, save, &problems)
		default:
			it.Skip = true
			it.Problems = append(it.Problems, "jenis Moodle \""+q.Type+"\" belum didukung, dilewati")
			f.Kind = string(KindEssay)
		}
		it.Form, it.Problems = f, append(it.Problems, problems...)
		items = append(items, it)
	}
	for i := range items {
		if !items[i].Skip {
			finalizeProblems(&items[i])
		}
	}
	return items, nil
}

// --- GIFT ---

var (
	reGiftCategory = regexp.MustCompile(`(?m)^\s*\$CATEGORY:\s*(.*)$`)
	reGiftTitle    = regexp.MustCompile(`^::((?:\\.|[^:])*)::`)
	reGiftFormat   = regexp.MustCompile(`^\[(html|moodle|plain|markdown)\]`)
)

// giftUnescape menghapus escape GIFT (\= \~ \# \{ \} \: \n).
func giftUnescape(s string) string {
	r := strings.NewReplacer(`\=`, "=", `\~`, "~", `\#`, "#", `\{`, "{", `\}`, "}", `\:`, ":", `\n`, "\n")
	return strings.TrimSpace(r.Replace(s))
}

// giftSplit memecah teks pada sep yang tidak di-escape.
func giftIndex(s string, set string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if strings.IndexByte(set, s[i]) >= 0 {
			return i
		}
	}
	return -1
}

// giftText mengubah teks GIFT menjadi HTML sumber AKRA.
func giftText(s string, isHTML bool) string {
	s = giftUnescape(s)
	s = reDollarTeX.ReplaceAllString(s, `\($1\)`)
	if isHTML {
		return s
	}
	return "<p>" + strings.ReplaceAll(html.EscapeString(s), "\n", "<br>") + "</p>"
}

// ParseGIFT membaca format GIFT Moodle (T130).
func ParseGIFT(data []byte, save ImageSaver) ([]ParsedItem, error) {
	if len(data) > MaxImportBytes {
		return nil, ErrImportFile
	}
	src := strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n")
	var lines []string
	for _, l := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "//") {
			continue
		}
		lines = append(lines, l)
	}
	var items []ParsedItem
	category, stim, n := "", -1, 0
	for _, block := range strings.Split(strings.Join(lines, "\n"), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if m := reGiftCategory.FindStringSubmatch(block); m != nil {
			parts := strings.Split(strings.Trim(m[1], "/ "), "/")
			category = strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
			stim = -1
			block = strings.TrimSpace(reGiftCategory.ReplaceAllString(block, ""))
			if block == "" {
				continue
			}
		}
		n++
		if n > MaxMoodleQuestions {
			return nil, ErrTooManyItems
		}
		title := ""
		if m := reGiftTitle.FindStringSubmatch(block); m != nil {
			title, block = giftUnescape(m[1]), strings.TrimSpace(block[len(m[0]):])
		}
		isHTML := false
		if m := reGiftFormat.FindStringSubmatch(block); m != nil {
			isHTML, block = m[1] == "html", strings.TrimSpace(block[len(m[0]):])
		}
		open := giftIndex(block, "{")
		origin := Snippet(strings.TrimSpace(title+": "+block), 400)
		if open < 0 {
			// Tanpa jawaban: deskripsi (calon stimulus).
			if title == "" {
				title = Snippet(giftUnescape(block), 80)
			}
			items = append(items, ParsedItem{Kind: "stimulus", Number: strconv.Itoa(n), StimIdx: -1,
				Stim: StimulusDraft{Title: title, Body: giftText(block, isHTML)}, Origin: origin})
			stim = len(items) - 1
			continue
		}
		closeRel := giftIndex(block[open:], "}")
		if closeRel < 0 {
			items = append(items, ParsedItem{Kind: "soal", Number: strconv.Itoa(n), StimIdx: stim, Skip: true, Origin: origin,
				Problems: []string{"kurung kurawal jawaban tidak ditutup"}, Form: emptyForm("esai")})
			continue
		}
		ans := block[open+1 : open+closeRel]
		text := strings.TrimSpace(block[:open] + " " + block[open+closeRel+1:])
		it := ParsedItem{Kind: "soal", Number: strconv.Itoa(n), StimIdx: stim, Origin: origin}
		f := emptyForm("")
		f.Body, f.Tags = giftText(text, isHTML), category
		a := strings.TrimSpace(ans)
		switch {
		case a == "":
			f.Kind = string(KindEssay)
		case a == "T" || a == "TRUE" || a == "F" || a == "FALSE":
			f.Kind = string(KindTF)
			f.Statements[0] = f.Body
			f.Body = "<p>Tentukan apakah pernyataan berikut benar atau salah.</p>"
			f.Truth[0] = map[bool]string{true: "B", false: "S"}[a == "T" || a == "TRUE"]
		case strings.HasPrefix(a, "#"):
			f.Kind, f.Input = string(KindShort), "angka"
			v := strings.TrimSpace(strings.TrimPrefix(a, "#"))
			if fb := giftIndex(v, "#"); fb >= 0 {
				v = v[:fb]
			}
			val, tol, _ := strings.Cut(v, ":")
			f.Accepted, f.Tolerance = strings.TrimSpace(val), strings.TrimSpace(tol)
		default:
			giftChoices(a, &f, &it)
		}
		it.Form = f
		items = append(items, it)
	}
	for i := range items {
		if !items[i].Skip {
			finalizeProblems(&items[i])
		}
	}
	return items, nil
}

// giftChoices membaca jawaban =benar ~salah (PG, isian, atau menjodohkan).
func giftChoices(a string, f *QuestionForm, it *ParsedItem) {
	type opt struct {
		right bool
		w     float64
		text  string
	}
	var opts []opt
	for len(a) > 0 {
		a = strings.TrimSpace(a)
		if a == "" {
			break
		}
		mark := a[0]
		if mark != '=' && mark != '~' {
			it.Problems = append(it.Problems, "jawaban GIFT tidak dikenali")
			it.Skip = true
			return
		}
		next := giftIndex(a[1:], "=~")
		var t string
		if next < 0 {
			t, a = a[1:], ""
		} else {
			t, a = a[1:next+1], a[next+1:]
		}
		if fb := giftIndex(t, "#"); fb >= 0 {
			t = t[:fb] // umpan balik
		}
		o := opt{right: mark == '=', text: strings.TrimSpace(t)}
		if strings.HasPrefix(o.text, "%") {
			if end := strings.Index(o.text[1:], "%"); end >= 0 {
				o.w, _ = strconv.ParseFloat(o.text[1:end+1], 64)
				o.text = strings.TrimSpace(o.text[end+2:])
				o.right = o.w > 0
			}
		}
		opts = append(opts, o)
	}
	allRight := true
	match := false
	for _, o := range opts {
		allRight = allRight && o.right
		match = match || strings.Contains(o.text, "->")
	}
	switch {
	case match:
		f.Kind = string(KindMatch)
		p := 0
		for _, o := range opts {
			l, r, ok := strings.Cut(o.text, "->")
			if !ok || p >= maxPairs {
				continue
			}
			l = giftUnescape(l)
			if l == "" {
				continue
			}
			f.Lefts[p], f.Rights[p] = html.EscapeString(l), html.EscapeString(giftUnescape(r))
			p++
		}
	case allRight:
		f.Kind = string(KindShort)
		var acc []string
		for _, o := range opts {
			acc = append(acc, giftUnescape(o.text))
		}
		f.Accepted = strings.Join(acc, "\n")
	default:
		f.Kind = string(KindMC)
		rights := 0
		for i, o := range opts {
			if i >= maxChoices {
				it.Problems = append(it.Problems, "lebih dari 6 opsi; opsi selebihnya dibuang")
				break
			}
			f.Choices[i] = giftText(o.text, false)
			if o.right {
				f.Correct = append(f.Correct, i)
				rights++
			}
		}
		if rights > 1 {
			f.Kind = string(KindMulti)
		}
	}
}

// --- Ekspor Moodle XML ---

// reMathSpan menemukan rumus di sumber AKRA untuk diekspor sebagai \(...\).
var reMathSpan = regexp.MustCompile(`<span class="math( math-display)?" data-tex="([^"]*)"></span>`)

func cdata(s string) string {
	return "<![CDATA[" + strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>") + "]]>"
}

// exportImage dipanggil untuk setiap gambar; mengembalikan nama file dan
// isinya.
type exportImage func(assetID string) (name string, data []byte, err error)

// moodleOut mengubah sumber AKRA menjadi HTML Moodle beserta file gambar.
func moodleOut(src string, img exportImage) (string, string) {
	var files strings.Builder
	out := reMathSpan.ReplaceAllStringFunc(src, func(m string) string {
		g := reMathSpan.FindStringSubmatch(m)
		tex := html.UnescapeString(g[2])
		if g[1] != "" {
			return `\[` + tex + `\]`
		}
		return `\(` + tex + `\)`
	})
	out = assetImgSrc.ReplaceAllStringFunc(out, func(m string) string {
		id := assetImgSrc.FindStringSubmatch(m)[1]
		name, data, err := img(id)
		if err != nil {
			return m
		}
		files.WriteString(`<file name="` + html.EscapeString(name) + `" path="/" encoding="base64">` + base64.StdEncoding.EncodeToString(data) + `</file>`)
		return strings.Replace(m, `data-asset="`+id+`"`, `src="@@PLUGINFILE@@/`+html.EscapeString(name)+`"`, 1)
	})
	return out, files.String()
}

var assetImgSrc = regexp.MustCompile(`<img data-asset="([0-9a-f-]{36})"`)

func moodleTextEl(tag, src string, img exportImage) string {
	h, files := moodleOut(src, img)
	return `<` + tag + ` format="html"><text>` + cdata(h) + `</text>` + files + `</` + tag + `>`
}

// ExportMoodleXML menulis soal (dan stimulus sebagai "description" tepat
// sebelum soal-soalnya) dalam format Moodle XML.
func ExportMoodleXML(category string, stimuli map[string]Stimulus, qs []Question, img exportImage) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<quiz>\n")
	b.WriteString(`<question type="category"><category><text>$course$/` + html.EscapeString(category) + `</text></category></question>` + "\n")
	done := map[string]bool{}
	for i, q := range qs {
		if q.StimulusID != "" && !done[q.StimulusID] {
			if s, ok := stimuli[q.StimulusID]; ok {
				done[q.StimulusID] = true
				b.WriteString(`<question type="description"><name><text>` + html.EscapeString(s.Title) + `</text></name>` +
					moodleTextEl("questiontext", s.BodySrc, img) + "</question>\n")
			}
		}
		name := fmt.Sprintf("%d. %s", i+1, Snippet(PlainText(q.BodySrc), 60))
		head := func(t string) {
			b.WriteString(`<question type="` + t + `"><name><text>` + html.EscapeString(name) + `</text></name>` +
				moodleTextEl("questiontext", q.BodySrc, img) + moodleTextEl("generalfeedback", q.ExplanationSrc, img) +
				`<defaultgrade>` + strconv.Itoa(q.Points) + `</defaultgrade>`)
		}
		switch q.Kind {
		case KindMC, KindMulti:
			head("multichoice")
			single := q.Kind == KindMC
			b.WriteString(`<single>` + strconv.FormatBool(single) + `</single><shuffleanswers>1</shuffleanswers><answernumbering>ABCD</answernumbering>`)
			nRight := len(q.Key.Correct)
			for _, c := range q.Options.Choices {
				fr := "0"
				for _, k := range q.Key.Correct {
					if k == c.ID {
						if single {
							fr = "100"
						} else {
							fr = strconv.FormatFloat(100/float64(max(nRight, 1)), 'f', 5, 64)
						}
					}
				}
				h, files := moodleOut(c.Src, img)
				b.WriteString(`<answer fraction="` + fr + `" format="html"><text>` + cdata(h) + `</text>` + files + `</answer>`)
			}
		case KindTF:
			// Satu soal benar/salah Moodle per pernyataan.
			for si, s := range q.Options.Statements {
				b.WriteString(`<question type="truefalse"><name><text>` + html.EscapeString(fmt.Sprintf("%s (%d)", name, si+1)) + `</text></name>` +
					moodleTextEl("questiontext", s.Src, img) + moodleTextEl("generalfeedback", q.ExplanationSrc, img) + `<defaultgrade>1</defaultgrade>`)
				t := q.Key.Truth[s.ID]
				b.WriteString(`<answer fraction="` + map[bool]string{true: "100", false: "0"}[t] + `"><text>true</text></answer>`)
				b.WriteString(`<answer fraction="` + map[bool]string{true: "0", false: "100"}[t] + `"><text>false</text></answer></question>` + "\n")
			}
			continue
		case KindShort:
			if q.Options.Input == "angka" {
				head("numerical")
				for _, a := range q.Key.Accepted {
					v, _ := parseNumber(a)
					b.WriteString(`<answer fraction="100"><text>` + strconv.FormatFloat(v, 'f', -1, 64) + `</text><tolerance>` +
						strconv.FormatFloat(q.Key.Tolerance, 'f', -1, 64) + `</tolerance></answer>`)
				}
			} else {
				head("shortanswer")
				b.WriteString(`<usecase>0</usecase>`)
				for _, a := range q.Key.Accepted {
					b.WriteString(`<answer fraction="100"><text>` + html.EscapeString(a) + `</text></answer>`)
				}
			}
		case KindMatch:
			head("matching")
			right := map[string]Choice{}
			for _, r := range q.Options.Right {
				right[r.ID] = r
			}
			used := map[string]bool{}
			for _, l := range q.Options.Left {
				r := right[q.Key.Pairs[l.ID]]
				used[r.ID] = true
				h, files := moodleOut(l.Src, img)
				b.WriteString(`<subquestion format="html"><text>` + cdata(h) + `</text>` + files + `<answer><text>` + html.EscapeString(PlainText(r.Src)) + `</text></answer></subquestion>`)
			}
			for _, r := range q.Options.Right {
				if !used[r.ID] {
					b.WriteString(`<subquestion format="html"><text></text><answer><text>` + html.EscapeString(PlainText(r.Src)) + `</text></answer></subquestion>`)
				}
			}
		case KindEssay:
			head("essay")
			b.WriteString(`<responseformat>editor</responseformat>` + moodleTextEl("graderinfo", q.Key.RubricSrc, img))
		}
		b.WriteString("</question>\n")
	}
	b.WriteString("</quiz>\n")
	return []byte(b.String())
}
