package grades

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/jackc/pgx/v5"

	"akra/internal/attendance"
	"akra/internal/classes"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// Rapor PDF (T74). Data rapor diambil dari ClassSummary (angka sama dengan
// rekap T72); tata letak ada di satu fungsi, WriteReportCardPDF, sehingga
// perubahan layout tidak menyentuh data.

// ActionReportCardPrint adalah aksi audit setiap cetak rapor.
const ActionReportCardPrint = "report_card.print"

var (
	// ErrEnrollmentNotFound: enrollment tidak ada, milik sekolah lain, bukan
	// tahun ajaran aktif, atau siswa sudah tidak terdaftar di kelas itu.
	ErrEnrollmentNotFound = errors.New("grades: enrollment not found")
	// ErrInvalidSemester: semester bukan 1 atau 2.
	ErrInvalidSemester = errors.New("grades: semester must be 1 or 2")
)

// IncompleteError: rapor tidak dicetak karena ada mapel yang belum lengkap.
type IncompleteError struct {
	Subjects []string // "Matematika (2 komponen belum dinilai)", ...
}

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("grades: report card incomplete: %d subjects", len(e.Subjects))
}

// CardSubject adalah satu baris mapel di rapor.
type CardSubject struct {
	Name   string
	KKM    int
	Score  int
	Letter string
	Note   string // catatan guru; kosong jika tidak diisi (belum ada sumbernya)
}

// ReportCard adalah isi rapor satu siswa untuk satu semester.
type ReportCard struct {
	SchoolName, SchoolAddress string
	StudentName, NIS          string
	ClassName, YearName       string
	Semester                  int // 1 = Ganjil, 2 = Genap
	KKMLabel                  string
	Subjects                  []CardSubject
	Attendance                attendance.Counts
	AttendanceFrom            time.Time
	AttendanceTo              time.Time
	HomeroomName              string
	PrincipalName             string
	SignerTitle               string
	PrintedAt                 time.Time // tanggal cetak (Asia/Jakarta)
}

// SemesterLabel mengembalikan "1 (Ganjil)" atau "2 (Genap)".
func SemesterLabel(s int) string {
	if s == 2 {
		return "2 (Genap)"
	}
	return "1 (Ganjil)"
}

// DefaultSemester menebak semester dari tanggal (Asia/Jakarta): Juli–Desember
// ganjil, Januari–Juni genap.
func DefaultSemester(t time.Time) int {
	if t.In(clock.Jakarta).Month() >= time.July {
		return 1
	}
	return 2
}

// semesterRange mengembalikan rentang kehadiran semester s di tahun ajaran
// [start, end]: ganjil dari awal tahun ajaran sampai 31 Desember, genap dari
// 1 Januari sampai akhir tahun ajaran. Nilai tetap nilai tahun ajaran
// (komponen nilai belum per semester).
func semesterRange(s int, start, end time.Time) (time.Time, time.Time) {
	newYear := time.Date(start.Year()+1, time.January, 1, 0, 0, 0, 0, time.UTC)
	if s == 1 {
		return start, minTime(end, newYear.AddDate(0, 0, -1))
	}
	return maxTime(start, newYear), end
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// CardEnrollment adalah enrollment aktif beserta kelasnya, untuk rapor.
type CardEnrollment struct {
	ID, StudentID string
	Class         classes.Class
}

// GetCardEnrollment mengembalikan enrollment aktif (masih terdaftar) di
// kelas tahun ajaran aktif milik schoolID.
func GetCardEnrollment(ctx context.Context, q database.DBTX, schoolID, enrollmentID string) (CardEnrollment, error) {
	if !database.IsUUID(enrollmentID) {
		return CardEnrollment{}, ErrEnrollmentNotFound
	}
	var e CardEnrollment
	var classID string
	err := q.QueryRow(ctx, `SELECT e.id, e.student_id, e.class_id FROM enrollments e
		WHERE e.school_id = $1 AND e.id = $2 AND e.end_date IS NULL`, schoolID, enrollmentID).Scan(&e.ID, &e.StudentID, &classID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CardEnrollment{}, ErrEnrollmentNotFound
	}
	if err != nil {
		return CardEnrollment{}, fmt.Errorf("grades: get enrollment: %w", err)
	}
	if e.Class, err = activeClass(ctx, q, schoolID, classID); err != nil {
		if errors.Is(err, ErrClassNotFound) {
			return CardEnrollment{}, ErrEnrollmentNotFound
		}
		return CardEnrollment{}, err
	}
	return e, nil
}

// BuildReportCard menyusun rapor enrollment e untuk semester (1 atau 2).
// IncompleteError jika ada mapel yang belum lengkap atau belum punya
// komponen nilai; rapor parsial tidak pernah dibuat.
func BuildReportCard(ctx context.Context, q database.DBTX, schoolID string, e CardEnrollment, semester int, printedAt time.Time) (ReportCard, error) {
	if semester != 1 && semester != 2 {
		return ReportCard{}, ErrInvalidSemester
	}
	summary, err := ClassSummary(ctx, q, schoolID, e.Class)
	if err != nil {
		return ReportCard{}, err
	}
	cfg, err := LoadReportConfig(ctx, q, schoolID)
	if err != nil {
		return ReportCard{}, err
	}
	card := ReportCard{ClassName: e.Class.Name, Semester: semester, KKMLabel: cfg.ThresholdLabel(), HomeroomName: e.Class.HomeroomTeacherName,
		PrincipalName: cfg.PrincipalName, SignerTitle: cfg.SignerTitle, PrintedAt: printedAt}
	var missing []string
	for _, sub := range summary.Subjects {
		var row *StudentGrades
		for i := range sub.Rows {
			if sub.Rows[i].EnrollmentID == e.ID {
				row = &sub.Rows[i]
			}
		}
		switch {
		case !sub.Configured:
			missing = append(missing, sub.Subject.Name+" (komponen nilai belum diatur)")
		case row == nil:
			return ReportCard{}, ErrEnrollmentNotFound
		case row.Result.Incomplete:
			missing = append(missing, fmt.Sprintf("%s (%d komponen belum dinilai)", sub.Subject.Name, row.Result.Missing))
		default:
			card.Subjects = append(card.Subjects, CardSubject{Name: sub.Subject.Name, KKM: sub.KKM, Score: row.Result.Score, Letter: row.Letter})
		}
	}
	if len(summary.Subjects) == 0 {
		missing = append(missing, "Belum ada mapel dengan guru pengampu di kelas ini")
	}
	if len(missing) > 0 {
		return ReportCard{}, &IncompleteError{Subjects: missing}
	}

	var yearStart, yearEnd time.Time
	err = q.QueryRow(ctx, `
		SELECT sc.name, coalesce(sc.address, ''), st.full_name, st.nis, y.name, y.start_date, y.end_date
		FROM enrollments e
		JOIN schools sc ON sc.id = e.school_id
		JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
		JOIN academic_years y ON y.school_id = e.school_id AND y.id = e.academic_year_id
		WHERE e.school_id = $1 AND e.id = $2`, schoolID, e.ID).Scan(
		&card.SchoolName, &card.SchoolAddress, &card.StudentName, &card.NIS, &card.YearName, &yearStart, &yearEnd)
	if err != nil {
		return ReportCard{}, fmt.Errorf("grades: report card identity: %w", err)
	}
	card.AttendanceFrom, card.AttendanceTo = semesterRange(semester, yearStart, yearEnd)
	// Kehadiran harian (T27) dari setiap kelas yang ditempati siswa di tahun
	// ajaran ini, dijumlahkan, sama seperti detail anak di portal.
	rows, err := q.Query(ctx, `SELECT DISTINCT class_id FROM enrollments WHERE school_id = $1 AND student_id = $2 AND academic_year_id = $3`,
		schoolID, e.StudentID, e.Class.AcademicYearID)
	if err != nil {
		return ReportCard{}, fmt.Errorf("grades: student classes: %w", err)
	}
	classIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return ReportCard{}, fmt.Errorf("grades: student classes: %w", err)
	}
	for _, classID := range classIDs {
		list, err := attendance.SummaryByStudent(ctx, q, schoolID, classID, "", card.AttendanceFrom, card.AttendanceTo)
		if err != nil {
			return ReportCard{}, err
		}
		for _, r := range list {
			if r.StudentID == e.StudentID {
				card.Attendance.Hadir += r.Hadir
				card.Attendance.Sakit += r.Sakit
				card.Attendance.Izin += r.Izin
				card.Attendance.Alpa += r.Alpa
			}
		}
	}
	return card, nil
}

// ReportCardFilename adalah nama file unduhan, mis. "rapor-2026001-1A-smt1.pdf".
func ReportCardFilename(c ReportCard) string {
	clean := strings.NewReplacer("/", "-", " ", "", `"`, "")
	return fmt.Sprintf("rapor-%s-%s-smt%d.pdf", clean.Replace(c.NIS), clean.Replace(c.ClassName), c.Semester)
}

// WriteReportCardPDF menulis rapor A4 tegak ke w. Seluruh tata letak ada di
// fungsi ini. compress false hanya untuk uji (teks bisa dicari di PDF).
func WriteReportCardPDF(w io.Writer, c ReportCard, compress bool) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(compress)
	pdf.SetTitle("Rapor "+c.StudentName, true)
	pdf.SetCreator("AKRA", true)
	// Tanggal dokumen = tanggal cetak, sehingga cetak ulang di hari yang sama identik.
	pdf.SetCreationDate(c.PrintedAt)
	pdf.SetModificationDate(c.PrintedAt)
	pdf.SetCatalogSort(true)
	pdf.SetMargins(18, 16, 18)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	width := pageW - left - right

	// Kop sekolah.
	pdf.SetFont("Helvetica", "B", 14)
	pdf.CellFormat(width, 7, tr(c.SchoolName), "", 1, "C", false, 0, "")
	if c.SchoolAddress != "" {
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(width, 4.5, tr(c.SchoolAddress), "", "C", false)
	}
	y := pdf.GetY() + 2
	pdf.SetLineWidth(0.6)
	pdf.Line(left, y, pageW-right, y)
	pdf.SetY(y + 5)
	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(width, 7, "LAPORAN HASIL BELAJAR SISWA", "", 1, "C", false, 0, "")
	pdf.Ln(3)

	// Identitas: dua kolom.
	half := width / 2
	ident := [][2][2]string{
		{{"Nama siswa", c.StudentName}, {"Kelas", c.ClassName}},
		{{"NIS", c.NIS}, {"Tahun ajaran", c.YearName}},
		{{"", ""}, {"Semester", SemesterLabel(c.Semester)}},
	}
	pdf.SetFont("Helvetica", "", 10)
	for _, line := range ident {
		for _, cell := range line {
			if cell[0] == "" {
				pdf.CellFormat(half, 6, "", "", 0, "L", false, 0, "")
				continue
			}
			pdf.CellFormat(28, 6, tr(cell[0]), "", 0, "L", false, 0, "")
			pdf.CellFormat(4, 6, ":", "", 0, "L", false, 0, "")
			pdf.CellFormat(half-32, 6, tr(cell[1]), "", 0, "L", false, 0, "")
		}
		pdf.Ln(6)
	}
	pdf.Ln(4)

	// Tabel nilai. Lebar kolom dalam mm; kolom mapel mengisi sisanya.
	const wNo, wKKM, wScore, wLetter, wNote = 10, 18, 18, 26, 44
	wName := width - (wNo + wKKM + wScore + wLetter + wNote)
	pdf.SetLineWidth(0.2)
	// row menggambar satu baris tabel. Nama mapel yang panjang dipecah ke
	// beberapa baris; tinggi baris mengikuti.
	row := func(v [6]string, header bool) {
		style, nameAlign := "", "L"
		if header {
			style, nameAlign = "B", "C"
			pdf.SetFillColor(239, 246, 255)
		}
		pdf.SetFont("Helvetica", style, 10)
		lines := pdf.SplitText(tr(v[1]), wName-2)
		text := 0.0 // tinggi teks nama: 5 mm per baris
		for range lines {
			text += 5
		}
		h := max(7.0, text+2)
		if pdf.GetY()+h > 277 {
			pdf.AddPage()
		}
		x, y := pdf.GetXY()
		pdf.CellFormat(wNo, h, tr(v[0]), "1", 0, "C", header, 0, "")
		pdf.CellFormat(wName, h, "", "1", 0, "", header, 0, "")
		pdf.CellFormat(wKKM, h, tr(v[2]), "1", 0, "C", header, 0, "")
		pdf.CellFormat(wScore, h, tr(v[3]), "1", 0, "C", header, 0, "")
		pdf.CellFormat(wLetter, h, tr(v[4]), "1", 0, "C", header, 0, "")
		pdf.CellFormat(wNote, h, tr(v[5]), "1", 0, nameAlign, header, 0, "")
		// Nama mapel ditulis di atas sel kosongnya, di tengah secara vertikal.
		pdf.SetXY(x+wNo, y+(h-text)/2)
		pdf.MultiCell(wName, 5, strings.Join(lines, "\n"), "", nameAlign, false)
		pdf.SetXY(x, y+h)
	}
	row([6]string{"No", "Mata Pelajaran", c.KKMLabel, "Nilai", "Predikat", "Catatan"}, true)
	for i, sub := range c.Subjects {
		row([6]string{fmt.Sprintf("%d", i+1), sub.Name, fmt.Sprintf("%d", sub.KKM), fmt.Sprintf("%d", sub.Score), sub.Letter, sub.Note}, false)
	}
	pdf.Ln(6)

	// Kehadiran.
	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(width, 6, "Ketidakhadiran", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	for _, row := range [][2]string{
		{"Sakit", fmt.Sprintf("%d hari", c.Attendance.Sakit)},
		{"Izin", fmt.Sprintf("%d hari", c.Attendance.Izin)},
		{"Tanpa keterangan", fmt.Sprintf("%d hari", c.Attendance.Alpa)},
	} {
		pdf.CellFormat(50, 6, tr(row[0]), "1", 0, "L", false, 0, "")
		pdf.CellFormat(30, 6, tr(row[1]), "1", 1, "C", false, 0, "")
	}
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(100, 116, 139)
	pdf.CellFormat(width, 5, tr("Periode "+clock.FormatDateID(c.AttendanceFrom)+" – "+clock.FormatDateID(c.AttendanceTo)+"."), "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(10)

	// Tanda tangan: wali kelas di kiri, kepala sekolah di kanan.
	const sigW = 70
	top := pdf.GetY()
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetXY(pageW-right-sigW, top)
	pdf.CellFormat(sigW, 5, tr(clock.FormatDateID(c.PrintedAt)), "", 2, "C", false, 0, "")
	pdf.SetXY(left, top+5)
	pdf.CellFormat(sigW, 5, "Wali Kelas,", "", 0, "C", false, 0, "")
	pdf.SetXY(pageW-right-sigW, top+5)
	pdf.CellFormat(sigW, 5, tr(c.SignerTitle+","), "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetXY(left, top+30)
	pdf.CellFormat(sigW, 5, tr(blankLine(c.HomeroomName)), "T", 0, "C", false, 0, "")
	pdf.SetXY(pageW-right-sigW, top+30)
	pdf.CellFormat(sigW, 5, tr(blankLine(c.PrincipalName)), "T", 0, "C", false, 0, "")

	if err := pdf.Error(); err != nil {
		return fmt.Errorf("grades: render report card: %w", err)
	}
	return pdf.Output(w)
}

// blankLine memberi ruang tulis tangan jika nama penanda tangan kosong.
func blankLine(name string) string {
	if strings.TrimSpace(name) == "" {
		return "(..............................)"
	}
	return name
}
