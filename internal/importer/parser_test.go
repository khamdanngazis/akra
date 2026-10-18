package importer

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// header adalah judul kolom baku sesuai urutan template.
func header() []any {
	h := make([]any, NumColumns)
	for i, c := range StudentColumns {
		h[i] = c.Header
	}
	return h
}

// buildXLSX membuat file .xlsx; rows[n] ditulis di baris lines[n] (default
// berurutan mulai baris 1).
func buildXLSX(t testing.TB, rows [][]any, lines ...int) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, r := range rows {
		line := i + 1
		if i < len(lines) {
			line = lines[i]
		}
		cell, _ := excelize.CoordinatesToCellName(1, line)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func student(nis, name string) []any {
	return []any{nis, name, "2019-05-02", "L", "1A", "Wali " + name, "081234567890", "wali@contoh.id"}
}

func parse(t *testing.T, data []byte) (Rows, error) {
	t.Helper()
	return ParseFile(bytes.NewReader(data))
}

func TestParseFile_ValidWithSkippedRows(t *testing.T) {
	data := buildXLSX(t, [][]any{header(), student("001", "Ani"), {}, student("002", "Budi"), student("003", "Citra")}, 1, 2, 3, 5, 6)
	rows, err := parse(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Data) != 3 || rows.Data[0].Line != 2 || rows.Data[1].Line != 5 || rows.Data[2].Line != 6 {
		t.Fatalf("data = %+v", rows.Data)
	}
	// Baris 3 (ditulis kosong) dan 4 (tidak ada) sama-sama dilewati.
	if fmt.Sprint(rows.Skipped) != "[{3 kosong} {4 kosong}]" {
		t.Errorf("skipped = %v", rows.Skipped)
	}
	got := rows.Data[1].Values
	if got[ColNIS] != "002" || got[ColFullName] != "Budi" || got[ColClass] != "1A" || got[ColGuardianEmail] != "wali@contoh.id" {
		t.Errorf("values = %q", got)
	}
}

func TestParseFile_TrailingEmptyRowsNotReported(t *testing.T) {
	data := buildXLSX(t, [][]any{header(), student("001", "Ani"), {""}, {" "}}, 1, 2, 10, 20)
	rows, err := parse(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Data) != 1 || len(rows.Skipped) != 0 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestParseFile_HeaderOrderAndExtraColumns(t *testing.T) {
	h := []any{"Catatan", "  email WALI ", "Kelas*", "nama   lengkap", "NIS", "Tanggal lahir", "Jenis kelamin", "Nama wali", "Telepon wali"}
	row := []any{"abaikan", "a@b.id", "2B", "Dewi", "010", "2018-01-31", "P", "Ibu Dewi", "0812"}
	rows, err := parse(t, buildXLSX(t, [][]any{h, row}))
	if err != nil {
		t.Fatal(err)
	}
	v := rows.Data[0].Values
	want := [NumColumns]string{"010", "Dewi", "2018-01-31", "P", "2B", "Ibu Dewi", "0812", "a@b.id"}
	if v != want {
		t.Errorf("values = %q, want %q", v, want)
	}
}

func TestParseFile_MissingColumns(t *testing.T) {
	h := header()[1:] // tanpa NIS
	_, err := parse(t, buildXLSX(t, [][]any{h, student("1", "A")[1:]}))
	var missing *MissingColumnsError
	if !errors.As(err, &missing) || missing.Message() != `Kolom "NIS" tidak ditemukan` {
		t.Fatalf("err = %v", err)
	}

	h = header()[:5] // tanpa tiga kolom wali
	_, err = parse(t, buildXLSX(t, [][]any{h}))
	if !errors.As(err, &missing) || missing.Message() != `Kolom "Nama wali", "Telepon wali", dan "Email wali" tidak ditemukan` {
		t.Fatalf("err = %v", err)
	}
}

func TestParseFile_DuplicateColumn(t *testing.T) {
	h := append(header(), "nis")
	_, err := parse(t, buildXLSX(t, [][]any{h, student("1", "A")}))
	var dup *DuplicateColumnError
	if !errors.As(err, &dup) || dup.Column != "NIS" {
		t.Fatalf("err = %v", err)
	}
	// Kolom bukan baku yang dobel tidak masalah.
	h = append(header(), "Catatan", "Catatan")
	if _, err := parse(t, buildXLSX(t, [][]any{h, student("1", "A")})); err != nil {
		t.Fatalf("extra duplicate column: %v", err)
	}
}

func TestParseFile_RejectsNonXLSX(t *testing.T) {
	csv := []byte("NIS,Nama lengkap,Tanggal lahir,Jenis kelamin,Kelas,Nama wali,Telepon wali,Email wali\n001,Ani,,,1A,,,\n")
	if _, err := parse(t, csv); !errors.Is(err, ErrNotXLSX) {
		t.Errorf("renamed csv: err = %v", err)
	}
	// ZIP yang bukan workbook.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("hello.txt")
	_, _ = w.Write([]byte("hello"))
	_ = zw.Close()
	if _, err := parse(t, buf.Bytes()); !errors.Is(err, ErrNotXLSX) {
		t.Errorf("plain zip: err = %v", err)
	}
	// File .xls lama (OLE) dan file .xlsx berpassword sama-sama bukan ZIP.
	if _, err := parse(t, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1rest")); !errors.Is(err, ErrNotXLSX) {
		t.Errorf("ole file: err = %v", err)
	}
	if _, err := parse(t, nil); !errors.Is(err, ErrFileEmpty) {
		t.Errorf("empty: err = %v", err)
	}
}

func TestParseFile_EmptySheets(t *testing.T) {
	if _, err := parse(t, buildXLSX(t, nil)); !errors.Is(err, ErrNoHeader) {
		t.Errorf("blank sheet: err = %v", err)
	}
	if _, err := parse(t, buildXLSX(t, [][]any{header(), {}, {}})); !errors.Is(err, ErrNoData) {
		t.Errorf("header only: err = %v", err)
	}
	tpl, err := StudentTemplate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parse(t, tpl); !errors.Is(err, ErrNoData) {
		t.Errorf("unfilled template: err = %v", err)
	}
}

// Template yang diisi tanpa menghapus baris contoh: baris contoh dilewati.
func TestParseFile_TemplateExampleRowSkipped(t *testing.T) {
	tpl, err := StudentTemplate()
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(tpl))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	row := student("007", "Ani")
	if err := f.SetSheetRow(TemplateSheet, "A3", &row); err != nil {
		t.Fatal(err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parse(t, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Data) != 1 || rows.Data[0].Line != 3 || rows.Data[0].Values[ColNIS] != "007" {
		t.Errorf("data = %+v", rows.Data)
	}
	if len(rows.Skipped) != 1 || rows.Skipped[0] != (Skip{Line: 2, Reason: SkipExample}) {
		t.Errorf("skipped = %v", rows.Skipped)
	}
	if rows.Sheet != TemplateSheet {
		t.Errorf("sheet = %q", rows.Sheet)
	}
}

func TestParseFile_FirstSheetOnly(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	h := header()
	_ = f.SetSheetRow("Sheet1", "A1", &h)
	r := student("001", "Ani")
	_ = f.SetSheetRow("Sheet1", "A2", &r)
	if _, err := f.NewSheet("Lain"); err != nil {
		t.Fatal(err)
	}
	r2 := student("999", "Lain")
	_ = f.SetSheetRow("Lain", "A2", &r2)
	buf, _ := f.WriteToBuffer()
	rows, err := parse(t, buf.Bytes())
	if err != nil || len(rows.Data) != 1 || rows.Data[0].Values[ColNIS] != "001" {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
}

// Tanggal yang diubah Excel menjadi angka seri dikembalikan sebagai
// YYYY-MM-DD; isi lain apa adanya.
func TestParseFile_ExcelDateSerial(t *testing.T) {
	row := student("001", "Ani")
	row[ColBirthDate] = 43587 // 2019-05-02
	row[ColGuardianPhone] = 81234567890
	rows, err := parse(t, buildXLSX(t, [][]any{header(), row}))
	if err != nil {
		t.Fatal(err)
	}
	v := rows.Data[0].Values
	if v[ColBirthDate] != "2019-05-02" || v[ColGuardianPhone] != "81234567890" {
		t.Errorf("values = %q", v)
	}
	for in, want := range map[string]string{"2019-05-02": "2019-05-02", "02/05/2019": "02/05/2019", "0": "0", "43587.5": "43587.5", "abc": "abc"} {
		if got := excelDate(in); got != want {
			t.Errorf("excelDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFile_CellCleanup(t *testing.T) {
	row := student("  001 ", "  Ani  ")
	row[ColGuardianName] = strings.Repeat("x", 3000)
	rows, err := parse(t, buildXLSX(t, [][]any{header(), row}))
	if err != nil {
		t.Fatal(err)
	}
	v := rows.Data[0].Values
	if v[ColNIS] != "001" || v[ColFullName] != "Ani" || len(v[ColGuardianName]) != maxCellLen {
		t.Errorf("values = %q…", v[:2])
	}
}

func TestParseFile_RowLimit(t *testing.T) {
	data := make([][]any, 0, MaxDataRows+2)
	data = append(data, header())
	for i := range MaxDataRows {
		data = append(data, []any{fmt.Sprintf("%05d", i), "Siswa", "", "", "1A"})
	}
	rows, err := parse(t, buildXLSX(t, data))
	if err != nil || len(rows.Data) != MaxDataRows {
		t.Fatalf("%d rows: len = %d, err = %v", MaxDataRows, len(rows.Data), err)
	}
	for i := range 1000 {
		data = append(data, []any{fmt.Sprintf("x%05d", i), "Siswa", "", "", "1A"})
	}
	if _, err := parse(t, buildXLSX(t, data)); !errors.Is(err, ErrTooManyRows) {
		t.Errorf("6000 rows: err = %v", err)
	}
}

// padXLSX menambah entri acak (tidak terkompresi) ke arsip .xlsx sampai
// ukuran file sekitar size byte. Isi workbook tidak berubah.
func padXLSX(t testing.TB, data []byte, size int) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "customXml/padding.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(w, rand.Reader, int64(size-len(data)-1024)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseFile_SizeLimit(t *testing.T) {
	small := buildXLSX(t, [][]any{header(), student("001", "Ani")})
	four := padXLSX(t, small, 4<<20)
	if len(four) < 4<<20-2048 || len(four) > MaxFileSize {
		t.Fatalf("padded size = %d", len(four))
	}
	if rows, err := parse(t, four); err != nil || len(rows.Data) != 1 {
		t.Errorf("4 MB: rows = %d, err = %v", len(rows.Data), err)
	}
	six := padXLSX(t, small, 6<<20)
	if _, err := parse(t, six); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("6 MB: err = %v", err)
	}
}

func TestHeaderKeyAndJoin(t *testing.T) {
	if headerKey("  Nama   Lengkap * ") != "nama lengkap" {
		t.Errorf("headerKey = %q", headerKey("  Nama   Lengkap * "))
	}
	if got := joinID([]string{"a", "b"}); got != "a dan b" {
		t.Errorf("join = %q", got)
	}
}
