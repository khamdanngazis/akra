package ref

import (
	"errors"
	"regexp"
	"testing"
)

// TestListsMatchFPD membandingkan isi daftar dengan F-PD 2027. Jika Dapodik
// mengubah referensi, uji ini harus diubah bersamaan dengan daftarnya.
func TestListsMatchFPD(t *testing.T) {
	want := map[string][]string{
		"agama":               {"01 Islam", "02 Kristen/ Protestan", "03 Katholik", "04 Hindu", "05 Budha", "06 Khonghucu", "07 Kepercayaan Kepada Tuhan YME"},
		"kewarganegaraan":     {"WNI Indonesia (WNI)", "WNA Asing (WNA)"},
		"tempat_tinggal":      {"01 Bersama orang tua", "02 Wali", "03 Kos", "04 Asrama", "05 Panti Asuhan"},
		"pendidikan":          {"01 Tidak sekolah", "02 Putus SD", "03 SD Sederajat", "04 SMP Sederajat", "05 SMA Sederajat", "06 D1", "07 D2", "08 D3", "09 D4/S1", "10 S2", "11 S3"},
		"penghasilan":         {"01 < Rp. 500.000", "02 Rp. 500.000-Rp.999.999", "03 Rp. 1.000.000-Rp.1.999.999", "04 Rp.2.000.000-Rp.4.999.999", "05 Rp.5.000.000-Rp.20.000.000", "06 > Rp.20.000.000", "07 Tidak Berpenghasilan"},
		"jenis_pendaftaran":   {"01 Siswa Baru", "02 Pindahan", "03 Kembali Bersekolah"},
		"alasan_keluar":       {"01 Mutasi", "02 Dikeluarkan", "03 Mengundurkan Diri", "04 Putus Sekolah", "05 Wafat", "06 Hilang"},
		"alasan_pip":          {"01 Dilarang pemda karena menerima bantuan serupa", "02 Menolak", "03 Sudah mampu"},
		"jenis_kesejahteraan": {"01 PKH", "02 PIP", "03 Kartu Perlindungan Sosial", "04 Kartu Keluarga Sejahtera", "05 Kartu Kesehatan"},
		"jenis_prestasi":      {"01 Sains", "02 Seni", "03 Olahraga", "04 Lain-lain"},
		"tingkat_prestasi":    {"01 Sekolah", "02 Kecamatan", "03 Kabupaten", "04 Provinsi", "05 Nasional", "06 Internasional"},
		"jenis_beasiswa":      {"01 Anak berprestasi", "02 Anak Miskin", "03 Pendidikan", "04 Unggulan"},
	}
	got := map[string]List{}
	for _, l := range All() {
		got[l.Key] = l
	}
	for key, items := range want {
		l, ok := got[key]
		if !ok {
			t.Errorf("list %q missing from All()", key)
			continue
		}
		if len(l.Options) != len(items) {
			t.Errorf("%s: %d options, want %d", key, len(l.Options), len(items))
			continue
		}
		for i, o := range l.Options {
			if s := o.Code + " " + o.Label; s != items[i] {
				t.Errorf("%s[%d] = %q, want %q", key, i, s, items[i])
			}
		}
	}

	// Daftar panjang: cek jumlah, ujung, dan beberapa entri yang mudah salah.
	spot := []struct {
		l     List
		n     int
		code  string
		label string
	}{
		{KebutuhanKhusus, 18, "01", "Tidak"},
		{KebutuhanKhusus, 18, "08", "Laras (E)"},
		{KebutuhanKhusus, 18, "18", "Autis (Q)"},
		{ModaTransportasi, 9, "06", "Ojek"},
		{ModaTransportasi, 9, "99", "Lainnya"},
		{Pekerjaan, 13, "05", "PNS/TNI/POLRI"},
		{Pekerjaan, 13, "12", "Pensiunan"},
		{Pekerjaan, 13, KodeMeninggal, "Meninggal Dunia"},
	}
	for _, s := range spot {
		if len(s.l.Options) != s.n {
			t.Errorf("%s: %d options, want %d", s.l.Key, len(s.l.Options), s.n)
		}
		if got, err := s.l.Label(s.code); err != nil || got != s.label {
			t.Errorf("%s.Label(%q) = %q, %v; want %q", s.l.Key, s.code, got, err, s.label)
		}
	}
	if !KebutuhanKhusus.Multi {
		t.Error("kebutuhan_khusus must allow multiple choices")
	}
}

func TestListsWellFormed(t *testing.T) {
	twoDigits := regexp.MustCompile(`^[0-9]{2}$`)
	keys := map[string]bool{}
	for _, l := range All() {
		if l.Key == "" || keys[l.Key] {
			t.Errorf("empty or duplicate list key %q", l.Key)
		}
		keys[l.Key] = true
		if len(l.Options) == 0 {
			t.Errorf("%s: no options", l.Key)
		}
		codes, labels := map[string]bool{}, map[string]bool{}
		for _, o := range l.Options {
			if l.Key != "kewarganegaraan" && !twoDigits.MatchString(o.Code) {
				t.Errorf("%s: code %q is not two digits", l.Key, o.Code)
			}
			if o.Label == "" {
				t.Errorf("%s: code %q has empty label", l.Key, o.Code)
			}
			if codes[o.Code] || labels[o.Label] {
				t.Errorf("%s: duplicate code or label %q %q", l.Key, o.Code, o.Label)
			}
			codes[o.Code], labels[o.Label] = true, true
		}
	}
}

func TestValidLabelCode(t *testing.T) {
	if !Agama.Valid("01") {
		t.Error(`Agama.Valid("01") = false`)
	}
	for _, bad := range []string{"", "1", "00", "08", " 01", "Islam"} {
		if Agama.Valid(bad) {
			t.Errorf("Agama.Valid(%q) = true", bad)
		}
		if _, err := Agama.Label(bad); !errors.Is(err, ErrUnknownCode) {
			t.Errorf("Agama.Label(%q) err = %v, want ErrUnknownCode", bad, err)
		}
	}
	if c, err := Penghasilan.Code("Tidak Berpenghasilan"); err != nil || c != "07" {
		t.Errorf(`Penghasilan.Code = %q, %v; want "07"`, c, err)
	}
	if _, err := Penghasilan.Code("tidak berpenghasilan"); !errors.Is(err, ErrUnknownCode) {
		t.Errorf("Code must match label exactly, err = %v", err)
	}
}
