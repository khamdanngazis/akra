package exam

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Format Excel soal (T131).
const (
	xlsxQuestions = "soal"
	xlsxStimuli   = "stimulus"
	xlsxGuide     = "petunjuk"
	xlsxRows      = 300 // baris berdropdown di template
)

// xlsxCols adalah kolom lembar soal, berurutan.
var xlsxCols = []string{"jenis", "soal", "opsi_a", "opsi_b", "opsi_c", "opsi_d", "opsi_e", "opsi_f",
	"kunci", "poin", "kesulitan", "level_kognitif", "tp", "tag", "pembahasan", "stimulus"}

func init() { parsers[SourceExcel] = ParseExcel }

// cellToSrc mengubah isi sel (teks, rumus \( \)) menjadi HTML sumber.
func cellToSrc(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" {
		return ""
	}
	var out []string
	for _, p := range strings.Split(s, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, "<p>"+html.EscapeString(p)+"</p>")
		}
	}
	return strings.Join(out, "")
}

var reBlockEnd = regexp.MustCompile(`</(p|li|tr)>|<br\s*/?>`)

// srcToCell mengubah HTML sumber menjadi isi sel: rumus menjadi \( \),
// paragraf menjadi baris baru, gambar menjadi [gambar] (Excel tanpa gambar).
func srcToCell(src string) string {
	s := reMathSpan.ReplaceAllStringFunc(src, func(m string) string {
		g := reMathSpan.FindStringSubmatch(m)
		tex := html.UnescapeString(g[2])
		if g[1] != "" {
			return `\[` + html.EscapeString(tex) + `\]`
		}
		return `\(` + html.EscapeString(tex) + `\)`
	})
	s = assetImgTag.ReplaceAllString(s, "[gambar]")
	s = reBlockEnd.ReplaceAllString(s, "\n")
	var lines []string
	for _, l := range strings.Split(textOnly(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

var assetImgTag = regexp.MustCompile(`<img [^>]*>`)

// textOnly membuang tag HTML dan mengembalikan teks (entity dibuka).
func textOnly(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return html.UnescapeString(b.String())
}

var xlsxKinds = map[string]Kind{
	"pg": KindMC, "pilihan ganda": KindMC, "pg_kompleks": KindMulti, "pg kompleks": KindMulti,
	"benar_salah": KindTF, "benar salah": KindTF, "isian": KindShort, "menjodohkan": KindMatch, "esai": KindEssay,
}

// ParseExcel membaca file Excel soal: lembar "soal" dan (opsional)
// "stimulus". Setiap butir diberi nomor baris Excel untuk laporan.
func ParseExcel(data []byte, _ ImageSaver) ([]ParsedItem, error) {
	if len(data) > MaxImportBytes || !bytes.HasPrefix(data, []byte("PK")) {
		return nil, ErrImportFile
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 100 << 20, UnzipXMLSizeLimit: 50 << 20})
	if err != nil {
		return nil, ErrImportFile
	}
	defer f.Close()
	sheet := xlsxQuestions
	if idx, _ := f.GetSheetIndex(sheet); idx < 0 {
		sheet = f.GetSheetList()[0]
	}
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) == 0 {
		return nil, ErrImportFile
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := col["soal"]; !ok {
		return nil, &FieldError{Fields: map[string]string{"file": "Kolom \"soal\" tidak ditemukan di baris pertama. Pakai template Excel AKRA."}}
	}
	get := func(r []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(r) {
			return ""
		}
		return strings.TrimSpace(r[i])
	}
	var items []ParsedItem
	stimIdx := map[string]int{}
	if idx, _ := f.GetSheetIndex(xlsxStimuli); idx >= 0 {
		srows, _ := f.GetRows(xlsxStimuli)
		for i, r := range srows {
			if i == 0 || len(r) == 0 {
				continue
			}
			cell := func(n int) string {
				if n < len(r) {
					return strings.TrimSpace(r[n])
				}
				return ""
			}
			code := strings.ToLower(cell(0))
			if code == "" {
				continue
			}
			title := cell(1)
			if title == "" {
				title = "Stimulus " + cell(0)
			}
			items = append(items, ParsedItem{Kind: "stimulus", Number: fmt.Sprintf("stimulus baris %d", i+1), StimIdx: -1,
				Stim: StimulusDraft{Title: title, Body: cellToSrc(cell(2))}, Origin: Snippet(title+": "+cell(2), 300)})
			stimIdx[code] = len(items) - 1
		}
	}
	n := 0
	for i, r := range rows[1:] {
		line := i + 2
		if strings.Join(r, "") == "" {
			continue
		}
		n++
		if n > MaxMoodleQuestions {
			return nil, ErrTooManyItems
		}
		it := ParsedItem{Kind: "soal", Number: fmt.Sprintf("baris %d", line), StimIdx: -1, TPCode: get(r, "tp"),
			Origin: Snippet(get(r, "soal"), 400)}
		fm := emptyForm("")
		kind, ok := xlsxKinds[strings.ToLower(get(r, "jenis"))]
		if !ok {
			if j := get(r, "jenis"); j != "" {
				it.Problems = append(it.Problems, fmt.Sprintf("baris %d: jenis \"%s\" tidak dikenal", line, j))
			}
			kind = KindMC
		}
		fm.Kind = string(kind)
		fm.Body = cellToSrc(get(r, "soal"))
		fm.Explanation = cellToSrc(get(r, "pembahasan"))
		fm.Points = get(r, "poin")
		if fm.Points == "" {
			fm.Points = "1"
		}
		fm.Difficulty = strings.ToLower(get(r, "kesulitan"))
		fm.CognitiveLevel = strings.ToUpper(get(r, "level_kognitif"))
		fm.Tags = get(r, "tag")
		var opts []string
		for _, c := range []string{"opsi_a", "opsi_b", "opsi_c", "opsi_d", "opsi_e", "opsi_f"} {
			opts = append(opts, get(r, c))
		}
		last := 0
		for j, o := range opts {
			if o != "" {
				last = j + 1
			}
		}
		opts = opts[:last]
		key := get(r, "kunci")
		switch kind {
		case KindMC, KindMulti:
			for j, o := range opts {
				fm.Choices[j] = cellToSrc(o)
			}
			pi := ParsedItem{Form: fm}
			applyLetterKey(&pi, key)
			fm = pi.Form
			if key != "" && len(fm.Correct) == 0 {
				it.Problems = append(it.Problems, fmt.Sprintf("baris %d: kunci \"%s\" bukan huruf opsi", line, key))
			}
		case KindTF:
			for j, o := range opts {
				if j < maxStatement {
					fm.Statements[j] = cellToSrc(o)
				}
			}
			for j, v := range strings.Split(key, ",") {
				v = strings.ToUpper(strings.TrimSpace(v))
				if j < maxStatement && (v == "B" || v == "S") {
					fm.Truth[j] = v
				}
			}
		case KindShort:
			fm.Accepted = strings.Join(strings.Split(key, "|"), "\n")
			if allNumbers(key) {
				fm.Input = "angka"
			}
		case KindMatch:
			p, d := 0, 0
			for _, o := range opts {
				l, rr, ok := strings.Cut(o, "=")
				l, rr = strings.TrimSpace(l), strings.TrimSpace(rr)
				switch {
				case !ok:
					it.Problems = append(it.Problems, fmt.Sprintf("baris %d: pasangan \"%s\" tidak berformat kiri = kanan", line, o))
				case l == "" && d < len(fm.Distractors):
					fm.Distractors[d] = cellToSrc(rr)
					d++
				case p < maxPairs:
					fm.Lefts[p], fm.Rights[p] = cellToSrc(l), cellToSrc(rr)
					p++
				}
			}
		case KindEssay:
			fm.Rubric = cellToSrc(key)
		}
		if code := strings.ToLower(get(r, "stimulus")); code != "" {
			if si, ok := stimIdx[code]; ok {
				it.StimIdx = si
			} else {
				it.Problems = append(it.Problems, fmt.Sprintf("baris %d: kode stimulus \"%s\" tidak ada di lembar stimulus", line, code))
			}
		}
		it.Form = fm
		items = append(items, it)
	}
	for i := range items {
		finalizeProblems(&items[i])
		if items[i].Kind == "soal" {
			for j, p := range items[i].Problems {
				if !strings.HasPrefix(p, "baris ") {
					items[i].Problems[j] = items[i].Number + ": " + p
				}
			}
		}
	}
	return items, nil
}

// xlsxRow mengubah soal menjadi baris Excel sesuai xlsxCols.
func xlsxRow(q Question, objCode, stimCode string) []any {
	row := make([]any, len(xlsxCols))
	set := func(name string, v any) {
		for i, c := range xlsxCols {
			if c == name {
				row[i] = v
			}
		}
	}
	set("jenis", string(q.Kind))
	set("soal", srcToCell(q.BodySrc))
	set("poin", q.Points)
	set("kesulitan", q.Difficulty)
	set("level_kognitif", q.CognitiveLevel)
	set("tp", objCode)
	set("tag", strings.Join(q.Tags, ", "))
	set("pembahasan", srcToCell(q.ExplanationSrc))
	set("stimulus", stimCode)
	opt := func(i int, v string) {
		if i < 6 {
			set("opsi_"+string(rune('a'+i)), v)
		}
	}
	switch q.Kind {
	case KindMC, KindMulti:
		var ks []string
		for i, c := range q.Options.Choices {
			opt(i, srcToCell(c.Src))
			for _, k := range q.Key.Correct {
				if k == c.ID {
					ks = append(ks, letter(i))
				}
			}
		}
		set("kunci", strings.Join(ks, ","))
	case KindTF:
		var ks []string
		for i, s := range q.Options.Statements {
			opt(i, srcToCell(s.Src))
			ks = append(ks, map[bool]string{true: "B", false: "S"}[q.Key.Truth[s.ID]])
		}
		set("kunci", strings.Join(ks, ","))
	case KindShort:
		set("kunci", strings.Join(q.Key.Accepted, " | "))
	case KindMatch:
		right := map[string]Choice{}
		for _, r := range q.Options.Right {
			right[r.ID] = r
		}
		used := map[string]bool{}
		i := 0
		for _, l := range q.Options.Left {
			r := right[q.Key.Pairs[l.ID]]
			used[r.ID] = true
			opt(i, srcToCell(l.Src)+" = "+srcToCell(r.Src))
			i++
		}
		for _, r := range q.Options.Right {
			if !used[r.ID] {
				opt(i, "= "+srcToCell(r.Src))
				i++
			}
		}
	case KindEssay:
		set("kunci", srcToCell(q.Key.RubricSrc))
	}
	return row
}

// buildWorkbook menyusun file Excel soal: lembar soal (dengan dropdown),
// stimulus, dan petunjuk. qs kosong = template dengan contoh.
func buildWorkbook(qs []Question, objCodes map[string]string, stims []Stimulus) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", xlsxQuestions); err != nil {
		return nil, err
	}
	header := make([]any, len(xlsxCols))
	for i, c := range xlsxCols {
		header[i] = c
	}
	if err := f.SetSheetRow(xlsxQuestions, "A1", &header); err != nil {
		return nil, err
	}
	stimCode := map[string]string{}
	if _, err := f.NewSheet(xlsxStimuli); err != nil {
		return nil, err
	}
	if err := f.SetSheetRow(xlsxStimuli, "A1", &[]any{"kode", "judul", "teks"}); err != nil {
		return nil, err
	}
	for i, s := range stims {
		code := "S" + strconv.Itoa(i+1)
		stimCode[s.ID] = code
		if err := f.SetSheetRow(xlsxStimuli, fmt.Sprintf("A%d", i+2), &[]any{code, s.Title, srcToCell(s.BodySrc)}); err != nil {
			return nil, err
		}
	}
	for i, q := range qs {
		row := xlsxRow(q, objCodes[q.ObjectiveID], stimCode[q.StimulusID])
		if err := f.SetSheetRow(xlsxQuestions, fmt.Sprintf("A%d", i+2), &row); err != nil {
			return nil, err
		}
	}
	if len(qs) == 0 {
		examples := [][]any{
			{"pg", `Hasil dari \(\frac{3}{4}+\frac{1}{2}\) adalah …`, `\(\frac{5}{4}\)`, "1", "2", "", "", "", "A", 1, "mudah", "C2", "1.1", "pecahan", "Samakan penyebut.", ""},
			{"pg_kompleks", "Pilih semua bilangan prima.", "2", "4", "5", "9", "", "", "A,C", 2, "sedang", "C2", "", "", "", ""},
			{"benar_salah", "Tentukan benar atau salah.", "Air mendidih 100 °C", "Matahari mengelilingi Bumi", "", "", "", "", "B,S", 2, "", "", "", "", "", ""},
			{"isian", "12 × 4 = …", "", "", "", "", "", "", "48", 1, "mudah", "C3", "", "", "", ""},
			{"menjodohkan", "Jodohkan provinsi dan ibu kotanya.", "Jawa Barat = Bandung", "Jawa Timur = Surabaya", "= Medan", "", "", "", "", 3, "", "", "", "", "", ""},
			{"esai", "Jelaskan proses fotosintesis.", "", "", "", "", "", "", "Menyebut cahaya (3), klorofil (3), hasil (4)", 10, "sulit", "C4", "", "", "", ""},
			{"pg", "Apa yang ditanam Ani?", "Pohon mangga", "Pohon jambu", "", "", "", "", "A", 1, "mudah", "C1", "", "", "", "S1"},
		}
		for i, ex := range examples {
			if err := f.SetSheetRow(xlsxQuestions, fmt.Sprintf("A%d", i+2), &ex); err != nil {
				return nil, err
			}
		}
		if err := f.SetSheetRow(xlsxStimuli, "A2", &[]any{"S1", "Teks untuk soal 7", "Ani menanam pohon mangga di halaman rumahnya."}); err != nil {
			return nil, err
		}
	}
	for _, v := range []struct {
		col   string
		list  []string
		title string
	}{
		{"A", []string{"pg", "pg_kompleks", "benar_salah", "isian", "menjodohkan", "esai"}, "Jenis soal"},
		{"K", Difficulties, "Kesulitan"},
		{"L", CognitiveLevels, "Level kognitif"},
	} {
		dv := excelize.NewDataValidation(true)
		dv.Sqref = fmt.Sprintf("%s2:%s%d", v.col, v.col, max(xlsxRows, len(qs)+50))
		if err := dv.SetDropList(v.list); err != nil {
			return nil, err
		}
		dv.SetError(excelize.DataValidationErrorStyleStop, v.title, "Pilih dari daftar.")
		if err := f.AddDataValidation(xlsxQuestions, dv); err != nil {
			return nil, err
		}
	}
	_ = f.SetColWidth(xlsxQuestions, "B", "B", 50)
	_ = f.SetColWidth(xlsxQuestions, "C", "H", 18)
	_ = f.SetColWidth(xlsxStimuli, "C", "C", 80)
	if _, err := f.NewSheet(xlsxGuide); err != nil {
		return nil, err
	}
	guide := []string{
		"PETUNJUK IMPOR SOAL DARI EXCEL",
		"Satu baris = satu soal di lembar \"soal\". Baris pertama adalah nama kolom; jangan diubah.",
		"jenis: pg, pg_kompleks, benar_salah, isian, menjodohkan, atau esai.",
		"pg / pg_kompleks: isi opsi_a sampai opsi_f; kunci huruf opsi, mis. B atau A,C.",
		"benar_salah: pernyataan di opsi_a dst.; kunci B atau S per pernyataan, mis. B,S,B.",
		"isian: kunci berisi jawaban diterima, pisahkan dengan |, mis. 48 | empat puluh delapan.",
		"menjodohkan: opsi berisi kiri = kanan; pengecoh ditulis = kanan (kiri kosong).",
		"esai: kunci berisi rubrik penilaian.",
		"Rumus: tulis LaTeX di antara \\( dan \\), mis. \\(\\frac{1}{2}\\).",
		"Gambar tidak didukung lewat Excel. Tambahkan gambar di editor setelah impor, atau pakai impor Word.",
		"tp: kode TP yang sudah ada di AKRA, mis. 3.2. tag: pisahkan dengan koma.",
		"stimulus: kode dari lembar \"stimulus\" (kolom kode, judul, teks). Soal dengan kode sama dikelompokkan sesuai urutan baris.",
	}
	for i, g := range guide {
		if err := f.SetCellValue(xlsxGuide, fmt.Sprintf("A%d", i+1), g); err != nil {
			return nil, err
		}
	}
	_ = f.SetColWidth(xlsxGuide, "A", "A", 120)
	f.SetActiveSheet(0)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExcelTemplate menghasilkan template Excel soal berisi contoh.
func ExcelTemplate() ([]byte, error) { return buildWorkbook(nil, nil, nil) }
