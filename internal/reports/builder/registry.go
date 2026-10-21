// Package builder merakit query laporan kustom dari daftar sumber dan kolom
// yang sudah ditetapkan di kode (T76, mitigasi R7).
//
// Pengguna hanya memilih kunci sumber, kunci kolom, operator, dan nilai.
// Pengguna tidak pernah menulis SQL: semua potongan SQL berasal dari registry
// di paket ini, dan semua nilai dari pengguna diteruskan sebagai parameter.
package builder

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Batas konfigurasi laporan kustom.
const (
	// MaxColumns adalah jumlah kolom maksimal dalam satu laporan.
	MaxColumns = 15
	// MaxFilters adalah jumlah filter maksimal dalam satu laporan.
	MaxFilters = 5
	// MaxRows adalah jumlah baris maksimal untuk ekspor langsung. Di atasnya
	// laporan dijalankan sebagai job (T49).
	MaxRows = 5000
	// MaxInValues adalah jumlah nilai maksimal pada satu filter `in`.
	MaxInValues = 50
	// MaxOrder adalah jumlah kunci urutan maksimal.
	MaxOrder = 3
)

// ColumnType adalah tipe nilai kolom.
type ColumnType string

const (
	TypeText  ColumnType = "text"
	TypeInt   ColumnType = "int"
	TypeMoney ColumnType = "money" // bilangan bulat rupiah (bigint), tidak pernah pecahan
	TypeDate  ColumnType = "date"
)

func (t ColumnType) valid() bool {
	switch t {
	case TypeText, TypeInt, TypeMoney, TypeDate:
		return true
	}
	return false
}

// Column adalah satu kolom yang boleh dipilih, difilter, dan diurutkan.
type Column struct {
	// Key adalah kunci stabil yang dipakai pengguna dan konfigurasi tersimpan.
	Key string
	// Label adalah judul kolom di tampilan dan ekspor.
	Label string
	Type  ColumnType
	// Expr adalah ekspresi SQL tetap (ditulis di kode, bukan dari pengguna).
	Expr string
}

// Source adalah satu sumber data laporan.
type Source struct {
	// Name adalah kunci sumber, mis. "attendance".
	Name  string
	Label string
	// From adalah klausa FROM beserta JOIN, tanpa kata "FROM".
	From string
	// SchoolExpr adalah ekspresi kolom school_id yang selalu difilter
	// `= $1` dengan school_id dari identitas pengguna.
	SchoolExpr string
	// Where adalah syarat dasar sumber (opsional), digabung dengan AND.
	Where string
	// DefaultOrder adalah urutan tetap yang dipakai setelah urutan pilihan
	// pengguna, agar hasil deterministik. Wajib.
	DefaultOrder string
	// Roles adalah peran yang boleh memakai sumber ini (membangun, menyimpan,
	// dan membaca konfigurasi bersama). Wajib.
	Roles   []string
	Columns []Column
}

// Allowed melaporkan apakah pengguna dengan peran roles boleh memakai sumber.
func (s *Source) Allowed(roles []string) bool {
	for _, r := range roles {
		if slices.Contains(s.Roles, r) {
			return true
		}
	}
	return false
}

// Column mengembalikan kolom dengan kunci tertentu.
func (s *Source) Column(key string) (Column, bool) {
	for _, c := range s.Columns {
		if c.Key == key {
			return c, true
		}
	}
	return Column{}, false
}

// Registry adalah kumpulan sumber yang sudah divalidasi. Aman dipakai
// bersamaan karena tidak berubah setelah dibuat.
type Registry struct {
	sources []Source
}

// Sources mengembalikan salinan daftar sumber sesuai urutan registrasi.
func (r *Registry) Sources() []Source {
	out := make([]Source, len(r.sources))
	copy(out, r.sources)
	return out
}

// Source mengembalikan sumber dengan nama tertentu.
func (r *Registry) Source(name string) (*Source, bool) {
	for i := range r.sources {
		if r.sources[i].Name == name {
			return &r.sources[i], true
		}
	}
	return nil, false
}

var identRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// NewRegistry memvalidasi sumber lalu membuat Registry. Kesalahan di sini
// adalah kesalahan pemrograman, bukan input pengguna.
func NewRegistry(sources ...Source) (*Registry, error) {
	seen := make(map[string]bool, len(sources))
	out := make([]Source, 0, len(sources))
	for _, s := range sources {
		if !identRE.MatchString(s.Name) {
			return nil, fmt.Errorf("builder: nama sumber %q tidak valid", s.Name)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("builder: sumber %q terdaftar dua kali", s.Name)
		}
		seen[s.Name] = true
		if s.Label == "" {
			return nil, fmt.Errorf("builder: sumber %q tanpa label", s.Name)
		}
		for _, req := range []struct{ what, frag string }{
			{"From", s.From}, {"SchoolExpr", s.SchoolExpr}, {"DefaultOrder", s.DefaultOrder},
		} {
			if strings.TrimSpace(req.frag) == "" {
				return nil, fmt.Errorf("builder: sumber %q: %s wajib diisi", s.Name, req.what)
			}
		}
		if len(s.Columns) == 0 {
			return nil, fmt.Errorf("builder: sumber %q tanpa kolom", s.Name)
		}
		if len(s.Roles) == 0 {
			return nil, fmt.Errorf("builder: sumber %q tanpa peran", s.Name)
		}
		for _, frag := range []string{s.From, s.SchoolExpr, s.Where, s.DefaultOrder} {
			if err := checkFragment(frag); err != nil {
				return nil, fmt.Errorf("builder: sumber %q: %w", s.Name, err)
			}
		}
		keys := make(map[string]bool, len(s.Columns))
		for _, c := range s.Columns {
			switch {
			case !identRE.MatchString(c.Key):
				return nil, fmt.Errorf("builder: sumber %q: kunci kolom %q tidak valid", s.Name, c.Key)
			case keys[c.Key]:
				return nil, fmt.Errorf("builder: sumber %q: kunci kolom %q ganda", s.Name, c.Key)
			case c.Label == "":
				return nil, fmt.Errorf("builder: sumber %q: kolom %q tanpa label", s.Name, c.Key)
			case !c.Type.valid():
				return nil, fmt.Errorf("builder: sumber %q: kolom %q bertipe %q tidak dikenal", s.Name, c.Key, c.Type)
			case strings.TrimSpace(c.Expr) == "":
				return nil, fmt.Errorf("builder: sumber %q: kolom %q tanpa ekspresi", s.Name, c.Key)
			}
			if err := checkFragment(c.Expr); err != nil {
				return nil, fmt.Errorf("builder: sumber %q: kolom %q: %w", s.Name, c.Key, err)
			}
			keys[c.Key] = true
		}
		s.Roles = slices.Clone(s.Roles)
		cols := make([]Column, len(s.Columns))
		copy(cols, s.Columns)
		s.Columns = cols
		out = append(out, s)
	}
	return &Registry{sources: out}, nil
}

// checkFragment menolak potongan SQL yang memuat placeholder, pemisah
// perintah, atau komentar. Potongan tetap tidak boleh membawa parameter
// sendiri karena penomoran parameter dipegang compile.go.
func checkFragment(f string) error {
	for _, bad := range []string{"$", ";", "--", "/*"} {
		if strings.Contains(f, bad) {
			return errors.New("potongan SQL memuat " + bad)
		}
	}
	return nil
}
