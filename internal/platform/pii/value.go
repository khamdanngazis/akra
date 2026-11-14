package pii

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// Value membungkus data pribadi yang sudah didekripsi. Semua cara mencetak
// yang umum (fmt, slog, html/template, JSON) hanya menghasilkan versi
// ter-mask, sehingga nilai utuh tidak bocor tanpa sengaja ke log atau
// halaman. Nilai utuh hanya bisa diambil lewat Reveal.
type Value struct {
	plain string
}

// New membuat Value dari teks utuh.
func New(plain string) Value { return Value{plain: plain} }

// IsZero melaporkan apakah nilai kosong.
func (v Value) IsZero() bool { return v.plain == "" }

// Reveal mengembalikan nilai utuh. Pemanggil bertanggung jawab memastikan
// pengguna berhak melihatnya.
func (v Value) Reveal() string { return v.plain }

// Masked mengembalikan versi ter-mask, mis. "3201********0001". Nilai pendek
// (< 10 karakter) hanya menampilkan dua karakter terakhir.
func (v Value) Masked() string {
	return Mask(v.plain)
}

// String dipakai fmt (%v, %s) dan html/template; selalu ter-mask.
func (v Value) String() string { return v.Masked() }

// GoString dipakai %#v; selalu ter-mask.
func (v Value) GoString() string { return "pii.Value(" + v.Masked() + ")" }

// LogValue dipakai slog; selalu ter-mask.
func (v Value) LogValue() slog.Value { return slog.StringValue(v.Masked()) }

// MarshalJSON selalu ter-mask agar nilai utuh tidak ikut ke JSON (mis.
// payload job atau audit). Kirim Reveal() secara eksplisit jika memang perlu.
func (v Value) MarshalJSON() ([]byte, error) { return json.Marshal(v.Masked()) }

// Mask mengganti bagian tengah s dengan bintang.
func Mask(s string) string {
	r := []rune(s)
	switch n := len(r); {
	case n == 0:
		return ""
	case n < 10:
		keep := 2
		if n <= keep {
			return strings.Repeat("*", n)
		}
		return strings.Repeat("*", n-keep) + string(r[n-keep:])
	default:
		return string(r[:4]) + strings.Repeat("*", n-8) + string(r[n-4:])
	}
}
