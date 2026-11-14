package dapodik

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/pii"
	"akra/internal/students"
)

func TestFPDNumbering(t *testing.T) {
	secs := FPD(Input{})
	// Data pribadi sampai kontak bernomor 1..50 tanpa celah.
	n := 0
	for _, s := range secs[:5] {
		for _, r := range s.Rows {
			n++
			if r.No != strconv.Itoa(n) {
				t.Fatalf("%s: row %q numbered %s, want %d", s.Title, r.Label, r.No, n)
			}
		}
	}
	if n != 50 {
		t.Errorf("personal..contact rows = %d, want 50", n)
	}
	for _, s := range secs {
		for _, r := range s.Rows {
			if r.Value != "" {
				t.Errorf("blank form has value %q in %s", r.Value, r.Label)
			}
		}
	}
}

func TestFPDValues(t *testing.T) {
	birth := time.Date(2019, 5, 17, 0, 0, 0, 0, time.UTC)
	in := Input{
		Student: students.Student{FullName: "Budi Santoso", NIS: "2027001", Gender: "L", BirthDate: &birth},
		Profile: students.ProfileForTest(map[string]string{"nik": "3201010101010001", "religion": "01", "citizenship": "WNI",
			"special_needs": "01", "has_kip": "false", "distance_km": "2.5", "travel_minutes": "45", "registration_type": "01", "entry_date": "2027-07-12"}),
		Parents: map[string]Parent{"ibu": {Name: "Sari", Details: students.GuardianDetails{NIK: pii.New("3201010101800002"), Occupation: "07"}}},
	}
	rows := map[string]string{}
	for _, s := range FPD(in) {
		for _, r := range s.Rows {
			rows[s.Title+"/"+r.Label] = r.Value
		}
	}
	want := map[string]string{
		"DATA PRIBADI/Nama Lengkap":                      "Budi Santoso",
		"DATA PRIBADI/Agama & Kepercayaan":               "01) Islam",
		"DATA PRIBADI/Kewarganegaraan":                   "Indonesia (WNI)",
		"DATA PRIBADI/Berkebutuhan Khusus":               "01) Tidak",
		"DATA PRIBADI/Apakah punya KIP":                  "Tidak",
		"DATA IBU KANDUNG/Nama":                          "Sari",
		"DATA IBU KANDUNG/Pekerjaan":                     "07) Pedagang Kecil",
		"DATA PERIODIK/Jarak tempat tinggal ke sekolah":  "Lebih dari 1 km",
		"DATA PERIODIK/Waktu tempuh ke sekolah":          "0 jam 45 menit",
		"REGISTRASI PESERTA DIDIK/Jenis Pendaftaran":     "01) Siswa Baru",
		"REGISTRASI PESERTA DIDIK/Tanggal Masuk Sekolah": "12 Juli 2027",
		"REGISTRASI PESERTA DIDIK/NIS/Nomor Induk PD":    "2027001",
	}
	for k, w := range want {
		if rows[k] != w {
			t.Errorf("%s = %q, want %q", k, rows[k], w)
		}
	}

	var buf bytes.Buffer
	long := in
	long.Student.FullName = strings.Repeat("Nama Sangat Panjang ", 8)
	if err := WriteFPD(&buf, "SD Uji", []Input{in, long, {}}, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	pdf := buf.Bytes()
	for _, s := range []string{"FORMULIR PESERTA DIDIK", "Budi Santoso", "3201010101010001", `01\) Islam`, "Wali Kelas"} {
		if !bytes.Contains(pdf, []byte(s)) {
			t.Errorf("PDF missing %q", s)
		}
	}
	if pages := bytes.Count(pdf, []byte("/Type /Page\n")); pages < 3 {
		t.Errorf("pages = %d, want at least one per student", pages)
	}
}

func TestFPDHandlers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := students.NewProfileService(e.tx, e.keys).Save(ctx, e.actor, e.budi, map[string]string{"nik": "3201010101010001"}); err != nil {
		t.Fatal(err)
	}
	// Kelas berisi 40 siswa: PDF dibuat tanpa macet.
	for i := 0; i < 38; i++ {
		var sid string
		if err := e.tx.QueryRow(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`, e.schoolA, fmt.Sprintf("x%03d", i), fmt.Sprintf("Siswa %02d", i)).Scan(&sid); err != nil {
			t.Fatal(err)
		}
		if _, err := e.tx.Exec(ctx, `INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date)
			SELECT $1, $2, $3, academic_year_id, current_date - 1 FROM classes WHERE id = $3`, e.schoolA, sid, e.classA); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	for _, p := range []string{"/dapodik/siswa/" + e.budi + "/fpd.pdf", "/dapodik/kelas/" + e.classA + "/fpd.pdf", "/dapodik/fpd-kosong.pdf"} {
		rec := e.do(http.MethodGet, p, "kepala_sekolah", nil)
		if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("FPD downloads took %v", d)
	}
	if rec := e.do(http.MethodGet, "/dapodik/siswa/"+e.otherB+"/fpd.pdf", "admin_tu", nil); rec.Code != http.StatusNotFound {
		t.Errorf("other school fpd: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/dapodik/fpd-kosong.pdf", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: %d", rec.Code)
	}
}
