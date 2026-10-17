package payments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/jackc/pgx/v5"

	"akra/internal/billing"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
)

// Receipt adalah data kuitansi satu pembayaran. Data sumbernya tetap
// (pembayaran append-only), jadi PDF dibuat saat diminta tanpa disimpan.
type Receipt struct {
	Number        string
	SchoolName    string
	SchoolAddress string
	StudentName   string
	NIS           string
	ClassName     string
	YearName      string
	FeeNames      string
	Period        string // YYYY-MM
	Amount        int64
	PaidOn        time.Time
	Method        Method
	BankAccount   string
	Reference     string
	Treasurer     string // nama pencatat pembayaran
	// VoidReason diisi jika pembayaran dibatalkan (pembatalan disetujui).
	Voided     bool
	VoidReason string
	// PrintedAt adalah tanggal cetak (Asia/Jakarta), diisi handler.
	PrintedAt time.Time
}

// GetReceipt mengambil data kuitansi pembayaran milik schoolID.
func GetReceipt(ctx context.Context, q database.DBTX, schoolID, paymentID string) (Receipt, error) {
	if !database.IsUUID(paymentID) {
		return Receipt{}, ErrPaymentNotFound
	}
	var r Receipt
	err := q.QueryRow(ctx, `
		SELECT p.receipt_number, sc.name, coalesce(sc.address, ''), st.full_name, st.nis, c.name, y.name,
		       coalesce(`+feeNamesSQL+`, ''), i.period, p.amount, p.paid_on, p.method,
		       coalesce(p.bank_account, ''), coalesce(p.reference, ''), u.full_name,
		       v.id IS NOT NULL, coalesce(v.reason, '')
		FROM payments p
		JOIN schools sc ON sc.id = p.school_id
		JOIN invoices i ON i.school_id = p.school_id AND i.id = p.invoice_id
		JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
		JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		JOIN academic_years y ON y.school_id = i.school_id AND y.id = i.academic_year_id
		JOIN users u ON u.school_id = p.school_id AND u.id = p.recorded_by
		LEFT JOIN payment_voids v ON v.payment_id = p.id AND v.status = 'approved'
		WHERE p.school_id = $1 AND p.id = $2`, schoolID, paymentID).Scan(
		&r.Number, &r.SchoolName, &r.SchoolAddress, &r.StudentName, &r.NIS, &r.ClassName, &r.YearName,
		&r.FeeNames, &r.Period, &r.Amount, &r.PaidOn, &r.Method, &r.BankAccount, &r.Reference, &r.Treasurer,
		&r.Voided, &r.VoidReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, ErrPaymentNotFound
	}
	if err != nil {
		return Receipt{}, mapError("get receipt", err)
	}
	return r, nil
}

// ReceiptFilename adalah nama file unduhan, mis. "kuitansi-KW-2026-000001.pdf".
func ReceiptFilename(number string) string {
	return "kuitansi-" + strings.ReplaceAll(number, "/", "-") + ".pdf"
}

// WriteReceiptPDF menulis kuitansi berukuran A5 mendatar ke w. compress
// false hanya untuk uji (teks bisa dicari di isi PDF).
func WriteReceiptPDF(w io.Writer, r Receipt, compress bool) error {
	pdf := fpdf.New("L", "mm", "A5", "")
	pdf.SetCompression(compress)
	pdf.SetTitle("Kuitansi "+r.Number, true)
	pdf.SetCreator("AKRA", true)
	pdf.SetMargins(14, 12, 14)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()
	// Font inti PDF memakai cp1252; teks UTF-8 diterjemahkan.
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pageW, pageH := pdf.GetPageSize()
	left, top, right, _ := pdf.GetMargins()
	width := pageW - left - right

	if r.Voided {
		// Watermark di belakang isi.
		pdf.SetTextColor(244, 199, 199)
		pdf.SetFont("Helvetica", "B", 64)
		pdf.TransformBegin()
		pdf.TransformRotate(20, pageW/2, pageH/2)
		pdf.SetXY(0, pageH/2-14)
		pdf.CellFormat(pageW, 28, "DIBATALKAN", "", 0, "C", false, 0, "")
		pdf.TransformEnd()
		pdf.SetTextColor(0, 0, 0)
		pdf.SetXY(left, top)
	}

	// Kop sekolah.
	pdf.SetFont("Helvetica", "B", 14)
	pdf.CellFormat(width, 7, tr(r.SchoolName), "", 1, "L", false, 0, "")
	if r.SchoolAddress != "" {
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(width, 4.5, tr(r.SchoolAddress), "", "L", false)
	}
	y := pdf.GetY() + 2
	pdf.SetLineWidth(0.6)
	pdf.Line(left, y, pageW-right, y)
	pdf.SetY(y + 4)

	// Judul dan nomor.
	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(width/2, 7, "KUITANSI PEMBAYARAN", "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(width/2, 7, "No. "+tr(r.Number), "", 1, "R", false, 0, "")
	pdf.Ln(3)

	method := methodLabels[r.Method]
	if r.Method == MethodTransfer {
		method += " ke " + r.BankAccount
		if r.Reference != "" {
			method += " (ref. " + r.Reference + ")"
		}
	}
	rows := [][2]string{
		{"Telah terima dari", "Orang tua/wali " + r.StudentName},
		{"Siswa", fmt.Sprintf("%s (NIS %s), kelas %s, %s", r.StudentName, r.NIS, r.ClassName, r.YearName)},
		{"Untuk pembayaran", r.FeeNames + " " + billing.PeriodLabel(r.Period)},
		{"Tanggal bayar", clock.FormatDateID(r.PaidOn)},
		{"Metode", method},
	}
	const labelW = 38
	for _, row := range rows {
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(labelW, 6, tr(row[0]), "", 0, "L", false, 0, "")
		pdf.CellFormat(4, 6, ":", "", 0, "L", false, 0, "")
		pdf.MultiCell(width-labelW-4, 6, tr(row[1]), "", "L", false)
	}
	pdf.Ln(2)

	// Nominal dan terbilang.
	const boxH = 11.0
	boxTop := pdf.GetY()
	pdf.SetFont("Helvetica", "B", 16)
	pdf.SetFillColor(239, 246, 255)
	pdf.CellFormat(70, boxH, tr(money.FormatRupiah(r.Amount)), "1", 0, "C", true, 0, "")
	pdf.SetFont("Helvetica", "I", 10)
	pdf.SetX(left + 74)
	pdf.MultiCell(width-74, 5.5, tr("Terbilang: "+money.TerbilangRupiah(r.Amount)), "", "L", false)
	// Terbilang bisa lebih pendek atau lebih tinggi dari kotak nominal.
	pdf.SetY(max(pdf.GetY(), boxTop+boxH) + 3)

	if r.Voided {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetTextColor(185, 28, 28)
		pdf.MultiCell(width, 5.5, tr("Pembayaran ini DIBATALKAN. Alasan: "+r.VoidReason), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}

	// Tanda tangan bendahara di kanan bawah.
	sigW := 62.0
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetXY(pageW-right-sigW, pageH-44)
	pdf.CellFormat(sigW, 5, tr(clock.FormatDateID(r.PaidOn)), "", 2, "C", false, 0, "")
	pdf.CellFormat(sigW, 5, "Bendahara,", "", 2, "C", false, 0, "")
	pdf.SetXY(pageW-right-sigW, pageH-24)
	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(sigW, 5, tr(r.Treasurer), "T", 0, "C", false, 0, "")

	pdf.SetFont("Helvetica", "", 7)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(left, pageH-14)
	pdf.CellFormat(width-sigW, 4, tr("Dicetak dari AKRA, "+clock.FormatDateID(r.PrintedAt)+"."), "", 0, "L", false, 0, "")

	if err := pdf.Error(); err != nil {
		return fmt.Errorf("payments: render receipt: %w", err)
	}
	return pdf.Output(w)
}
