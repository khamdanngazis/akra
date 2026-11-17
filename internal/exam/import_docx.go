package exam

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Batas dokumen Word (T129).
const (
	MaxImportBytes     = 20 << 20
	MaxImportQuestions = 500
	maxZipEntries      = 3000
	maxZipTotal        = 200 << 20
	maxXMLPart         = 60 << 20
)

// Error impor yang ditampilkan ke guru.
var (
	ErrNotDocx      = errors.New("exam: file is not a .docx document")
	ErrDocxMacro    = errors.New("exam: document contains macros")
	ErrTooManyItems = errors.New("exam: too many questions")
)

// ImageSaver menyimpan gambar dari file impor dan mengembalikan ID aset.
type ImageSaver func(data []byte, name string) (string, error)

// StimulusDraft adalah stimulus hasil baca file impor.
type StimulusDraft struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Numbered bool   `json:"numbered"`
}

// ParsedItem adalah satu butir hasil baca file impor (Word, Moodle,
// Excel): soal atau stimulus.
type ParsedItem struct {
	Kind     string // "soal" atau "stimulus"
	Number   string // nomor/baris asal, untuk laporan
	Form     QuestionForm
	Stim     StimulusDraft
	StimIdx  int // indeks stimulus induk di daftar, -1 bila tidak ada
	Problems []string
	Origin   string // cuplikan teks asli
	Skip     bool   // tidak bisa diimpor (✗)
	// TPCode adalah kode TP dari file, dicocokkan ke TP bank saat diproses.
	TPCode string
}

// zipFiles membuka .docx dengan batas jumlah dan ukuran isi (bom zip).
func zipFiles(data []byte) (map[string]*zip.File, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotDocx
	}
	if len(zr.File) > maxZipEntries {
		return nil, ErrNotDocx
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
		if total > maxZipTotal {
			return nil, ErrNotDocx
		}
		files[f.Name] = f
	}
	return files, nil
}

func readZip(f *zip.File, limit int64) ([]byte, error) {
	if f == nil {
		return nil, ErrNotDocx
	}
	rc, err := f.Open()
	if err != nil {
		return nil, ErrNotDocx
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, ErrNotDocx
	}
	return b, nil
}

// readXMLPart mem-parse bagian XML. Decoder Go tidak memuat entitas luar
// (aman dari XXE); DOCTYPE ditolak sebagai pengaman tambahan.
func readXMLPart(f *zip.File) (*xnode, error) {
	b, err := readZip(f, maxXMLPart)
	if err != nil {
		return nil, err
	}
	if bytes.Contains(b[:min(len(b), 1024)], []byte("<!DOCTYPE")) {
		return nil, ErrNotDocx
	}
	var n xnode
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	if err := d.Decode(&n); err != nil {
		return nil, ErrNotDocx
	}
	return &n, nil
}

// numbering membaca penomoran otomatis Word (daftar bernomor).
type numbering struct {
	numToAbs map[string]string
	fmt      map[string]map[int][2]string // abstractNumId → ilvl → {numFmt, lvlText}
	counters map[string][]int             // numId → counter per level
}

func readNumbering(files map[string]*zip.File) *numbering {
	nb := &numbering{numToAbs: map[string]string{}, fmt: map[string]map[int][2]string{}, counters: map[string][]int{}}
	n, err := readXMLPart(files["word/numbering.xml"])
	if err != nil {
		return nb
	}
	for _, an := range n.children("abstractNum") {
		id := an.attr("abstractNumId")
		nb.fmt[id] = map[int][2]string{}
		for _, lvl := range an.children("lvl") {
			il, _ := strconv.Atoi(lvl.attr("ilvl"))
			f, t := "", ""
			if c := lvl.child("numFmt"); c != nil {
				f = c.attr("val")
			}
			if c := lvl.child("lvlText"); c != nil {
				t = c.attr("val")
			}
			nb.fmt[id][il] = [2]string{f, t}
		}
	}
	for _, num := range n.children("num") {
		if a := num.child("abstractNumId"); a != nil {
			nb.numToAbs[num.attr("numId")] = a.attr("val")
		}
	}
	return nb
}

// label mengembalikan teks nomor otomatis paragraf, mis. "3." atau "B.".
func (nb *numbering) label(numID string, ilvl int) (string, string) {
	f, ok := nb.fmt[nb.numToAbs[numID]][ilvl]
	if !ok || ilvl < 0 || ilvl > 8 {
		return "", ""
	}
	c := nb.counters[numID]
	if len(c) < 9 {
		c = make([]int, 9)
	}
	c[ilvl]++
	for i := ilvl + 1; i < len(c); i++ {
		c[i] = 0
	}
	nb.counters[numID] = c
	var v string
	switch f[0] {
	case "decimal":
		v = strconv.Itoa(c[ilvl])
	case "upperLetter":
		v = string(rune('A' + (c[ilvl]-1)%26))
	case "lowerLetter":
		v = string(rune('a' + (c[ilvl]-1)%26))
	default:
		return "", f[0]
	}
	text := f[1]
	if text == "" {
		text = "%" + strconv.Itoa(ilvl+1) + "."
	}
	return strings.ReplaceAll(text, "%"+strconv.Itoa(ilvl+1), v), f[0]
}

// dblock adalah satu paragraf atau tabel hasil baca.
type dblock struct {
	html     string // HTML inline (paragraf) atau tabel lengkap
	text     string // teks polos untuk mengenali penanda
	label    string // nomor otomatis Word
	rtl      bool
	table    bool
	cells    []string // teks sel tabel (untuk tabel kunci)
	images   int
	problems []string
}

type docxReader struct {
	files  map[string]*zip.File
	rels   map[string]string
	nb     *numbering
	save   ImageSaver
	assets map[string]string // path media → ID aset (gambar dipakai ulang)
}

func (dr *docxReader) image(target, alt string) (string, error) {
	p := path.Clean(path.Join("word", target))
	if id, ok := dr.assets[p]; ok {
		return id, nil
	}
	data, err := readZip(dr.files[p], MaxAssetBytes*4)
	if err != nil {
		return "", err
	}
	id, err := dr.save(data, path.Base(p))
	if err != nil {
		return "", err
	}
	dr.assets[p] = id
	return id, nil
}

func imgTag(id, alt string) string {
	return `<img data-asset="` + id + `" alt="` + html.EscapeString(alt) + `">`
}

// para membaca satu paragraf menjadi HTML inline dan teks polos.
func (dr *docxReader) para(p *xnode) dblock {
	var b dblock
	var hb, tb strings.Builder
	if ppr := p.child("pPr"); ppr != nil {
		if ppr.child("bidi") != nil {
			b.rtl = true
		}
		if np := ppr.child("numPr"); np != nil && np.child("numId") != nil {
			il := 0
			if l := np.child("ilvl"); l != nil {
				il, _ = strconv.Atoi(l.attr("val"))
			}
			b.label, _ = dr.nb.label(np.child("numId").attr("val"), il)
		}
	}
	var visit func(n *xnode)
	visit = func(n *xnode) {
		for i := range n.Nodes {
			c := &n.Nodes[i]
			switch c.XMLName.Local {
			case "r":
				dr.run(c, &b, &hb, &tb)
			case "oMathPara", "hyperlink", "smartTag", "ins", "fldSimple", "sdt", "sdtContent":
				visit(c)
			case "oMath":
				tex, err := ommlToLaTeX(c)
				if err != nil {
					b.problems = append(b.problems, "rumus Equation tidak dikenali ("+err.Error()+"), ketik ulang di editor")
					hb.WriteString("[rumus]")
					tb.WriteString("[rumus]")
					continue
				}
				hb.WriteString(`<span class="math" data-tex="` + html.EscapeString(tex) + `"></span>`)
				tb.WriteString(`\(` + tex + `\)`)
			}
		}
	}
	visit(p)
	b.html, b.text = hb.String(), tb.String()
	return b
}

// run membaca satu run: teks berformat, gambar, atau objek OLE.
func (dr *docxReader) run(r *xnode, b *dblock, hb, tb *strings.Builder) {
	var open, close string
	if rpr := r.child("rPr"); rpr != nil {
		if rpr.child("rtl") != nil {
			b.rtl = true
		}
		on := func(name string) bool {
			c := rpr.child(name)
			return c != nil && c.attr("val") != "0" && c.attr("val") != "false" && c.attr("val") != "none"
		}
		for _, f := range []struct{ el, tag string }{{"b", "strong"}, {"i", "em"}, {"u", "u"}} {
			if on(f.el) {
				open += "<" + f.tag + ">"
				close = "</" + f.tag + ">" + close
			}
		}
		switch rpr.child("vertAlign").attr("val") {
		case "superscript":
			open, close = open+"<sup>", "</sup>"+close
		case "subscript":
			open, close = open+"<sub>", "</sub>"+close
		}
	}
	for i := range r.Nodes {
		c := &r.Nodes[i]
		switch c.XMLName.Local {
		case "t":
			hb.WriteString(open + html.EscapeString(c.Content) + close)
			tb.WriteString(c.Content)
		case "tab":
			hb.WriteString(" ")
			tb.WriteString("\t")
		case "br":
			hb.WriteString("<br>")
			tb.WriteString(" ")
		case "drawing":
			var rid, alt string
			walkX(c, func(x *xnode) bool {
				switch x.XMLName.Local {
				case "blip":
					rid = x.attr("embed")
				case "docPr":
					alt = strings.TrimSpace(x.attr("descr"))
					if alt == "" {
						alt = strings.TrimSpace(x.attr("title"))
					}
				}
				return true
			})
			dr.embed(rid, alt, b, hb, false)
		case "object", "pict":
			rid := ""
			walkX(c, func(x *xnode) bool {
				if x.XMLName.Local == "imagedata" {
					rid = x.attr("id")
				}
				return true
			})
			dr.embed(rid, "", b, hb, c.XMLName.Local == "object")
		}
	}
}

// embed menyisipkan gambar (atau pratinjau objek rumus OLE/MathType).
func (dr *docxReader) embed(rid, alt string, b *dblock, hb *strings.Builder, ole bool) {
	target := dr.rels[rid]
	if target == "" {
		return
	}
	switch strings.ToLower(path.Ext(target)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		if ole {
			b.problems = append(b.problems, "rumus MathType lama (WMF/EMF) tidak bisa dibaca; ketik ulang dengan tombol Σ Rumus")
		} else {
			b.problems = append(b.problems, "gambar berformat WMF/EMF tidak bisa dibaca; unggah ulang sebagai PNG/JPEG")
		}
		hb.WriteString("[rumus]")
		return
	}
	if alt == "" {
		alt = "Gambar soal"
		if ole {
			alt = "Rumus"
		}
	}
	id, err := dr.image(target, alt)
	if err != nil {
		b.problems = append(b.problems, "gambar "+path.Base(target)+" tidak bisa dibaca")
		return
	}
	hb.WriteString(imgTag(id, alt))
	b.images++
	if ole {
		b.problems = append(b.problems, "rumus diimpor sebagai gambar — ketik ulang bila perlu")
	}
}

func walkX(n *xnode, f func(*xnode) bool) {
	for i := range n.Nodes {
		if f(&n.Nodes[i]) {
			walkX(&n.Nodes[i], f)
		}
	}
}

// table membaca tabel menjadi HTML dan teks sel.
func (dr *docxReader) table(t *xnode) dblock {
	b := dblock{table: true}
	var hb strings.Builder
	hb.WriteString("<table><tbody>")
	for _, tr := range t.children("tr") {
		hb.WriteString("<tr>")
		for _, tc := range tr.children("tc") {
			var parts []string
			var texts []string
			for _, p := range tc.children("p") {
				pb := dr.para(p)
				parts = append(parts, pb.html)
				texts = append(texts, pb.text)
				b.problems = append(b.problems, pb.problems...)
				b.images += pb.images
			}
			hb.WriteString("<td>" + strings.Join(parts, "<br>") + "</td>")
			b.cells = append(b.cells, strings.TrimSpace(strings.Join(texts, " ")))
		}
		hb.WriteString("</tr>")
	}
	hb.WriteString("</tbody></table>")
	b.html, b.text = hb.String(), strings.Join(b.cells, " ")
	return b
}

// readDocxBlocks membaca semua paragraf dan tabel di body dokumen.
func readDocxBlocks(data []byte, save ImageSaver) ([]dblock, error) {
	if len(data) > MaxImportBytes {
		return nil, ErrNotDocx
	}
	files, err := zipFiles(data)
	if err != nil {
		return nil, err
	}
	for name := range files {
		if strings.HasSuffix(strings.ToLower(name), "vbaproject.bin") {
			return nil, ErrDocxMacro
		}
	}
	dr := &docxReader{files: files, rels: map[string]string{}, save: save, assets: map[string]string{}}
	if rels, err := readXMLPart(files["word/_rels/document.xml.rels"]); err == nil {
		for _, r := range rels.children("Relationship") {
			// Tautan luar (TargetMode="External") tidak pernah diambil.
			if r.attr("TargetMode") != "External" {
				dr.rels[r.attr("Id")] = r.attr("Target")
			}
		}
	}
	dr.nb = readNumbering(files)
	doc, err := readXMLPart(files["word/document.xml"])
	if err != nil {
		return nil, err
	}
	body := doc.child("body")
	if body == nil {
		return nil, ErrNotDocx
	}
	var out []dblock
	var visit func(n *xnode)
	visit = func(n *xnode) {
		for i := range n.Nodes {
			c := &n.Nodes[i]
			switch c.XMLName.Local {
			case "p":
				out = append(out, dr.para(c))
			case "tbl":
				out = append(out, dr.table(c))
			case "sdt", "sdtContent", "customXml":
				visit(c)
			}
		}
	}
	visit(body)
	return out, nil
}

var (
	reQNum       = regexp.MustCompile(`^\s*(\d{1,3})\s*[.)]\s*`)
	reOpt        = regexp.MustCompile(`^\s*([A-Fa-f])\s*[.)]\s+`)
	reArabicOpt  = regexp.MustCompile(`^\s*(أ|ب|ج|د|هـ)\s*[.)]\s*`)
	reMarker     = regexp.MustCompile(`(?i)^\s*(kunci(?:\s+jawaban)?|jawaban|jenis|tp|kesulitan|pembahasan|rubrik|poin|level|tag)\s*[:=]\s*`)
	reKeyHeading = regexp.MustCompile(`(?i)^\s*kunci\s+jawaban\s*:?\s*$`)
	reKeyPair    = regexp.MustCompile(`(\d{1,3})\s*[.):]?\s*([A-Fa-f](?:\s*,\s*[A-Fa-f])*)\b`)
	reStimMarker = regexp.MustCompile(`(?i)^\s*stimulus\s*:\s*(.*)$`)
	reStimRange  = regexp.MustCompile(`(?i)(?:bacalah|perhatikan|cermati|teks|gambar|tabel|grafik|bacaan|stimulus|wacana).*?(?:soal|nomor|no\.)\s*(?:nomor\s*|no\.\s*)?(\d{1,3})\s*(?:-|–|—|s\.?\s*d\.?|sampai|hingga|dan)\s*(\d{1,3})`)
	reStimEnd    = regexp.MustCompile(`(?i)^\s*(akhir\s+stimulus|tanpa\s+stimulus)\s*\.?$`)
)

var arabicOptIndex = map[string]int{"أ": 0, "ب": 1, "ج": 2, "د": 3, "هـ": 4}

// draft adalah soal yang sedang dibaca.
type draft struct {
	number   string
	body     []string
	options  []string
	key      string
	answers  string
	kind     string
	tp       string
	diff     string
	points   string
	level    string
	tags     string
	expl     []string
	rubric   []string
	section  string // "body", "option", "expl", "rubric"
	problems []string
	origin   strings.Builder
	stim     int
}

func pWrap(b dblock) string {
	if b.table {
		return b.html
	}
	if b.rtl {
		return `<p dir="rtl" lang="ar">` + b.html + `</p>`
	}
	return `<p>` + b.html + `</p>`
}

// stripPrefix membuang awalan penanda dari HTML inline berdasarkan panjang
// awalan di teks polos (penanda selalu di run teks pertama).
func stripPrefix(h, text string, n int) string {
	prefix := text[:min(max(n, 0), len(text))]
	// Hapus karakter awalan satu per satu dari teks HTML, melewati tag.
	var out strings.Builder
	i := 0
	for j := 0; j < len(h); j++ {
		if i >= len(prefix) {
			out.WriteString(h[j:])
			break
		}
		if h[j] == '<' {
			k := strings.IndexByte(h[j:], '>')
			if k < 0 {
				out.WriteString(h[j:])
				break
			}
			out.WriteString(h[j : j+k+1])
			j += k
			continue
		}
		if h[j] == '&' {
			k := strings.IndexByte(h[j:], ';')
			if k > 0 {
				ent := html.UnescapeString(h[j : j+k+1])
				if strings.HasPrefix(prefix[i:], ent) {
					i += len(ent)
					j += k
					continue
				}
			}
		}
		if h[j] == prefix[i] {
			i++
			continue
		}
		out.WriteString(h[j:])
		break
	}
	return strings.TrimSpace(out.String())
}

// ParseDocx membaca dokumen Word menjadi butir impor (T129).
func ParseDocx(data []byte, save ImageSaver) ([]ParsedItem, error) {
	blocks, err := readDocxBlocks(data, save)
	if err != nil {
		return nil, err
	}
	return segmentBlocks(blocks)
}

type stimState struct {
	idx      int
	from, to int  // rentang nomor soal; 0 = semua soal berikutnya
	open     bool // sedang mengumpulkan isi bacaan
}

func segmentBlocks(blocks []dblock) ([]ParsedItem, error) {
	var items []ParsedItem
	var cur *draft
	var stim *stimState
	keys := map[string]string{}
	inKeys := false
	questions := 0

	flush := func() {
		if cur == nil {
			return
		}
		items = append(items, cur.finish())
		cur = nil
	}
	startStim := func(title string, from, to int) {
		flush()
		items = append(items, ParsedItem{Kind: "stimulus", StimIdx: -1, Stim: StimulusDraft{Title: title}, Origin: Snippet(title, 300)})
		stim = &stimState{idx: len(items) - 1, from: from, to: to, open: true}
	}

	for _, b := range blocks {
		full := strings.TrimSpace(b.label + " " + b.text)
		if b.label == "" {
			full = strings.TrimSpace(b.text)
		}
		if full == "" && b.images == 0 && !b.table {
			continue
		}
		// Kunci jawaban di akhir dokumen (teks atau tabel).
		if reKeyHeading.MatchString(full) {
			flush()
			inKeys = true
			continue
		}
		if inKeys || (b.table && looksLikeKeyTable(b.cells)) {
			for _, m := range reKeyPair.FindAllStringSubmatch(full, -1) {
				keys[m[1]] = strings.ToUpper(strings.ReplaceAll(m[2], " ", ""))
			}
			if inKeys || b.table {
				continue
			}
		}
		// Penanda stimulus template: "Stimulus: judul".
		if m := reStimMarker.FindStringSubmatch(full); m != nil && !b.table {
			title := strings.TrimSpace(m[1])
			from, to := 0, 0
			if r := reStimRange.FindStringSubmatch(full); r != nil {
				from, _ = strconv.Atoi(r[1])
				to, _ = strconv.Atoi(r[2])
			}
			if title == "" {
				title = "Stimulus"
			}
			startStim(title, from, to)
			continue
		}
		if reStimEnd.MatchString(full) {
			flush()
			stim = nil
			continue
		}
		// Soal baru.
		num := ""
		rest := 0
		if m := reQNum.FindStringSubmatchIndex(full); m != nil && !b.table && (b.label == "" || !isLetterLabel(b.label)) {
			num, rest = full[m[2]:m[3]], m[1]
		}
		if num != "" && (cur == nil || cur.section != "option" || reOpt.FindString(full) == "") {
			flush()
			questions++
			if questions > MaxImportQuestions {
				return nil, ErrTooManyItems
			}
			n, _ := strconv.Atoi(num)
			cur = &draft{number: num, section: "body", stim: -1}
			if stim != nil {
				if stim.from == 0 || (n >= stim.from && n <= stim.to) {
					cur.stim = stim.idx
					stim.open = false
				} else if stim.to > 0 && n > stim.to {
					stim = nil
				}
			}
			cur.add(b, full, rest)
			continue
		}
		// Pola stimulus heuristik: "Bacalah teks berikut untuk soal nomor 1–5".
		if r := reStimRange.FindStringSubmatch(full); r != nil && !b.table && (cur == nil || cur.section != "body" || len(cur.body) > 0 && cur.hasAnswer()) {
			from, _ := strconv.Atoi(r[1])
			to, _ := strconv.Atoi(r[2])
			startStim(Snippet(full, 120), from, to)
			items[stim.idx].Stim.Body = pWrap(b)
			continue
		}
		if cur == nil {
			if stim != nil && stim.open {
				items[stim.idx].Stim.Body += pWrap(b)
				items[stim.idx].Problems = append(items[stim.idx].Problems, b.problems...)
				if items[stim.idx].Origin != "" && len(items[stim.idx].Origin) < 300 {
					items[stim.idx].Origin = Snippet(items[stim.idx].Origin+" "+full, 300)
				}
			}
			continue // judul dokumen, petunjuk umum
		}
		cur.add(b, full, 0)
	}
	flush()
	// Kunci dari tabel/daftar akhir dokumen.
	for i := range items {
		it := &items[i]
		if it.Kind != "soal" || it.Number == "" {
			continue
		}
		if k, ok := keys[it.Number]; ok && len(it.Form.Correct) == 0 && (it.Form.Kind == string(KindMC) || it.Form.Kind == string(KindMulti)) {
			applyLetterKey(it, k)
		}
	}
	for i := range items {
		finalizeProblems(&items[i])
	}
	return items, nil
}

func isLetterLabel(l string) bool {
	return reOpt.MatchString(l+" ") || reArabicOpt.MatchString(l)
}

// looksLikeKeyTable melaporkan apakah tabel berisi pasangan nomor–kunci.
func looksLikeKeyTable(cells []string) bool {
	if len(cells) < 2 {
		return false
	}
	head := strings.ToLower(strings.Join(cells, " "))
	if strings.Contains(head, "kunci") {
		return true
	}
	pairs := len(reKeyPair.FindAllString(strings.Join(cells, " "), -1))
	return pairs >= 3 && pairs*2 >= len(cells)/2
}

func (d *draft) hasAnswer() bool { return d.key != "" || d.answers != "" || len(d.options) > 0 }

// add menambahkan satu blok ke soal; skip adalah panjang awalan nomor soal
// yang sudah dibaca.
func (d *draft) add(b dblock, full string, skip int) {
	d.problems = append(d.problems, b.problems...)
	if d.origin.Len() < 400 {
		d.origin.WriteString(full + " ")
	}
	text := full
	h := b.html
	if b.label != "" && skip > 0 {
		// Nomor dari penomoran otomatis Word tidak ada di HTML.
		h = strings.TrimSpace(h)
		text = strings.TrimSpace(full[skip:])
	} else if skip > 0 {
		h = stripPrefix(b.html, b.text, lead(b.text)+skip)
		text = strings.TrimSpace(full[skip:])
	}
	if skip > 0 {
		if strings.TrimSpace(text) != "" || b.images > 0 {
			d.body = append(d.body, pWrap(dblock{html: h, rtl: b.rtl}))
		}
		return
	}
	if b.table {
		d.appendSection(b.html)
		return
	}
	if m := reMarker.FindStringSubmatchIndex(text); m != nil {
		name := strings.ToLower(text[m[2]:m[3]])
		val := strings.TrimSpace(text[m[1]:])
		off := lead(b.text) + m[1]
		if b.label != "" {
			off = m[1] - len(b.label) - 1
		}
		valHTML := stripPrefix(b.html, b.text, max(0, off))
		switch {
		case strings.HasPrefix(name, "kunci"):
			d.key, d.section = val, "body"
		case name == "jawaban":
			d.answers, d.section = val, "body"
		case name == "jenis":
			d.kind = strings.ToLower(val)
		case name == "tp":
			d.tp = val
		case name == "kesulitan":
			d.diff = strings.ToLower(val)
		case name == "poin":
			d.points = val
		case name == "level":
			d.level = strings.ToUpper(val)
		case name == "tag":
			d.tags = val
		case name == "pembahasan":
			d.section = "expl"
			if val != "" {
				d.expl = append(d.expl, pWrap(dblock{html: valHTML, rtl: b.rtl}))
			}
		case name == "rubrik":
			d.section = "rubric"
			if val != "" {
				d.rubric = append(d.rubric, pWrap(dblock{html: valHTML, rtl: b.rtl}))
			}
		}
		return
	}
	if d.section == "expl" || d.section == "rubric" {
		d.appendSection(pWrap(b))
		return
	}
	// Opsi: "A." di teks atau huruf dari penomoran otomatis Word.
	idx, optHTML := -1, ""
	if b.label != "" && isLetterLabel(b.label) {
		idx = int(strings.ToUpper(b.label)[0] - 'A')
		if m := reArabicOpt.FindStringSubmatch(b.label); m != nil {
			idx = arabicOptIndex[m[1]]
		}
		optHTML = strings.TrimSpace(b.html)
	} else if m := reOpt.FindStringSubmatchIndex(text); m != nil {
		idx = int(strings.ToUpper(text[m[2]:m[3]])[0] - 'A')
		optHTML = stripPrefix(b.html, b.text, lead(b.text)+m[1])
	} else if m := reArabicOpt.FindStringSubmatchIndex(text); m != nil {
		idx = arabicOptIndex[text[m[2]:m[3]]]
		optHTML = stripPrefix(b.html, b.text, lead(b.text)+m[1])
	}
	if idx >= 0 && idx < maxChoices && (len(d.options) == idx || (len(d.options) > 0 && idx == len(d.options))) {
		d.options = append(d.options, wrapOpt(optHTML, b.rtl))
		d.section = "option"
		return
	}
	if d.section == "option" && len(d.options) > 0 {
		d.options[len(d.options)-1] += pWrap(b) // lanjutan opsi
		return
	}
	d.body = append(d.body, pWrap(b))
}

// lead adalah jumlah byte spasi di awal s (teks dicocokkan setelah TrimSpace).
func lead(s string) int { return len(s) - len(strings.TrimLeft(s, " \t\u00a0")) }

func wrapOpt(h string, rtl bool) string {
	if rtl {
		return `<p dir="rtl" lang="ar">` + h + `</p>`
	}
	return h
}

func (d *draft) appendSection(h string) {
	switch d.section {
	case "expl":
		d.expl = append(d.expl, h)
	case "rubric":
		d.rubric = append(d.rubric, h)
	case "option":
		if len(d.options) > 0 {
			d.options[len(d.options)-1] += h
			return
		}
		fallthrough
	default:
		d.body = append(d.body, h)
	}
}

var kindAliases = map[string]Kind{
	"pg": KindMC, "pilihan ganda": KindMC, "pg kompleks": KindMulti, "pilihan ganda kompleks": KindMulti,
	"benar salah": KindTF, "benar/salah": KindTF, "b/s": KindTF, "isian": KindShort, "isian singkat": KindShort,
	"menjodohkan": KindMatch, "jodohkan": KindMatch, "esai": KindEssay, "essay": KindEssay, "uraian": KindEssay,
}

// finish menyusun butir soal dari draf.
func (d *draft) finish() ParsedItem {
	it := ParsedItem{Kind: "soal", Number: d.number, StimIdx: d.stim, Origin: Snippet(strings.TrimSpace(d.origin.String()), 400), TPCode: d.tp}
	f := emptyForm("")
	f.Body = strings.Join(d.body, "")
	f.Explanation = strings.Join(d.expl, "")
	f.Rubric = strings.Join(d.rubric, "")
	f.Points = d.points
	if f.Points == "" {
		f.Points = "1"
	}
	f.Tags = d.tags
	switch d.diff {
	case "mudah", "sedang", "sulit":
		f.Difficulty = d.diff
	case "sukar":
		f.Difficulty = "sulit"
	}
	if CognitiveGroup(d.level) != "" {
		f.CognitiveLevel = d.level
	}
	kind, ok := kindAliases[strings.Join(strings.Fields(d.kind), " ")]
	switch {
	case ok:
	case d.kind != "":
		it.Problems = append(it.Problems, "jenis \""+d.kind+"\" tidak dikenal, dianggap pilihan ganda")
		kind = KindMC
	case len(d.options) > 0 && strings.Contains(d.key, ","):
		kind = KindMulti
	case len(d.options) > 0:
		kind = KindMC
	case d.answers != "":
		kind = KindShort
	default:
		kind = KindEssay
	}
	f.Kind = string(kind)
	it.Problems = append(it.Problems, d.problems...)
	switch kind {
	case KindMC, KindMulti:
		copy(f.Choices, d.options)
		it.Form = f
		if d.key != "" {
			applyLetterKey(&it, d.key)
		}
		return it
	case KindTF:
		copy(f.Statements, d.options)
		for i, v := range strings.Split(d.key, ",") {
			v = strings.ToUpper(strings.TrimSpace(v))
			if i < len(f.Truth) {
				switch v {
				case "B", "BENAR":
					f.Truth[i] = "B"
				case "S", "SALAH":
					f.Truth[i] = "S"
				}
			}
		}
	case KindShort:
		f.Accepted = strings.Join(strings.Split(d.answers, "|"), "\n")
		f.Input = "teks"
		if allNumbers(d.answers) {
			f.Input = "angka"
		}
	case KindMatch:
		for i, o := range d.options {
			if i >= maxPairs {
				break
			}
			plain := PlainText(o)
			l, r, ok := strings.Cut(plain, " = ")
			if !ok {
				l, r, ok = strings.Cut(plain, "=")
			}
			if !ok {
				it.Problems = append(it.Problems, fmt.Sprintf("pasangan %d tidak berformat \"kiri = kanan\"", i+1))
				continue
			}
			f.Lefts[i], f.Rights[i] = html.EscapeString(strings.TrimSpace(l)), html.EscapeString(strings.TrimSpace(r))
		}
	}
	it.Form = f
	return it
}

func allNumbers(s string) bool {
	any := false
	for _, a := range strings.Split(s, "|") {
		if a = strings.TrimSpace(a); a != "" {
			if _, ok := parseNumber(a); !ok {
				return false
			}
			any = true
		}
	}
	return any
}

// applyLetterKey mengisi kunci PG dari huruf ("B" atau "A, C").
func applyLetterKey(it *ParsedItem, key string) {
	it.Form.Correct = nil
	letters := strings.Split(strings.ToUpper(strings.ReplaceAll(key, " ", "")), ",")
	for _, l := range letters {
		if len(l) == 1 && l[0] >= 'A' && l[0] < 'A'+maxChoices {
			it.Form.Correct = append(it.Form.Correct, int(l[0]-'A'))
		}
	}
	if len(it.Form.Correct) > 1 && it.Form.Kind == string(KindMC) {
		it.Form.Kind = string(KindMulti)
	}
}

// finalizeProblems menambah masalah yang terlihat tanpa validasi penuh.
func finalizeProblems(it *ParsedItem) {
	if it.Kind != "soal" {
		if strings.TrimSpace(PlainText(it.Stim.Body)) == "" && !strings.Contains(it.Stim.Body, "<img") {
			it.Problems = append(it.Problems, "isi stimulus kosong")
		}
		return
	}
	k := Kind(it.Form.Kind)
	if (k == KindMC || k == KindMulti) && len(it.Form.Correct) == 0 {
		it.Problems = append(it.Problems, "kunci tidak ditemukan")
	}
	if strings.TrimSpace(PlainText(it.Form.Body)) == "" && !strings.Contains(it.Form.Body, "<img") && !strings.Contains(it.Form.Body, "data-tex") {
		it.Skip = true
		it.Problems = append(it.Problems, "teks soal kosong")
	}
}
