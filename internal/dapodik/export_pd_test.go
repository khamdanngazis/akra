package dapodik

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"akra/internal/platform/pii"
	"akra/internal/students"
)

// Header baris 5 dan 6 template-daftar-pd.xlsx Dapodik 2027.
var dapodikPDRow5 = []string{"No", "Nama", "NIPD", "JK", "NISN", "Tempat Lahir", "Tanggal Lahir", "NIK", "Agama", "Alamat", "RT", "RW", "Dusun",
	"Kelurahan", "Kecamatan", "Kode Pos", "Jenis Tinggal", "Alat Transportasi", "Telepon", "HP", "E-Mail", "SKHUN", "Penerima KPS", "No. KPS",
	"Data Ayah", "", "", "", "", "", "Data Ibu", "", "", "", "", "", "Data Wali", "", "", "", "", "", "Rombel Saat Ini", "No Peserta Ujian Nasional",
	"No Seri Ijazah", "Penerima KIP", "Nomor KIP", "Nama di KIP", "Nomor KKS", "No Registrasi Akta Lahir", "Bank", "Nomor Rekening Bank",
	"Rekening Atas Nama", "Layak PIP (usulan dari sekolah)", "Alasan Layak PIP", "Kebutuhan Khusus", "Sekolah Asal", "Anak ke-berapa", "Lintang",
	"Bujur", "No KK", "Berat Badan", "Tinggi Badan", "Lingkar Kepala", "Jml. Saudara\nKandung", "Jarak Rumah\nke Sekolah (KM)"}

func TestPDExportHeaders(t *testing.T) {
	birth := time.Date(2019, 5, 17, 0, 0, 0, 0, time.UTC)
	rows := []ExportRow{{ClassName: "1A", Input: Input{
		Student: students.Student{FullName: "Budi", NIS: "001", Gender: "L", BirthDate: &birth},
		Profile: students.ProfileForTest(map[string]string{"nik": "0012345678901234", "nisn": "0091234567", "religion": "01"}),
		Parents: map[string]Parent{"ibu": {Name: "Sari", Details: students.GuardianDetails{NIK: pii.New("3201010101800002"), Education: "09"}}},
	}}}
	var buf bytes.Buffer
	if err := WritePDExport(&buf, "SD Uji", "TU", rows, time.Now()); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := f.GetRows("Sheet1")
	if len(all) < 7 || all[0][0] != "Daftar Peserta Didik" {
		t.Fatalf("rows = %d", len(all))
	}
	r5 := append(all[4], make([]string, 66)...)[:66]
	for i, want := range dapodikPDRow5 {
		if r5[i] != want {
			t.Errorf("row 5 col %d = %q, want %q", i+1, r5[i], want)
		}
	}
	if all[5][24] != "Nama" || all[5][29] != "NIK" || all[5][41] != "NIK" {
		t.Errorf("row 6 sub-headers = %v", all[5][24:42])
	}
	merges, _ := f.GetMergeCells("Sheet1")
	if len(merges) != 51 {
		t.Errorf("merges = %d, want 51 as in template", len(merges))
	}
	data := all[6]
	if data[7] != "0012345678901234" || data[4] != "0091234567" || data[8] != "Islam" || data[30] != "Sari" || data[32] != "D4/S1" || data[42] != "1A" {
		t.Errorf("data row = %v", data)
	}
	if typ, _ := f.GetCellType("Sheet1", "H7"); typ != excelize.CellTypeInlineString && typ != excelize.CellTypeSharedString {
		t.Errorf("NIK cell type = %v, want text", typ)
	}
	if len(PDHeaders()) != 66 || PDHeaders()[30] != "Data Ibu / Nama" {
		t.Errorf("PDHeaders = %v", PDHeaders()[28:32])
	}
}

func TestExportPDHandler(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rec := e.do(http.MethodGet, "/dapodik/ekspor-pd.xlsx?kelas="+e.classA, "kepala_sekolah", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d", rec.Code)
	}
	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	all, _ := f.GetRows("Sheet1")
	if len(all) != 8 { // 6 baris judul/header + 2 siswa kelas 1A
		t.Errorf("rows = %d", len(all))
	}
	var n int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, e.schoolA, ActionExportPD).Scan(&n)
	if n != 1 {
		t.Errorf("export audits = %d", n)
	}
	if rec := e.do(http.MethodGet, "/dapodik/ekspor-pd.xlsx", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: %d", rec.Code)
	}
}
