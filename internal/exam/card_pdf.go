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
	return WriteCardsPDFWith(w, lh, cards, printedAt, compress, CardOptions{})
}

// CardOptions mengatur cetak kartu peserta.
type CardOptions struct {
	// Guide menambah halaman belakang berisi panduan peserta singkat di
	// setiap kartu (T154), untuk cetak bolak-balik (balik di sisi panjang).
	Guide bool
}

// WriteCardsPDFWith seperti WriteCardsPDF dengan pilihan opts.
func WriteCardsPDFWith(w io.Writer, lh Letterhead, cards []Card, printedAt time.Time, compress bool, opts CardOptions) error {
	pdf, tr := newDocPDF("Kartu Peserta Ujian", "P", printedAt, compress)
	pdf.SetMargins(cardMargin, cardMargin, cardMargin)
	pdf.SetAutoPageBreak(false, 0)
	pageW, pageH := pdf.GetPageSize()
	cw := (pageW - 2*cardMargin) / cardCols
	ch := (pageH - 2*cardMargin) / cardRows
	perPage := cardCols * cardRows
	for i, c := range cards {
		slot := i % perPage
		if slot == 0 {
			if opts.Guide && i > 0 {
				writeCardGuides(pdf, tr, perPage)
			}
			pdf.AddPage()
			cutLines(pdf, cw, ch)
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
	} else if opts.Guide {
		writeCardGuides(pdf, tr, (len(cards)-1)%perPage+1)
	}
	return pdf.Output(w)
}

// cutLines menggambar garis potong kartu di halaman aktif.
func cutLines(pdf *fpdf.Fpdf, cw, ch float64) {
	pageW, pageH := pdf.GetPageSize()
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

// cardGuideLines adalah panduan peserta singkat di balik kartu.
var cardGuideLines = [][2]string{
	{"Masuk", "Pindai QR di kartu, lalu ketik NIS dan kode."},
	{"Kerjakan", "Jawaban tersimpan otomatis. Hijau = sudah dijawab."},
	{"Ragu-ragu", "Tandai soal yang ingin dicek lagi (kuning)."},
	{"Offline", "Tetap kerjakan. Jangan tutup halaman ujian."},
	{"Kumpulkan", "Cek nomor abu-abu dan kuning, lalu Kumpulkan."},
}

// writeCardGuides menambah halaman belakang untuk n kartu di halaman
// sebelumnya. Kolom dicerminkan agar panduan tepat di balik kartunya.
func writeCardGuides(pdf *fpdf.Fpdf, tr func(string) string, n int) {
	pdf.AddPage()
	pageW, pageH := pdf.GetPageSize()
	cw := (pageW - 2*cardMargin) / cardCols
	ch := (pageH - 2*cardMargin) / cardRows
	cutLines(pdf, cw, ch)
	for slot := 0; slot < n; slot++ {
		col := cardCols - 1 - slot%cardCols
		x := cardMargin + float64(col)*cw + 4
		y := cardMargin + float64(slot/cardCols)*ch + 4
		inner := cw - 8
		pdf.SetTextColor(13, 148, 136)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetXY(x, y)
		pdf.CellFormat(inner, 4.5, tr("CARA MENGIKUTI UJIAN"), "", 2, "L", false, 0, "")
		pdf.Ln(1)
		for k, l := range cardGuideLines {
			ly := pdf.GetY()
			pdf.SetFillColor(13, 148, 136)
			pdf.Circle(x+2, ly+2.2, 2, "F")
			pdf.SetTextColor(255, 255, 255)
			pdf.SetFont("Helvetica", "B", 6.5)
			pdf.SetXY(x, ly+0.5)
			pdf.CellFormat(4, 3.4, fmt.Sprint(k+1), "", 0, "C", false, 0, "")
			pdf.SetTextColor(15, 23, 42)
			pdf.SetFont("Helvetica", "B", 7.5)
			pdf.SetXY(x+5.5, ly)
			pdf.CellFormat(17, 4.4, tr(l[0]), "", 0, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 7.5)
			pdf.CellFormat(inner-22.5, 4.4, fitText(pdf, tr, l[1], inner-22.5), "", 2, "L", false, 0, "")
			pdf.SetY(ly + 6.2)
		}
		pdf.SetTextColor(71, 85, 105)
		pdf.SetFont("Helvetica", "I", 7)
		pdf.SetXY(x, y+ch-14)
		pdf.CellFormat(inner, 4, tr("Ada masalah? Angkat tangan dan panggil pengawas."), "", 0, "L", false, 0, "")
	}
}
