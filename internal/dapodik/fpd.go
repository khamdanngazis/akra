package dapodik

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"akra/internal/dapodik/ref"
	"akra/internal/platform/clock"
	"akra/internal/students"
)

// FPDRow adalah satu baris Formulir Peserta Didik (F-PD).
type FPDRow struct {
	No    string // nomor di F-PD, mis. "9"
	Label string
	Value string // kosong = dicetak sebagai garis isian
}

// FPDSection adalah satu bagian F-PD.
type FPDSection struct {
	Title string
	Rows  []FPDRow
}

// coded menulis pilihan sebagai "01) Islam", seperti F-PD.
func coded(l ref.List, codes string) string {
	if codes == "" {
		return ""
	}
	var out []string
	for _, c := range strings.Split(codes, ",") {
		if lbl, err := l.Label(c); err == nil {
			out = append(out, c+") "+lbl)
		}
	}
	return strings.Join(out, ", ")
}

// FPD menyusun isi F-PD 2027 untuk satu siswa. Input kosong menghasilkan
// formulir kosong (semua garis isian).
func FPD(in Input) []FPDSection {
	p := func(k string) string { return in.Profile.Secret(k).Reveal() }
	refOf := func(k string) string {
		f, ok := students.LookupProfileField(k)
		if !ok || f.Ref == nil {
			return p(k)
		}
		return coded(*f.Ref, p(k))
	}
	yes := func(k string) string {
		switch p(k) {
		case "true":
			return "Ya"
		case "false":
			if in.Profile.Exists {
				return "Tidak"
			}
		}
		return ""
	}
	birth := ""
	if in.Student.BirthDate != nil {
		birth = clock.FormatDateID(*in.Student.BirthDate)
	}
	date := func(k string) string {
		if t, err := time.Parse("2006-01-02", p(k)); err == nil {
			return clock.FormatDateID(t)
		}
		return ""
	}
	gender := map[string]string{"L": "Laki-laki", "P": "Perempuan"}[in.Student.Gender]

	out := []FPDSection{{Title: "DATA PRIBADI", Rows: []FPDRow{
		{"1", "Nama Lengkap", in.Student.FullName},
		{"2", "Jenis kelamin", gender},
		{"3", "NISN", p("nisn")},
		{"4", "NIK/No. KITAS (untuk WNA)", p("nik")},
		{"5", "No KK", p("kk_number")},
		{"6", "Tempat Lahir", p("birth_place")},
		{"7", "Tanggal Lahir", birth},
		{"8", "No Registrasi Akta Lahir", p("birth_cert_number")},
		{"9", "Agama & Kepercayaan", refOf("religion")},
		{"10", "Kewarganegaraan", strings.TrimSpace(labels(ref.Kewarganegaraan, p("citizenship")) + " " + p("country"))},
		{"11", "Berkebutuhan Khusus", refOf("special_needs")},
		{"12", "Alamat Jalan", p("street")},
		{"13", "RT", p("rt")},
		{"14", "RW", p("rw")},
		{"15", "Nama Dusun", p("hamlet")},
		{"16", "Nama Kelurahan/Desa", p("village")},
		{"17", "Kecamatan", p("district")},
		{"18", "Kode Pos", p("postal_code")},
		{"19", "Lintang", p("latitude")},
		{"20", "Bujur", p("longitude")},
		{"21", "Tempat Tinggal", refOf("residence_type")},
		{"22", "Moda Transportasi", refOf("transport_mode")},
		{"23", "Anak keberapa", p("child_order")},
		{"24", "Pekerjaan (diperuntukan untuk warga belajar)", ""},
		{"25", "Apakah punya KIP", yes("has_kip")},
		{"26", "No. KIP", p("kip_number")},
		{"27", "Alasan layak/menolak PIP", refOf("pip_reason")},
	}}}
	parent := func(title, role string, start int, withNeeds bool) FPDSection {
		g, ok := in.Parents[role]
		var d students.GuardianDetails
		name := ""
		if ok {
			d, name = g.Details, g.Name
		}
		year := ""
		if d.BirthYear > 0 {
			year = strconv.Itoa(d.BirthYear)
		}
		rows := []FPDRow{
			{strconv.Itoa(start), "Nama", name},
			{strconv.Itoa(start + 1), "NIK", d.NIK.Reveal()},
			{strconv.Itoa(start + 2), "Tahun Lahir", year},
			{strconv.Itoa(start + 3), "Pendidikan", coded(ref.Pendidikan, d.Education)},
			{strconv.Itoa(start + 4), "Pekerjaan", coded(ref.Pekerjaan, d.Occupation)},
			{strconv.Itoa(start + 5), "Penghasilan bulanan", coded(ref.Penghasilan, d.Income)},
		}
		if withNeeds {
			rows = append(rows, FPDRow{strconv.Itoa(start + 6), "Berkebutuhan Khusus", coded(ref.KebutuhanKhusus, strings.Join(d.SpecialNeeds, ","))})
		}
		return FPDSection{Title: title, Rows: rows}
	}
	out = append(out,
		parent("DATA AYAH KANDUNG", students.RelationFather, 28, true),
		parent("DATA IBU KANDUNG", students.RelationMother, 35, true),
		parent("DATA WALI", students.RelationGuardian, 42, false),
		FPDSection{Title: "KONTAK", Rows: []FPDRow{
			{"48", "Nomor telepon rumah", p("home_phone")},
			{"49", "Nomor HP", p("mobile_phone")},
			{"50", "Email", p("email")},
		}},
	)
	distance, km := "", p("distance_km")
	if x, err := strconv.ParseFloat(km, 64); err == nil {
		distance = "Lebih dari 1 km"
		if x < 1 {
			distance = "Kurang dari 1 km"
		}
	}
	travel := ""
	if m, err := strconv.Atoi(p("travel_minutes")); err == nil {
		travel = strconv.Itoa(m/60) + " jam " + strconv.Itoa(m%60) + " menit"
	}
	out = append(out,
		FPDSection{Title: "DATA PERIODIK", Rows: []FPDRow{
			{"1", "Tinggi Badan (cm)", p("height_cm")},
			{"2", "Berat Badan (kg)", p("weight_kg")},
			{"3", "Lingkar Kepala (cm)", p("head_cm")},
			{"4", "Jarak tempat tinggal ke sekolah", distance},
			{"5", "Sebutkan (dalam kilometer)", km},
			{"6", "Waktu tempuh ke sekolah", travel},
			{"7", "Jumlah saudara kandung", p("siblings")},
			{"8", "No. KKS", p("kks_number")},
		}},
		FPDSection{Title: "REGISTRASI PESERTA DIDIK", Rows: []FPDRow{
			{"1", "Kompetensi Keahlian (khusus SMK)", ""},
			{"2", "Jenis Pendaftaran", refOf("registration_type")},
			{"3", "NIS/Nomor Induk PD", in.Student.NIS},
			{"4", "Tanggal Masuk Sekolah", date("entry_date")},
			{"5", "Sekolah Asal", p("previous_school")},
			{"6", "Nomor Peserta UN SMP/MTs", p("exam_number")},
			{"7", "No. Seri Ijazah SMP/MTs", p("diploma_serial")},
			{"8", "No. SKHUN SMP/MTs", p("skhun_number")},
		}},
		FPDSection{Title: "PENDAFTARAN KELUAR (diisi jika peserta didik sudah keluar)", Rows: []FPDRow{
			{"1", "Keluar Karena", ""},
			{"2", "Tanggal Keluar", ""},
			{"3", "Alasan", ""},
		}},
	)
	return out
}

// WriteFPD menulis F-PD untuk satu atau beberapa siswa (satu siswa per
// halaman baru). Input kosong menghasilkan formulir kosong.
func WriteFPD(w io.Writer, schoolName string, inputs []Input, printedAt time.Time, compress bool) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(compress)
	pdf.SetTitle("Formulir Peserta Didik", true)
	pdf.SetCreator("AKRA", true)
	pdf.SetCreationDate(printedAt)
	pdf.SetModificationDate(printedAt)
	pdf.SetCatalogSort(true)
	pdf.SetMargins(14, 12, 14)
	pdf.SetAutoPageBreak(true, 14)
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	width := pageW - left - right
	const noW, labelW, lineH = 8.0, 62.0, 5.6

	for _, in := range inputs {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "B", 12)
		pdf.CellFormat(width-20, 6, tr("FORMULIR PESERTA DIDIK"), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "B", 11)
		pdf.CellFormat(20, 6, "F-PD", "1", 1, "C", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(width, 4.5, tr(schoolName), "", "L", false)
		pdf.CellFormat(width, 5, tr("Tanggal: "+clock.FormatDateID(printedAt)), "", 1, "L", false, 0, "")
		pdf.Ln(1)
		for _, sec := range FPD(in) {
			if pdf.GetY() > 265 {
				pdf.AddPage()
			}
			pdf.SetFont("Helvetica", "B", 9.5)
			pdf.SetFillColor(230, 236, 245)
			pdf.CellFormat(width, 6, tr(sec.Title), "", 1, "L", true, 0, "")
			for _, row := range sec.Rows {
				pdf.SetFont("Helvetica", "", 9)
				pdf.CellFormat(noW, lineH, row.No, "", 0, "R", false, 0, "")
				pdf.CellFormat(labelW, lineH, " "+tr(row.Label), "", 0, "L", false, 0, "")
				pdf.CellFormat(3, lineH, ":", "", 0, "L", false, 0, "")
				valueW := width - noW - labelW - 3
				if row.Value == "" {
					x, y := pdf.GetXY()
					pdf.SetDrawColor(160, 160, 160)
					pdf.Line(x, y+lineH-1, x+valueW, y+lineH-1)
					pdf.SetDrawColor(0, 0, 0)
					pdf.Ln(lineH)
					continue
				}
				pdf.SetFont("Helvetica", "B", 9)
				pdf.MultiCell(valueW, lineH, tr(row.Value), "", "L", false)
			}
			pdf.Ln(1)
		}
		// Pernyataan dan tanda tangan, seperti F-PD.
		if pdf.GetY() > 245 {
			pdf.AddPage()
		}
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "I", 8.5)
		pdf.MultiCell(width, 4.2, tr("Yang bertanda tangan Orang Tua/Wali dan Wali Kelas bertanggung jawab secara hukum terhadap kebenaran data yang tercantum."), "", "L", false)
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "", 9)
		colW := width / 2
		pdf.CellFormat(colW, 5, tr("..................., ....................."), "", 0, "C", false, 0, "")
		pdf.CellFormat(colW, 5, tr("..................., ....................."), "", 1, "C", false, 0, "")
		pdf.CellFormat(colW, 5, "Wali Kelas", "", 0, "C", false, 0, "")
		pdf.CellFormat(colW, 5, tr("Orang Tua/Wali Siswa"), "", 1, "C", false, 0, "")
		pdf.Ln(16)
		pdf.CellFormat(colW, 5, "(.............................................)", "", 0, "C", false, 0, "")
		pdf.CellFormat(colW, 5, "(.............................................)", "", 1, "C", false, 0, "")
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "I", 7.5)
		pdf.CellFormat(width, 4, tr("Dicetak dari AKRA, "+clock.FormatDateID(printedAt)+". Urutan dan kode mengikuti F-PD Dapodik "+LayoutVersion+"."), "", 1, "L", false, 0, "")
	}
	return pdf.Output(w)
}
