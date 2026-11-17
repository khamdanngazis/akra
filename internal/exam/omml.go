package exam

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// xnode adalah elemen XML generik untuk membaca .docx (T129).
type xnode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",chardata"`
	Nodes   []xnode    `xml:",any"`
}

// child mengembalikan anak pertama bernama local.
func (n *xnode) child(local string) *xnode {
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			return &n.Nodes[i]
		}
	}
	return nil
}

func (n *xnode) children(local string) []*xnode {
	var out []*xnode
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			out = append(out, &n.Nodes[i])
		}
	}
	return out
}

func (n *xnode) attr(local string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// propVal membaca m:xxxPr/m:name/@m:val.
func ommlProp(n *xnode, pr, name string) (string, bool) {
	p := n.child(pr)
	if p == nil {
		return "", false
	}
	c := p.child(name)
	if c == nil {
		return "", false
	}
	return c.attr("val"), true
}

// ommlUnsupported mencatat elemen OMML yang belum dikenali.
type ommlUnsupported struct{ Elem string }

func (e ommlUnsupported) Error() string { return "unsupported OMML element " + e.Elem }

// ommlToLaTeX mengubah satu m:oMath menjadi LaTeX.
func ommlToLaTeX(n *xnode) (string, error) {
	var b strings.Builder
	if err := ommlConv(&b, n); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

var ommlSymbols = map[rune]string{
	'×': `\times `, '÷': `\div `, '±': `\pm `, '∓': `\mp `, '≤': `\le `, '≥': `\ge `, '≠': `\neq `,
	'≈': `\approx `, '≡': `\equiv `, '∞': `\infty `, '°': `^{\circ}`, '∠': `\angle `, '△': `\triangle `,
	'→': `\rightarrow `, '⇒': `\Rightarrow `, '⇔': `\Leftrightarrow `, '∈': `\in `, '∉': `\notin `,
	'⊂': `\subset `, '∪': `\cup `, '∩': `\cap `, '∅': `\emptyset `, '·': `\cdot `, '⋅': `\cdot `,
	'∥': `\parallel `, '⊥': `\perp `, '∴': `\therefore `, '∆': `\Delta `, '−': `-`,
	'α': `\alpha `, 'β': `\beta `, 'γ': `\gamma `, 'δ': `\delta `, 'θ': `\theta `, 'λ': `\lambda `,
	'μ': `\mu `, 'π': `\pi `, 'σ': `\sigma `, 'φ': `\varphi `, 'ω': `\omega `, 'Δ': `\Delta `,
	'Σ': `\Sigma `, 'Ω': `\Omega `,
}

func ommlText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if v, ok := ommlSymbols[r]; ok {
			b.WriteString(v)
			continue
		}
		switch r {
		case '{', '}', '%', '#', '&', '$', '_':
			b.WriteString(`\` + string(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func ommlGroup(b *strings.Builder, n *xnode) error {
	b.WriteString("{")
	if n != nil {
		if err := ommlConv(b, n); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

var naryChr = map[string]string{"": `\int`, "∫": `\int`, "∬": `\iint`, "∮": `\oint`, "∑": `\sum`, "∏": `\prod`, "⋃": `\bigcup`, "⋂": `\bigcap`}
var accChr = map[string]string{"": `\hat`, "̂": `\hat`, "^": `\hat`, "⃗": `\vec`, "→": `\vec`, "̅": `\bar`, "¯": `\bar`, "̃": `\tilde`, "˙": `\dot`, "̇": `\dot`}
var funcNames = map[string]bool{"sin": true, "cos": true, "tan": true, "cot": true, "sec": true, "csc": true, "log": true, "ln": true, "lim": true, "exp": true, "max": true, "min": true}

func ommlConv(b *strings.Builder, n *xnode) error {
	for i := range n.Nodes {
		c := &n.Nodes[i]
		var err error
		switch c.XMLName.Local {
		case "r":
			for _, t := range c.children("t") {
				b.WriteString(ommlText(t.Content))
			}
		case "f":
			typ, _ := ommlProp(c, "fPr", "type")
			if typ == "lin" {
				err = ommlConv(b, c.child("num"))
				b.WriteString("/")
				if err == nil {
					err = ommlConv(b, c.child("den"))
				}
				break
			}
			cmd := `\frac`
			if typ == "noBar" {
				cmd = `\binom`
			}
			b.WriteString(cmd)
			if err = ommlGroup(b, c.child("num")); err == nil {
				err = ommlGroup(b, c.child("den"))
			}
		case "sSup":
			if err = ommlGroup(b, c.child("e")); err == nil {
				b.WriteString("^")
				err = ommlGroup(b, c.child("sup"))
			}
		case "sSub":
			if err = ommlGroup(b, c.child("e")); err == nil {
				b.WriteString("_")
				err = ommlGroup(b, c.child("sub"))
			}
		case "sSubSup":
			if err = ommlGroup(b, c.child("e")); err == nil {
				b.WriteString("_")
				if err = ommlGroup(b, c.child("sub")); err == nil {
					b.WriteString("^")
					err = ommlGroup(b, c.child("sup"))
				}
			}
		case "rad":
			hide, _ := ommlProp(c, "radPr", "degHide")
			deg := c.child("deg")
			b.WriteString(`\sqrt`)
			if hide != "1" && hide != "on" && deg != nil && len(deg.Nodes) > 0 {
				b.WriteString("[")
				if err = ommlConv(b, deg); err != nil {
					break
				}
				b.WriteString("]")
			}
			err = ommlGroup(b, c.child("e"))
		case "d":
			beg, ok := ommlProp(c, "dPr", "begChr")
			if !ok {
				beg = "("
			}
			end, ok := ommlProp(c, "dPr", "endChr")
			if !ok {
				end = ")"
			}
			sep, ok := ommlProp(c, "dPr", "sepChr")
			if !ok {
				sep = "|"
			}
			b.WriteString(`\left` + ommlDelim(beg) + " ")
			for j, e := range c.children("e") {
				if j > 0 {
					b.WriteString(ommlDelim(sep) + " ")
				}
				if err = ommlConv(b, e); err != nil {
					break
				}
			}
			b.WriteString(` \right` + ommlDelim(end))
		case "m":
			b.WriteString(`\begin{matrix}`)
			for j, row := range c.children("mr") {
				if j > 0 {
					b.WriteString(`\\`)
				}
				for k, e := range row.children("e") {
					if k > 0 {
						b.WriteString("&")
					}
					if err = ommlConv(b, e); err != nil {
						break
					}
				}
			}
			b.WriteString(`\end{matrix}`)
		case "nary":
			chr, _ := ommlProp(c, "naryPr", "chr")
			cmd, ok := naryChr[chr]
			if !ok {
				return ommlUnsupported{"nary " + chr}
			}
			b.WriteString(cmd)
			if s := c.child("sub"); s != nil && len(s.Nodes) > 0 {
				b.WriteString("_")
				ommlGroup(b, s)
			}
			if s := c.child("sup"); s != nil && len(s.Nodes) > 0 {
				b.WriteString("^")
				ommlGroup(b, s)
			}
			err = ommlGroup(b, c.child("e"))
		case "func":
			var name strings.Builder
			if err = ommlConv(&name, c.child("fName")); err != nil {
				break
			}
			fn := strings.TrimSpace(name.String())
			if strings.HasPrefix(fn, "{lim}_") {
				// lim dengan batas bawah (m:limLow di dalam m:fName)
				b.WriteString(`\lim_` + strings.TrimPrefix(fn, "{lim}_") + " ")
			} else if funcNames[fn] {
				b.WriteString(`\` + fn + " ")
			} else {
				b.WriteString(`\operatorname{` + fn + "} ")
			}
			err = ommlGroup(b, c.child("e"))
		case "limLow":
			if err = ommlGroup(b, c.child("e")); err == nil {
				b.WriteString("_")
				err = ommlGroup(b, c.child("lim"))
			}
		case "limUpp":
			if err = ommlGroup(b, c.child("e")); err == nil {
				b.WriteString("^")
				err = ommlGroup(b, c.child("lim"))
			}
		case "acc":
			chr, _ := ommlProp(c, "accPr", "chr")
			cmd, ok := accChr[chr]
			if !ok {
				return ommlUnsupported{"acc " + chr}
			}
			b.WriteString(cmd)
			err = ommlGroup(b, c.child("e"))
		case "bar":
			pos, _ := ommlProp(c, "barPr", "pos")
			if pos == "bot" {
				b.WriteString(`\underline`)
			} else {
				b.WriteString(`\overline`)
			}
			err = ommlGroup(b, c.child("e"))
		case "eqArr":
			b.WriteString(`\begin{aligned}`)
			for j, e := range c.children("e") {
				if j > 0 {
					b.WriteString(`\\`)
				}
				if err = ommlConv(b, e); err != nil {
					break
				}
			}
			b.WriteString(`\end{aligned}`)
		case "box", "borderBox", "groupChr", "e", "oMath":
			err = ommlConv(b, c)
		case "rPr", "ctrlPr", "fPr", "radPr", "dPr", "mPr", "naryPr", "funcPr", "accPr", "barPr", "sSupPr", "sSubPr", "sSubSupPr", "limLowPr", "limUppPr", "boxPr", "groupChrPr", "bookmarkStart", "bookmarkEnd", "proofErr":
			// properti tampilan, diabaikan
		default:
			return ommlUnsupported{c.XMLName.Local}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func ommlDelim(s string) string {
	switch s {
	case "":
		return "."
	case "{":
		return `\{`
	case "}":
		return `\}`
	case "|":
		return "|"
	case "‖":
		return `\|`
	case "⌊":
		return `\lfloor`
	case "⌋":
		return `\rfloor`
	case "⟨", "〈":
		return `\langle`
	case "⟩", "〉":
		return `\rangle`
	}
	return s
}

// parseXMLFragment mem-parse fragmen XML m:oMath (untuk uji).
func parseXMLFragment(s string) (*xnode, error) {
	var n xnode
	if err := xml.Unmarshal([]byte(s), &n); err != nil {
		return nil, fmt.Errorf("parse omml: %w", err)
	}
	return &n, nil
}
