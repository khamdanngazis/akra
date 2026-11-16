package docx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// Pembuat dokumen contoh sintetis untuk spike. Dokumen guru sungguhan
// belum tersedia; lihat ADR 0001 bagian keterbatasan.

const ns = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office"`

// Fungsi bantu OMML.
func mr(s string) string { return `<m:r><m:t xml:space="preserve">` + s + `</m:t></m:r>` }
func me(s string) string { return `<m:e>` + s + `</m:e>` }
func frac(n, d string) string {
	return `<m:f><m:num>` + n + `</m:num><m:den>` + d + `</m:den></m:f>`
}
func sup(e, s string) string { return `<m:sSup>` + me(e) + `<m:sup>` + s + `</m:sup></m:sSup>` }
func sub(e, s string) string { return `<m:sSub>` + me(e) + `<m:sub>` + s + `</m:sub></m:sSub>` }
func sqrt(e string) string {
	return `<m:rad><m:radPr><m:degHide m:val="1"/></m:radPr><m:deg/>` + me(e) + `</m:rad>`
}
func root(d, e string) string { return `<m:rad><m:deg>` + d + `</m:deg>` + me(e) + `</m:rad>` }
func paren(e string) string   { return `<m:d>` + me(e) + `</m:d>` }
func matrix(rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<m:d><m:e><m:m>`)
	for _, r := range rows {
		b.WriteString(`<m:mr>`)
		for _, c := range r {
			b.WriteString(me(mr(c)))
		}
		b.WriteString(`</m:mr>`)
	}
	b.WriteString(`</m:m></m:e></m:d>`)
	return b.String()
}
func nary(chr, lo, hi, e string) string {
	return `<m:nary><m:naryPr><m:chr m:val="` + chr + `"/></m:naryPr><m:sub>` + lo + `</m:sub><m:sup>` + hi + `</m:sup>` + me(e) + `</m:nary>`
}
func fn(name, e string) string {
	return `<m:func><m:fName>` + mr(name) + `</m:fName>` + me(e) + `</m:func>`
}
func lim(low, e string) string {
	return `<m:func><m:fName><m:limLow>` + me(mr("lim")) + `<m:lim>` + low + `</m:lim></m:limLow></m:fName>` + me(e) + `</m:func>`
}
func vec(e string) string {
	return `<m:acc><m:accPr><m:chr m:val="⃗"/></m:accPr>` + me(e) + `</m:acc>`
}
func eqArr(a, b string) string { return `<m:eqArr>` + me(a) + me(b) + `</m:eqArr>` }
func omath(s string) string    { return `<m:oMath>` + s + `</m:oMath>` }

func wr(s string) string { return `<w:r><w:t xml:space="preserve">` + s + `</w:t></w:r>` }
func wsup(s string) string {
	return `<w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:t>` + s + `</w:t></w:r>`
}
func wsub(s string) string {
	return `<w:r><w:rPr><w:vertAlign w:val="subscript"/></w:rPr><w:t>` + s + `</w:t></w:r>`
}
func p(parts ...string) string { return `<w:p>` + strings.Join(parts, "") + `</w:p>` }
func pRTL(s string) string {
	return `<w:p><w:pPr><w:bidi/></w:pPr><w:r><w:rPr><w:rtl/></w:rPr><w:t xml:space="preserve">` + s + `</w:t></w:r></w:p>`
}
func img(rid string) string {
	return `<w:r><w:drawing><wp:inline><a:graphic><a:graphicData><pic:pic><pic:blipFill><a:blip r:embed="` + rid + `"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`
}
func ole(rid string) string {
	return `<w:r><w:object><v:shape><v:imagedata r:id="` + rid + `" o:title=""/></v:shape><o:OLEObject ProgID="Equation.DSMT4"/></w:object></w:r>`
}
func tbl(rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<w:tbl>`)
	for _, r := range rows {
		b.WriteString(`<w:tr>`)
		for _, c := range r {
			b.WriteString(`<w:tc>` + p(wr(c)) + `</w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

// question menyusun soal pilihan ganda standar format guru.
func question(n int, stem []string, opts []string, key string) string {
	var b strings.Builder
	b.WriteString(p(append([]string{wr(fmt.Sprintf("%d. ", n))}, stem...)...))
	for i, o := range opts {
		b.WriteString(p(wr(string(rune('A'+i))+". "), o))
	}
	b.WriteString(p(wr("Kunci: " + key)))
	return b.String()
}

// Sample adalah satu dokumen contoh beserta jumlah soal yang diharapkan.
type Sample struct {
	Name      string
	Body      string
	Media     map[string]string // rId -> nama file media
	Questions int
}

// Samples mengembalikan dokumen contoh sintetis.
func Samples() []Sample {
	var mtk strings.Builder
	x := mr("x")
	formulas := [][]string{
		{omath(frac(mr("3"), mr("4")) + mr("+") + frac(mr("1"), mr("2")) + mr("=…"))},
		{omath(sup(mr("2"), mr("5")) + mr("×") + sup(mr("2"), mr("3")))},
		{omath(sqrt(mr("144")) + mr("+") + root(mr("3"), mr("27")))},
		{omath(sup(x, mr("2")) + mr("−5x+6=0"))},
		{omath(mr("x=") + frac(mr("−b±")+sqrt(sup(mr("b"), mr("2"))+mr("−4ac")), mr("2a")))},
		{omath(mr("A=") + matrix([][]string{{"2", "1"}, {"3", "4"}}))},
		{omath(nary("∑", mr("i=1"), mr("n"), mr("i")) + mr("="))},
		{omath(nary("∫", mr("0"), mr("2"), sup(x, mr("2"))+mr("dx")))},
		{omath(fn("sin", mr("30°")) + mr("+") + fn("cos", mr("60°")))},
		{omath(lim(mr("x→2"), frac(sup(x, mr("2"))+mr("−4"), mr("x−2"))))},
		{omath(sub(mr("log"), mr("2")) + mr("8+") + sub(mr("log"), mr("3")) + mr("9"))},
		{omath(mr("2x−3≤7"))},
		{omath(vec(mr("a")) + mr("·") + vec(mr("b")))},
		{omath(paren(mr("a+b")) + mr("²"))},
		{omath(mr("π") + sup(mr("r"), mr("2")) + mr(", r=7"))},
		{omath(eqArr(mr("2x+y=5"), mr("x−y=1")))}, // sistem persamaan (eqArr)
	}
	for i, f := range formulas {
		mtk.WriteString(question(i+1, append([]string{wr("Hasil dari ")}, f...),
			[]string{wr("1"), wr("2"), wr("3"), wr("4"), wr("5")}, "C"))
	}
	n := len(formulas)
	// soal bertabel
	n++
	mtk.WriteString(p(wr(fmt.Sprintf("%d. Perhatikan tabel nilai berikut.", n))))
	mtk.WriteString(tbl([][]string{{"Nilai", "Frekuensi"}, {"6", "4"}, {"7", "10"}, {"8", "6"}}))
	mtk.WriteString(p(wr("Rata-rata nilai adalah …")))
	for i, o := range []string{"6,5", "7,1", "7,5", "8"} {
		mtk.WriteString(p(wr(string(rune('A'+i)) + ". " + o)))
	}
	mtk.WriteString(p(wr("Kunci: B")))
	// soal bergambar
	n++
	mtk.WriteString(question(n, []string{wr("Perhatikan gambar segitiga berikut. "), img("rIdImg1"), wr(" Luasnya adalah …")},
		[]string{wr("12 cm²"), wr("24 cm²"), wr("30 cm²"), wr("48 cm²")}, "B"))

	// IPA: pangkat dan indeks memakai format huruf (bukan equation).
	var ipa strings.Builder
	ipaQ := [][]string{
		{wr("Rumus kimia air adalah H"), wsub("2"), wr("O. Massa molekul relatifnya …")},
		{wr("Kecepatan cahaya 3 × 10"), wsup("8"), wr(" m/s. Dalam km/jam …")},
		{wr("Reaksi CO"), wsub("2"), wr(" + H"), wsub("2"), wr("O → H"), wsub("2"), wr("CO"), wsub("3"), wr(" termasuk …")},
		{wr("Satuan gaya dalam SI adalah kg m s"), wsup("−2"), wr(", disebut …")},
		{wr("Ion SO"), wsub("4"), wsup("2−"), wr(" bermuatan …")},
	}
	for i, s := range ipaQ {
		ipa.WriteString(question(i+1, s, []string{wr("satu"), wr("dua"), wr("tiga"), wr("empat")}, "A"))
	}

	// Matematika dengan MathType lama (OLE): pratinjau PNG atau WMF.
	var mt strings.Builder
	for i := 0; i < 10; i++ {
		rid := "rIdPng"
		if i%2 == 1 {
			rid = "rIdWmf"
		}
		mt.WriteString(question(i+1, []string{wr("Nilai dari "), ole(rid), wr(" adalah …")},
			[]string{wr("1"), wr("2"), wr("3"), wr("4")}, "D"))
	}

	// PAI / Bahasa Arab.
	var arab strings.Builder
	arabQ := []struct{ stem, rtl string }{
		{"Arti kalimat berikut adalah …", "ذَهَبَ أَحْمَدُ إِلَى الْمَدْرَسَةِ"},
		{"Hukum bacaan nun sukun pada lafaz berikut adalah …", "مِنْ رَبِّهِمْ"},
		{"Lanjutan ayat berikut adalah …", "بِسْمِ اللَّهِ الرَّحْمَٰنِ الرَّحِيمِ"},
		{"Kata yang bergaris bawah termasuk isim …", "هٰذَا كِتَابٌ جَدِيدٌ"},
		{"Terjemahan yang tepat adalah …", "أَنَا طَالِبٌ فِي الْمَدْرَسَةِ الْمُتَوَسِّطَةِ"},
	}
	for i, q := range arabQ {
		arab.WriteString(p(wr(fmt.Sprintf("%d. %s", i+1, q.stem))))
		arab.WriteString(pRTL(q.rtl))
		for j, o := range []string{"الْحَمْدُ لِلَّهِ", "مَالِكِ يَوْمِ الدِّينِ", "إِيَّاكَ نَعْبُدُ", "اهْدِنَا الصِّرَاطَ"} {
			arab.WriteString(`<w:p><w:pPr><w:bidi/></w:pPr>` + wr(string(rune('A'+j))+". ") + `<w:r><w:rPr><w:rtl/></w:rPr><w:t>` + o + `</w:t></w:r></w:p>`)
		}
		arab.WriteString(p(wr("Kunci: A")))
	}

	media := map[string]string{"rIdImg1": "media/image1.png", "rIdPng": "media/image2.png", "rIdWmf": "media/image3.wmf"}
	return []Sample{
		{Name: "matematika-omml", Body: mtk.String(), Media: media, Questions: n},
		{Name: "ipa-format-huruf", Body: ipa.String(), Media: media, Questions: len(ipaQ)},
		{Name: "matematika-mathtype", Body: mt.String(), Media: media, Questions: 10},
		{Name: "pai-arab", Body: arab.String(), Media: media, Questions: len(arabQ)},
	}
}

// Build menyusun file .docx minimal dari Sample.
func Build(s Sample) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name, body string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}
	var rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for id, target := range s.Media {
		rels.WriteString(`<Relationship Id="` + id + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="` + target + `"/>`)
	}
	rels.WriteString(`</Relationships>`)
	parts := map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="wmf" ContentType="image/x-wmf"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":                  `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": rels.String(),
		"word/document.xml":            `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ` + ns + `><w:body>` + s.Body + `</w:body></w:document>`,
		"word/media/image1.png":        "png",
		"word/media/image2.png":        "png",
		"word/media/image3.wmf":        "wmf",
	}
	for name, body := range parts {
		if err := put(name, body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
