package exam

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	qrcode "github.com/skip2/go-qrcode"
)

// Ukuran kartu peserta: 2 × 4 per A4 dengan garis potong (T135).
const (
	cardCols   = 2
	cardRows   = 4
	cardMargin = 10.0
)

// newDocPDF membuat dokumen A4 dengan metadata standar.
func newDocPDF(title string, orientation string, printedAt time.Time, compress bool) (*fpdf.Fpdf, func(string) string) {
	pdf := fpdf.New(orientation, "mm", "A4", "")
	pdf.SetCompression(compress)
	pdf.SetTitle(title, true)
	pdf.SetCreator("AKRA", true)
	pdf.SetCreationDate(printedAt)
	pdf.SetModificationDate(printedAt)
	pdf.SetCatalogSort(true)
	return pdf, pdf.UnicodeTranslatorFromDescriptor("")
}

// fitText memotong s agar muat di lebar w dengan font aktif.
func fitText(pdf *fpdf.Fpdf, tr func(string) string, s string, w float64) string {
	t := tr(s)
	if pdf.GetStringWidth(t) <= w {
		return t
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		t = tr(string(r) + "…")
		if pdf.GetStringWidth(t) <= w {
			return t
		}
	}
	return t
}

// formatCode memisah kode 8 karakter menjadi dua kelompok agar mudah dibaca.
func formatCode(c string) string {
	if len(c) == CodeLength {
		return c[:4] + "-" + c[4:]
	}
	return c
}

// WriteCardsPDF menulis kartu peserta: 8 per A4 dengan garis potong.
// Setiap kartu memuat identitas, nomor peserta, ruang dan kursi, jadwal,
// kode masuk, dan QR berisi tautan login dengan NIS terisi (tanpa kode).
func WriteCardsPDF(w io.Writer, lh Letterhead, cards []Card, printedAt time.Time, compress bool) error {
	pdf, tr := newDocPDF("Kartu Peserta Ujian", "P", printedAt, compress)
	pdf.SetMargins(cardMargin, cardMargin, cardMargin)
	pdf.SetAutoPageBreak(false, 0)
	pageW, pageH := pdf.GetPageSize()
	cw := (pageW - 2*cardMargin) / cardCols
	ch := (pageH - 2*cardMargin) / cardRows
	for i, c := range cards {
		slot := i % (cardCols * cardRows)
		if slot == 0 {
			pdf.AddPage()
			// Garis potong.
			pdf.SetDrawColor(160, 160, 160)
			pdf.SetLineWidth(0.2)
			pdf.SetDashPattern([]float64{2, 1.5}, 0)
			for col := 0; col <= cardCols; col++ {
				x := cardMargin + float64(col)*cw
				pdf.Line(x, cardMargin, x, pageH-cardMargin)
			}
			for row := 0; row <= cardRows; row++ {
				y := cardMargin + float64(row)*ch
				pdf.Line(cardMargin, y, pageW-cardMargin, y)
			}
			pdf.SetDashPattern([]float64{}, 0)
		}
		x := cardMargin + float64(slot%cardCols)*cw + 3
		y := cardMargin + float64(slot/cardCols)*ch + 3
		inner := cw - 6
		qrSize := 22.0
		textW := inner - qrSize - 2
		pdf.SetTextColor(15, 23, 42)
		pdf.SetFont("Helvetica", "B", 8)
		pdf.SetXY(x, y)
		pdf.CellFormat(textW, 4, fitText(pdf, tr, lh.School, textW), "", 2, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 7)
		pdf.CellFormat(textW, 3.5, fitText(pdf, tr, "KARTU PESERTA · "+c.Title, textW), "", 2, "L", false, 0, "")
		pdf.Ln(1)
		pdf.SetX(x)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(textW, 5, fitText(pdf, tr, c.Name, textW), "", 2, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 7.5)
		line := func(label, v string) {
			if v == "" {
				return
			}
			pdf.SetX(x)
			pdf.CellFormat(textW, 3.6, fitText(pdf, tr, label+": "+v, textW), "", 2, "L", false, 0, "")
		}
		line("NIS / No. daftar", c.Identifier)
		if c.ClassName != "" {
			line("Kelas", c.ClassName)
		}
		pdf.SetX(x)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.CellFormat(textW, 4.5, fitText(pdf, tr, "No. peserta: "+c.Number, textW), "", 2, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 7.5)
		if c.Room != "" {
			room := c.Room
			if c.Seat > 0 {
				room += fmt.Sprintf(" · kursi %d", c.Seat)
			}
			line("Ruang", room)
		}
		// QR di kanan atas.
		if png, err := qrcode.Encode(c.LoginURL, qrcode.Medium, 256); err == nil {
			name := fmt.Sprintf("qr%d", i)
			pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
			pdf.ImageOptions(name, x+inner-qrSize, y, qrSize, qrSize, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		}
		pdf.SetFont("Helvetica", "", 5.5)
		pdf.SetXY(x+inner-qrSize, y+qrSize)
		pdf.CellFormat(qrSize, 3, tr("Pindai untuk masuk"), "", 0, "C", false, 0, "")
		// Kode masuk.
		cy := y + 33
		pdf.SetXY(x, cy)
		pdf.SetFont("Helvetica", "", 7)
		pdf.CellFormat(22, 7, tr("Kode masuk"), "", 0, "L", false, 0, "")
		pdf.SetFont("Courier", "B", 14)
		pdf.SetFillColor(241, 245, 249)
		code := formatCode(c.Code)
		if code == "" {
			code = "(sesi selesai)"
			pdf.SetFont("Helvetica", "I", 8)
		}
		pdf.CellFormat(inner-22, 7, tr(code), "1", 2, "C", true, 0, "")
		// Jadwal.
		pdf.SetFont("Helvetica", "", 6.5)
		maxLines := 4
		for k, l := range c.Lines {
			if k == maxLines {
				pdf.SetX(x)
				pdf.CellFormat(inner, 3, tr(fmt.Sprintf("+%d jadwal lain: lihat beranda peserta", len(c.Lines)-maxLines)), "", 2, "L", false, 0, "")
				break
			}
			pdf.SetX(x)
			pdf.CellFormat(inner, 3, fitText(pdf, tr, l, inner), "", 2, "L", false, 0, "")
		}
		pdf.SetFont("Helvetica", "", 5.5)
		pdf.SetXY(x, y+ch-9)
		login, _, _ := strings.Cut(c.LoginURL, "?")
		pdf.CellFormat(inner, 3, fitText(pdf, tr, "Masuk di "+login+" dengan NIS dan kode di atas. Jangan berikan kode ke orang lain.", inner), "", 0, "L", false, 0, "")
	}
	if len(cards) == 0 {
		pdf.AddPage()
	}
	return pdf.Output(w)
}
