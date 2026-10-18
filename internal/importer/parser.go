package importer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	// MaxFileSize adalah ukuran maksimal file impor.
	MaxFileSize = 5 << 20
	// MaxDataRows adalah jumlah maksimal baris data (selain header dan baris
	// yang dilewati) dalam satu file.
	MaxDataRows = 5000
	// maxSheetRows membatasi jumlah baris yang dibaca, termasuk baris kosong,
	// agar file dengan jutaan baris kosong tidak memakan waktu.
	maxSheetRows = 50000
	// maxUnzipSize membatasi ukuran isi file setelah diekstrak (zip bomb).
	maxUnzipSize = 100 << 20
	// maxCellLen membatasi panjang satu sel; sisanya dipotong. Validasi
	// panjang yang sebenarnya ada di T54.
	maxCellLen = 1000
)

var (
	// ErrFileEmpty: tidak ada file atau file kosong.
	ErrFileEmpty = errors.New("importer: empty file")
	// ErrFileTooLarge: file melebihi MaxFileSize.
	ErrFileTooLarge = errors.New("importer: file too large")
	// ErrNotXLSX: isi file bukan workbook .xlsx (mis. CSV yang diganti nama,
	// .xls lama, atau file berpassword).
	ErrNotXLSX = errors.New("importer: not an xlsx file")
	// ErrNoHeader: sheet pertama kosong.
	ErrNoHeader = errors.New("importer: header row not found")
	// ErrNoData: header ada tetapi tidak ada baris data.
	ErrNoData = errors.New("importer: no data rows")
	// ErrTooManyRows: baris data melebihi MaxDataRows.
	ErrTooManyRows = errors.New("importer: too many rows")
)

// MissingColumnsError dikembalikan jika header tidak memuat semua kolom baku.
type MissingColumnsError struct {
	Columns []string
}

func (e *MissingColumnsError) Error() string {
	return "importer: missing columns: " + strings.Join(e.Columns, ", ")
}

// Message adalah pesan untuk pengguna, mis. `Kolom "NIS" tidak ditemukan`.
func (e *MissingColumnsError) Message() string {
	q := make([]string, len(e.Columns))
	for i, c := range e.Columns {
		q[i] = strconv.Quote(c)
	}
	return "Kolom " + joinID(q) + " tidak ditemukan"
}

// DuplicateColumnError dikembalikan jika satu kolom baku muncul dua kali di
// header, sehingga tidak jelas kolom mana yang dipakai.
type DuplicateColumnError struct {
	Column string
}

func (e *DuplicateColumnError) Error() string {
	return "importer: duplicate column: " + e.Column
}

// Message adalah pesan untuk pengguna.
func (e *DuplicateColumnError) Message() string {
	return "Kolom " + strconv.Quote(e.Column) + " muncul lebih dari sekali"
}

// Row adalah satu baris data. Values diindeks dengan Col* (urutan
// StudentColumns), sudah di-trim; Line adalah nomor baris di Excel.
type Row struct {
	Line   int                `json:"line"`
	Values [NumColumns]string `json:"values"`
}

// Skip adalah baris yang dilewati beserta alasannya.
type Skip struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Alasan baris dilewati.
const (
	SkipEmpty   = "kosong"
	SkipExample = "baris contoh"
)

// Rows adalah hasil parsing satu file.
type Rows struct {
	Sheet   string
	Data    []Row
	Skipped []Skip
}

// zipMagic adalah awalan file ZIP; file .xlsx adalah arsip ZIP.
var zipMagic = []byte("PK\x03\x04")

// ParseFile membaca sheet pertama file .xlsx: baris pertama yang berisi
// adalah header, dan kolom dicocokkan dengan StudentColumns berdasarkan nama
// (tidak peka huruf besar dan spasi), jadi urutannya boleh berbeda. Kolom lain
// diabaikan. Baris kosong dan baris contoh template dilewati. ParseFile tidak
// menulis ke database.
func ParseFile(r io.Reader) (Rows, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return Rows{}, fmt.Errorf("importer: read file: %w", err)
	}
	switch {
	case len(data) == 0:
		return Rows{}, ErrFileEmpty
	case len(data) > MaxFileSize:
		return Rows{}, ErrFileTooLarge
	case !bytes.HasPrefix(data, zipMagic):
		return Rows{}, ErrNotXLSX
	}

	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: maxUnzipSize, UnzipXMLSizeLimit: maxUnzipSize})
	if err != nil {
		return Rows{}, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return Rows{}, ErrNotXLSX
	}
	out := Rows{Sheet: sheets[0]}

	iter, err := f.Rows(out.Sheet)
	if err != nil {
		return Rows{}, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	defer iter.Close()

	var cols []int // cols[i] = indeks kolom sheet untuk StudentColumns[i]
	line := 0
	for iter.Next() {
		line++
		if line > maxSheetRows {
			return Rows{}, ErrTooManyRows
		}
		// Nilai mentah: tanggal yang diubah Excel menjadi angka seri tetap
		// bisa dikenali, dan angka tidak diformat ulang (mis. 1E+11).
		cells, err := iter.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return Rows{}, fmt.Errorf("%w: %v", ErrNotXLSX, err)
		}
		for i := range cells {
			cells[i] = clean(cells[i])
		}
		if cols == nil {
			if isEmpty(cells) {
				continue
			}
			if cols, err = matchHeader(cells); err != nil {
				return Rows{}, err
			}
			continue
		}
		switch {
		case isEmpty(cells):
			out.Skipped = append(out.Skipped, Skip{Line: line, Reason: SkipEmpty})
			continue
		case hasExampleMarker(cells):
			out.Skipped = append(out.Skipped, Skip{Line: line, Reason: SkipExample})
			continue
		}
		if len(out.Data) == MaxDataRows {
			return Rows{}, ErrTooManyRows
		}
		row := Row{Line: line}
		for i, c := range cols {
			if c < len(cells) {
				row.Values[i] = cells[c]
			}
		}
		row.Values[ColBirthDate] = excelDate(row.Values[ColBirthDate])
		out.Data = append(out.Data, row)
	}
	if err := iter.Error(); err != nil {
		return Rows{}, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	if cols == nil {
		return Rows{}, ErrNoHeader
	}
	// Baris kosong di akhir sheet (sisa format template) bukan "dilewati".
	for n := len(out.Skipped); n > 0; n-- {
		s := out.Skipped[n-1]
		if s.Reason != SkipEmpty || (len(out.Data) > 0 && s.Line < out.Data[len(out.Data)-1].Line) {
			break
		}
		out.Skipped = out.Skipped[:n-1]
	}
	if len(out.Data) == 0 {
		return Rows{}, ErrNoData
	}
	return out, nil
}

// matchHeader memetakan kolom baku ke posisinya di header.
func matchHeader(cells []string) ([]int, error) {
	pos := make(map[string]int, len(cells))
	for i, c := range cells {
		k := headerKey(c)
		if k == "" {
			continue
		}
		if _, dup := pos[k]; dup {
			for _, sc := range StudentColumns {
				if headerKey(sc.Header) == k {
					return nil, &DuplicateColumnError{Column: sc.Header}
				}
			}
			continue // kolom lain yang dobel tidak dipakai
		}
		pos[k] = i
	}
	cols := make([]int, len(StudentColumns))
	var missing []string
	for i, sc := range StudentColumns {
		p, ok := pos[headerKey(sc.Header)]
		if !ok {
			missing = append(missing, sc.Header)
			continue
		}
		cols[i] = p
	}
	if missing != nil {
		return nil, &MissingColumnsError{Columns: missing}
	}
	return cols, nil
}

// headerKey menormalkan judul kolom: huruf kecil, spasi dirapatkan, dan
// tanda bintang penanda wajib (mis. "NIS*") dibuang.
func headerKey(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "* ")
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// clean merapikan isi sel: spasi di tepi dibuang, termasuk spasi tak terputus
// dari salinan web, dan isi yang terlalu panjang dipotong.
func clean(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", " "))
	if len(s) > maxCellLen {
		s = strings.ToValidUTF8(s[:maxCellLen], "")
	}
	return s
}

func isEmpty(cells []string) bool {
	for _, c := range cells {
		if c != "" {
			return false
		}
	}
	return true
}

func hasExampleMarker(cells []string) bool {
	for _, c := range cells {
		if strings.EqualFold(c, ExampleMarker) {
			return true
		}
	}
	return false
}

// excelDate mengubah angka seri tanggal Excel (sel yang diubah Excel menjadi
// tanggal) menjadi YYYY-MM-DD. Nilai lain dikembalikan apa adanya untuk
// divalidasi di T54.
func excelDate(v string) string {
	n, err := strconv.ParseFloat(v, 64)
	// 1 = 1900-01-01, 109574 = 2199-12-31; angka di luar itu bukan tanggal lahir.
	if err != nil || n < 1 || n > 109574 || n != float64(int(n)) {
		return v
	}
	t, err := excelize.ExcelDateToTime(n, false)
	if err != nil {
		return v
	}
	return t.Format(time.DateOnly)
}

// joinID menggabungkan daftar dengan koma dan "dan": "a", "a dan b",
// "a, b, dan c".
func joinID(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " dan " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", dan " + items[len(items)-1]
}
