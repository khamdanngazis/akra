package exam

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"akra/internal/platform/clock"
)

// docHeader menulis kop sekolah dan judul dokumen.
func docHeader(pdf *fpdf.Fpdf, tr func(string) string, lh Letterhead, title string, lines ...string) {
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	width := pageW - left - right
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(width, 6, tr(lh.School), "", 1, "C", false, 0, "")
	if lh.Address != "" {
		pdf.SetFont("Helvetica", "", 8)
		pdf.CellFormat(width, 4, tr(lh.Address), "", 1, "C", false, 0, "")
	}
	y := pdf.GetY() + 1
	pdf.SetLineWidth(0.6)
	pdf.Line(left, y, pageW-right, y)
	pdf.SetLineWidth(0.2)
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(width, 6, tr(title), "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	for _, l := range lines {
		if l != "" {
			pdf.CellFormat(width, 5, tr(l), "", 1, "C", false, 0, "")
		}
	}
	pdf.Ln(3)
}

func sessionLines(d SessionDoc) []string {
	subj := d.ExamName
	if d.Subject != "" && !strings.Contains(d.ExamName, d.Subject) {
		subj += " · " + d.Subject
	}
	where := d.Session.Title()
	if d.Room != "" && d.Session.Label == "" {
		where = d.Room
	}
	return []string{subj, d.When + " · " + where}
}

func proctorNames(d SessionDoc) string {
	if len(d.Session.Proctors) == 0 {
		return ""
	}
	return strings.Join(d.Session.Proctors, ", ")
}

// signBlock menulis kolom tanda tangan di kanan.
func signBlock(pdf *fpdf.Fpdf, tr func(string) string, place, role, name string) {
	pageW, _ := pdf.GetPageSize()
	_, _, right, _ := pdf.GetMargins()
	x := pageW - right - 70
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 9)
	if place != "" {
		pdf.SetX(x)
		pdf.CellFormat(70, 5, tr(place), "", 1, "C", false, 0, "")
	}
	pdf.SetX(x)
	pdf.CellFormat(70, 5, tr(role), "", 1, "C", false, 0, "")
	pdf.Ln(16)
	pdf.SetX(x)
	label := "(................................................)"
	if name != "" {
		label = name
	}
	pdf.CellFormat(70, 5, tr(label), "", 1, "C", false, 0, "")
}

// WriteAttendancePDF menulis daftar hadir: satu halaman (atau lebih) per
// sesi, dengan kolom tanda tangan berselang kiri-kanan.
func WriteAttendancePDF(w io.Writer, lh Letterhead, docs []SessionDoc, printedAt time.Time, compress bool) error {
	pdf, tr := newDocPDF("Daftar Hadir Peserta Ujian", "P", printedAt, compress)
	pdf.SetMargins(15, 12, 15)
	pdf.SetAutoPageBreak(true, 15)
	pageW, _ := pdf.GetPageSize()
	width := pageW - 30
	cols := []struct {
		t string
		w float64
		a string
	}{{"No", 9, "C"}, {"No. peserta", 30, "L"}, {"Nama", 62, "L"}, {"Kelas", 16, "C"}, {"Paket", 13, "C"}, {"Tanda tangan", width - 130, "L"}}
	for _, d := range docs {
		pdf.AddPage()
		docHeader(pdf, tr, lh, "DAFTAR HADIR PESERTA", sessionLines(d)...)
		head := func() {
			pdf.SetFont("Helvetica", "B", 8.5)
			pdf.SetFillColor(238, 242, 247)
			for _, c := range cols {
				pdf.CellFormat(c.w, 7, tr(c.t), "1", 0, "C", true, 0, "")
			}
			pdf.Ln(-1)
		}
		head()
		pdf.SetFont("Helvetica", "", 8.5)
		for i, p := range d.Participants {
			if pdf.GetY() > 270 {
				pdf.AddPage()
				head()
				pdf.SetFont("Helvetica", "", 8.5)
			}
			sig := strconv.Itoa(i+1) + ". ........................"
			align := "L"
			if i%2 == 1 {
				align = "R"
			}
			vals := []string{strconv.Itoa(i + 1), p.Number, p.Name, p.ClassName, p.Variant, sig}
			for k, c := range cols {
				a := c.a
				if k == len(cols)-1 {
					a = align
				}
				pdf.CellFormat(c.w, 8, fitText(pdf, tr, vals[k], c.w-1), "1", 0, a, false, 0, "")
			}
			pdf.Ln(-1)
		}
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "", 9)
		pdf.CellFormat(width, 5, tr(fmt.Sprintf("Peserta terdaftar: %d    Hadir: ........    Tidak hadir: ........", len(d.Participants))), "", 1, "L", false, 0, "")
		signBlock(pdf, tr, "", "Pengawas", proctorNames(d))
	}
	if len(docs) == 0 {
		pdf.AddPage()
	}
	return pdf.Output(w)
}

// WriteMinutesPDF menulis berita acara pelaksanaan per sesi. Jumlah hadir
// dan kejadian diisi otomatis setelah data pelaksanaan tersedia (T143);
// sampai itu kolomnya diisi pengawas.
func WriteMinutesPDF(w io.Writer, lh Letterhead, docs []SessionDoc, attendance map[string]SessionAttendance, printedAt time.Time, compress bool) error {
	pdf, tr := newDocPDF("Berita Acara Pelaksanaan Ujian", "P", printedAt, compress)
	pdf.SetMargins(20, 15, 20)
	pdf.SetAutoPageBreak(true, 15)
	pageW, _ := pdf.GetPageSize()
	width := pageW - 40
	for _, d := range docs {
		pdf.AddPage()
		docHeader(pdf, tr, lh, "BERITA ACARA PELAKSANAAN UJIAN", d.ExamName)
		a := d.Session.StartsAt.In(lh.Loc)
		b := d.Session.EndsAt.In(lh.Loc)
		pdf.SetFont("Helvetica", "", 10)
		intro := fmt.Sprintf("Pada hari ini %s tanggal %s, telah diselenggarakan %s%s dari pukul %s sampai %s %s.",
			dayNames[a.Weekday()], clock.FormatDateID(a), d.ExamName, map[bool]string{true: " mata pelajaran " + d.Subject, false: ""}[d.Subject != "" && !strings.Contains(d.ExamName, d.Subject)],
			a.Format("15.04"), b.Format("15.04"), clock.ZoneLabel(lh.Loc))
		pdf.MultiCell(width, 5.5, tr(intro), "", "J", false)
		pdf.Ln(2)
		at, ok := attendance[d.Session.ID]
		present, absent, absentNos, notes := "........", "........", "", ""
		if ok {
			present, absent = strconv.Itoa(at.Present), strconv.Itoa(at.Absent)
			absentNos = strings.Join(at.AbsentNumbers, ", ")
			notes = strings.Join(at.Notes, "\n")
		}
		rows := [][2]string{
			{"Ruang / sesi", d.Session.Title()},
			{"Lokasi", d.Session.LocationLabel()},
			{"Peserta terdaftar", strconv.Itoa(len(d.Participants))},
			{"Hadir", present},
			{"Tidak hadir", absent},
			{"Nomor peserta tidak hadir", absentNos},
		}
		for _, r := range rows {
			pdf.SetFont("Helvetica", "", 10)
			pdf.CellFormat(60, 7, tr(r[0]), "1", 0, "L", false, 0, "")
			val := r[1]
			if val == "" {
				val = " "
			}
			pdf.MultiCell(width-60, 7, tr(val), "1", "L", false)
		}
		pdf.Ln(3)
		pdf.CellFormat(width, 6, tr("Catatan selama pelaksanaan:"), "", 1, "L", false, 0, "")
		if notes != "" {
			pdf.MultiCell(width, 5.5, tr(notes), "1", "L", false)
		} else {
			for i := 0; i < 6; i++ {
				pdf.CellFormat(width, 7, "", "B", 1, "L", false, 0, "")
			}
		}
		signer := proctorNames(d)
		if ok && at.SignedAt != nil {
			signer = at.SignedBy
		}
		signBlock(pdf, tr, "", "Pengawas", signer)
		if ok && at.SignedAt != nil {
			pdf.SetFont("Helvetica", "I", 8)
			pdf.MultiCell(width, 4.5, tr("Ditandatangani secara elektronik oleh "+at.SignedBy+" pada "+
				clock.FormatDateID(at.SignedAt.In(lh.Loc))+" pukul "+at.SignedAt.In(lh.Loc).Format("15.04")+" "+clock.ZoneLabel(lh.Loc)+
				" (dikonfirmasi dengan sandi akun)."), "", "R", false)
		}
	}
	if len(docs) == 0 {
		pdf.AddPage()
	}
	return pdf.Output(w)
}

// SessionAttendance adalah data pelaksanaan satu sesi untuk berita acara
// (diisi dari attempt dan catatan pengawas, T143).
type SessionAttendance struct {
	Present, Absent int
	AbsentNumbers   []string
	Notes           []string
	// Ditandatangani pengawas di layar pengawas (konfirmasi sandi).
	SignedBy string
	SignedAt *time.Time
}

// WriteSeatingPDF menulis denah tempat duduk: satu halaman per ruang+
// gelombang, kursi berurutan per baris dari depan.
func WriteSeatingPDF(w io.Writer, lh Letterhead, title string, plans []RoomPlan, printedAt time.Time, compress bool) error {
	pdf, tr := newDocPDF("Denah Tempat Duduk", "L", printedAt, compress)
	pdf.SetMargins(12, 10, 12)
	pdf.SetAutoPageBreak(false, 0)
	pageW, pageH := pdf.GetPageSize()
	width := pageW - 24
	for _, p := range plans {
		pdf.AddPage()
		sub := p.Room.Name
		if p.Shift > 1 {
			sub += " · Gelombang " + strconv.Itoa(p.Shift)
		}
		docHeader(pdf, tr, lh, "DENAH TEMPAT DUDUK", title, sub)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetFillColor(226, 232, 240)
		pdf.CellFormat(width, 7, tr("DEPAN · Meja pengawas"), "1", 1, "C", true, 0, "")
		pdf.Ln(3)
		cols := 5
		if p.Capacity <= 20 {
			cols = 4
		}
		if p.Capacity > 40 {
			cols = 6
		}
		gap := 3.0
		bw := (width - float64(cols-1)*gap) / float64(cols)
		rows := (len(p.Seats) + cols - 1) / cols
		top := pdf.GetY()
		bh := min(18.0, (pageH-top-12)/float64(max(rows, 1))-gap)
		for i, s := range p.Seats {
			col, row := i%cols, i/cols
			x := 12 + float64(col)*(bw+gap)
			y := top + float64(row)*(bh+gap)
			pdf.Rect(x, y, bw, bh, "D")
			pdf.SetXY(x+1, y+1)
			pdf.SetFont("Helvetica", "B", 8)
			pdf.CellFormat(bw-2, 4, tr(fmt.Sprintf("%d · %s", s.Seat, s.Number)), "", 2, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 7.5)
			pdf.CellFormat(bw-2, 4, fitText(pdf, tr, s.Name, bw-2), "", 2, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 7)
			pdf.CellFormat(bw-2, 3.5, tr(s.ClassName), "", 2, "L", false, 0, "")
		}
	}
	if len(plans) == 0 {
		pdf.AddPage()
	}
	return pdf.Output(w)
}

// WriteDoorSignPDF menulis tempelan pintu ruang: nama ruang, gelombang,
// rentang nomor peserta, dan jadwal.
func WriteDoorSignPDF(w io.Writer, lh Letterhead, title string, plans []RoomPlan, printedAt time.Time, compress bool) error {
	pdf, tr := newDocPDF("Tempelan Pintu Ruang", "P", printedAt, compress)
	pdf.SetMargins(20, 20, 20)
	pdf.SetAutoPageBreak(true, 15)
	pageW, _ := pdf.GetPageSize()
	width := pageW - 40
	byRoom := map[string][]RoomPlan{}
	var order []string
	for _, p := range plans {
		if _, ok := byRoom[p.Room.ID]; !ok {
			order = append(order, p.Room.ID)
		}
		byRoom[p.Room.ID] = append(byRoom[p.Room.ID], p)
	}
	for _, id := range order {
		ps := byRoom[id]
		pdf.AddPage()
		pdf.SetFont("Helvetica", "", 12)
		pdf.CellFormat(width, 7, tr(lh.School), "", 1, "C", false, 0, "")
		pdf.CellFormat(width, 7, tr(title), "", 1, "C", false, 0, "")
		pdf.Ln(12)
		pdf.SetFont("Helvetica", "B", 54)
		pdf.CellFormat(width, 26, tr(ps[0].Room.Name), "", 1, "C", false, 0, "")
		pdf.Ln(8)
		for _, p := range ps {
			pdf.SetFont("Helvetica", "B", 18)
			label := "Peserta"
			if len(ps) > 1 || p.Shift > 1 {
				label = "Gelombang " + strconv.Itoa(p.Shift)
			}
			pdf.CellFormat(width, 10, tr(label), "", 1, "C", false, 0, "")
			pdf.SetFont("Helvetica", "", 22)
			pdf.CellFormat(width, 12, tr(fmt.Sprintf("No. %s – %s", p.FirstNo, p.LastNo)), "", 1, "C", false, 0, "")
			pdf.SetFont("Helvetica", "", 12)
			pdf.CellFormat(width, 7, tr(fmt.Sprintf("%d peserta", len(p.Seats))), "", 1, "C", false, 0, "")
			pdf.SetFont("Helvetica", "", 10)
			for _, t := range p.Times {
				pdf.CellFormat(width, 5.5, tr(t), "", 1, "C", false, 0, "")
			}
			pdf.Ln(6)
		}
	}
	if len(order) == 0 {
		pdf.AddPage()
	}
	return pdf.Output(w)
}
