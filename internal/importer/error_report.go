package importer

import (
	"context"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	// ErrorReportSheet adalah nama sheet laporan kesalahan.
	ErrorReportSheet = "Perbaiki"
	// Judul kolom tambahan di laporan kesalahan. Parser mengabaikan kolom
	// selain kolom baku, jadi file yang sudah diperbaiki bisa diunggah ulang.
	errorReportLineHeader  = "Baris asli"
	errorReportErrorHeader = "Kesalahan"
	// KindSaveFailed adalah jenis untuk baris valid yang gagal disimpan (T55).
	KindSaveFailed = "gagal_disimpan"
)

// kindLabels adalah nama jenis kesalahan untuk ringkasan.
var kindLabels = map[string]string{
	KindNISEmpty:      "NIS kosong",
	KindNISTooLong:    "NIS terlalu panjang",
	KindNISDuplicate:  "NIS ganda di file",
	KindNameEmpty:     "nama kosong",
	KindNameTooLong:   "nama terlalu panjang",
	KindBirthDate:     "tanggal lahir tidak valid",
	KindGender:        "jenis kelamin tidak valid",
	KindClassEmpty:    "kelas kosong",
	KindClassNotFound: "kelas tidak ditemukan",
	KindGuardianName:  "nama wali tidak valid",
	KindGuardianPhone: "telepon wali tidak valid",
	KindGuardianEmail: "email wali tidak valid",
	KindSaveFailed:    "gagal disimpan",
}

// KindLabel mengembalikan nama jenis kesalahan untuk pengguna.
func KindLabel(kind string) string {
	if l, ok := kindLabels[kind]; ok {
		return l
	}
	return "kesalahan lain"
}

// KindCount adalah jumlah baris untuk satu jenis kesalahan.
type KindCount struct {
	Kind  string
	Label string
	Rows  int
}

// ErrorSummary menghitung jumlah baris per jenis kesalahan. Satu baris
// dengan dua jenis kesalahan dihitung di kedua jenis. Baris yang gagal
// disimpan (T55) masuk jenis KindSaveFailed. Urut dari yang terbanyak.
func (s *Service) ErrorSummary(ctx context.Context, schoolID, batchID string) ([]KindCount, error) {
	rows, err := s.db.Query(ctx, `
		SELECT kind, count(DISTINCT line) FROM (
			SELECT r.line, e->>'kind' AS kind
			FROM import_rows r, jsonb_array_elements(r.errors) e
			WHERE r.school_id = $1 AND r.batch_id = $2 AND r.status = 'error'
			UNION ALL
			SELECT line, $3 FROM import_rows
			WHERE school_id = $1 AND batch_id = $2 AND result = 'gagal'
		) k
		GROUP BY kind ORDER BY count(DISTINCT line) DESC, kind`, schoolID, batchID, KindSaveFailed)
	if err != nil {
		return nil, fmt.Errorf("importer: error summary: %w", err)
	}
	defer rows.Close()
	var out []KindCount
	for rows.Next() {
		var k KindCount
		if err := rows.Scan(&k.Kind, &k.Rows); err != nil {
			return nil, fmt.Errorf("importer: error summary: %w", err)
		}
		k.Label = KindLabel(k.Kind)
		out = append(out, k)
	}
	return out, rows.Err()
}

// reportRow adalah satu baris laporan kesalahan.
type reportRow struct {
	Line     int
	Raw      [NumColumns]string
	Messages []string
}

// errorReportRows mengembalikan semua baris error dan baris yang gagal
// disimpan, urut nomor baris asli.
func (s *Service) errorReportRows(ctx context.Context, schoolID, batchID string) ([]reportRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT line, raw,
		       CASE WHEN status = 'error'
		            THEN coalesce((SELECT array_agg(e->>'message') FROM jsonb_array_elements(errors) e), '{}')
		            ELSE ARRAY[coalesce(result_error, '')] END
		FROM import_rows
		WHERE school_id = $1 AND batch_id = $2 AND (status = 'error' OR result = 'gagal')
		ORDER BY line`, schoolID, batchID)
	if err != nil {
		return nil, fmt.Errorf("importer: error report rows: %w", err)
	}
	defer rows.Close()
	var out []reportRow
	for rows.Next() {
		var r reportRow
		if err := rows.Scan(&r.Line, &r.Raw, &r.Messages); err != nil {
			return nil, fmt.Errorf("importer: error report rows: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrorReport membuat file Excel berisi baris yang perlu diperbaiki:
// "Baris asli", kedelapan kolom template (isi apa adanya), lalu
// "Kesalahan". Setelah diperbaiki, file ini bisa diunggah ulang karena
// parser hanya membaca kolom template. ErrBatchNotFound jika batch bukan
// milik sekolah.
func (s *Service) ErrorReport(ctx context.Context, schoolID, batchID string) ([]byte, error) {
	if _, err := s.GetBatch(ctx, schoolID, batchID); err != nil {
		return nil, err
	}
	rows, err := s.errorReportRows(ctx, schoolID, batchID)
	if err != nil {
		return nil, err
	}
	return buildErrorReport(rows)
}

func buildErrorReport(rows []reportRow) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := ErrorReportSheet
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	text, err := f.NewStyle(&excelize.Style{NumFmt: numFmtText})
	if err != nil {
		return nil, err
	}
	header, err := f.NewStyle(&excelize.Style{
		NumFmt: numFmtText,
		Font:   &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:   excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"0B3D91"}},
	})
	if err != nil {
		return nil, err
	}
	muted, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Color: "64748B"}, Alignment: &excelize.Alignment{Vertical: "top"}})
	if err != nil {
		return nil, err
	}
	errStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: "B91C1C"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FEF2F2"}},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"},
	})
	if err != nil {
		return nil, err
	}

	// Kolom: A = Baris asli, B..I = kolom template, J = Kesalahan.
	headers := append([]string{errorReportLineHeader}, columnHeaders()...)
	headers = append(headers, errorReportErrorHeader)
	last, _ := excelize.ColumnNumberToName(len(headers))
	firstData, _ := excelize.ColumnNumberToName(2)
	lastData, _ := excelize.ColumnNumberToName(1 + NumColumns)
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellStr(sheet, cell, h); err != nil {
			return nil, err
		}
	}
	if err := f.SetCellStyle(sheet, "A1", last+"1", header); err != nil {
		return nil, err
	}

	n := max(len(rows), 1)
	// Kolom template bertipe teks agar NIS 007 tetap 007 saat diperbaiki.
	if err := f.SetCellStyle(sheet, firstData+"2", fmt.Sprintf("%s%d", lastData, n+1), text); err != nil {
		return nil, err
	}
	for i, r := range rows {
		line := i + 2
		if err := f.SetCellInt(sheet, fmt.Sprintf("A%d", line), r.Line); err != nil {
			return nil, err
		}
		for c, v := range r.Raw {
			cell, _ := excelize.CoordinatesToCellName(c+2, line)
			// SetCellStr: isi yang diawali "=" tetap teks, bukan rumus.
			if err := f.SetCellStr(sheet, cell, v); err != nil {
				return nil, err
			}
		}
		if err := f.SetCellStr(sheet, fmt.Sprintf("%s%d", last, line), strings.Join(r.Messages, "\n")); err != nil {
			return nil, err
		}
	}
	if len(rows) > 0 {
		if err := f.SetCellStyle(sheet, "A2", fmt.Sprintf("A%d", n+1), muted); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(sheet, last+"2", fmt.Sprintf("%s%d", last, n+1), errStyle); err != nil {
			return nil, err
		}
	}
	if err := f.SetColWidth(sheet, "A", "A", 11); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(sheet, firstData, lastData, 20); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(sheet, last, last, 60); err != nil {
		return nil, err
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, err
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func columnHeaders() []string {
	h := make([]string, NumColumns)
	for i, c := range StudentColumns {
		h[i] = c.Header
	}
	return h
}
