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
	sheet, data, skipped, err := scanSheet(r, matchHeader, 1)
	if err != nil {
		return Rows{}, err
	}
	return studentRows(sheet, data, skipped), nil
}

// rawRow adalah satu baris data dengan sel yang sudah disusun sesuai urutan
// kolom baku target.
type rawRow struct {
	Line   int
	Values []string
}

// studentRows mengubah baris mentah (urutan StudentColumns) menjadi Rows.
func studentRows(sheet string, data []rawRow, skipped []Skip) Rows {
	out := Rows{Sheet: sheet, Skipped: skipped}
	for _, d := range data {
		row := Row{Line: d.Line}
		copy(row.Values[:], d.Values)
		row.Values[ColBirthDate] = excelDate(row.Values[ColBirthDate])
		out.Data = append(out.Data, row)
	}
	return out
}

// openSheet memeriksa ukuran dan jenis file lalu membuka sheet pertamanya.
func openSheet(r io.Reader) (*excelize.File, string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("importer: read file: %w", err)
	}
	switch {
	case len(data) == 0:
		return nil, "", ErrFileEmpty
	case len(data) > MaxFileSize:
		return nil, "", ErrFileTooLarge
	case !bytes.HasPrefix(data, zipMagic):
		return nil, "", ErrNotXLSX
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: maxUnzipSize, UnzipXMLSizeLimit: maxUnzipSize})
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		f.Close()
		return nil, "", ErrNotXLSX
	}
	return f, sheets[0], nil
}

// scanSheet membaca sheet pertama. Baris berisi diserahkan ke header, yang
// mengembalikan posisi kolom sheet untuk setiap kolom baku target (urutan
// target); baris pertama yang diterima header adalah baris header. Hingga
// headerTries baris berisi pertama dicoba (mis. file ekspor dengan baris
// judul laporan di atas header); jika tidak ada yang cocok, error dari baris
// pertama dikembalikan. Baris kosong dan baris contoh template dilewati;
// baris kosong di akhir sheet tidak dilaporkan. Batas ukuran file, jumlah
// baris, dan panjang sel sama untuk semua target.
func scanSheet(r io.Reader, header func(cells []string) ([]int, error), headerTries int) (string, []rawRow, []Skip, error) {
	f, sheet, err := openSheet(r)
	if err != nil {
		return "", nil, nil, err
	}
	defer f.Close()
	iter, err := f.Rows(sheet)
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	defer iter.Close()

	var cols []int       // cols[i] = indeks kolom sheet untuk kolom baku ke-i
	var pending []string // baris header terakhir (untuk deteksi sub-header)
	var firstErr error
	tries := 0
	var data []rawRow
	var skipped []Skip
	line := 0
	for iter.Next() {
		line++
		if line > maxSheetRows {
			return "", nil, nil, ErrTooManyRows
		}
		// Nilai mentah: tanggal yang diubah Excel menjadi angka seri tetap
		// bisa dikenali, dan angka tidak diformat ulang (mis. 1E+11).
		cells, err := iter.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return "", nil, nil, fmt.Errorf("%w: %v", ErrNotXLSX, err)
		}
		for i := range cells {
			cells[i] = clean(cells[i])
		}
		if cols == nil {
			if isEmpty(cells) {
				continue
			}
			// Header bergrup dua baris (ekspor Dapodik): coba gabungan baris
			// sebelumnya dengan baris ini dulu.
			if pending != nil {
				if combined, ok := combineGroupHeader(pending, cells); ok {
					if c, err := header(combined); err == nil {
						cols, pending = c, nil
						continue
					}
				}
			}
			if cols, err = header(cells); err != nil {
				cols = nil
				pending = cells
				if firstErr == nil {
					firstErr = err
				}
				if tries++; tries >= headerTries {
					return "", nil, nil, firstErr
				}
				continue
			}
			pending = cells
			continue
		}
		if pending != nil {
			// Baris tepat sesudah header yang merupakan sub-header grup tidak
			// ikut sebagai data.
			if _, ok := combineGroupHeader(pending, cells); ok {
				if c, err := header(mustCombine(pending, cells)); err == nil {
					cols = c
				}
				pending = nil
				continue
			}
			pending = nil
		}
		switch {
		case isEmpty(cells):
			skipped = append(skipped, Skip{Line: line, Reason: SkipEmpty})
			continue
		case hasExampleMarker(cells):
			skipped = append(skipped, Skip{Line: line, Reason: SkipExample})
			continue
		}
		if len(data) == MaxDataRows {
			return "", nil, nil, ErrTooManyRows
		}
		row := rawRow{Line: line, Values: make([]string, len(cols))}
		for i, c := range cols {
			if c >= 0 && c < len(cells) {
				row.Values[i] = cells[c]
			}
		}
		data = append(data, row)
	}
	if err := iter.Error(); err != nil {
		return "", nil, nil, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	if cols == nil {
		if firstErr != nil {
			return "", nil, nil, firstErr
		}
		return "", nil, nil, ErrNoHeader
	}
	// Baris kosong di akhir sheet (sisa format template) bukan "dilewati".
	for n := len(skipped); n > 0; n-- {
		s := skipped[n-1]
		if s.Reason != SkipEmpty || (len(data) > 0 && s.Line < data[len(data)-1].Line) {
			break
		}
		skipped = skipped[:n-1]
	}
	if len(data) == 0 {
		return "", nil, nil, ErrNoData
	}
	return sheet, data, skipped, nil
}

// headerScanRows adalah jumlah baris berisi pertama yang diperiksa untuk
// mencari baris header di file contoh (T81, T101).
const headerScanRows = 20

// ReadHeader mengembalikan isi baris header di sheet pertama, untuk membuat
// mapping dari file contoh (T81). Header adalah baris dengan sel berisi
// terbanyak di antara headerScanRows baris berisi pertama, sehingga baris
// judul laporan (mis. "Daftar Peserta Didik" di ekspor Dapodik) dilewati.
// Jika baris sesudahnya berisi sub-header di bawah sel gabungan, keduanya
// digabung menjadi "Grup / Sub" (T101). Sel kosong di tengah ikut (string
// kosong); sel kosong di akhir dibuang.
func ReadHeader(r io.Reader) ([]string, error) {
	f, sheet, err := openSheet(r)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	iter, err := f.Rows(sheet)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	defer iter.Close()
	var rows [][]string
	for line := 1; iter.Next() && line <= maxSheetRows && len(rows) < headerScanRows+1; line++ {
		cells, err := iter.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNotXLSX, err)
		}
		for i := range cells {
			cells[i] = clean(cells[i])
		}
		if isEmpty(cells) {
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return nil, ErrNoHeader
	}
	best := 0
	for i, r := range rows[:min(len(rows), headerScanRows)] {
		if filled(r) > filled(rows[best]) {
			best = i
		}
	}
	cells := rows[best]
	if best+1 < len(rows) {
		if combined, ok := combineGroupHeader(cells, rows[best+1]); ok {
			cells = combined
		}
	}
	for len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells, nil
}

func mustCombine(header, next []string) []string {
	out, _ := combineGroupHeader(header, next)
	return out
}

// Table adalah isi sheet pertama: header (sudah digabung untuk header
// bergrup) dan baris data sesudahnya.
type Table struct {
	Header []string
	Rows   [][]string // sudah di-trim; panjang bisa kurang dari Header
	Lines  []int      // nomor baris Excel untuk setiap Rows
}

// ReadTable membaca header seperti ReadHeader lalu semua baris data
// berisi sesudahnya (maksimal MaxDataRows). Dipakai impor ekspor Dapodik
// (T101) yang memetakan kolom berdasarkan judul.
func ReadTable(r io.Reader) (Table, error) {
	f, sheet, err := openSheet(r)
	if err != nil {
		return Table{}, err
	}
	defer f.Close()
	iter, err := f.Rows(sheet)
	if err != nil {
		return Table{}, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	defer iter.Close()
	type line struct {
		n     int
		cells []string
	}
	var rows []line
	for n := 1; iter.Next(); n++ {
		if n > maxSheetRows {
			return Table{}, ErrTooManyRows
		}
		cells, err := iter.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return Table{}, fmt.Errorf("%w: %v", ErrNotXLSX, err)
		}
		for i := range cells {
			cells[i] = clean(cells[i])
		}
		if isEmpty(cells) {
			continue
		}
		rows = append(rows, line{n, cells})
	}
	if len(rows) == 0 {
		return Table{}, ErrNoHeader
	}
	best := 0
	for i := range rows[:min(len(rows), headerScanRows)] {
		if filled(rows[i].cells) > filled(rows[best].cells) {
			best = i
		}
	}
	t := Table{Header: rows[best].cells}
	dataStart := best + 1
	if dataStart < len(rows) {
		if combined, ok := combineGroupHeader(t.Header, rows[dataStart].cells); ok {
			t.Header, dataStart = combined, dataStart+1
		}
	}
	for _, r := range rows[dataStart:] {
		if hasExampleMarker(r.cells) {
			continue
		}
		if len(t.Rows) == MaxDataRows {
			return Table{}, ErrTooManyRows
		}
		t.Rows, t.Lines = append(t.Rows, r.cells), append(t.Lines, r.n)
	}
	if len(t.Rows) == 0 {
		return Table{}, ErrNoData
	}
	return t, nil
}

// filled menghitung sel berisi.
func filled(cells []string) int {
	n := 0
	for _, c := range cells {
		if c != "" {
			n++
		}
	}
	return n
}

// combineGroupHeader menggabungkan header dengan baris sub-header di
// bawahnya, seperti ekspor Dapodik ("Data Ibu" di atas "Nama", "NIK", ...).
// next dianggap sub-header jika berisi minimal dua sel dan setiap selnya
// berada di bawah sel grup (sel header berisi yang diikuti sel header
// kosong) atau di bawah sel header kosong milik grup itu. Hasilnya: kolom
// bergrup menjadi "Grup / Sub", kolom lain tetap.
func combineGroupHeader(header, next []string) ([]string, bool) {
	if filled(next) < 2 {
		return nil, false
	}
	n := max(len(header), len(next))
	at := func(cells []string, i int) string {
		if i < len(cells) {
			return cells[i]
		}
		return ""
	}
	out := make([]string, n)
	group := ""
	for i := 0; i < n; i++ {
		h, sub := at(header, i), at(next, i)
		switch {
		case h != "" && at(header, i+1) == "" && at(next, i+1) != "" && sub != "":
			group = h // awal grup
		case h != "":
			group = ""
		}
		switch {
		case sub == "":
			out[i] = h
		case group != "":
			out[i] = group + " / " + sub
		default:
			return nil, false // sub-header di bawah kolom biasa: bukan baris sub-header
		}
	}
	return out, true
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
