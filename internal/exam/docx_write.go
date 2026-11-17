package exam

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"sort"
	"strings"
)

// docxWriter menyusun .docx sederhana: template Word resmi (T129) dan
// dokumen uji. Hanya bagian minimal yang dibaca Word dan LibreOffice.
type docxWriter struct {
	body      strings.Builder
	numbering bool // tulis numbering.xml (daftar bernomor otomatis)
	extra     map[string][]byte
	media     map[string][]byte // nama file → isi
	rels      []string
}

func newDocx() *docxWriter {
	return &docxWriter{media: map[string][]byte{}, extra: map[string][]byte{}}
}

// NP menambah paragraf bernomor otomatis Word: level 0 "1.", level 1 "A.".
func (d *docxWriter) NP(level int, parts ...string) {
	d.numbering = true
	d.body.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="1"/></w:numPr></w:pPr>`, level) + strings.Join(parts, "") + `</w:p>`)
}

const numberingXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>` +
	`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="upperLetter"/><w:lvlText w:val="%2."/></w:lvl></w:abstractNum>` +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`

const docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
	`xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office"`

// Run teks dengan format opsional ("b", "i", "u", "sub", "sup", "rtl").
func wRun(text string, format ...string) string {
	var pr strings.Builder
	for _, f := range format {
		switch f {
		case "b":
			pr.WriteString(`<w:b/>`)
		case "i":
			pr.WriteString(`<w:i/>`)
		case "u":
			pr.WriteString(`<w:u w:val="single"/>`)
		case "sub":
			pr.WriteString(`<w:vertAlign w:val="subscript"/>`)
		case "sup":
			pr.WriteString(`<w:vertAlign w:val="superscript"/>`)
		case "rtl":
			pr.WriteString(`<w:rtl/>`)
		}
	}
	rpr := ""
	if pr.Len() > 0 {
		rpr = `<w:rPr>` + pr.String() + `</w:rPr>`
	}
	return `<w:r>` + rpr + `<w:t xml:space="preserve">` + html.EscapeString(text) + `</w:t></w:r>`
}

// Pembangun OMML (Equation bawaan Word).
func mRun(s string) string {
	return `<m:r><m:t xml:space="preserve">` + html.EscapeString(s) + `</m:t></m:r>`
}
func mE(s string) string { return `<m:e>` + s + `</m:e>` }
func mFrac(n, d string) string {
	return `<m:f><m:num>` + n + `</m:num><m:den>` + d + `</m:den></m:f>`
}
func mSup(e, s string) string { return `<m:sSup>` + mE(e) + `<m:sup>` + s + `</m:sup></m:sSup>` }
func mSub(e, s string) string { return `<m:sSub>` + mE(e) + `<m:sub>` + s + `</m:sub></m:sSub>` }
func mSqrt(e string) string {
	return `<m:rad><m:radPr><m:degHide m:val="1"/></m:radPr><m:deg/>` + mE(e) + `</m:rad>`
}
func mMath(s string) string { return `<m:oMath>` + s + `</m:oMath>` }

// P menambah paragraf berisi run (wRun/mMath/gambar).
func (d *docxWriter) P(parts ...string) {
	d.body.WriteString(`<w:p>` + strings.Join(parts, "") + `</w:p>`)
}

// RTL menambah paragraf Arab (kanan ke kiri).
func (d *docxWriter) RTL(text string) {
	d.body.WriteString(`<w:p><w:pPr><w:bidi/></w:pPr>` + wRun(text, "rtl") + `</w:p>`)
}

// Table menambah tabel berisi teks.
func (d *docxWriter) Table(rows [][]string) {
	d.body.WriteString(`<w:tbl>`)
	for _, r := range rows {
		d.body.WriteString(`<w:tr>`)
		for _, c := range r {
			d.body.WriteString(`<w:tc><w:p>` + wRun(c) + `</w:p></w:tc>`)
		}
		d.body.WriteString(`</w:tr>`)
	}
	d.body.WriteString(`</w:tbl>`)
}

// Image menyimpan gambar dan mengembalikan run gambarnya.
func (d *docxWriter) Image(name string, data []byte, alt string) string {
	d.media[name] = data
	rid := fmt.Sprintf("rIdImg%d", len(d.rels)+1)
	d.rels = append(d.rels, `<Relationship Id="`+rid+`" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/`+name+`"/>`)
	return `<w:r><w:drawing><wp:inline><wp:extent cx="1905000" cy="1905000"/><wp:docPr id="` + fmt.Sprint(len(d.rels)) + `" name="` + name + `" descr="` + html.EscapeString(alt) + `"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="` + name + `"/><pic:cNvPicPr/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip r:embed="` + rid + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1905000" cy="1905000"/></a:xfrm><a:prstGeom prst="rect"/></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`
}

// Bytes menghasilkan isi file .docx.
func (d *docxWriter) Bytes() ([]byte, error) {
	rels := append([]string{}, d.rels...)
	files := map[string][]byte{}
	if d.numbering {
		rels = append(rels, `<Relationship Id="rIdNum" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`)
		files["word/numbering.xml"] = []byte(numberingXML)
	}
	files["[Content_Types].xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	files["_rels/.rels"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	files["word/_rels/document.xml.rels"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		strings.Join(rels, "") + `</Relationships>`)
	files["word/document.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ` + docxNS + `><w:body>` + d.body.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="709" w:footer="709" w:gutter="0"/></w:sectPr></w:body></w:document>`)
	for name, data := range d.media {
		files["word/media/"+name] = data
	}
	for name, data := range d.extra {
		files[name] = data
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// [Content_Types].xml ditulis pertama (lazim untuk paket OPC).
	order := []string{"[Content_Types].xml"}
	for name := range files {
		if name != order[0] {
			order = append(order, name)
		}
	}
	sort.Strings(order[1:])
	for _, name := range order {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WordTemplate menghasilkan template Word resmi berisi petunjuk dan contoh
// semua jenis soal, rumus Equation Word, stimulus, dan teks Arab.
func WordTemplate() ([]byte, error) {
	d := newDocx()
	d.P(wRun("TEMPLATE SOAL AKRA", "b"))
	d.P(wRun("Ganti contoh di bawah dengan soal Anda. Satu soal diawali nomor (1. atau 1)), opsi diawali huruf (A. atau a.), lalu baris Kunci. Rumus: pakai Insert → Equation di Word. Baris TP, Kesulitan, Poin, dan Pembahasan boleh dihapus."))
	d.P(wRun("1. Hasil dari "), mMath(mFrac(mRun("3"), mRun("4"))+mRun("+")+mFrac(mRun("1"), mRun("2"))), wRun(" adalah …"))
	d.P(wRun("A. "), mMath(mFrac(mRun("5"), mRun("4"))))
	d.P(wRun("B. "), mMath(mFrac(mRun("4"), mRun("6"))))
	d.P(wRun("C. 1"))
	d.P(wRun("D. 2"))
	d.P(wRun("Kunci: A"))
	d.P(wRun("TP: 1.1"))
	d.P(wRun("Kesulitan: mudah"))
	d.P(wRun("Pembahasan: Samakan penyebut: 3/4 + 2/4 = 5/4."))
	d.P(wRun("2. Rumus kimia air adalah H"), wRun("2", "sub"), wRun("O dan nilai "), mMath(mSqrt(mRun("144"))+mRun("+")+mSup(mRun("2"), mRun("3"))), wRun(" adalah …"))
	d.P(wRun("Jenis: isian"))
	d.P(wRun("Jawaban: 20 | dua puluh"))
	d.P(wRun("3. Pilih semua bilangan prima."))
	d.P(wRun("Jenis: PG kompleks"))
	d.P(wRun("A. 2"))
	d.P(wRun("B. 4"))
	d.P(wRun("C. 5"))
	d.P(wRun("D. 9"))
	d.P(wRun("Kunci: A, C"))
	d.P(wRun("4. Tentukan benar atau salah."))
	d.P(wRun("Jenis: benar salah"))
	d.P(wRun("A. Air mendidih pada 100 °C di permukaan laut."))
	d.P(wRun("B. Matahari mengelilingi Bumi."))
	d.P(wRun("Kunci: B, S"))
	d.P(wRun("5. Jodohkan provinsi dengan ibu kotanya."))
	d.P(wRun("Jenis: menjodohkan"))
	d.P(wRun("A. Jawa Barat = Bandung"))
	d.P(wRun("B. Jawa Timur = Surabaya"))
	d.P(wRun("C. Bali = Denpasar"))
	d.P(wRun("6. Jelaskan proses fotosintesis secara singkat."))
	d.P(wRun("Jenis: esai"))
	d.P(wRun("Poin: 10"))
	d.P(wRun("Rubrik: Menyebut cahaya matahari (3), klorofil (3), dan hasil glukosa serta oksigen (4)."))
	d.P(wRun("Stimulus: Teks untuk soal nomor 7–8", "b"))
	d.P(wRun("Bacalah teks berikut. Ani menanam pohon mangga di halaman rumahnya. Setiap pagi ia menyiram pohon itu."))
	d.P(wRun("7. Apa yang ditanam Ani?"))
	d.P(wRun("A. Pohon jambu"))
	d.P(wRun("B. Pohon mangga"))
	d.P(wRun("Kunci: B"))
	d.P(wRun("8. Kapan Ani menyiram pohon?"))
	d.P(wRun("A. Setiap pagi"))
	d.P(wRun("B. Setiap sore"))
	d.P(wRun("Kunci: A"))
	d.P(wRun("9. Arti kalimat berikut adalah …"))
	d.RTL("ذَهَبَ أَحْمَدُ إِلَى الْمَدْرَسَةِ")
	d.P(wRun("A. Ahmad pergi ke sekolah"))
	d.P(wRun("B. Ahmad pulang dari sekolah"))
	d.P(wRun("Kunci: A"))
	return d.Bytes()
}
