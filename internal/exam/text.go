package exam

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// isHarakat melaporkan apakah r tanda baca vokal Arab (harakat, tanwin,
// syaddah, sukun, alif khanjariyah, tanda tilawah) atau tatwil.
func isHarakat(r rune) bool {
	return (r >= 0x064B && r <= 0x065F) || r == 0x0670 || (r >= 0x06D6 && r <= 0x06ED) || r == 0x0640
}

// arabicFold menyamakan variasi huruf Arab yang sering tertukar saat
// mengetik: alif berhamzah/madah/wasla → alif, ta marbutah → ha,
// alif maqsurah → ya.
var arabicFold = map[rune]rune{
	'أ': 'ا', 'إ': 'ا', 'آ': 'ا', 'ٱ': 'ا',
	'ة': 'ه',
	'ى': 'ي',
}

// NormalizeArabic menghapus harakat dan menyamakan variasi huruf Arab.
// Huruf Latin tidak diubah.
func NormalizeArabic(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isHarakat(r) {
			continue
		}
		if f, ok := arabicFold[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NormalizeSearch menyiapkan teks untuk pencarian: huruf kecil, tanpa
// harakat, variasi huruf Arab disamakan, spasi dirapatkan.
func NormalizeSearch(s string) string {
	s = strings.ToLower(NormalizeArabic(s))
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == 0x00A0
	}), " ")
}

// PlainText mengambil teks polos dari HTML sumber soal, termasuk LaTeX
// rumus (atribut data-tex), untuk pencarian dan cuplikan.
func PlainText(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	space := func() {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
	}
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.Join(strings.Fields(b.String()), " ")
		case html.TextToken:
			b.Write(z.Text())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "p", "div", "br", "li", "td", "th", "tr":
				space()
			}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				if string(k) == "data-tex" {
					space()
					b.Write(v)
					space()
				}
			}
		case html.EndTagToken:
			space()
		}
	}
}

// Snippet memotong teks polos menjadi paling banyak n karakter.
func Snippet(plain string, n int) string {
	r := []rune(plain)
	if len(r) <= n {
		return plain
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// searchText menyusun kolom search_text soal: teks soal, opsi, dan tag.
func searchText(q Question) string {
	parts := []string{PlainText(q.BodySrc)}
	for _, list := range [][]Choice{q.Options.Choices, q.Options.Statements, q.Options.Left, q.Options.Right} {
		for _, c := range list {
			parts = append(parts, PlainText(c.Src))
		}
	}
	parts = append(parts, q.Tags...)
	return NormalizeSearch(strings.Join(parts, " "))
}
