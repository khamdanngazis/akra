package admission

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"akra/internal/platform/clock"
)

// ProofData adalah isi bukti pendaftaran (T95). Sengaja tanpa NIK, No KK,
// atau data sensitif lain.
type ProofData struct {
	SchoolName         string
	RegistrationNumber string
	FullName           string
	BirthDate          time.Time
	PeriodName         string
	TrackName          string
	YearName           string
	SubmittedAt        time.Time
	Documents          []string
	Tests              []string // "Tes tertulis: Senin, 10 Januari 2027 08.00 WIB, Aula"
	Status             string
	VerificationCode   string
	PrintedAt          time.Time
}

// VerificationCode adalah kode pendek yang bisa dicocokkan petugas dengan
// data di sistem (bukan rahasia; hanya mencegah bukti palsu yang diketik
// ulang tanpa melihat sistem).
func VerificationCode(applicantID, registrationNumber string) string {
	h := sha256.Sum256([]byte("akra-proof\x00" + applicantID + "\x00" + registrationNumber))
	return strings.ToUpper(hex.EncodeToString(h[:4]))
}

// WriteProofPDF menulis bukti pendaftaran ukuran A5.
func WriteProofPDF(w io.Writer, d ProofData, compress bool) error {
	pdf := fpdf.New("P", "mm", "A5", "")
	pdf.SetCompression(compress)
	pdf.SetTitle("Bukti Pendaftaran "+d.RegistrationNumber, true)
	pdf.SetCreator("AKRA", true)
	pdf.SetCreationDate(d.PrintedAt)
	pdf.SetModificationDate(d.PrintedAt)
	pdf.SetCatalogSort(true)
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 12)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	width := pageW - left - right

	pdf.SetFont("Helvetica", "B", 13)
	pdf.MultiCell(width, 6, tr(d.SchoolName), "", "L", false)
	y := pdf.GetY() + 1
	pdf.SetLineWidth(0.5)
	pdf.Line(left, y, pageW-right, y)
	pdf.SetY(y + 4)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(width, 7, "BUKTI PENDAFTARAN SISWA BARU", "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(width, 6, "No. "+tr(d.RegistrationNumber), "", 1, "C", false, 0, "")
	pdf.Ln(3)

	rows := [][2]string{
		{"Nama calon siswa", d.FullName},
		{"Tanggal lahir", clock.FormatDateID(d.BirthDate)},
		{"Tahun ajaran", d.YearName},
		{"Gelombang", d.PeriodName},
		{"Jalur", d.TrackName},
		{"Tanggal kirim", clock.FormatDateID(d.SubmittedAt.In(clock.Jakarta))},
		{"Status", d.Status},
	}
	if len(d.Documents) > 0 {
		rows = append(rows, [2]string{"Dokumen diunggah", strings.Join(d.Documents, ", ")})
	}
	for i, t := range d.Tests {
		label := ""
		if i == 0 {
			label = "Jadwal tes"
		}
		rows = append(rows, [2]string{label, t})
	}
	labelW := 34.0
	for _, row := range rows {
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(labelW, 6, tr(row[0]), "", 0, "L", false, 0, "")
		pdf.CellFormat(4, 6, ":", "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "B", 10)
		pdf.MultiCell(width-labelW-4, 6, tr(row[1]), "", "L", false)
	}
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "", 9)
	pdf.MultiCell(width, 4.5, tr("Simpan bukti ini. Tunjukkan nomor pendaftaran saat menghubungi sekolah atau mengikuti tes. Kode verifikasi: "+d.VerificationCode+"."), "", "L", false)
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.CellFormat(width, 4, tr("Dicetak dari AKRA, "+clock.FormatDateID(d.PrintedAt)+"."), "", 1, "L", false, 0, "")
	return pdf.Output(w)
}
