package main

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// Aturan tata letak halaman staf (T162):
//   - setiap halaman punya satu header halaman berisi <h1>: .page-header,
//     atau .work-head di layar kerja (pengawas, pusat kendali);
//   - header berisi paling banyak satu tombol utama (.btn-primary) dan dua
//     tombol di luar menu "Lainnya";
//   - tidak ada kartu (.card) di dalam kartu lain;
//   - tombol berbahaya (merah) hanya di dalam menu (<details>) atau form
//     dengan konfirmasi.
//
// pageLint mengembalikan pelanggaran pada HTML halaman.
func pageLint(body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return []string{"parse: " + err.Error()}
	}
	var out []string
	headers := 0
	var walk func(n *html.Node, inCard, inMenu, inConfirm bool)
	walk = func(n *html.Node, inCard, inMenu, inConfirm bool) {
		if n.Type == html.ElementNode {
			cls := nodeClasses(n)
			switch {
			case slices.Contains(cls, "page-header") || slices.Contains(cls, "work-head"):
				headers++
				if p := countClass(n, "btn-primary"); p > 1 {
					out = append(out, fmt.Sprintf("page header has %d primary buttons", p))
				}
				if b := visibleButtons(n); b > 2 {
					out = append(out, fmt.Sprintf("page header has %d buttons outside the Lainnya menu", b))
				}
				if !hasTag(n, "h1") {
					out = append(out, "page header without <h1>")
				}
			case slices.Contains(cls, "card"):
				if inCard {
					out = append(out, "card inside card: "+describe(n))
				}
				inCard = true
			}
			if n.Data == "details" {
				inMenu = true
			}
			if n.Data == "form" && (attr(n, "data-confirm") != "" || strings.Contains(attr(n, "x-on:submit")+attr(n, "@submit")+attr(n, "onsubmit"), "confirm(")) {
				inConfirm = true
			}
			if isButton(n, cls) && danger(cls) && !inMenu && !inConfirm {
				out = append(out, "danger button outside a menu or confirmation: "+describe(n))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inCard, inMenu, inConfirm)
		}
	}
	walk(doc, false, false, false)
	if headers != 1 {
		out = append(out, fmt.Sprintf("%d page headers", headers))
	}
	return out
}

func nodeClasses(n *html.Node) []string { return strings.Fields(attr(n, "class")) }

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func countClass(n *html.Node, class string) int {
	c := 0
	if n.Type == html.ElementNode && slices.Contains(nodeClasses(n), class) {
		c++
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c += countClass(ch, class)
	}
	return c
}

func hasTag(n *html.Node, tag string) bool {
	if n.Type == html.ElementNode && n.Data == tag {
		return true
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if hasTag(ch, tag) {
			return true
		}
	}
	return false
}

func isButton(n *html.Node, cls []string) bool {
	return n.Data == "button" || slices.Contains(cls, "btn")
}

func danger(cls []string) bool {
	return slices.Contains(cls, "btn-danger") || slices.ContainsFunc(cls, func(c string) bool { return strings.HasPrefix(c, "text-red-") })
}

func describe(n *html.Node) string {
	var b strings.Builder
	var text func(*html.Node)
	text = func(m *html.Node) {
		if m.Type == html.TextNode {
			b.WriteString(strings.TrimSpace(m.Data) + " ")
		}
		for c := m.FirstChild; c != nil && b.Len() < 60; c = c.NextSibling {
			text(c)
		}
	}
	text(n)
	return fmt.Sprintf("<%s class=%q> %s", n.Data, attr(n, "class"), strings.TrimSpace(b.String()))
}

// visibleButtons menghitung tombol (.btn) di n yang tidak berada di dalam
// menu (<details>), tidak termasuk tombol pembuka menu itu sendiri.
func visibleButtons(n *html.Node) int {
	c := 0
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode || ch.Data == "details" {
			continue
		}
		if slices.Contains(nodeClasses(ch), "btn") && !hasAttr(ch, "hidden") {
			c++
		}
		c += visibleButtons(ch)
	}
	return c
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
