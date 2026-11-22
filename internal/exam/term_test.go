package exam

import (
	"bytes"
	"html/template"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"akra/web"
)

// TestTermsDefined memastikan setiap istilah "Apa ini?" yang dipakai di
// template punya nama dan penjelasan (T154).
func TestTermsDefined(t *testing.T) {
	tpl := template.Must(template.ParseFS(web.FS, "templates/partials/term.html"))
	use := regexp.MustCompile(`\{\{template "term" "([a-z_]+)"\}\}`)
	keys := map[string]bool{}
	_ = fs.WalkDir(web.FS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		b, _ := fs.ReadFile(web.FS, p)
		for _, m := range use.FindAllSubmatch(b, -1) {
			keys[string(m[1])] = true
		}
		return nil
	})
	for _, want := range []string{"varian", "kktp", "daya_beda", "lokasi"} {
		if !keys[want] {
			t.Errorf("term %q (T154) not used in any template", want)
		}
	}
	for k := range keys {
		for _, part := range []string{"term_name", "term_text"} {
			var b bytes.Buffer
			if err := tpl.ExecuteTemplate(&b, part, k); err != nil || strings.TrimSpace(b.String()) == "" {
				t.Errorf("term %q: %s empty (%v)", k, part, err)
			}
		}
	}
}
