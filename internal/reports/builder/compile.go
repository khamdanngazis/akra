package builder

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Operator adalah operator filter yang diizinkan.
type Operator string

const (
	OpEq       Operator = "="
	OpNe       Operator = "!="
	OpGte      Operator = ">="
	OpLte      Operator = "<="
	OpContains Operator = "contains"
	OpIn       Operator = "in"
)

// Kesalahan yang berasal dari konfigurasi pengguna. Pemanggil memakai
// errors.Is untuk memetakannya ke pesan di form.
var (
	ErrNoSchool           = errors.New("builder: school_id kosong")
	ErrUnknownSource      = errors.New("builder: sumber tidak dikenal")
	ErrUnknownColumn      = errors.New("builder: kolom tidak dikenal")
	ErrNoColumns          = errors.New("builder: pilih minimal satu kolom")
	ErrDuplicateColumn    = errors.New("builder: kolom dipilih dua kali")
	ErrTooManyColumns     = errors.New("builder: kolom melebihi batas")
	ErrTooManyFilters     = errors.New("builder: filter melebihi batas")
	ErrTooManyOrder       = errors.New("builder: urutan melebihi batas")
	ErrOperatorNotAllowed = errors.New("builder: operator tidak diizinkan")
	ErrInvalidValue       = errors.New("builder: nilai filter tidak valid")
)

// Filter adalah satu syarat. Untuk OpIn isi Values, selain itu isi Value.
type Filter struct {
	Column string
	Op     Operator
	Value  string
	Values []string
}

// Order adalah satu kunci urutan.
type Order struct {
	Column string
	Desc   bool
}

// Config adalah konfigurasi laporan dari pengguna. Semua isinya dianggap
// tidak tepercaya dan divalidasi terhadap registry.
type Config struct {
	Source  string
	Columns []string
	Filters []Filter
	Order   []Order
}

// Options mengatur batas baris.
type Options struct {
	// NoLimit menghilangkan LIMIT. Hanya untuk job ekspor di server (T49).
	NoLimit bool
}

// Query adalah hasil rakitan, siap dijalankan dengan pgx.
type Query struct {
	SQL string
	// Args dimulai dengan school_id sebagai $1.
	Args []any
	// Columns menurut urutan SELECT, untuk judul dan tipe di tampilan/ekspor.
	Columns []Column
	// Limit adalah nilai LIMIT (MaxRows+1) atau 0 bila NoLimit. Jika jumlah
	// baris yang kembali lebih dari MaxRows, laporan harus lewat job T49.
	Limit int
}

// Compile memvalidasi cfg terhadap reg lalu merakit SQL. Hasil selalu sama
// untuk konfigurasi yang sama. schoolID wajib dari auth.CurrentUser(ctx).
func Compile(reg *Registry, schoolID string, cfg Config, opts Options) (Query, error) {
	if schoolID == "" {
		return Query{}, ErrNoSchool
	}
	src, ok := reg.Source(cfg.Source)
	if !ok {
		return Query{}, fmt.Errorf("%w: %q", ErrUnknownSource, cfg.Source)
	}

	switch {
	case len(cfg.Columns) == 0:
		return Query{}, ErrNoColumns
	case len(cfg.Columns) > MaxColumns:
		return Query{}, fmt.Errorf("%w (maksimal %d)", ErrTooManyColumns, MaxColumns)
	case len(cfg.Filters) > MaxFilters:
		return Query{}, fmt.Errorf("%w (maksimal %d)", ErrTooManyFilters, MaxFilters)
	case len(cfg.Order) > MaxOrder:
		return Query{}, fmt.Errorf("%w (maksimal %d)", ErrTooManyOrder, MaxOrder)
	}

	selected := make([]Column, 0, len(cfg.Columns))
	chosen := make(map[string]bool, len(cfg.Columns))
	for _, key := range cfg.Columns {
		col, ok := src.Column(key)
		if !ok {
			return Query{}, fmt.Errorf("%w: %q", ErrUnknownColumn, key)
		}
		if chosen[key] {
			return Query{}, fmt.Errorf("%w: %q", ErrDuplicateColumn, key)
		}
		chosen[key] = true
		selected = append(selected, col)
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	for i, col := range selected {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(col.Expr)
		b.WriteString(` AS "`)
		b.WriteString(col.Key) // aman: kunci dicek identRE saat registrasi
		b.WriteString(`"`)
	}
	b.WriteString(" FROM ")
	b.WriteString(src.From)

	// school_id selalu $1 dan selalu ada, apa pun isi konfigurasi.
	args := []any{schoolID}
	b.WriteString(" WHERE ")
	b.WriteString(src.SchoolExpr)
	b.WriteString(" = $1")
	if strings.TrimSpace(src.Where) != "" {
		b.WriteString(" AND (")
		b.WriteString(src.Where)
		b.WriteString(")")
	}

	for _, f := range cfg.Filters {
		col, ok := src.Column(f.Column)
		if !ok {
			return Query{}, fmt.Errorf("%w: %q", ErrUnknownColumn, f.Column)
		}
		if !opAllowed(col.Type, f.Op) {
			return Query{}, fmt.Errorf("%w: %q untuk kolom %q (%s)", ErrOperatorNotAllowed, f.Op, f.Column, col.Type)
		}
		arg, err := filterArg(col, f)
		if err != nil {
			return Query{}, err
		}
		args = append(args, arg)
		n := strconv.Itoa(len(args))
		b.WriteString(" AND ")
		b.WriteString(col.Expr)
		switch f.Op {
		case OpContains:
			b.WriteString(` ILIKE $` + n + ` ESCAPE '\'`)
		case OpIn:
			b.WriteString(" = ANY($" + n + ")")
		default:
			b.WriteString(" " + string(f.Op) + " $" + n)
		}
	}

	b.WriteString(" ORDER BY ")
	for _, o := range cfg.Order {
		col, ok := src.Column(o.Column)
		if !ok {
			return Query{}, fmt.Errorf("%w: %q", ErrUnknownColumn, o.Column)
		}
		b.WriteString(col.Expr)
		if o.Desc {
			b.WriteString(" DESC")
		}
		b.WriteString(", ")
	}
	b.WriteString(src.DefaultOrder)

	q := Query{Columns: selected}
	if !opts.NoLimit {
		q.Limit = MaxRows + 1
		args = append(args, q.Limit)
		b.WriteString(" LIMIT $" + strconv.Itoa(len(args)))
	}
	q.SQL = b.String()
	q.Args = args
	return q, nil
}

// opAllowed menentukan operator yang sah per tipe kolom. `contains` hanya
// untuk teks; pembanding urutan (>=, <=) tidak untuk teks karena urutan
// teks bergantung pada collation database.
func opAllowed(t ColumnType, op Operator) bool {
	switch op {
	case OpEq, OpNe, OpIn:
		return true
	case OpGte, OpLte:
		return t != TypeText
	case OpContains:
		return t == TypeText
	}
	return false
}

// filterArg mengubah nilai teks dari pengguna menjadi argumen bertipe.
// Nilai tidak pernah disambungkan ke SQL.
func filterArg(col Column, f Filter) (any, error) {
	if f.Op == OpIn {
		if len(f.Values) == 0 || len(f.Values) > MaxInValues {
			return nil, fmt.Errorf("%w: kolom %q: isi 1 sampai %d nilai", ErrInvalidValue, f.Column, MaxInValues)
		}
		switch col.Type {
		case TypeInt, TypeMoney:
			out := make([]int64, 0, len(f.Values))
			for _, v := range f.Values {
				n, err := parseInt(col, v)
				if err != nil {
					return nil, err
				}
				out = append(out, n)
			}
			return out, nil
		case TypeDate:
			out := make([]time.Time, 0, len(f.Values))
			for _, v := range f.Values {
				d, err := parseDate(col, v)
				if err != nil {
					return nil, err
				}
				out = append(out, d)
			}
			return out, nil
		default:
			out := make([]string, len(f.Values))
			copy(out, f.Values)
			return out, nil
		}
	}

	switch col.Type {
	case TypeInt, TypeMoney:
		return parseInt(col, f.Value)
	case TypeDate:
		return parseDate(col, f.Value)
	}
	if f.Op == OpContains {
		if f.Value == "" {
			return nil, fmt.Errorf("%w: kolom %q: kata kunci kosong", ErrInvalidValue, f.Column)
		}
		return "%" + escapeLike(f.Value) + "%", nil
	}
	return f.Value, nil
}

func parseInt(col Column, v string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: kolom %q: bukan bilangan bulat", ErrInvalidValue, col.Key)
	}
	if col.Type == TypeMoney && n < 0 {
		return 0, fmt.Errorf("%w: kolom %q: nominal tidak boleh negatif", ErrInvalidValue, col.Key)
	}
	return n, nil
}

func parseDate(col Column, v string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", strings.TrimSpace(v))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: kolom %q: tanggal harus YYYY-MM-DD", ErrInvalidValue, col.Key)
	}
	return d, nil
}

// escapeLike membuat %, _, dan \ dicocokkan apa adanya (ESCAPE '\').
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
