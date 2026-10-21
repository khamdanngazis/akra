package builder

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testSchool = "11111111-1111-1111-1111-111111111111"

// bigRegistry punya satu sumber dengan 16 kolom untuk menguji batas kolom.
func bigRegistry(t *testing.T) *Registry {
	t.Helper()
	cols := make([]Column, 0, MaxColumns+1)
	for i := 0; i <= MaxColumns; i++ {
		cols = append(cols, Column{
			Key: fmt.Sprintf("c%d", i), Label: fmt.Sprintf("C%d", i),
			Type: TypeText, Expr: fmt.Sprintf("t.c%d", i),
		})
	}
	r, err := NewRegistry(Source{
		Name: "big", Label: "Besar", From: "t", SchoolExpr: "t.school_id",
		DefaultOrder: "t.id", Roles: []string{"admin_tu"}, Columns: cols,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustCompile(t *testing.T, cfg Config) Query {
	t.Helper()
	q, err := Compile(Default, testSchool, cfg, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return q
}

func TestDefaultRegistryValid(t *testing.T) {
	got := make([]string, 0, 4)
	for _, s := range Default.Sources() {
		got = append(got, s.Name)
	}
	want := []string{"attendance", "arrears", "payments", "grades"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sumber = %v, want %v", got, want)
	}
}

func TestCompile_RejectsUnknownColumnKey(t *testing.T) {
	cases := map[string]Config{
		"select": {Source: "payments", Columns: []string{"nis", "password"}},
		"filter": {Source: "payments", Columns: []string{"nis"},
			Filters: []Filter{{Column: "1=1 --", Op: OpEq, Value: "x"}}},
		"order": {Source: "payments", Columns: []string{"nis"},
			Order: []Order{{Column: "st.nis; DROP TABLE students"}}},
		"expr, bukan kunci": {Source: "payments", Columns: []string{"st.nis"}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile(Default, testSchool, cfg, Options{}); !errors.Is(err, ErrUnknownColumn) {
				t.Fatalf("err = %v, want ErrUnknownColumn", err)
			}
		})
	}
}

func TestCompile_RejectsUnknownSource(t *testing.T) {
	_, err := Compile(Default, testSchool, Config{Source: "students; DROP", Columns: []string{"nis"}}, Options{})
	if !errors.Is(err, ErrUnknownSource) {
		t.Fatalf("err = %v, want ErrUnknownSource", err)
	}
}

func TestCompile_RejectsOperatorOutsideList(t *testing.T) {
	ops := []Operator{"LIKE", "ILIKE", "<", ">", "<>", "OR", "= 1 OR 1=1 --", "", "IN", "Contains", "is null"}
	for _, op := range ops {
		t.Run(string(op), func(t *testing.T) {
			_, err := Compile(Default, testSchool, Config{
				Source: "payments", Columns: []string{"nis"},
				Filters: []Filter{{Column: "nis", Op: op, Value: "x"}},
			}, Options{})
			if !errors.Is(err, ErrOperatorNotAllowed) {
				t.Fatalf("err = %v, want ErrOperatorNotAllowed", err)
			}
		})
	}
}

func TestCompile_OperatorPerType(t *testing.T) {
	bad := []struct {
		col string
		op  Operator
	}{
		{"nis", OpGte},         // teks: urutan bergantung collation
		{"nis", OpLte},         // teks
		{"amount", OpContains}, // contains hanya teks
		{"payment_date", OpContains},
	}
	for _, b := range bad {
		_, err := Compile(Default, testSchool, Config{
			Source: "payments", Columns: []string{"nis"},
			Filters: []Filter{{Column: b.col, Op: b.op, Value: "1"}},
		}, Options{})
		if !errors.Is(err, ErrOperatorNotAllowed) {
			t.Errorf("%s %s: err = %v, want ErrOperatorNotAllowed", b.col, b.op, err)
		}
	}
	for _, op := range []Operator{OpEq, OpNe, OpGte, OpLte} {
		mustCompile(t, Config{Source: "payments", Columns: []string{"nis"},
			Filters: []Filter{{Column: "amount", Op: op, Value: "150000"}}})
	}
}

func TestCompile_FilterValueIsParameterNotSQL(t *testing.T) {
	evil := "'; DROP TABLE students; --"
	for _, op := range []Operator{OpEq, OpNe, OpContains} {
		t.Run(string(op), func(t *testing.T) {
			q := mustCompile(t, Config{
				Source: "payments", Columns: []string{"nis"},
				Filters: []Filter{{Column: "student_name", Op: op, Value: evil}},
			})
			if strings.Contains(strings.ToUpper(q.SQL), "DROP") || strings.Contains(q.SQL, "'; ") {
				t.Fatalf("nilai bocor ke SQL: %s", q.SQL)
			}
			found := false
			for _, a := range q.Args {
				if s, ok := a.(string); ok && strings.Contains(s, evil) {
					found = true
				}
			}
			if !found {
				t.Fatalf("nilai tidak ada di Args: %#v", q.Args)
			}
		})
	}

	t.Run("in", func(t *testing.T) {
		q := mustCompile(t, Config{
			Source: "payments", Columns: []string{"nis"},
			Filters: []Filter{{Column: "method", Op: OpIn, Values: []string{"tunai", evil}}},
		})
		if strings.Contains(strings.ToUpper(q.SQL), "DROP") {
			t.Fatalf("nilai bocor ke SQL: %s", q.SQL)
		}
		got, ok := q.Args[1].([]string)
		if !ok || !reflect.DeepEqual(got, []string{"tunai", evil}) {
			t.Fatalf("Args[1] = %#v", q.Args[1])
		}
	})
}

func TestCompile_TypedValuesRejectGarbage(t *testing.T) {
	cases := []Filter{
		{Column: "amount", Op: OpEq, Value: "1; DROP TABLE payments"},
		{Column: "amount", Op: OpEq, Value: "150.5"},
		{Column: "amount", Op: OpEq, Value: "-1"},
		{Column: "amount", Op: OpIn, Values: []string{"1", "abc"}},
		{Column: "payment_date", Op: OpEq, Value: "2026-13-45"},
		{Column: "payment_date", Op: OpGte, Value: "'; DROP TABLE x; --"},
		{Column: "student_name", Op: OpContains, Value: ""},
		{Column: "method", Op: OpIn},
	}
	for _, f := range cases {
		_, err := Compile(Default, testSchool, Config{
			Source: "payments", Columns: []string{"nis"}, Filters: []Filter{f},
		}, Options{})
		if !errors.Is(err, ErrInvalidValue) {
			t.Errorf("%+v: err = %v, want ErrInvalidValue", f, err)
		}
	}

	over := make([]string, MaxInValues+1)
	for i := range over {
		over[i] = "x"
	}
	_, err := Compile(Default, testSchool, Config{
		Source: "payments", Columns: []string{"nis"},
		Filters: []Filter{{Column: "method", Op: OpIn, Values: over}},
	}, Options{})
	if !errors.Is(err, ErrInvalidValue) {
		t.Errorf("in terlalu banyak: err = %v", err)
	}
}

func TestCompile_TypedArgs(t *testing.T) {
	q := mustCompile(t, Config{
		Source: "payments", Columns: []string{"nis"},
		Filters: []Filter{
			{Column: "amount", Op: OpGte, Value: " 150000 "},
			{Column: "payment_date", Op: OpLte, Value: "2026-10-21"},
			{Column: "amount", Op: OpIn, Values: []string{"1", "2"}},
			{Column: "payment_date", Op: OpIn, Values: []string{"2026-10-01"}},
		},
	})
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	want := []any{
		testSchool, int64(150000), day("2026-10-21"),
		[]int64{1, 2}, []time.Time{day("2026-10-01")}, MaxRows + 1,
	}
	if !reflect.DeepEqual(q.Args, want) {
		t.Fatalf("Args = %#v\nwant   %#v", q.Args, want)
	}
}

func TestCompile_ContainsEscapesWildcards(t *testing.T) {
	q := mustCompile(t, Config{
		Source: "payments", Columns: []string{"nis"},
		Filters: []Filter{{Column: "student_name", Op: OpContains, Value: `100%_a\b`}},
	})
	if got, want := q.Args[1], `%100\%\_a\\b%`; got != want {
		t.Fatalf("pola = %q, want %q", got, want)
	}
	if !strings.Contains(q.SQL, `ILIKE $2 ESCAPE '\'`) {
		t.Fatalf("SQL tanpa ESCAPE: %s", q.SQL)
	}
}

func TestCompile_EveryQueryScopedBySchool(t *testing.T) {
	for _, src := range Default.Sources() {
		keys := make([]string, 0, len(src.Columns))
		for _, c := range src.Columns {
			keys = append(keys, c.Key)
		}
		cfgs := map[string]Config{
			"polos": {Source: src.Name, Columns: keys},
			"dengan filter dan urutan": {
				Source: src.Name, Columns: keys[:1],
				Filters: []Filter{{Column: keys[0], Op: OpEq, Value: "1"}},
				Order:   []Order{{Column: keys[0], Desc: true}},
			},
		}
		for name, cfg := range cfgs {
			t.Run(src.Name+"/"+name, func(t *testing.T) {
				if cfg.Filters != nil && src.Columns[0].Type != TypeText {
					t.Skip("kolom pertama bukan teks")
				}
				q := mustCompile(t, cfg)
				if !strings.Contains(q.SQL, "school_id = $1") {
					t.Fatalf("tanpa school_id = $1: %s", q.SQL)
				}
				if q.Args[0] != testSchool {
					t.Fatalf("Args[0] = %v", q.Args[0])
				}
				// $n hanya boleh berurutan dari 1.
				for i := range q.Args {
					if !strings.Contains(q.SQL, fmt.Sprintf("$%d", i+1)) {
						t.Fatalf("$%d tidak dipakai: %s", i+1, q.SQL)
					}
				}
			})
		}
	}
}

func TestCompile_SchoolFilterSurvivesBaseWhere(t *testing.T) {
	// school_id harus di luar kurung Where sumber, sehingga OR di Where
	// tidak bisa melewatinya.
	r, err := NewRegistry(Source{
		Name: "x", Label: "X", From: "t", SchoolExpr: "t.school_id",
		Where: "t.a = 1 OR t.b = 2", DefaultOrder: "t.id", Roles: []string{"admin_tu"},
		Columns: []Column{{Key: "a", Label: "A", Type: TypeText, Expr: "t.a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	q, err := Compile(r, testSchool, Config{Source: "x", Columns: []string{"a"}}, Options{NoLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT t.a AS "a" FROM t WHERE t.school_id = $1 AND (t.a = 1 OR t.b = 2) ORDER BY t.id`
	if q.SQL != want {
		t.Fatalf("SQL = %s\nwant  %s", q.SQL, want)
	}
}

func TestCompile_RequiresSchool(t *testing.T) {
	_, err := Compile(Default, "", Config{Source: "payments", Columns: []string{"nis"}}, Options{})
	if !errors.Is(err, ErrNoSchool) {
		t.Fatalf("err = %v, want ErrNoSchool", err)
	}
}

func TestCompile_Limits(t *testing.T) {
	big := bigRegistry(t)
	keys := make([]string, 0, MaxColumns+1)
	for i := 0; i <= MaxColumns; i++ {
		keys = append(keys, fmt.Sprintf("c%d", i))
	}

	if _, err := Compile(big, testSchool, Config{Source: "big", Columns: keys[:MaxColumns]}, Options{}); err != nil {
		t.Fatalf("15 kolom harus lolos: %v", err)
	}
	if _, err := Compile(big, testSchool, Config{Source: "big", Columns: keys}, Options{}); !errors.Is(err, ErrTooManyColumns) {
		t.Fatalf("16 kolom: err = %v, want ErrTooManyColumns", err)
	}

	filters := func(n int) []Filter {
		out := make([]Filter, n)
		for i := range out {
			out[i] = Filter{Column: "c0", Op: OpEq, Value: "x"}
		}
		return out
	}
	if _, err := Compile(big, testSchool, Config{Source: "big", Columns: keys[:1], Filters: filters(MaxFilters)}, Options{}); err != nil {
		t.Fatalf("5 filter harus lolos: %v", err)
	}
	if _, err := Compile(big, testSchool, Config{Source: "big", Columns: keys[:1], Filters: filters(MaxFilters + 1)}, Options{}); !errors.Is(err, ErrTooManyFilters) {
		t.Fatalf("6 filter: err = %v, want ErrTooManyFilters", err)
	}

	if _, err := Compile(big, testSchool, Config{Source: "big", Columns: nil}, Options{}); !errors.Is(err, ErrNoColumns) {
		t.Fatalf("tanpa kolom: err = %v", err)
	}
	if _, err := Compile(big, testSchool, Config{Source: "big", Columns: []string{"c0", "c0"}}, Options{}); !errors.Is(err, ErrDuplicateColumn) {
		t.Fatalf("kolom ganda: err = %v", err)
	}
	order := []Order{{Column: "c0"}, {Column: "c1"}, {Column: "c2"}, {Column: "c3"}}
	if _, err := Compile(big, testSchool, Config{Source: "big", Columns: keys[:1], Order: order}, Options{}); !errors.Is(err, ErrTooManyOrder) {
		t.Fatalf("urutan: err = %v", err)
	}
}

func TestCompile_RowLimit(t *testing.T) {
	cfg := Config{Source: "payments", Columns: []string{"nis"}}

	q := mustCompile(t, cfg)
	if q.Limit != MaxRows+1 || q.Args[len(q.Args)-1] != MaxRows+1 {
		t.Fatalf("Limit = %d, Args = %#v", q.Limit, q.Args)
	}
	if !strings.HasSuffix(q.SQL, fmt.Sprintf(" LIMIT $%d", len(q.Args))) {
		t.Fatalf("LIMIT tidak di akhir: %s", q.SQL)
	}

	nq, err := Compile(Default, testSchool, cfg, Options{NoLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	if nq.Limit != 0 || strings.Contains(nq.SQL, "LIMIT") || len(nq.Args) != 1 {
		t.Fatalf("NoLimit: %+v", nq)
	}
}

func TestCompile_Golden(t *testing.T) {
	q := mustCompile(t, Config{
		Source:  "payments",
		Columns: []string{"nis", "amount"},
		Filters: []Filter{
			{Column: "amount", Op: OpGte, Value: "100000"},
			{Column: "method", Op: OpIn, Values: []string{"tunai", "transfer"}},
		},
		Order: []Order{{Column: "amount", Desc: true}},
	})
	want := `SELECT st.nis AS "nis", p.amount AS "amount" FROM payments p
 JOIN invoices i ON i.school_id = p.school_id AND i.id = p.invoice_id
 JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
 JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
 JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id WHERE p.school_id = $1 AND p.amount >= $2 AND p.method = ANY($3) ORDER BY p.amount DESC, p.paid_on DESC, p.receipt_number LIMIT $4`
	if q.SQL != want {
		t.Fatalf("SQL = %s\nwant  %s", q.SQL, want)
	}
	if got := []string{q.Columns[0].Label, q.Columns[1].Label}; !reflect.DeepEqual(got, []string{"NIS", "Nominal"}) {
		t.Fatalf("Columns = %v", got)
	}
}

func TestCompile_Deterministic(t *testing.T) {
	cfg := Config{
		Source:  "arrears",
		Columns: []string{"nis", "student_name", "remaining", "days_overdue"},
		Filters: []Filter{
			{Column: "class_name", Op: OpIn, Values: []string{"1A", "1B"}},
			{Column: "remaining", Op: OpGte, Value: "50000"},
		},
		Order: []Order{{Column: "remaining", Desc: true}, {Column: "student_name"}},
	}
	first := mustCompile(t, cfg)
	for i := 0; i < 100; i++ {
		got := mustCompile(t, cfg)
		if got.SQL != first.SQL || !reflect.DeepEqual(got.Args, first.Args) {
			t.Fatalf("hasil berbeda pada pemanggilan ke-%d", i)
		}
	}
}

func TestCompile_DoesNotMutateConfig(t *testing.T) {
	cfg := Config{
		Source: "payments", Columns: []string{"nis"},
		Filters: []Filter{{Column: "method", Op: OpIn, Values: []string{"tunai"}}},
	}
	q := mustCompile(t, cfg)
	q.Args[1].([]string)[0] = "diubah"
	if cfg.Filters[0].Values[0] != "tunai" {
		t.Fatal("Args berbagi memori dengan Config")
	}
}

func TestNewRegistry_RejectsBadSources(t *testing.T) {
	ok := func() Source {
		return Source{
			Name: "s", Label: "S", From: "t", SchoolExpr: "t.school_id", DefaultOrder: "t.id", Roles: []string{"admin_tu"},
			Columns: []Column{{Key: "a", Label: "A", Type: TypeText, Expr: "t.a"}},
		}
	}
	if _, err := NewRegistry(ok()); err != nil {
		t.Fatalf("sumber valid ditolak: %v", err)
	}

	mut := map[string]func(*Source){
		"nama kapital":       func(s *Source) { s.Name = "Sumber" },
		"tanpa school_id":    func(s *Source) { s.SchoolExpr = "" },
		"tanpa urutan":       func(s *Source) { s.DefaultOrder = " " },
		"tanpa kolom":        func(s *Source) { s.Columns = nil },
		"tanpa peran":        func(s *Source) { s.Roles = nil },
		"kunci tak valid":    func(s *Source) { s.Columns[0].Key = `a"; DROP` },
		"kunci ganda":        func(s *Source) { s.Columns = append(s.Columns, s.Columns[0]) },
		"tipe tak dikenal":   func(s *Source) { s.Columns[0].Type = "float" },
		"tanpa ekspresi":     func(s *Source) { s.Columns[0].Expr = "" },
		"placeholder":        func(s *Source) { s.Columns[0].Expr = "t.a = $1" },
		"titik koma":         func(s *Source) { s.From = "t; DROP TABLE x" },
		"komentar":           func(s *Source) { s.Where = "t.a = 1 -- x" },
		"komentar blok":      func(s *Source) { s.DefaultOrder = "t.id /* x */" },
		"tanpa label sumber": func(s *Source) { s.Label = "" },
	}
	for name, f := range mut {
		t.Run(name, func(t *testing.T) {
			s := ok()
			f(&s)
			if _, err := NewRegistry(s); err == nil {
				t.Fatal("seharusnya ditolak")
			}
		})
	}

	if _, err := NewRegistry(ok(), ok()); err == nil {
		t.Fatal("nama sumber ganda seharusnya ditolak")
	}
}

// Kolom nominal dan nilai tidak boleh berubah menjadi pecahan atau float
// secara tidak sengaja (aturan proyek: tanpa float di paket keuangan dan nilai).
func TestDefaultSources_NoFloatSQL(t *testing.T) {
	for _, s := range Default.Sources() {
		frags := []string{s.From, s.Where, s.DefaultOrder}
		for _, c := range s.Columns {
			frags = append(frags, c.Expr)
			if c.Type == TypeMoney && strings.Contains(c.Expr, "::int)") {
				t.Errorf("%s.%s: nominal harus bigint", s.Name, c.Key)
			}
		}
		for _, f := range frags {
			l := strings.ToLower(f)
			for _, bad := range []string{"float", "real", "double", "numeric", "decimal", "round("} {
				if strings.Contains(l, bad) {
					t.Errorf("sumber %s memuat %q: %s", s.Name, bad, f)
				}
			}
		}
	}
}
