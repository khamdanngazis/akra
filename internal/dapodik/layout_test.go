package dapodik

import (
	"strings"
	"testing"
	"time"

	"akra/internal/platform/pii"
	"akra/internal/students"
)

// dapodik2027Labels adalah urutan label form Peserta Didik di classic/app.js
// Dapodik 2027 (Data Pribadi sampai Data Periodik), tanpa field sistem (ID,
// Status data, Peserta didik, Sekolah, Jurusan sp) dan bagian pendaftaran
// keluar. "Waktu tempuh ke sekolah (jam / menit)" di Dapodik adalah dua kotak;
// di sini dipecah menjadi dua baris.
var dapodik2027Labels = []string{
	"Nama", "Jenis kelamin", "NISN", "Kewarganegaraan", "NIK", "No KK", "Tempat lahir", "Tanggal lahir", "No Registrasi Akta Lahir",
	"Agama & Kepercayaan", "Kebutuhan khusus", "Alamat jalan", "RT", "RW", "Nama dusun", "Desa/Kelurahan", "Kecamatan", "Kode pos",
	"Lintang", "Bujur", "Tempat tinggal", "Moda transportasi", "No KKS (Kartu Keluarga Sejahtera)", "Anak ke-berapa (berdasarkan KK)",
	"Pekerjaan (diperuntukan warga belajar)", "Penerima KPS/PKH", "No KPS/PKH", "Penerima KIP (Kartu Indonesia Pintar)", "No KIP",
	"Usulan dari sekolah (Layak PIP)", "Alasan layak PIP",
	"Nama Bank", "Nomor Rekening", "Kantor Cabang Pembantu (KCP)", "Rekening Atas Nama",
	"Nama ayah", "NIK Ayah", "Tahun lahir ayah", "Pendidikan ayah", "Pekerjaan ayah", "Penghasilan ayah", "Berkebutuhan khusus ayah",
	"Nama ibu", "NIK Ibu", "Tahun lahir ibu", "Pendidikan", "Pekerjaan ibu", "Penghasilan ibu", "Berkebutuhan khusus ibu",
	"Mempunyai wali", "Nama wali", "NIK Wali", "Tahun lahir wali", "Pendidikan wali", "Pekerjaan wali", "Penghasilan wali",
	"Nomor telepon Rumah", "Nomor HP", "Email",
	"Jenis pendaftaran", "Nomor Induk PD / NIS", "Tanggal masuk sekolah", "Sekolah asal", "Apakah pernah PAUD Formal (TK)",
	"Apakah pernah PAUD Non Formal (KB/TPA/SPS)", "Hobby", "Cita-cita",
	"No Peserta UN SMP/MTs", "No Seri Ijazah SMP/MTs", "No Seri SKHUN",
	"Tinggi badan (cm)", "Berat badan (kg)", "Lingkar kepala", "Jarak rumah ke sekolah", "Sebutkan (dalam kilometer)",
	"Waktu tempuh ke sekolah (jam)", "Waktu tempuh ke sekolah (menit)", "Jumlah saudara kandung",
}

func TestLayoutMatchesDapodik(t *testing.T) {
	var got []string
	for _, s := range Layout {
		for _, f := range s.Fields {
			got = append(got, f.Label)
		}
	}
	if strings.Join(got, "|") != strings.Join(dapodik2027Labels, "|") {
		for i := range got {
			if i >= len(dapodik2027Labels) || got[i] != dapodik2027Labels[i] {
				t.Fatalf("label %d = %q, want %q", i, got[i], dapodik2027Labels[min(i, len(dapodik2027Labels)-1)])
			}
		}
		t.Fatalf("got %d labels, want %d", len(got), len(dapodik2027Labels))
	}
	// Setiap key dikenal oleh Values.
	known := map[string]bool{"full_name": true, "gender": true, "nis": true, "birth_date": true, "has_wali": true,
		"distance_band": true, "travel_hours": true, "travel_rest_minutes": true}
	for _, f := range students.ProfileFields {
		known[f.Key] = true
	}
	for _, s := range Layout {
		for _, f := range s.Fields {
			if f.Key == "" {
				if f.Note == "" {
					t.Errorf("%q: unsupported field without note", f.Label)
				}
				continue
			}
			if _, role, ok := strings.Cut(f.Key, "."); ok {
				_ = role
				continue
			}
			if !known[f.Key] {
				t.Errorf("%q: unknown key %q", f.Label, f.Key)
			}
		}
	}
}

func TestValues(t *testing.T) {
	birth := time.Date(2019, 5, 17, 0, 0, 0, 0, time.UTC)
	prof := students.ProfileForTest(map[string]string{
		"nik": "3201010101010001", "religion": "01", "citizenship": "WNI", "special_needs": "02,03", "has_kip": "true",
		"pip_eligible": "false", "distance_km": "0.5", "travel_minutes": "75", "entry_date": "2027-07-12", "registration_type": "01",
	})
	in := Input{
		Student: students.Student{FullName: "Budi", NIS: "2027001", Gender: "L", BirthDate: &birth},
		Profile: prof,
		Parents: map[string]Parent{
			"ibu": {Name: "Sari", Details: students.GuardianDetails{NIK: pii.New("3201010101800002"), BirthYear: 1985, Education: "09", Occupation: "98", Income: "07", SpecialNeeds: []string{"01"}}},
		},
	}
	v := Values(in)
	want := map[string]string{
		"gender": "Laki-laki", "birth_date": "17/05/2019", "religion": "Islam", "citizenship": "Indonesia (WNI)",
		"special_needs": "Netra (A), Rungu (B)", "has_kip": "Ya", "pip_eligible": "Tidak", "distance_band": "Kurang dari 1 km",
		"travel_hours": "1", "travel_rest_minutes": "15", "entry_date": "12/07/2027", "registration_type": "Siswa Baru",
		"nik": "3201010101010001", "nis": "2027001", "has_wali": "Tidak",
		"ibu.full_name": "Sari", "ibu.nik": "3201010101800002", "ibu.birth_year": "1985", "ibu.education": "D4/S1",
		"ibu.occupation": "Meninggal Dunia", "ibu.income": "Tidak Berpenghasilan", "ibu.special_needs": "Tidak",
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
	sheet := Sheet(in)
	if sheet[0].Title != "Data Pribadi" || sheet[0].Fields[0].Value != "Budi" || !sheet[0].Fields[24].Unsupported {
		t.Errorf("sheet = %+v", sheet[0].Fields[:3])
	}
}
