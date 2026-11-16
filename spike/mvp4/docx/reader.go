// Package docx adalah prototipe spike T123: membaca soal dari .docx.
// Bukan kode produksi; kode final dibuat di T129.
package docx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

// Block adalah satu paragraf atau tabel hasil baca.
type Block struct {
	Text     string // teks dengan rumus sebagai $...$
	RTL      bool   // w:bidi pada paragraf atau w:rtl pada run
	Images   []string
	Table    bool
	Formulas int
	// Masalah yang membuat soal tidak terimpor sempurna.
	Problems []string
}

// Read membaca semua blok di body dokumen.
func Read(r io.ReaderAt, size int64) ([]Block, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	rels, err := readRels(files["word/_rels/document.xml.rels"])
	if err != nil {
		return nil, err
	}
	doc, err := readXML(files["word/document.xml"])
	if err != nil {
		return nil, err
	}
	body := doc.child("body")
	if body == nil {
		return nil, fmt.Errorf("docx: no body")
	}
	var out []Block
	for i := range body.Nodes {
		n := &body.Nodes[i]
		switch n.XMLName.Local {
		case "p":
			out = append(out, para(n, rels))
		case "tbl":
			b := Block{Table: true}
			var cells []string
			walk(n, func(c *Node) bool {
				if c.XMLName.Local == "tc" {
					var parts []string
					for _, p := range c.children("p") {
						pb := para(p, rels)
						parts = append(parts, pb.Text)
						b.Formulas += pb.Formulas
						b.Images = append(b.Images, pb.Images...)
						b.Problems = append(b.Problems, pb.Problems...)
					}
					cells = append(cells, strings.Join(parts, " "))
					return false
				}
				return true
			})
			b.Text = "[tabel: " + strings.Join(cells, " | ") + "]"
			out = append(out, b)
		}
	}
	return out, nil
}

func walk(n *Node, f func(*Node) bool) {
	for i := range n.Nodes {
		if f(&n.Nodes[i]) {
			walk(&n.Nodes[i], f)
		}
	}
}

func para(p *Node, rels map[string]string) Block {
	var b Block
	var sb strings.Builder
	if pPr := p.child("pPr"); pPr != nil && pPr.child("bidi") != nil {
		b.RTL = true
	}
	var visit func(n *Node)
	visit = func(n *Node) {
		for i := range n.Nodes {
			c := &n.Nodes[i]
			switch c.XMLName.Local {
			case "r":
				va := ""
				if rPr := c.child("rPr"); rPr != nil {
					if rPr.child("rtl") != nil {
						b.RTL = true
					}
					va = rPr.child("vertAlign").attr("val")
				}
				switch va {
				case "superscript":
					sb.WriteString("<sup>")
					visit(c)
					sb.WriteString("</sup>")
				case "subscript":
					sb.WriteString("<sub>")
					visit(c)
					sb.WriteString("</sub>")
				default:
					visit(c)
				}
			case "t":
				sb.WriteString(c.Content)
			case "tab":
				sb.WriteString("\t")
			case "oMathPara":
				visit(c)
			case "oMath":
				tex, err := OMMLToLaTeX(c)
				b.Formulas++
				if err != nil {
					b.Problems = append(b.Problems, err.Error())
					sb.WriteString("[rumus gagal]")
				} else {
					sb.WriteString("$" + tex + "$")
				}
			case "drawing":
				walk(c, func(x *Node) bool {
					if x.XMLName.Local == "blip" {
						b.Images = append(b.Images, rels[x.attr("embed")])
					}
					return true
				})
			case "object":
				// Objek OLE (MathType/Equation 3.0): pakai gambar pratinjau.
				img := ""
				walk(c, func(x *Node) bool {
					if x.XMLName.Local == "imagedata" {
						img = rels[x.attr("id")]
					}
					return true
				})
				ext := strings.ToLower(path.Ext(img))
				switch ext {
				case ".png", ".jpg", ".jpeg", ".gif":
					b.Images = append(b.Images, img)
					sb.WriteString("[gambar rumus]")
				default:
					b.Problems = append(b.Problems, "ole preview "+ext+" (WMF/EMF) not convertible")
					sb.WriteString("[rumus OLE]")
				}
			case "pPr", "rPr":
			default:
				visit(c)
			}
		}
	}
	visit(p)
	b.Text = sb.String()
	return b
}

func readXML(f *zip.File) (*Node, error) {
	if f == nil {
		return nil, fmt.Errorf("docx: missing part")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var n Node
	if err := xml.NewDecoder(rc).Decode(&n); err != nil {
		return nil, fmt.Errorf("docx: decode %s: %w", f.Name, err)
	}
	return &n, nil
}

func readRels(f *zip.File) (map[string]string, error) {
	out := map[string]string{}
	if f == nil {
		return out, nil
	}
	n, err := readXML(f)
	if err != nil {
		return nil, err
	}
	for _, r := range n.children("Relationship") {
		out[r.attr("Id")] = r.attr("Target")
	}
	return out, nil
}

// Question adalah soal hasil segmentasi.
type Question struct {
	Number   string
	Stem     []string
	Options  []string
	Key      string
	Formulas int
	Images   int
	RTL      bool
	Problems []string
}

// Perfect melaporkan apakah soal terimpor tanpa masalah.
func (q Question) Perfect() bool {
	return len(q.Problems) == 0 && q.Key != "" && len(q.Stem) > 0
}

var (
	reNum    = regexp.MustCompile(`^\s*(\d{1,3})[.)]\s*`)
	reOpt    = regexp.MustCompile(`^\s*([A-Ea-e])[.)]\s+`)
	reKey    = regexp.MustCompile(`(?i)^\s*kunci\s*(jawaban)?\s*[:=]\s*([A-E])\b`)
	arabicLH = regexp.MustCompile(`^\s*([أبجدهـ])[.)]\s*`) // opsi Arab: أ ب ج د هـ
)

// Segment memecah blok menjadi soal: "1." memulai soal, "A."/"a." opsi,
// "Kunci: B" kunci jawaban.
func Segment(blocks []Block) []Question {
	var qs []Question
	cur := -1
	for _, b := range blocks {
		t := strings.TrimSpace(b.Text)
		if t == "" && len(b.Images) == 0 {
			continue
		}
		if m := reNum.FindStringSubmatch(t); m != nil && !b.Table {
			qs = append(qs, Question{Number: m[1]})
			cur = len(qs) - 1
			t = t[len(m[0]):]
		}
		if cur < 0 {
			continue
		}
		q := &qs[cur]
		q.Formulas += b.Formulas
		q.Images += len(b.Images)
		q.RTL = q.RTL || b.RTL
		q.Problems = append(q.Problems, b.Problems...)
		switch {
		case reKey.MatchString(t):
			q.Key = strings.ToUpper(reKey.FindStringSubmatch(t)[2])
		case reOpt.MatchString(t):
			q.Options = append(q.Options, reOpt.ReplaceAllString(t, ""))
		case arabicLH.MatchString(t):
			q.Options = append(q.Options, arabicLH.ReplaceAllString(t, ""))
		case len(q.Options) > 0 && t != "":
			// lanjutan opsi terakhir
			q.Options[len(q.Options)-1] += " " + t
		default:
			if t != "" || len(b.Images) > 0 {
				q.Stem = append(q.Stem, t)
			}
		}
	}
	return qs
}
