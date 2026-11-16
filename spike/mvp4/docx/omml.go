package docx

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Node adalah elemen XML generik untuk menelusuri OMML.
type Node struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",chardata"`
	Nodes   []Node     `xml:",any"`
}

// child mengembalikan anak pertama bernama local.
func (n *Node) child(local string) *Node {
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			return &n.Nodes[i]
		}
	}
	return nil
}

func (n *Node) children(local string) []*Node {
	var out []*Node
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			out = append(out, &n.Nodes[i])
		}
	}
	return out
}

func (n *Node) attr(local string) string {
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
func propVal(n *Node, pr, name string) (string, bool) {
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

// UnsupportedError mencatat elemen OMML yang belum dikenali.
type UnsupportedError struct{ Elem string }

func (e UnsupportedError) Error() string { return "unsupported OMML element " + e.Elem }

// OMMLToLaTeX mengubah satu m:oMath menjadi LaTeX.
func OMMLToLaTeX(n *Node) (string, error) {
	var b strings.Builder
	if err := conv(&b, n); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

var symbols = map[rune]string{
	'×': `\times `, '÷': `\div `, '±': `\pm `, '∓': `\mp `, '≤': `\le `, '≥': `\ge `, '≠': `\neq `,
	'≈': `\approx `, '≡': `\equiv `, '∞': `\infty `, '°': `^{\circ}`, '∠': `\angle `, '△': `\triangle `,
	'→': `\rightarrow `, '⇒': `\Rightarrow `, '⇔': `\Leftrightarrow `, '∈': `\in `, '∉': `\notin `,
	'⊂': `\subset `, '∪': `\cup `, '∩': `\cap `, '∅': `\emptyset `, '·': `\cdot `, '⋅': `\cdot `,
	'∥': `\parallel `, '⊥': `\perp `, '∴': `\therefore `, '∆': `\Delta `, '−': `-`,
	'α': `\alpha `, 'β': `\beta `, 'γ': `\gamma `, 'δ': `\delta `, 'θ': `\theta `, 'λ': `\lambda `,
	'μ': `\mu `, 'π': `\pi `, 'σ': `\sigma `, 'φ': `\varphi `, 'ω': `\omega `, 'Δ': `\Delta `,
	'Σ': `\Sigma `, 'Ω': `\Omega `,
}

func text(s string) string {
	var b strings.Builder
	for _, r := range s {
		if v, ok := symbols[r]; ok {
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

func group(b *strings.Builder, n *Node) error {
	b.WriteString("{")
	if n != nil {
		if err := conv(b, n); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

var naryChr = map[string]string{"": `\int`, "∫": `\int`, "∬": `\iint`, "∮": `\oint`, "∑": `\sum`, "∏": `\prod`, "⋃": `\bigcup`, "⋂": `\bigcap`}
var accChr = map[string]string{"": `\hat`, "̂": `\hat`, "^": `\hat`, "⃗": `\vec`, "→": `\vec`, "̅": `\bar`, "¯": `\bar`, "̃": `\tilde`, "˙": `\dot`, "̇": `\dot`}
var funcNames = map[string]bool{"sin": true, "cos": true, "tan": true, "cot": true, "sec": true, "csc": true, "log": true, "ln": true, "lim": true, "exp": true, "max": true, "min": true}

func conv(b *strings.Builder, n *Node) error {
	for i := range n.Nodes {
		c := &n.Nodes[i]
		var err error
		switch c.XMLName.Local {
		case "r":
			for _, t := range c.children("t") {
				b.WriteString(text(t.Content))
			}
		case "f":
			typ, _ := propVal(c, "fPr", "type")
			if typ == "lin" {
				err = conv(b, c.child("num"))
				b.WriteString("/")
				if err == nil {
					err = conv(b, c.child("den"))
				}
				break
			}
			cmd := `\frac`
			if typ == "noBar" {
				cmd = `\binom`
			}
			b.WriteString(cmd)
			if err = group(b, c.child("num")); err == nil {
				err = group(b, c.child("den"))
			}
		case "sSup":
			if err = group(b, c.child("e")); err == nil {
				b.WriteString("^")
				err = group(b, c.child("sup"))
			}
		case "sSub":
			if err = group(b, c.child("e")); err == nil {
				b.WriteString("_")
				err = group(b, c.child("sub"))
			}
		case "sSubSup":
			if err = group(b, c.child("e")); err == nil {
				b.WriteString("_")
				if err = group(b, c.child("sub")); err == nil {
					b.WriteString("^")
					err = group(b, c.child("sup"))
				}
			}
		case "rad":
			hide, _ := propVal(c, "radPr", "degHide")
			deg := c.child("deg")
			b.WriteString(`\sqrt`)
			if hide != "1" && hide != "on" && deg != nil && len(deg.Nodes) > 0 {
				b.WriteString("[")
				if err = conv(b, deg); err != nil {
					break
				}
				b.WriteString("]")
			}
			err = group(b, c.child("e"))
		case "d":
			beg, ok := propVal(c, "dPr", "begChr")
			if !ok {
				beg = "("
			}
			end, ok := propVal(c, "dPr", "endChr")
			if !ok {
				end = ")"
			}
			sep, ok := propVal(c, "dPr", "sepChr")
			if !ok {
				sep = "|"
			}
			b.WriteString(`\left` + delim(beg) + " ")
			for j, e := range c.children("e") {
				if j > 0 {
					b.WriteString(delim(sep) + " ")
				}
				if err = conv(b, e); err != nil {
					break
				}
			}
			b.WriteString(` \right` + delim(end))
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
					if err = conv(b, e); err != nil {
						break
					}
				}
			}
			b.WriteString(`\end{matrix}`)
		case "nary":
			chr, _ := propVal(c, "naryPr", "chr")
			cmd, ok := naryChr[chr]
			if !ok {
				return UnsupportedError{"nary " + chr}
			}
			b.WriteString(cmd)
			if s := c.child("sub"); s != nil && len(s.Nodes) > 0 {
				b.WriteString("_")
				group(b, s)
			}
			if s := c.child("sup"); s != nil && len(s.Nodes) > 0 {
				b.WriteString("^")
				group(b, s)
			}
			err = group(b, c.child("e"))
		case "func":
			var name strings.Builder
			if err = conv(&name, c.child("fName")); err != nil {
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
			err = group(b, c.child("e"))
		case "limLow":
			if err = group(b, c.child("e")); err == nil {
				b.WriteString("_")
				err = group(b, c.child("lim"))
			}
		case "limUpp":
			if err = group(b, c.child("e")); err == nil {
				b.WriteString("^")
				err = group(b, c.child("lim"))
			}
		case "acc":
			chr, _ := propVal(c, "accPr", "chr")
			cmd, ok := accChr[chr]
			if !ok {
				return UnsupportedError{"acc " + chr}
			}
			b.WriteString(cmd)
			err = group(b, c.child("e"))
		case "bar":
			pos, _ := propVal(c, "barPr", "pos")
			if pos == "bot" {
				b.WriteString(`\underline`)
			} else {
				b.WriteString(`\overline`)
			}
			err = group(b, c.child("e"))
		case "eqArr":
			b.WriteString(`\begin{aligned}`)
			for j, e := range c.children("e") {
				if j > 0 {
					b.WriteString(`\\`)
				}
				if err = conv(b, e); err != nil {
					break
				}
			}
			b.WriteString(`\end{aligned}`)
		case "box", "borderBox", "groupChr", "e", "oMath":
			err = conv(b, c)
		case "rPr", "ctrlPr", "fPr", "radPr", "dPr", "mPr", "naryPr", "funcPr", "accPr", "barPr", "sSupPr", "sSubPr", "sSubSupPr", "limLowPr", "limUppPr", "boxPr", "groupChrPr", "bookmarkStart", "bookmarkEnd", "proofErr":
			// properti tampilan, diabaikan
		default:
			return UnsupportedError{c.XMLName.Local}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func delim(s string) string {
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

// ParseOMML mem-parse fragmen XML m:oMath (untuk uji).
func ParseOMML(s string) (*Node, error) {
	var n Node
	if err := xml.Unmarshal([]byte(s), &n); err != nil {
		return nil, fmt.Errorf("parse omml: %w", err)
	}
	return &n, nil
}
