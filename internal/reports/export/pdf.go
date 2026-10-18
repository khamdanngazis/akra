package export

import (
	"fmt"
	"io"

	"github.com/go-pdf/fpdf"

	"akra/internal/platform/clock"
	"akra/internal/platform/money"
)

// WritePDF menulis t sebagai PDF A4 dengan judul, sekolah, keterangan,
// tabel (header diulang di setiap halaman), baris total, tanggal cetak, dan
// nomor halaman. Output reproducible untuk isi dan tanggal cetak yang sama.
func WritePDF(w io.Writer, t Table) error {
	orient := "P"
	if t.Landscape {
		orient = "L"
	}
	pdf := fpdf.New(orient, "mm", "A4", "")
	pdf.SetTitle(t.Title, true)
	pdf.SetCreator("AKRA", true)
	pdf.SetCreationDate(t.PrintedAt)
	pdf.SetModificationDate(t.PrintedAt)
	pdf.SetCatalogSort(true)
	pdf.SetCompression(!t.Uncompressed)
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 16)
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.AliasNbPages("{nb}")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(100, 116, 139)
		pdf.CellFormat(0, 4, tr(fmt.Sprintf("Dicetak dari AKRA, %s · halaman %d/{nb}", clock.FormatDateID(t.PrintedAt), pdf.PageNo())), "", 0, "R", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})

	header := func() {
		pdf.SetFont("Helvetica", "B", 8)
		pdf.SetFillColor(226, 232, 240)
		for _, c := range t.Columns {
			align := "L"
			if c.Numeric {
				align = "R"
			}
			pdf.CellFormat(c.Width, 6, fit(pdf, tr(c.Header), c.Width), "1", 0, align, true, 0, "")
		}
		pdf.Ln(-1)
	}
	pdf.SetHeaderFuncMode(func() {
		if pdf.PageNo() > 1 {
			header()
		}
	}, true)

	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 14)
	pdf.CellFormat(0, 7, tr(t.Title), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 5, tr(t.SchoolName), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	for _, m := range t.Meta {
		pdf.CellFormat(0, 5, tr(m), "", 1, "L", false, 0, "")
	}
	pdf.Ln(3)
	header()

	row := func(cells []any, bold bool) {
		style := ""
		if bold {
			style = "B"
		}
		pdf.SetFont("Helvetica", style, 8)
		for i, v := range cells {
			if i >= len(t.Columns) {
				break
			}
			c := t.Columns[i]
			align := "L"
			if c.Numeric {
				align = "R"
			}
			text := fmt.Sprint(v)
			switch x := v.(type) {
			case Percent:
				text = x.String()
			case Money:
				text = money.FormatRupiah(int64(x))
			}
			pdf.CellFormat(c.Width, 5.5, fit(pdf, tr(text), c.Width), "1", 0, align, false, 0, "")
		}
		pdf.Ln(-1)
	}
	// Subtotal kolom Money untuk satu kelompok.
	subtotal := func(sums map[int]int64) {
		cells := make([]any, len(t.Columns))
		for i := range cells {
			cells[i] = ""
			if v, ok := sums[i]; ok {
				cells[i] = Money(v)
			}
		}
		// Label di kolom kedua (kolom pertama biasanya sempit, mis. NIS).
		cells[min(1, len(cells)-1)] = "Subtotal"
		row(cells, true)
	}
	groupHeading := func(label string) {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetFillColor(241, 245, 249)
		pdf.CellFormat(tableWidth(t), 6, tr(label), "1", 1, "L", true, 0, "")
	}
	grouped := len(t.Groups) == len(t.Rows) && len(t.Rows) > 0
	current := ""
	sums := map[int]int64{}
	for i, r := range t.Rows {
		if grouped && (i == 0 || t.Groups[i] != current) {
			if i > 0 {
				subtotal(sums)
			}
			current, sums = t.Groups[i], map[int]int64{}
			groupHeading(current)
		}
		row(r, false)
		for j, v := range r {
			if m, ok := v.(Money); ok {
				sums[j] += int64(m)
			}
		}
	}
	if grouped {
		subtotal(sums)
	}
	if t.Total != nil {
		row(t.Total, true)
	}
	if len(t.Summary) > 0 {
		pdf.Ln(4)
		for _, s := range t.Summary {
			pdf.SetFont("Helvetica", "", 9)
			pdf.CellFormat(60, 5.5, tr(s.Label), "", 0, "L", false, 0, "")
			text := fmt.Sprint(s.Value)
			if m, ok := s.Value.(Money); ok {
				text = money.FormatRupiah(int64(m))
			}
			pdf.SetFont("Helvetica", "B", 9)
			pdf.CellFormat(50, 5.5, tr(text), "", 1, "L", false, 0, "")
		}
	}
	if len(t.Rows) == 0 {
		pdf.SetFont("Helvetica", "I", 9)
		pdf.CellFormat(0, 8, "Tidak ada data untuk filter ini.", "", 1, "L", false, 0, "")
	}
	if err := pdf.Error(); err != nil {
		return fmt.Errorf("export: render pdf: %w", err)
	}
	return pdf.Output(w)
}

func tableWidth(t Table) float64 {
	var w float64
	for _, c := range t.Columns {
		w += c.Width
	}
	return w
}

// fit memotong teks yang lebih lebar dari kolom (font aktif) agar tidak
// menimpa kolom sebelah dan baris tetap satu tinggi.
func fit(pdf *fpdf.Fpdf, text string, width float64) string {
	for pdf.GetStringWidth(text) > width-2 && len(text) > 1 {
		text = text[:len(text)-1]
	}
	return text
}
