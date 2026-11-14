package dapodik

import (
	"fmt"
	"io"
	"time"

	"github.com/xuri/excelize/v2"

	"akra/internal/platform/clock"
	"akra/internal/students"
)

// pdColumns adalah header ekspor "Daftar Peserta Didik" Dapodik 2027
// (template_excel/template-daftar-pd.xlsx): kolom A sampai BN. Grup berisi
// sub-header di baris 6.
var pdColumns = []struct {
	Header string
	Group  []string // sub-header; kosong = header digabung baris 5–6
	Width  float64
}{
	{"No", nil, 6}, {"Nama", nil, 32.5}, {"NIPD", nil, 17}, {"JK", nil, 5}, {"NISN", nil, 11}, {"Tempat Lahir", nil, 17},
	{"Tanggal Lahir", nil, 14}, {"NIK", nil, 19}, {"Agama", nil, 12}, {"Alamat", nil, 30}, {"RT", nil, 5}, {"RW", nil, 5},
	{"Dusun", nil, 15}, {"Kelurahan", nil, 18}, {"Kecamatan", nil, 18}, {"Kode Pos", nil, 9}, {"Jenis Tinggal", nil, 18},
	{"Alat Transportasi", nil, 18}, {"Telepon", nil, 14}, {"HP", nil, 15}, {"E-Mail", nil, 22}, {"SKHUN", nil, 14},
	{"Penerima KPS", nil, 10}, {"No. KPS", nil, 12},
	{"Data Ayah", []string{"Nama", "Tahun Lahir", "Jenjang Pendidikan", "Pekerjaan", "Penghasilan", "NIK"}, 16},
	{"Data Ibu", []string{"Nama", "Tahun Lahir", "Jenjang Pendidikan", "Pekerjaan", "Penghasilan", "NIK"}, 16},
	{"Data Wali", []string{"Nama", "Tahun Lahir", "Jenjang Pendidikan", "Pekerjaan", "Penghasilan", "NIK"}, 16},
	{"Rombel Saat Ini", nil, 14}, {"No Peserta Ujian Nasional", nil, 22}, {"No Seri Ijazah", nil, 18}, {"Penerima KIP", nil, 10},
	{"Nomor KIP", nil, 14}, {"Nama di KIP", nil, 20}, {"Nomor KKS", nil, 14}, {"No Registrasi Akta Lahir", nil, 20},
	{"Bank", nil, 12}, {"Nomor Rekening Bank", nil, 18}, {"Rekening Atas Nama", nil, 20}, {"Layak PIP (usulan dari sekolah)", nil, 14},
	{"Alasan Layak PIP", nil, 22}, {"Kebutuhan Khusus", nil, 16}, {"Sekolah Asal", nil, 22}, {"Anak ke-berapa", nil, 9},
	{"Lintang", nil, 11}, {"Bujur", nil, 11}, {"No KK", nil, 19}, {"Berat Badan", nil, 8}, {"Tinggi Badan", nil, 8},
	{"Lingkar Kepala", nil, 8}, {"Jml. Saudara\nKandung", nil, 9}, {"Jarak Rumah\nke Sekolah (KM)", nil, 11},
}

// PDHeaders mengembalikan header ekspor dalam satu baris datar: kolom
// bergrup menjadi "Grup / Sub", mis. "Data Ibu / Nama" (dipakai impor T101).
func PDHeaders() []string {
	var out []string
	for _, c := range pdColumns {
		if len(c.Group) == 0 {
			out = append(out, c.Header)
			continue
		}
		for _, g := range c.Group {
			out = append(out, c.Header+" / "+g)
		}
	}
	return out
}

// ExportRow adalah satu siswa untuk ekspor, dengan nama rombel aktifnya.
type ExportRow struct {
	Input
	ClassName string
}

func pdRow(n int, r ExportRow) []string {
	v := Values(r.Input)
	p := func(k string) string { return r.Profile.Secret(k).Reveal() }
	birth := ""
	if r.Student.BirthDate != nil {
		birth = r.Student.BirthDate.Format("2006-01-02")
	}
	parent := func(role string) []string {
		return []string{v[role+".full_name"], v[role+".birth_year"], v[role+".education"], v[role+".occupation"], v[role+".income"], v[role+".nik"]}
	}
	row := []string{
		fmt.Sprint(n), r.Student.FullName, r.Student.NIS, r.Student.Gender, p("nisn"), p("birth_place"), birth, p("nik"),
		v["religion"], p("street"), p("rt"), p("rw"), p("hamlet"), p("village"), p("district"), p("postal_code"),
		v["residence_type"], v["transport_mode"], p("home_phone"), p("mobile_phone"), p("email"), p("skhun_number"), "", "",
	}
	row = append(row, parent(students.RelationFather)...)
	row = append(row, parent(students.RelationMother)...)
	row = append(row, parent(students.RelationGuardian)...)
	row = append(row,
		r.ClassName, p("exam_number"), p("diploma_serial"), v["has_kip"], p("kip_number"), p("kip_name"), p("kks_number"),
		p("birth_cert_number"), "", "", "", v["pip_eligible"], v["pip_reason"], v["special_needs"], p("previous_school"),
		p("child_order"), p("latitude"), p("longitude"), p("kk_number"), p("weight_kg"), p("height_cm"), p("head_cm"),
		p("siblings"), p("distance_km"),
	)
	return row
}

// WritePDExport menulis Excel dengan kolom yang sama persis dengan ekspor
// "Daftar Peserta Didik" Dapodik: judul di baris 1–4, header di baris 5–6
// (kolom bergrup memakai sub-header), data mulai baris 7. Semua nilai
// disimpan sebagai teks agar nol di depan NIK/NISN tidak hilang.
func WritePDExport(w io.Writer, schoolName, downloader string, rows []ExportRow, printedAt time.Time) error {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Sheet1"
	set := func(cell, value string) error { return f.SetCellStr(sheet, cell, value) }
	if err := set("A1", "Daftar Peserta Didik"); err != nil {
		return err
	}
	_ = set("A2", schoolName)
	_ = set("A4", "Tanggal Unduh: "+clock.FormatDateID(printedAt))
	_ = set("C4", "Pengunduh: "+downloader)

	header, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{{Type: "left", Style: 1}, {Type: "right", Style: 1}, {Type: "top", Style: 1}, {Type: "bottom", Style: 1}}})
	if err != nil {
		return err
	}
	col := 1
	for _, c := range pdColumns {
		start, _ := excelize.CoordinatesToCellName(col, 5)
		_ = set(start, c.Header)
		if len(c.Group) == 0 {
			end, _ := excelize.CoordinatesToCellName(col, 6)
			if err := f.MergeCell(sheet, start, end); err != nil {
				return err
			}
			name, _ := excelize.ColumnNumberToName(col)
			_ = f.SetColWidth(sheet, name, name, c.Width)
			col++
			continue
		}
		for i, g := range c.Group {
			cell, _ := excelize.CoordinatesToCellName(col+i, 6)
			_ = set(cell, g)
			name, _ := excelize.ColumnNumberToName(col + i)
			_ = f.SetColWidth(sheet, name, name, c.Width)
		}
		end, _ := excelize.CoordinatesToCellName(col+len(c.Group)-1, 5)
		if err := f.MergeCell(sheet, start, end); err != nil {
			return err
		}
		col += len(c.Group)
	}
	last, _ := excelize.CoordinatesToCellName(col-1, 6)
	if err := f.SetCellStyle(sheet, "A5", last, header); err != nil {
		return err
	}
	for i, r := range rows {
		for j, v := range pdRow(i+1, r) {
			cell, _ := excelize.CoordinatesToCellName(j+1, 7+i)
			if err := set(cell, v); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}
