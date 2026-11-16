package docx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// TestReport mencetak persentase soal yang terimpor sempurna per dokumen
// contoh. Bila KATEX_JS berisi path katex.min.js, setiap LaTeX hasil
// konversi juga divalidasi dengan KaTeX.
func TestReport(t *testing.T) {
	var render func(string) (string, error)
	if p := os.Getenv("KATEX_JS"); p != "" {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		vm := goja.New()
		if _, err := vm.RunString(string(src)); err != nil {
			t.Fatal(err)
		}
		vm.RunString(`function r(s){return katex.renderToString(s,{throwOnError:true})}`)
		if err := vm.ExportTo(vm.Get("r"), &render); err != nil {
			t.Fatal(err)
		}
	}
	total, perfect := 0, 0
	for _, s := range Samples() {
		data, err := Build(s)
		if err != nil {
			t.Fatal(err)
		}
		blocks, err := Read(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		qs := Segment(blocks)
		if len(qs) != s.Questions {
			t.Errorf("%s: got %d questions, want %d", s.Name, len(qs), s.Questions)
		}
		ok := 0
		for _, q := range qs {
			if render != nil {
				for _, part := range append(append([]string{}, q.Stem...), q.Options...) {
					for i, seg := range strings.Split(part, "$") {
						if i%2 == 1 {
							if _, err := render(seg); err != nil {
								q.Problems = append(q.Problems, "katex: "+seg)
							}
						}
					}
				}
			}
			if q.Perfect() {
				ok++
			} else {
				t.Logf("  %s no %s: %v", s.Name, q.Number, q.Problems)
			}
		}
		total += len(qs)
		perfect += ok
		rtl := 0
		for _, q := range qs {
			if q.RTL {
				rtl++
			}
		}
		t.Logf("%-22s soal=%2d sempurna=%2d (%3.0f%%) rtl=%d", s.Name, len(qs), ok, 100*float64(ok)/float64(len(qs)), rtl)
		if testing.Verbose() && len(qs) > 0 {
			q := qs[len(qs)/2]
			t.Logf("  contoh no %s: %q opsi=%d kunci=%s", q.Number, strings.Join(q.Stem, " / "), len(q.Options), q.Key)
		}
	}
	t.Logf("TOTAL soal=%d sempurna=%d (%.0f%%)", total, perfect, 100*float64(perfect)/float64(total))
	fmt.Println()
}

// TestRealDocs membaca semua .docx di DOCX_DIR (dokumen guru sungguhan,
// tidak di-commit) dan mencetak persentase soal yang terimpor sempurna.
func TestRealDocs(t *testing.T) {
	dir := os.Getenv("DOCX_DIR")
	if dir == "" {
		t.Skip("DOCX_DIR is not set")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.docx"))
	if err != nil {
		t.Fatal(err)
	}
	total, perfect := 0, 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		blocks, err := Read(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		qs := Segment(blocks)
		ok := 0
		for _, q := range qs {
			if q.Perfect() {
				ok++
			} else {
				t.Logf("  %s no %s: key=%q problems=%v", filepath.Base(f), q.Number, q.Key, q.Problems)
			}
		}
		total += len(qs)
		perfect += ok
		t.Logf("%-30s soal=%3d sempurna=%3d", filepath.Base(f), len(qs), ok)
	}
	if total > 0 {
		t.Logf("TOTAL soal=%d sempurna=%d (%.0f%%)", total, perfect, 100*float64(perfect)/float64(total))
	}
}
