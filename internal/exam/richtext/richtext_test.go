package richtext

import (
	"io/fs"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"akra/internal/exam/mathrender"
	"akra/web"
)

func renderer(t *testing.T) *mathrender.Renderer {
	t.Helper()
	static, _ := fs.Sub(web.FS, "static")
	r, err := mathrender.New(static)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Sanitizer menolak skrip, atribut on*, javascript:, dan style (kriteria T128).
func TestSanitizeRejectsDangerous(t *testing.T) {
	cases := []string{
		`<p>a<script>alert(1)</script>b</p>`,
		`<p onclick="alert(1)">a</p>`,
		`<img src=x onerror="alert(1)">`,
		`<a href="javascript:alert(1)">klik</a>`,
		`<p style="background:url(javascript:alert(1))">a</p>`,
		`<span style="position:fixed;top:0">a</span>`,
		`<iframe src="https://evil"></iframe>`,
		`<svg onload="alert(1)"><circle/></svg>`,
		`<math><mi xlink:href="javascript:alert(1)">x</mi></math>`,
		`<p><style>*{display:none}</style>a</p>`,
		`<img data-asset="11111111-1111-4111-8111-111111111111" src="javascript:alert(1)" onerror="x">`,
		`<p dir="javascript:x">a</p>`,
		`<td colspan="1 onmouseover=alert(1)">a</td>`,
		`<!-- <script>alert(1)</script> -->`,
		`<form action="/x"><input name="a"></form>`,
		`<p>\(\href{javascript:alert(1)}{x}\)</p>`,
	}
	r := renderer(t)
	for _, in := range cases {
		doc, err := Process(in, r)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		for _, out := range []string{doc.Src, doc.HTML} {
			if msg := unsafeMarkup(out); msg != "" {
				t.Errorf("%s\n  -> %s\n  %s", in, out, msg)
			}
		}
	}
}

func TestSanitizeKeepsFormatting(t *testing.T) {
	in := `<div><b>Tebal</b> <i>miring</i> <u>garis</u> H<sub>2</sub>O x<sup>2</sup></div>` +
		`<ul><li>satu</li></ul><table><tr><td colspan="2">sel</td></tr></table>` +
		`<p dir="rtl" lang="ar">بِسْمِ اللَّهِ</p>` +
		`<img data-asset="11111111-1111-4111-8111-111111111111" alt="Segitiga siku-siku" width="300">` +
		`<img src="https://web.example/x.png">`
	src, doc, err := Sanitize(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<strong>Tebal</strong>", "<em>miring</em>", "<u>garis</u>", "<sub>2</sub>", "<sup>2</sup>",
		"<li>satu</li>", `<td colspan="2">`, `<p dir="rtl" lang="ar">بِسْمِ اللَّهِ</p>`, `data-asset="11111111-1111-4111-8111-111111111111"`, `alt="Segitiga siku-siku"`} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	if doc.DroppedImages != 1 {
		t.Errorf("DroppedImages = %d, want 1 (external image)", doc.DroppedImages)
	}
}

// Tempel dari Word membawa class mso, style, span kosong, dan o:p.
func TestSanitizeWordPaste(t *testing.T) {
	in := `<p class="MsoNormal" style="margin:0cm"><span lang="IN" style="font-size:12pt;font-family:'Times New Roman'">1. Hasil dari <b>3 × 4</b> adalah<o:p></o:p></span></p>`
	src, _, err := Sanitize(in)
	if err != nil {
		t.Fatal(err)
	}
	if src != `<p>1. Hasil dari <strong>3 × 4</strong> adalah</p>` {
		t.Errorf("word paste = %s", src)
	}
}

func TestProcessMath(t *testing.T) {
	r := renderer(t)
	doc, err := Process(`Akar dari \(x^2-5x+6=0\) adalah \[x=\frac{-b\pm\sqrt{b^2-4ac}}{2a}\] dan <span class="math" data-tex="\begin{pmatrix}1&amp;2&amp;3\\4&amp;5&amp;6\\7&amp;8&amp;9\end{pmatrix}">lama</span>`, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.MathErrors) != 0 {
		t.Fatalf("math errors: %v", doc.MathErrors[0])
	}
	if strings.Count(doc.Src, `class="math`) != 3 || !strings.Contains(doc.Src, `data-tex="x^2-5x+6=0"`) {
		t.Errorf("src = %s", doc.Src)
	}
	if !strings.Contains(doc.HTML, `class="katex"`) || !strings.Contains(doc.HTML, `class="math-display"`) || strings.Contains(doc.HTML, "lama") {
		t.Errorf("html missing katex output or kept stale content")
	}
	if !strings.HasPrefix(doc.Src, "<p>") {
		t.Errorf("inline text not wrapped in a paragraph: %s", doc.Src)
	}

	bad, err := Process(`\(\frac{1}{\)`, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad.MathErrors) != 1 || !strings.Contains(bad.HTML, "math-error") {
		t.Errorf("invalid formula not reported: %+v", bad.MathErrors)
	}
	// Rumus kimia, integral, pecahan bersarang.
	for _, tex := range []string{`\frac{\frac{1}{2}}{\frac{3}{4}}`, `\int_0^1 x^2\,dx`, `\mathrm{H_2SO_4} \rightarrow 2\mathrm{H^+} + \mathrm{SO_4^{2-}}`} {
		if _, err := r.Render(tex, false); err != nil {
			t.Errorf("%s: %v", tex, err)
		}
	}
}

func TestProcessArabicAndAssets(t *testing.T) {
	r := renderer(t)
	doc, err := Process(`<p dir="rtl">إِذَا كَانَ \(x+2=5\) فَمَا قِيمَةُ \(x\)؟</p><p>Latin <img data-asset="11111111-1111-4111-8111-111111111111" alt=""></p>`, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.HTML, `<p dir="rtl">إِذَا كَانَ <span class="math-inline">`) {
		t.Errorf("rtl paragraph lost: %s", doc.HTML[:120])
	}
	if !strings.Contains(doc.HTML, `<p dir="auto">Latin`) {
		t.Error("paragraph without dir did not get dir=auto")
	}
	if doc.MissingAlt != 1 || len(doc.Assets) != 1 {
		t.Errorf("assets = %v, missing alt = %d", doc.Assets, doc.MissingAlt)
	}
	out := ResolveAssets(doc.HTML, func(id string) string { return "/storage/x?sig=1&id=" + id })
	if !strings.Contains(out, `<img src="/storage/x?sig=1&amp;id=11111111-1111-4111-8111-111111111111" data-asset=`) {
		t.Errorf("ResolveAssets = %s", out)
	}
	if !(Doc{Src: "<p> </p>"}).Empty() || (Doc{Src: `<p><span class="math" data-tex="x"></span></p>`}).Empty() {
		t.Error("Empty")
	}
}

// unsafeMarkup memeriksa elemen dan atribut hasil (bukan teks): tidak ada
// elemen berbahaya, atribut on*, href, atau URL javascript:. Atribut style
// hanya boleh di dalam keluaran KaTeX (tata letak rumus buatan server).
func unsafeMarkup(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var stack []bool // true bila elemen berada di dalam keluaran KaTeX
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.EndTagToken:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, more := z.TagName()
			switch string(name) {
			case "script", "iframe", "svg", "math", "form", "input", "a", "style", "object", "embed":
				return "element <" + string(name) + ">"
			}
			inKatex := len(stack) > 0 && stack[len(stack)-1]
			for more {
				var k, v []byte
				k, v, more = z.TagAttr()
				key, val := strings.ToLower(string(k)), strings.ToLower(string(v))
				switch {
				case strings.HasPrefix(key, "on"):
					return "attribute " + key
				case key == "href":
					return "attribute href"
				case key == "class" && strings.Contains(" "+val+" ", " katex "):
					inKatex = true
				case key == "style" && !inKatex:
					return "style outside KaTeX output"
				case key != "data-tex" && strings.Contains(val, "javascript:"):
					return "javascript: in attribute " + key
				}
			}
			if tt == html.StartTagToken && string(name) != "img" && string(name) != "br" {
				stack = append(stack, inKatex)
			}
		}
	}
}
