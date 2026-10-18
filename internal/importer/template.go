// Package importer berisi impor data siswa dan orang tua dari Excel.
package importer

import (
	"fmt"

	"github.com/xuri/excelize/v2"

	"akra/internal/students"
)

// Column adalah satu kolom baku template impor siswa.
type Column struct {
	Header   string
	Required bool
	Format   string // keterangan format untuk halaman impor
}

// Indeks kolom di StudentColumns; urutan kolom di Excel tetap.
const (
	ColNIS = iota
	ColFullName
	ColBirthDate
	ColGender
	ColClass
	ColGuardianName
	ColGuardianPhone
	ColGuardianEmail
)

// StudentColumns adalah kolom template impor siswa (dipakai juga parser T53).
var StudentColumns = []Column{
	ColNIS:           {Header: "NIS", Required: true, Format: "Teks, misalnya 007"},
	ColFullName:      {Header: "Nama lengkap", Required: true, Format: "Teks"},
	ColBirthDate:     {Header: "Tanggal lahir", Format: "YYYY-MM-DD, misalnya 2019-05-02"},
	ColGender:        {Header: "Jenis kelamin", Format: "L atau P"},
	ColClass:         {Header: "Kelas", Required: true, Format: "Nama kelas, misalnya 1A"},
	ColGuardianName:  {Header: "Nama wali", Format: "Teks"},
	ColGuardianPhone: {Header: "Telepon wali", Format: "Teks, misalnya 081234567890"},
	ColGuardianEmail: {Header: "Email wali", Format: "Email"},
}

const (
	// TemplateSheet adalah nama sheet data di template.
	TemplateSheet = "Siswa"
	// ExampleMarker ditulis di kolom setelah kolom data pada baris contoh.
	ExampleMarker = "contoh, hapus baris ini"
	// templateRows adalah jumlah baris data yang diberi format teks dan dropdown.
	templateRows = 2000
)

var exampleRow = []string{
	ColNIS:           "007",
	ColFullName:      "Siti Aminah",
	ColBirthDate:     "2019-05-02",
	ColGender:        students.GenderFemale,
	ColClass:         "1A",
	ColGuardianName:  "Aminah",
	ColGuardianPhone: "081234567890",
	ColGuardianEmail: "aminah@contoh.id",
}

// numFmtText adalah format bawaan Excel "@" (teks): 007 tidak menjadi 7.
const numFmtText = 49

// StudentTemplate membuat file template impor siswa (.xlsx).
func StudentTemplate() ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", TemplateSheet); err != nil {
		return nil, err
	}

	textStyle, err := f.NewStyle(&excelize.Style{NumFmt: numFmtText})
	if err != nil {
		return nil, err
	}
	headerStyle, err := f.NewStyle(&excelize.Style{
		NumFmt: numFmtText,
		Font:   &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:   excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"0B3D91"}},
	})
	if err != nil {
		return nil, err
	}
	exampleStyle, err := f.NewStyle(&excelize.Style{
		NumFmt: numFmtText,
		Font:   &excelize.Font{Italic: true, Color: "64748B"},
		Fill:   excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E2E8F0"}},
	})
	if err != nil {
		return nil, err
	}

	lastCol, err := excelize.ColumnNumberToName(len(StudentColumns))
	if err != nil {
		return nil, err
	}
	markerCol, err := excelize.ColumnNumberToName(len(StudentColumns) + 1)
	if err != nil {
		return nil, err
	}
	// Semua kolom data bertipe teks agar Excel tidak mengubah NIS, telepon, atau tanggal.
	if err := f.SetCellStyle(TemplateSheet, "A3", fmt.Sprintf("%s%d", lastCol, templateRows+1), textStyle); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(TemplateSheet, "A", lastCol, 20); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(TemplateSheet, markerCol, markerCol, 26); err != nil {
		return nil, err
	}

	for i, c := range StudentColumns {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetCellStr(TemplateSheet, col+"1", c.Header); err != nil {
			return nil, err
		}
		if err := f.SetCellStr(TemplateSheet, col+"2", exampleRow[i]); err != nil {
			return nil, err
		}
	}
	if err := f.SetCellStr(TemplateSheet, markerCol+"2", ExampleMarker); err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(TemplateSheet, "A1", lastCol+"1", headerStyle); err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(TemplateSheet, "A2", markerCol+"2", exampleStyle); err != nil {
		return nil, err
	}

	genderCol, _ := excelize.ColumnNumberToName(ColGender + 1)
	dv := excelize.NewDataValidation(true)
	dv.Sqref = fmt.Sprintf("%s2:%s%d", genderCol, genderCol, templateRows+1)
	if err := dv.SetDropList([]string{students.GenderMale, students.GenderFemale}); err != nil {
		return nil, err
	}
	dv.SetError(excelize.DataValidationErrorStyleStop, "Jenis kelamin", "Isi L atau P, atau kosongkan.")
	if err := f.AddDataValidation(TemplateSheet, dv); err != nil {
		return nil, err
	}

	if err := f.SetPanes(TemplateSheet, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	}); err != nil {
		return nil, err
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
