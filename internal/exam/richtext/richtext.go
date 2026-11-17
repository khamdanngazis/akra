// Package richtext membersihkan dan merender teks kaya soal (T128).
//
// Sumber (src) adalah HTML dari editor guru atau hasil impor. Process
// membersihkannya dengan allowlist ketat (tanpa atribut style, on*, atau
// tautan), mengubah rumus \( ... \) dan \[ ... \] menjadi elemen rumus,
// lalu merender HTML tampilan dengan KaTeX di server.
package richtext

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"akra/internal/exam/mathrender"
)

// MaxSource membatasi ukuran sumber satu bagian teks.
const MaxSource = 200 << 10

// ErrTooLarge dikembalikan untuk sumber di atas MaxSource.
var ErrTooLarge = errors.New("richtext: source too large")

// Doc adalah hasil Process.
type Doc struct {
	Src  string // sumber bersih, disimpan sebagai *_src
	HTML string // HTML tampilan, disimpan sebagai *_html
	// Assets adalah ID gambar (exam_assets) yang dipakai.
	Assets []string
	// MathErrors berisi rumus yang tidak valid. Soal dengan rumus tidak
	// valid ditolak saat simpan.
	MathErrors []*mathrender.Error
	// MissingAlt adalah jumlah gambar tanpa teks alternatif.
	MissingAlt int
	// DroppedImages adalah gambar dari luar (mis. tempel dari web) yang
	// dibuang karena bukan unggahan AKRA.
	DroppedImages int
}

// Empty melaporkan apakah dokumen tidak berisi teks, rumus, maupun gambar.
func (d Doc) Empty() bool {
	return strings.TrimSpace(textOf(d.Src)) == "" && len(d.Assets) == 0 && !strings.Contains(d.Src, "data-tex=")
}

// Elemen yang dibuang beserta seluruh isinya.
var dropWithContent = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true, "noscript": true, "template": true,
	"svg": true, "math": true, "head": true, "meta": true, "link": true, "title": true, "form": true, "input": true,
	"button": true, "select": true, "textarea": true, "frame": true, "frameset": true, "applet": true, "base": true,
	"video": true, "audio": true, "canvas": true, "xml": true,
}

// Elemen yang dipertahankan (setelah normalisasi nama).
var allowed = map[string]bool{
	"p": true, "br": true, "strong": true, "em": true, "u": true, "sub": true, "sup": true,
	"ul": true, "ol": true, "li": true, "table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"img": true, "span": true,
}

// rename menyeragamkan elemen setara.
var rename = map[string]string{
	"b": "strong", "i": "em", "div": "p", "h1": "p", "h2": "p", "h3": "p", "h4": "p", "h5": "p", "h6": "p",
	"blockquote": "p", "pre": "p",
}

var blockDir = map[string]bool{"p": true, "li": true, "td": true, "th": true, "ul": true, "ol": true, "table": true}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, key) {
			return a.Val, true
		}
	}
	return "", false
}

func smallInt(v string, max int) (string, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > max {
		return "", false
	}
	return strconv.Itoa(n), true
}

// cleaner menyusun pohon bersih dari pohon hasil parse.
type cleaner struct {
	doc *Doc
}

func newElem(name string) *html.Node {
	return &html.Node{Type: html.ElementNode, Data: name, DataAtom: atom.Lookup([]byte(name))}
}

// cleanChildren membersihkan anak-anak src ke dalam dst.
func (c *cleaner) cleanChildren(dst, src *html.Node) {
	for ch := src.FirstChild; ch != nil; ch = ch.NextSibling {
		c.clean(dst, ch)
	}
}

func (c *cleaner) clean(dst, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		appendText(dst, n.Data)
		return
	case html.ElementNode:
	default:
		return // komentar, doctype
	}
	name := strings.ToLower(n.Data)
	if dropWithContent[name] {
		return
	}
	if r, ok := rename[name]; ok {
		name = r
	}
	switch name {
	case "img":
		id, _ := attr(n, "data-asset")
		if !uuidRe.MatchString(id) {
			c.doc.DroppedImages++
			return
		}
		img := newElem("img")
		img.Attr = append(img.Attr, html.Attribute{Key: "data-asset", Val: id})
		alt, _ := attr(n, "alt")
		alt = strings.Join(strings.Fields(alt), " ")
		if len([]rune(alt)) > 300 {
			alt = string([]rune(alt)[:300])
		}
		img.Attr = append(img.Attr, html.Attribute{Key: "alt", Val: alt})
		for _, k := range []string{"width", "height"} {
			if v, ok := attr(n, k); ok {
				if v, ok := smallInt(v, 4000); ok {
					img.Attr = append(img.Attr, html.Attribute{Key: k, Val: v})
				}
			}
		}
		dst.AppendChild(img)
		return
	case "span":
		cls, _ := attr(n, "class")
		if tex, ok := attr(n, "data-tex"); ok && hasClass(cls, "math") {
			appendMath(dst, tex, hasClass(cls, "math-display") || attrIs(n, "data-display", "1"))
			return
		}
		lang, _ := attr(n, "lang")
		dir, _ := attr(n, "dir")
		if !strings.EqualFold(lang, "ar") && !validDir(dir) {
			c.cleanChildren(dst, n) // span tanpa makna: buka bungkusnya
			return
		}
		sp := newElem("span")
		if strings.EqualFold(lang, "ar") {
			sp.Attr = append(sp.Attr, html.Attribute{Key: "lang", Val: "ar"})
		}
		if validDir(dir) {
			sp.Attr = append(sp.Attr, html.Attribute{Key: "dir", Val: strings.ToLower(dir)})
		}
		c.cleanChildren(sp, n)
		dst.AppendChild(sp)
		return
	}
	if !allowed[name] {
		c.cleanChildren(dst, n) // mis. <a>, <font>, <o:p>: pertahankan teksnya saja
		return
	}
	el := newElem(name)
	if blockDir[name] {
		if dir, ok := attr(n, "dir"); ok && validDir(dir) {
			el.Attr = append(el.Attr, html.Attribute{Key: "dir", Val: strings.ToLower(dir)})
		}
		if lang, _ := attr(n, "lang"); strings.EqualFold(lang, "ar") {
			el.Attr = append(el.Attr, html.Attribute{Key: "lang", Val: "ar"})
		}
	}
	if name == "td" || name == "th" {
		for _, k := range []string{"colspan", "rowspan"} {
			if v, ok := attr(n, k); ok {
				if v, ok := smallInt(v, 20); ok {
					el.Attr = append(el.Attr, html.Attribute{Key: k, Val: v})
				}
			}
		}
	}
	// Paragraf di dalam paragraf (hasil div bersarang) diratakan.
	if name == "p" && insideP(dst) {
		c.cleanChildren(dst, n)
		appendBr(dst)
		return
	}
	c.cleanChildren(el, n)
	dst.AppendChild(el)
}

func insideP(n *html.Node) bool {
	for ; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && n.Data == "p" {
			return true
		}
	}
	return false
}

func appendBr(dst *html.Node) {
	if dst.LastChild != nil && !(dst.LastChild.Type == html.ElementNode && dst.LastChild.Data == "br") {
		dst.AppendChild(newElem("br"))
	}
}

func hasClass(cls, want string) bool {
	for _, c := range strings.Fields(cls) {
		if c == want {
			return true
		}
	}
	return false
}

func attrIs(n *html.Node, k, v string) bool { got, ok := attr(n, k); return ok && got == v }

func validDir(d string) bool {
	switch strings.ToLower(d) {
	case "ltr", "rtl", "auto":
		return true
	}
	return false
}

func appendMath(dst *html.Node, tex string, display bool) {
	sp := newElem("span")
	cls := "math"
	if display {
		cls = "math math-display"
	}
	sp.Attr = []html.Attribute{{Key: "class", Val: cls}, {Key: "data-tex", Val: strings.TrimSpace(tex)}}
	dst.AppendChild(sp)
}

// mathDelim menemukan \( ... \) dan \[ ... \] di teks.
var mathDelim = regexp.MustCompile(`\\\((.+?)\\\)|\\\[(.+?)\\\]`)

// appendText menambahkan teks ke dst, mengubah \( \) dan \[ \] menjadi
// elemen rumus.
func appendText(dst *html.Node, s string) {
	for {
		loc := mathDelim.FindStringSubmatchIndex(s)
		if loc == nil {
			break
		}
		if loc[0] > 0 {
			dst.AppendChild(&html.Node{Type: html.TextNode, Data: s[:loc[0]]})
		}
		if loc[2] >= 0 {
			appendMath(dst, s[loc[2]:loc[3]], false)
		} else {
			appendMath(dst, s[loc[4]:loc[5]], true)
		}
		s = s[loc[1]:]
	}
	if s != "" {
		dst.AppendChild(&html.Node{Type: html.TextNode, Data: s})
	}
}

// Sanitize membersihkan src tanpa merender rumus.
func Sanitize(src string) (string, Doc, error) {
	if len(src) > MaxSource {
		return "", Doc{}, ErrTooLarge
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(src), ctx)
	if err != nil {
		return "", Doc{}, fmt.Errorf("richtext: parse: %w", err)
	}
	var doc Doc
	root := newElem("div")
	c := &cleaner{doc: &doc}
	for _, n := range nodes {
		c.clean(root, n)
	}
	wrapInline(root)
	var b strings.Builder
	for ch := root.FirstChild; ch != nil; ch = ch.NextSibling {
		if err := html.Render(&b, ch); err != nil {
			return "", Doc{}, fmt.Errorf("richtext: render: %w", err)
		}
	}
	return strings.TrimSpace(b.String()), doc, nil
}

// wrapInline membungkus teks dan elemen inline di tingkat atas ke dalam
// paragraf, sehingga arah tulisan per paragraf selalu bisa diatur.
func wrapInline(root *html.Node) {
	var cur *html.Node
	for ch := root.FirstChild; ch != nil; {
		next := ch.NextSibling
		block := ch.Type == html.ElementNode && (ch.Data == "p" || ch.Data == "ul" || ch.Data == "ol" || ch.Data == "table")
		if block {
			cur = nil
		} else {
			if ch.Type == html.TextNode && strings.TrimSpace(ch.Data) == "" && cur == nil {
				root.RemoveChild(ch)
				ch = next
				continue
			}
			if cur == nil {
				cur = newElem("p")
				root.InsertBefore(cur, ch)
			}
			root.RemoveChild(ch)
			cur.AppendChild(ch)
		}
		ch = next
	}
}

// Process membersihkan src lalu merender HTML tampilan dengan rumus.
func Process(src string, r *mathrender.Renderer) (Doc, error) {
	clean, doc, err := Sanitize(src)
	if err != nil {
		return Doc{}, err
	}
	doc.Src = clean
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(clean), ctx)
	if err != nil {
		return Doc{}, fmt.Errorf("richtext: reparse: %w", err)
	}
	var b strings.Builder
	for _, n := range nodes {
		if err := renderNode(&b, n, r, &doc); err != nil {
			return Doc{}, err
		}
	}
	doc.HTML = b.String()
	return doc, nil
}

func renderNode(b *strings.Builder, n *html.Node, r *mathrender.Renderer, doc *Doc) error {
	switch n.Type {
	case html.TextNode:
		b.WriteString(html.EscapeString(n.Data))
		return nil
	case html.ElementNode:
	default:
		return nil
	}
	switch n.Data {
	case "span":
		if tex, ok := attr(n, "data-tex"); ok {
			cls, _ := attr(n, "class")
			display := hasClass(cls, "math-display")
			out, err := r.Render(tex, display)
			var me *mathrender.Error
			if errors.As(err, &me) {
				doc.MathErrors = append(doc.MathErrors, me)
				b.WriteString(`<span class="math-error">` + html.EscapeString(tex) + `</span>`)
				return nil
			}
			if err != nil {
				return err
			}
			if display {
				b.WriteString(`<span class="math-display">` + out + `</span>`)
			} else {
				b.WriteString(`<span class="math-inline">` + out + `</span>`)
			}
			return nil
		}
	case "img":
		id, _ := attr(n, "data-asset")
		alt, _ := attr(n, "alt")
		if strings.TrimSpace(alt) == "" {
			doc.MissingAlt++
		}
		doc.Assets = append(doc.Assets, id)
		b.WriteString(`<img data-asset="` + id + `" alt="` + html.EscapeString(alt) + `"`)
		for _, k := range []string{"width", "height"} {
			if v, ok := attr(n, k); ok {
				b.WriteString(` ` + k + `="` + v + `"`)
			}
		}
		b.WriteString(` loading="lazy">`)
		return nil
	case "br":
		b.WriteString("<br>")
		return nil
	}
	b.WriteString("<" + n.Data)
	hasDir := false
	for _, a := range n.Attr {
		hasDir = hasDir || a.Key == "dir"
		b.WriteString(" " + a.Key + `="` + html.EscapeString(a.Val) + `"`)
	}
	if !hasDir && (n.Data == "p" || n.Data == "li") {
		b.WriteString(` dir="auto"`)
	}
	b.WriteString(">")
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if err := renderNode(b, ch, r, doc); err != nil {
			return err
		}
	}
	b.WriteString("</" + n.Data + ">")
	return nil
}

func textOf(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String()
		case html.TextToken:
			b.Write(z.Text())
		}
	}
}

// assetImg menemukan <img data-asset="..."> di HTML tampilan buatan Process.
var assetImg = regexp.MustCompile(`<img data-asset="([0-9a-f-]{36})"`)

// ResolveAssets menambahkan src ke setiap gambar di HTML tampilan, memakai
// URL dari urlFor (URL bertanda tangan yang berlaku sebentar). HTML harus
// berasal dari Process (bukan masukan pengguna).
func ResolveAssets(h string, urlFor func(id string) string) string {
	return assetImg.ReplaceAllStringFunc(h, func(m string) string {
		id := assetImg.FindStringSubmatch(m)[1]
		return `<img src="` + html.EscapeString(urlFor(id)) + `" data-asset="` + id + `"`
	})
}
