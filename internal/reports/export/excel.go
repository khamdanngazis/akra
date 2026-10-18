package export

import (
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"akra/internal/platform/clock"
	"akra/internal/platform/money"
)

// WriteExcel menulis t sebagai .xlsx: sheet dataSheet berisi header (bold)
// di baris pertama, data, dan baris total (bold); sheet "Info" berisi
// judul, sekolah, keterangan, dan tanggal cetak. Angka ditulis sebagai
// angka (persentase dengan format 0,0%), bukan teks, agar bisa dihitung.
func WriteExcel(w io.Writer, t Table, dataSheet string) error {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", dataSheet); err != nil {
		return fmt.Errorf("export: rename sheet: %w", err)
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	pctFmt := "0.0%"
	pct, err := f.NewStyle(&excelize.Style{CustomNumFmt: &pctFmt})
	if err != nil {
		return err
	}
	boldPct, err := f.NewStyle(&excelize.Style{CustomNumFmt: &pctFmt, Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	moneyFmt := `"Rp"#,##0`
	moneyStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &moneyFmt})
	if err != nil {
		return err
	}
	boldMoney, err := f.NewStyle(&excelize.Style{CustomNumFmt: &moneyFmt, Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}

	widths := make([]int, len(t.Columns))
	for i, c := range t.Columns {
		widths[i] = utf8.RuneCountInString(c.Header)
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(dataSheet, cell, c.Header); err != nil {
			return err
		}
	}
	if err := f.SetRowStyle(dataSheet, 1, 1, bold); err != nil {
		return err
	}
	writeRow := func(rowNum int, cells []any, isTotal bool) error {
		for i, v := range cells {
			cell, _ := excelize.CoordinatesToCellName(i+1, rowNum)
			style := 0
			if isTotal {
				style = bold
			}
			var text string
			switch x := v.(type) {
			case Percent:
				text = x.String()
				if x < 0 {
					if err := f.SetCellValue(dataSheet, cell, ""); err != nil {
						return err
					}
				} else {
					if err := f.SetCellValue(dataSheet, cell, float64(x)/1000); err != nil {
						return err
					}
					style = pct
					if isTotal {
						style = boldPct
					}
				}
			case Money:
				text = money.FormatRupiah(int64(x))
				if err := f.SetCellValue(dataSheet, cell, int64(x)); err != nil {
					return err
				}
				style = moneyStyle
				if isTotal {
					style = boldMoney
				}
			default:
				text = fmt.Sprint(x)
				if err := f.SetCellValue(dataSheet, cell, x); err != nil {
					return err
				}
			}
			if style != 0 {
				if err := f.SetCellStyle(dataSheet, cell, cell, style); err != nil {
					return err
				}
			}
			if i < len(widths) {
				widths[i] = max(widths[i], utf8.RuneCountInString(text))
			}
		}
		return nil
	}
	for r, row := range t.Rows {
		if err := writeRow(r+2, row, false); err != nil {
			return fmt.Errorf("export: write row: %w", err)
		}
	}
	if t.Total != nil {
		if err := writeRow(len(t.Rows)+2, t.Total, true); err != nil {
			return fmt.Errorf("export: write total: %w", err)
		}
	}
	for i, wd := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(dataSheet, col, col, float64(min(max(wd, 6), 50)+2)); err != nil {
			return err
		}
	}
	if err := f.SetPanes(dataSheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}

	if len(t.Summary) > 0 {
		if _, err := f.NewSheet("Ringkasan"); err != nil {
			return err
		}
		for i, s := range t.Summary {
			row := i + 1
			if err := f.SetCellValue("Ringkasan", fmt.Sprintf("A%d", row), s.Label); err != nil {
				return err
			}
			cell := fmt.Sprintf("B%d", row)
			switch x := s.Value.(type) {
			case Money:
				_ = f.SetCellValue("Ringkasan", cell, int64(x))
				_ = f.SetCellStyle("Ringkasan", cell, cell, moneyStyle)
			default:
				_ = f.SetCellValue("Ringkasan", cell, x)
			}
		}
		_ = f.SetColWidth("Ringkasan", "A", "A", 28)
		_ = f.SetColWidth("Ringkasan", "B", "B", 20)
	}

	if _, err := f.NewSheet("Info"); err != nil {
		return err
	}
	info := append([]string{t.Title, t.SchoolName}, t.Meta...)
	info = append(info, "Dicetak "+clock.FormatDateID(t.PrintedAt))
	for i, line := range info {
		if err := f.SetCellValue("Info", fmt.Sprintf("A%d", i+1), line); err != nil {
			return err
		}
	}
	_ = f.SetCellStyle("Info", "A1", "A1", bold)
	_ = f.SetColWidth("Info", "A", "A", 60)
	if idx, err := f.GetSheetIndex(dataSheet); err == nil {
		f.SetActiveSheet(idx)
	}
	return f.Write(w)
}
