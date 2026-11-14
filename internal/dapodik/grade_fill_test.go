package dapodik

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// gradeTemplate membuat file tiruan format import nilai Dapodik: kepala,
// header, token di kolom B (tersembunyi) dan C, validasi data, dan sheet
// diproteksi password, seperti file "Unduh Format Excel untuk Import".
func gradeTemplate(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	_ = f.SetSheetName("Sheet1", gradeSheet)
	set := func(cell, v string) { _ = f.SetCellStr(gradeSheet, cell, v) }
	set("A6", "Format Excel Import Nilai")
	set("A8", "Token Mata Evaluasi")
	set("E8", "TOKEN-ME-1")
	set("A9", "Mata Evaluasi")
	set("E9", "Bahasa Indonesia")
	set("A11", "Rombongan Belajar")
	set("E11", "1A")
	set("A12", "KKM Pengetahuan")
	set("A16", "No")
	set("B16", "Token\nNilai")
	set("C16", "Token\nAnggota Rombel")
	set("D16", "Nama Siswa")
	set("E16", "NIS")
	set("F16", "NISN")
	for i, s := range [][]string{{"1", "TN-AAA", "TR-AAA", "Budi", "001", "0091234567"}, {"2", "TN-BBB", "TR-BBB", "Citra", "002", ""}, {"3", "TN-CCC", "TR-CCC", "Tidak Ada", "999", ""}} {
		_ = f.SetSheetRow(gradeSheet, "A"+itoa(18+i), &s)
	}
	_ = f.SetColVisible(gradeSheet, "B", false)
	dv := excelize.NewDataValidation(true)
	dv.Sqref = "H18:H200"
	_ = dv.SetDropList([]string{"A", "B", "C", "D"})
	_ = f.AddDataValidation(gradeSheet, dv)
	_ = f.ProtectSheet(gradeSheet, &excelize.SheetProtectionOptions{Password: "rahasia", FormatCells: false})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func itoa(n int) string { return strconv.Itoa(n) }

func sheetXML(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, zf := range zr.File {
		if strings.HasPrefix(zf.Name, "xl/worksheets/sheet") {
			rc, _ := zf.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatal("worksheet not found")
	return ""
}

func checkFilled(t *testing.T, orig, out []byte) {
	t.Helper()
	x := sheetXML(t, out)
	if !strings.Contains(x, "<sheetProtection") {
		t.Error("sheet protection lost")
	}
	if !strings.Contains(x, "<dataValidation") {
		t.Error("data validation lost")
	}
	if !strings.Contains(x, `hidden="1"`) && !strings.Contains(x, `hidden="true"`) {
		t.Error("hidden column lost")
	}
	a, _ := excelize.OpenReader(bytes.NewReader(orig))
	b, _ := excelize.OpenReader(bytes.NewReader(out))
	for r := 18; r < 21; r++ {
		for _, c := range []string{"B", "C", "D", "E", "F"} {
			va, _ := a.GetCellValue(gradeSheet, c+itoa(r))
			vb, _ := b.GetCellValue(gradeSheet, c+itoa(r))
			if va != vb {
				t.Errorf("%s%d changed: %q -> %q", c, r, va, vb)
			}
		}
	}
}

func TestFillGradeFile(t *testing.T) {
	orig := gradeTemplate(t)
	info, rows, err := ReadGradeFile(orig)
	if err != nil || info.Subject != "Bahasa Indonesia" || info.ClassName != "1A" || len(rows) != 3 || rows[0].TokenRombel != "TR-AAA" {
		t.Fatalf("read = %+v %+v %v", info, rows, err)
	}
	lookup := func(nisn, nis string) (GradeEntry, bool, bool) {
		switch {
		case nisn == "0091234567":
			return GradeEntry{Score: 88, Letter: "B"}, true, true
		case nis == "002":
			return GradeEntry{}, true, false // belum lengkap
		}
		return GradeEntry{}, false, false
	}
	out, rep, err := FillGradeFile(orig, 75, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Filled != 1 || len(rep.NoScore) != 1 || len(rep.Unmatched) != 1 || !rep.KKMFilled {
		t.Errorf("report = %+v", rep)
	}
	f, _ := excelize.OpenReader(bytes.NewReader(out))
	g, _ := f.GetCellValue(gradeSheet, "G18")
	h, _ := f.GetCellValue(gradeSheet, "H18")
	g2, _ := f.GetCellValue(gradeSheet, "G19")
	kkm, _ := f.GetCellValue(gradeSheet, "E12")
	sikap, _ := f.GetCellValue(gradeSheet, "K18")
	if g != "88" || h != "B" || g2 != "" || kkm != "75" || sikap != "" {
		t.Errorf("cells G18=%q H18=%q G19=%q E12=%q K18=%q", g, h, g2, kkm, sikap)
	}
	checkFilled(t, orig, out)

	// Predikat selain A–D tidak ditulis.
	_, rep, _ = FillGradeFile(orig, 0, func(string, string) (GradeEntry, bool, bool) {
		return GradeEntry{Score: 90, Letter: "Sangat Baik"}, true, true
	})
	if !rep.LetterSkip || rep.KKMFilled {
		t.Errorf("letter skip report = %+v", rep)
	}
	if _, _, err := ReadGradeFile([]byte("bukan excel")); err != ErrNotGradeFormat {
		t.Errorf("not excel: %v", err)
	}
}

// TestFillRealDapodikTemplate memakai template asli dari installer Dapodik
// jika DAPODIK_GRADE_TEMPLATE diisi (file tidak disertakan di repo).
func TestFillRealDapodikTemplate(t *testing.T) {
	path := os.Getenv("DAPODIK_GRADE_TEMPLATE")
	if path == "" {
		t.Skip("DAPODIK_GRADE_TEMPLATE is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Tiru isi yang dibuat Dapodik saat format diunduh.
	f, _ := excelize.OpenReader(bytes.NewReader(raw))
	_ = f.SetCellStr(gradeSheet, "E9", "Matematika")
	_ = f.SetSheetRow(gradeSheet, "A18", &[]string{"1", "TN-1", "TR-1", "Budi", "001", "0091234567"})
	var buf bytes.Buffer
	_ = f.Write(&buf)
	orig := buf.Bytes()
	out, rep, err := FillGradeFile(orig, 70, func(string, string) (GradeEntry, bool, bool) { return GradeEntry{Score: 81, Letter: "B"}, true, true })
	if err != nil || rep.Filled != 1 {
		t.Fatalf("fill real template: %+v %v", rep, err)
	}
	checkFilled(t, orig, out)
}
