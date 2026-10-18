package money

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// financeDirs adalah paket di jalur keuangan (relatif ke folder ini).
// Semua nominal di sana int64 rupiah; float tidak boleh muncul sama sekali.
// internal/reports/export tidak termasuk: float di sana hanya untuk tata
// letak dan persentase kehadiran, bukan uang.
var financeDirs = []string{
	".",              // internal/platform/money
	"../../billing",  // tagihan, status, tunggakan, tagihan massal
	"../../payments", // pembayaran, pembatalan, kuitansi
	"../../portal",   // tagihan dan kuitansi di portal orang tua
	"../../home",     // ringkasan keuangan di beranda
	"../../reports",  // laporan tunggakan (tanpa subpaket export)
}

// layoutFiles boleh berisi literal desimal: koordinat tata letak PDF dalam
// milimeter. Nominal di sana tetap teks dari money.FormatRupiah; tipe dan
// konversi float tetap dilarang.
var layoutFiles = map[string]bool{"../../payments/receipt_pdf.go": true}

// floatInSQL menangkap tipe atau cast pecahan di string SQL.
var floatInSQL = regexp.MustCompile(`(?i)::\s*(float[48]?|real|numeric|decimal)\b|\bdouble precision\b|\bto_char\s*\([^)]*\b9`)

// intConstant adalah konstanta bilangan bulat paket math yang boleh dipakai.
var intConstant = regexp.MustCompile(`^(Max|Min)(Int|Uint)(8|16|32|64)?$`)

// floatVerb menangkap format pecahan di string fmt (%f, %.2f, %e, %g).
var floatVerb = regexp.MustCompile(`%[-+# 0]*\d*(\.\d+)?[feEgG]`)

// Uji T60: tidak ada konversi ke float di jalur keuangan. Dicek di kode
// non-uji: tipe float32/float64, fungsi paket math (konstanta bilangan bulat
// seperti math.MinInt64 boleh), strconv.*Float, literal desimal,
// format %f/%g/%e, dan cast pecahan di SQL. Juga migrasi: tidak ada kolom
// pecahan.
func TestNoFloatInFinancePaths(t *testing.T) {
	fset := token.NewFileSet()
	checked := 0
	for _, dir := range financeDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, path, nil, 0)
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
					pkg, ok := x.X.(*ast.Ident)
					switch {
					case !ok:
					case pkg.Name == "strconv" && strings.Contains(x.Sel.Name, "Float"):
						t.Errorf("%s: uses strconv.%s", fset.Position(x.Pos()), x.Sel.Name)
					case pkg.Name == "math" && !intConstant.MatchString(x.Sel.Name):
						t.Errorf("%s: uses math.%s", fset.Position(x.Pos()), x.Sel.Name)
					}
				case *ast.BasicLit:
					if x.Kind == token.FLOAT && !layoutFiles[path] {
						t.Errorf("%s: float literal %s", fset.Position(x.Pos()), x.Value)
					}
					if x.Kind == token.STRING {
						s, _ := strconv.Unquote(x.Value)
						if floatInSQL.MatchString(s) {
							t.Errorf("%s: fractional type in SQL: %q", fset.Position(x.Pos()), floatInSQL.FindString(s))
						}
						if floatVerb.MatchString(s) {
							t.Errorf("%s: fractional format verb %q", fset.Position(x.Pos()), floatVerb.FindString(s))
						}
					}
				}
				return true
			})
		}
	}
	if checked < 30 {
		t.Fatalf("only %d files checked; financeDirs is probably wrong", checked)
	}

	migrations, err := filepath.Glob("../../../db/migrations/*.up.sql")
	if err != nil || len(migrations) == 0 {
		t.Fatalf("migrations not found: %v", err)
	}
	column := regexp.MustCompile(`(?i)\b(float[48]?|real|double precision|numeric|decimal)\b`)
	for _, m := range migrations {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code, _, _ := strings.Cut(line, "--")
			if column.MatchString(code) {
				t.Errorf("%s:%d: fractional column type: %s", filepath.Base(m), i+1, strings.TrimSpace(line))
			}
		}
	}
}

// Pemeriksa di atas memang menangkap pola yang dilarang.
func TestNoFloatPatterns(t *testing.T) {
	for _, s := range []string{"SELECT amount::float", "x::numeric(12,2)", "sum(a)::double precision", "CAST(x AS real)::real"} {
		if !floatInSQL.MatchString(s) {
			t.Errorf("not caught: %q", s)
		}
	}
	for _, s := range []string{"Rp%.2f", "%f", "%8.3g", "%e"} {
		if !floatVerb.MatchString(s) {
			t.Errorf("not caught: %q", s)
		}
	}
	for _, s := range []string{"SELECT amount::bigint", "%d", "%s", "%06d", "100%"} {
		if floatInSQL.MatchString(s) || floatVerb.MatchString(s) {
			t.Errorf("false positive: %q", s)
		}
	}
}
