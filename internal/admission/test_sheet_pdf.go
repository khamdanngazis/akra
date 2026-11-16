package admission

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/jackc/pgx/v5"

	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// TestSheetData adalah isi lembar penilaian satu tes untuk penguji (T119).
// Hanya nomor pendaftaran, nama, dan jalur; tanpa data pribadi lain.
type TestSheetData struct {
	SchoolName string
	PeriodName string
	Test       SelectionTest
	Rows       []TestSheetRow
	PrintedAt  time.Time
	// Loc adalah zona waktu sekolah untuk jadwal (T126); nil = WIB.
	Loc *time.Location
}

// TestSheetRow adalah satu peserta tes.
type TestSheetRow struct {
	RegistrationNumber, FullName, TrackName string
}

// testParticipants mengembalikan peserta tes periode: pendaftar berstatus
// dikirim atau diverifikasi di jalur metode tes, urut nomor pendaftaran.
func testParticipants(ctx context.Context, q database.DBTX, schoolID, periodID string) ([]TestSheetRow, error) {
	rows, err := q.Query(ctx, `
		SELECT coalesce(a.registration_number, ''), a.full_name, t.name
		FROM applicants a JOIN admission_tracks t ON t.id = a.track_id AND t.method = 'tes'
		WHERE a.school_id = $1 AND a.period_id = $2 AND a.status IN ('dikirim', 'diverifikasi')
		ORDER BY a.registration_number`, schoolID, periodID)
	if err != nil {
		return nil, mapError("test participants", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TestSheetRow, error) {
		var x TestSheetRow
		return x, r.Scan(&x.RegistrationNumber, &x.FullName, &x.TrackName)
	})
}

// WriteTestSheetPDF menulis lembar penilaian A4: keterangan tes, acuan
// penilaian, dan tabel peserta dengan kolom nilai dan catatan kosong.
func WriteTestSheetPDF(w io.Writer, d TestSheetData, compress bool) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(compress)
	pdf.SetTitle("Lembar Penilaian "+d.Test.Name, true)
	pdf.SetCreator("AKRA", true)
	pdf.SetCreationDate(d.PrintedAt)
	pdf.SetModificationDate(d.PrintedAt)
	pdf.SetCatalogSort(true)
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 18)
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	width := pageW - left - right

	cols := []struct {
		title string
		w     float64
		align string
	}{{"No", 10, "C"}, {"Nomor", 34, "L"}, {"Nama", 64, "L"}, {"Jalur", 26, "L"}, {"Nilai", 18, "C"}, {"Catatan", width - 152, "L"}}
	header := func() {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetFillColor(238, 242, 247)
		for _, c := range cols {
			pdf.CellFormat(c.w, 7, tr(c.title), "1", 0, "C", true, 0, "")
		}
		pdf.Ln(-1)
	}
	pdf.SetHeaderFunc(func() {
		if pdf.PageNo() > 1 {
			header()
		}
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.CellFormat(width, 4, tr(d.Test.Name+" · halaman "+strconv.Itoa(pdf.PageNo())), "", 0, "R", false, 0, "")
	})
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 13)
	pdf.MultiCell(width, 6, tr(d.SchoolName), "", "L", false)
	y := pdf.GetY() + 1
	pdf.SetLineWidth(0.5)
	pdf.Line(left, y, pageW-right, y)
	pdf.SetLineWidth(0.2)
	pdf.SetY(y + 4)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(width, 7, tr("LEMBAR PENILAIAN "+d.Test.Name), "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(width, 5, tr(d.PeriodName), "", 1, "C", false, 0, "")
	pdf.Ln(3)

	info := [][2]string{{"Nilai", "0–" + strconv.Itoa(d.Test.MaxScore)}}
	if d.Test.PassScore > 0 {
		info[0][1] += " (minimal lulus " + strconv.Itoa(d.Test.PassScore) + ")"
	}
	if d.Test.ScheduledAt != nil {
		loc := d.Loc
		if loc == nil {
			loc = clock.Jakarta
		}
		info = append(info, [2]string{"Jadwal", clock.FormatDateTimeID(*d.Test.ScheduledAt, loc)})
	}
	if d.Test.Location != "" {
		info = append(info, [2]string{"Lokasi", d.Test.Location})
	}
	info = append(info, [2]string{"Penguji", ""})
	for _, row := range info {
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(24, 6, tr(row[0]), "", 0, "L", false, 0, "")
		pdf.CellFormat(4, 6, ":", "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "B", 10)
		if row[1] == "" {
			x, y := pdf.GetXY()
			pdf.Line(x, y+5, x+80, y+5)
			pdf.Ln(6)
			continue
		}
		pdf.MultiCell(width-28, 6, tr(row[1]), "", "L", false)
	}
	if d.Test.Rubric != "" {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(width, 6, "Acuan penilaian", "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9.5)
		pdf.SetFillColor(248, 250, 252)
		pdf.MultiCell(width, 5, tr(d.Test.Rubric), "1", "L", true)
	}
	pdf.Ln(4)

	header()
	pdf.SetFont("Helvetica", "", 9.5)
	if len(d.Rows) == 0 {
		pdf.CellFormat(width, 8, tr("Belum ada peserta (pendaftar berstatus dikirim atau diverifikasi di jalur metode tes)."), "1", 1, "C", false, 0, "")
	}
	for i, r := range d.Rows {
		vals := []string{strconv.Itoa(i + 1), r.RegistrationNumber, r.FullName, r.TrackName, "", ""}
		for j, c := range cols {
			text := tr(vals[j])
			for pdf.GetStringWidth(text) > c.w-2 && len(text) > 1 {
				text = text[:len(text)-1]
			}
			pdf.CellFormat(c.w, 9, text, "1", 0, c.align, false, 0, "")
		}
		pdf.Ln(-1)
	}
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(width-70, 5, "", "", 0, "L", false, 0, "")
	pdf.CellFormat(70, 5, tr("Penguji,"), "", 1, "C", false, 0, "")
	pdf.Ln(16)
	pdf.CellFormat(width-70, 5, "", "", 0, "L", false, 0, "")
	pdf.CellFormat(70, 5, "(..............................................)", "", 1, "C", false, 0, "")
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.CellFormat(width, 4, tr("Dicetak dari AKRA, "+clock.FormatDateID(d.PrintedAt)+". Masukkan nilai ke AKRA di menu Tes seleksi > Isi nilai."), "", 1, "L", false, 0, "")
	return pdf.Output(w)
}
