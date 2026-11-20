package exam

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// Unduhan Excel hasil ujian (T148). Isi dibangun dari ExamAnalysis yang
// sama dengan halaman (label dan pembulatan sama), sehingga angka Excel
// cocok dengan tampilan. Nilai, p, dan D ditulis sebagai angka agar bisa
// diolah di Excel.

// Nama sheet unduhan analisis.
const (
	SheetSummary    = "Ringkasan"
	SheetResults    = "Hasil"
	SheetItems      = "Analisis butir"
	SheetObjectives = "Analisis TP"
	SheetFollow     = "Remedial-Pengayaan"
)

// scoreNum mengubah nilai ×100 menjadi angka dua desimal untuk Excel.
func scoreNum(x100 int) float64 { return float64(x100) / 100 }

// Passed melaporkan nilai final mencapai KKTP.
func (p ParticipantResult) Passed(kktp int) bool { return p.Final() && *p.ScoreX100 >= kktp*100 }

// passText menampilkan ketuntasan nilai final.
func (p ParticipantResult) passText(kktp int) string {
	if !p.Final() {
		return ""
	}
	if p.Passed(kktp) {
		return "Tuntas"
	}
	return "Belum tuntas"
}

// summaryRows menyusun sheet Ringkasan.
func summaryRows(a ExamAnalysis) [][]any {
	s := a.Summary
	rows := [][]any{
		{"Ujian", a.Exam.Name},
		{"Mata pelajaran", a.Exam.SubjectName},
		{a.KKTP.Label, a.KKTP.Value},
		{"Peserta terdaftar", s.Registered},
		{"Hadir", s.Present},
		{"Tidak hadir", s.Absent},
		{"Nilai final", s.Final},
		{"Menunggu koreksi esai", s.Awaiting},
	}
	if s.Final > 0 {
		rows = append(rows,
			[]any{"Rata-rata", scoreNum(s.Mean)},
			[]any{"Tertinggi", scoreNum(s.Max)},
			[]any{"Terendah", scoreNum(s.Min)},
			[]any{"Tuntas", s.Pass},
			[]any{"Belum tuntas", s.Fail},
			[]any{"Persentase tuntas", s.PassPercent()})
	}
	rows = append(rows, []any{}, []any{"Sebaran nilai", "Jumlah"})
	for _, b := range s.Bins {
		rows = append(rows, []any{b.Label, b.Count})
	}
	return rows
}

// resultHeader dan resultRows menyusun sheet Hasil.
var resultHeader = []any{"No", "Nama", "Nomor", "Kelas", "Sesi", "Paket", "Status", "Nilai", "Ketuntasan", "Durasi", "Integritas"}

func resultRows(a ExamAnalysis) [][]any {
	out := make([][]any, 0, len(a.Rows))
	for i, p := range a.Rows {
		var score any = ""
		if p.Score() != "" {
			score = scoreNum(*p.ScoreX100)
		}
		sess := p.SessionTitle
		if k := p.KindLabel(); k != "" {
			sess += " (" + k + ")"
		}
		out = append(out, []any{i + 1, p.Name, p.Number, p.ClassName, sess, p.VariantLabel, p.StatusLabel(), score,
			p.passText(a.KKTP.Value), p.Duration(), p.IntegrityText()})
	}
	return out
}

// itemHeader dan itemRows menyusun sheet Analisis butir.
var itemHeader = []any{"No", "Soal", "Jenis", "TP", "Poin", "Peserta", "p", "Kesukaran", "D", "Daya beda", "Keputusan", "Sebaran opsi", "Kosong", "Catatan"}

func itemRows(a ExamAnalysis) [][]any {
	out := make([][]any, 0, len(a.Items))
	for _, s := range a.Items {
		var p, d any = "", ""
		if s.N > 0 && !s.Voided {
			p = round2(s.P)
		}
		if s.HasD && !s.Voided {
			d = round2(s.D)
		}
		notes := ""
		for i, w := range s.Warnings {
			if i > 0 {
				notes += " "
			}
			notes += w
		}
		out = append(out, []any{s.Number, s.Snippet, s.Kind.Label(), s.ObjectiveCode, s.Points, s.N, p, s.PLabel(), d, s.DLabel(),
			s.Decision(), s.OptionsText(), s.Blank, notes})
	}
	return out
}

// objectiveHeader dan objectiveRows menyusun tabel ketuntasan per TP.
var objectiveHeader = []any{"Kode", "Tujuan pembelajaran", "Soal", "Siswa", "Tuntas", "% tuntas", "Rata-rata capaian (%)"}

func objectiveRows(a ExamAnalysis) [][]any {
	out := make([][]any, 0, len(a.Objectives.Objectives))
	for _, o := range a.Objectives.Objectives {
		out = append(out, []any{o.Code, o.Description, o.Questions, o.Students, o.Mastered, o.Percent, o.Average})
	}
	return out
}

// studentMapHeader dan studentMapRows menyusun peta TP per siswa.
func studentMapHeader(a ExamAnalysis) []any {
	h := []any{"Nama", "Nomor", "Kelas", "Nilai"}
	for _, o := range a.Objectives.Objectives {
		h = append(h, o.Code+" (%)")
	}
	return append(h, "TP belum tuntas")
}

func studentMapRows(a ExamAnalysis) [][]any {
	out := make([][]any, 0, len(a.Objectives.Students))
	for _, s := range a.Objectives.Students {
		row := []any{s.Name, s.Number, s.ClassName, scoreNum(s.ScoreX100)}
		for _, c := range s.Cells {
			if c.Has {
				row = append(row, c.Percent)
			} else {
				row = append(row, "")
			}
		}
		out = append(out, append(row, s.WeakCodes()))
	}
	return out
}

// followHeader dan followRows menyusun daftar remedial dan pengayaan.
var followHeader = []any{"Tindak lanjut", "Nama", "Nomor", "Kelas", "Nilai", "TP belum tuntas"}

func followRows(a ExamAnalysis) [][]any {
	var out [][]any
	for _, s := range a.Follow.Remedial {
		out = append(out, []any{"Remedial", s.Name, s.Number, s.ClassName, scoreNum(s.ScoreX100), s.WeakCodes()})
	}
	for _, s := range a.Follow.Enrichment {
		out = append(out, []any{"Pengayaan", s.Name, s.Number, s.ClassName, scoreNum(s.ScoreX100), s.WeakCodes()})
	}
	return out
}

// AnalysisXLSX menulis hasil dan analisis ujian ke Excel.
func AnalysisXLSX(a ExamAnalysis) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", SheetSummary); err != nil {
		return nil, err
	}
	for _, sh := range []string{SheetResults, SheetItems, SheetObjectives, SheetFollow} {
		if _, err := f.NewSheet(sh); err != nil {
			return nil, err
		}
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	dec2 := "0.00"
	num, err := f.NewStyle(&excelize.Style{CustomNumFmt: &dec2})
	if err != nil {
		return nil, err
	}
	// write menulis tabel mulai baris start; kolom angka diberi format dua
	// desimal. Mengembalikan baris berikutnya.
	write := func(sh string, start int, header []any, rows [][]any, decCols ...int) (int, error) {
		if header != nil {
			if err := f.SetSheetRow(sh, fmt.Sprintf("A%d", start), &header); err != nil {
				return 0, err
			}
			end, _ := excelize.CoordinatesToCellName(len(header), start)
			_ = f.SetCellStyle(sh, fmt.Sprintf("A%d", start), end, bold)
			start++
		}
		for i, r := range rows {
			row := r
			if err := f.SetSheetRow(sh, fmt.Sprintf("A%d", start+i), &row); err != nil {
				return 0, err
			}
			for _, c := range decCols {
				cell, _ := excelize.CoordinatesToCellName(c, start+i)
				_ = f.SetCellStyle(sh, cell, cell, num)
			}
		}
		return start + len(rows), nil
	}
	if _, err := write(SheetSummary, 1, nil, summaryRows(a)); err != nil {
		return nil, err
	}
	if _, err := write(SheetResults, 1, resultHeader, resultRows(a), 8); err != nil {
		return nil, err
	}
	if _, err := write(SheetItems, 1, itemHeader, itemRows(a), 7, 9); err != nil {
		return nil, err
	}
	next, err := write(SheetObjectives, 1, objectiveHeader, objectiveRows(a))
	if err != nil {
		return nil, err
	}
	if _, err := write(SheetObjectives, next+1, studentMapHeader(a), studentMapRows(a), 4); err != nil {
		return nil, err
	}
	if _, err := write(SheetFollow, 1, followHeader, followRows(a), 5); err != nil {
		return nil, err
	}
	_ = f.SetColWidth(SheetSummary, "A", "A", 24)
	_ = f.SetColWidth(SheetResults, "B", "B", 30)
	_ = f.SetColWidth(SheetResults, "E", "E", 22)
	_ = f.SetColWidth(SheetResults, "K", "K", 36)
	_ = f.SetColWidth(SheetItems, "B", "B", 50)
	_ = f.SetColWidth(SheetItems, "L", "L", 28)
	_ = f.SetColWidth(SheetItems, "N", "N", 60)
	_ = f.SetColWidth(SheetObjectives, "A", "A", 24)
	_ = f.SetColWidth(SheetObjectives, "B", "B", 50)
	_ = f.SetColWidth(SheetFollow, "B", "B", 30)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
