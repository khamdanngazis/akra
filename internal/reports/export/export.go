// Package export menulis tabel laporan ke Excel (.xlsx) dan PDF. Kedua
// format menerima Table yang sama, sehingga isinya sama dengan layar.
package export

import (
	"strconv"
	"time"
)

// Percent adalah persentase dalam persepuluhan persen (925 = 92,5%); nilai
// negatif berarti belum ada data.
type Percent int

// String memformat Percent seperti di layar, mis. "92,5%" atau "–".
func (p Percent) String() string {
	if p < 0 {
		return "–"
	}
	return strconv.Itoa(int(p)/10) + "," + strconv.Itoa(int(p)%10) + "%"
}

// Money adalah nominal rupiah.
type Money int64

// Column adalah satu kolom tabel.
type Column struct {
	Header string
	// Width lebar kolom PDF dalam mm.
	Width float64
	// Numeric rata kanan (angka, persentase, rupiah).
	Numeric bool
}

// Table adalah isi laporan. Sel bertipe string, int, int64, Percent, atau
// Money.
type Table struct {
	Title      string
	SchoolName string
	// Meta adalah baris keterangan di bawah judul, mis. "Periode: ...".
	Meta      []string
	Columns   []Column
	Rows      [][]any
	Total     []any // baris ringkasan; nil jika tidak ada
	PrintedAt time.Time
	// Landscape untuk PDF dengan banyak kolom.
	Landscape bool
	// Uncompressed hanya untuk uji: teks PDF bisa dicari di isi file.
	Uncompressed bool
}
