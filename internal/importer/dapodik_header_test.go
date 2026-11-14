package importer_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"akra/internal/dapodik"
	"akra/internal/importer"
	"akra/internal/platform/pii"
	"akra/internal/students"
)

// dapodikExport membuat file berformat ekspor "Daftar Peserta Didik"
// Dapodik: judul di baris 1–4, header dua baris dengan sel gabungan.
func dapodikExport(t *testing.T) []byte {
	t.Helper()
	birth := time.Date(2019, 5, 17, 0, 0, 0, 0, time.UTC)
	rows := []dapodik.ExportRow{
		{ClassName: "1A", Input: dapodik.Input{
			Student: students.Student{FullName: "Budi Santoso", NIS: "007", Gender: "L", BirthDate: &birth},
			Profile: students.ProfileForTest(map[string]string{"mobile_phone": "081234567890"}),
			Parents: map[string]dapodik.Parent{"ibu": {Name: "Sari", Details: students.GuardianDetails{NIK: pii.New("3201010101800002")}}},
		}},
		{ClassName: "1B", Input: dapodik.Input{Student: students.Student{FullName: "Ani", NIS: "008", Gender: "P"}}},
	}
	var buf bytes.Buffer
	if err := dapodik.WritePDExport(&buf, "SD Uji", "TU", rows, time.Now()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadHeaderDapodikExport(t *testing.T) {
	header, err := importer.ReadHeader(bytes.NewReader(dapodikExport(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(header) != 66 || header[1] != "Nama" || header[2] != "NIPD" || header[30] != "Data Ibu / Nama" || header[24] != "Data Ayah / Nama" || header[42] != "Rombel Saat Ini" {
		t.Errorf("header = %d cols: %v", len(header), header[:6])
	}
	if strings.Join(header, "|") != strings.Join(dapodik.PDHeaders(), "|") {
		t.Error("header differs from Dapodik export columns")
	}
}

func TestParseMappedDapodikExport(t *testing.T) {
	mp := importer.Mapping{Name: "Ekspor Dapodik", Target: importer.TargetStudents, Columns: importer.ColumnMap{
		"NIS": "NIPD", "Nama lengkap": "Nama", "Kelas": "Rombel Saat Ini", "Jenis kelamin": "JK", "Nama wali": "Data Ibu / Nama", "Telepon wali": "HP",
	}}
	rows, err := importer.ParseMapped(bytes.NewReader(dapodikExport(t)), mp)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Data) != 2 {
		t.Fatalf("data rows = %d (sub-header must not be data)", len(rows.Data))
	}
	st, err := rows.StudentRows()
	if err != nil {
		t.Fatal(err)
	}
	r := st.Data[0].Values
	if r[importer.ColNIS] != "007" || r[importer.ColFullName] != "Budi Santoso" || r[importer.ColClass] != "1A" || r[importer.ColGuardianName] != "Sari" || st.Data[0].Line != 7 {
		t.Errorf("first row = %+v line %d", r, st.Data[0].Line)
	}
}
