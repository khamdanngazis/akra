package grades

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// floatInSQL menangkap tipe float di string SQL. Cast ::numeric boleh:
// kolom score memang desimal eksak (T70).
var floatInSQL = regexp.MustCompile(`(?i)::\s*(float[48]?|real)\b|\bdouble precision\b`)

// floatVerb menangkap format pecahan di string fmt (%f, %.2f, %e, %g).
var floatVerb = regexp.MustCompile(`%[-+# 0]*\d*(\.\d+)?[feEgG]`)

// Uji T71: paket grades tidak memakai float di kode non-uji. Dicek tipe
// float32/float64, paket math, strconv.*Float, literal desimal, format
// %f/%g/%e, dan tipe float di SQL. Uji boleh memakai math/big sebagai
// pembanding.
func TestNoFloatInGrades(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "math" || p == "math/big" {
				t.Errorf("%s: imports %s", name, p)
			}
		}
		f, err = parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if x.Name == "float32" || x.Name == "float64" {
					t.Errorf("%s: uses %s", fset.Position(x.Pos()), x.Name)
				}
			case *ast.SelectorExpr:
				if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "strconv" && strings.Contains(x.Sel.Name, "Float") {
					t.Errorf("%s: uses strconv.%s", fset.Position(x.Pos()), x.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind == token.FLOAT {
					t.Errorf("%s: float literal %s", fset.Position(x.Pos()), x.Value)
				}
				if x.Kind == token.STRING {
					s, _ := strconv.Unquote(x.Value)
					if floatInSQL.MatchString(s) {
						t.Errorf("%s: float type in SQL: %q", fset.Position(x.Pos()), floatInSQL.FindString(s))
					}
					if floatVerb.MatchString(s) {
						t.Errorf("%s: fractional format verb %q", fset.Position(x.Pos()), floatVerb.FindString(s))
					}
				}
			}
			return true
		})
	}
	if checked < 5 {
		t.Fatalf("only %d files checked", checked)
	}
}

// Pemeriksa di atas memang menangkap pola yang dilarang.
func TestNoFloatPatternsGrades(t *testing.T) {
	for _, s := range []string{"x::float8", "y::double precision", "z::real", "%.2f", "%g"} {
		if !floatInSQL.MatchString(s) && !floatVerb.MatchString(s) {
			t.Errorf("pattern %q not caught", s)
		}
	}
	for _, s := range []string{"$5::numeric", "(g.score * 100)::int", "%d,%02d", "%s"} {
		if floatInSQL.MatchString(s) || floatVerb.MatchString(s) {
			t.Errorf("allowed %q caught", s)
		}
	}
}
