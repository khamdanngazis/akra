package students

import (
	"context"
	"errors"
	"testing"
)

// seedWilayah mengisi sebagian kecil data wilayah untuk uji.
func seedWilayah(t *testing.T, exec func(string) error) {
	t.Helper()
	if err := exec(`INSERT INTO wilayah (kode, nama, tingkat, induk, kode_pos) VALUES
		('32', 'Jawa Barat', 1, NULL, NULL), ('32.73', 'Kota Bandung', 2, '32', NULL),
		('32.73.04', 'Bojongloa Kaler', 3, '32.73', NULL), ('32.73.04.1001', 'Kopo', 4, '32.73.04', '40233')
		ON CONFLICT (kode) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
}

func TestProfileWilayah(t *testing.T) {
	p := newProfileFixture(t)
	ctx := context.Background()
	seedWilayah(t, func(sql string) error { _, err := p.tx.Exec(ctx, sql); return err })

	// Kode desa: nama desa/kecamatan dari data wilayah, kode pos terisi.
	saved, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"wilayah_kode": "32.73.04.1001", "village": "salah ketik", "district": ""})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Get("village") != "Kopo" || saved.Get("district") != "Bojongloa Kaler" || saved.Get("postal_code") != "40233" || saved.Get("wilayah_kode") != "32.73.04.1001" {
		t.Errorf("saved village=%q district=%q pos=%q kode=%q", saved.Get("village"), saved.Get("district"), saved.Get("postal_code"), saved.Get("wilayah_kode"))
	}
	var ve *ValidationError
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"wilayah_kode": "32.73.04.9999"}); !errors.As(err, &ve) || ve.Fields["wilayah_kode"] == "" {
		t.Errorf("unknown village code: %v", err)
	}
	if _, err := p.svc.Save(ctx, p.actorA, p.budi, map[string]string{"wilayah_kode": "32.73"}); !errors.As(err, &ve) || ve.Fields["wilayah_kode"] == "" {
		t.Errorf("district-level code: %v", err)
	}
	// Isian manual tanpa kode tetap bisa.
	manual, err := p.svc.Save(ctx, p.actorA, p.ani, map[string]string{"village": "Desa Pemekaran", "district": "Bojongloa Kaler"})
	if err != nil || manual.Get("village") != "Desa Pemekaran" || manual.Get("wilayah_kode") != "" {
		t.Errorf("manual = %q %q, %v", manual.Get("village"), manual.Get("wilayah_kode"), err)
	}

	// Data lama tanpa kode: disarankan bila nama cocok unik.
	v := map[string]string{"village": "Kopo", "district": "Kec. Bojongloa Kaler"}
	if err := SuggestWilayah(ctx, p.tx, v); err != nil || v[WilayahSuggestKey] != "32.73.04.1001" {
		t.Errorf("suggest = %q, %v", v[WilayahSuggestKey], err)
	}
	fv := wilayahFieldView(v, nil)
	if fv.Input != "wilayah" || fv.Sub["suggest"] != "32.73.04.1001" || fv.Hint == "" {
		t.Errorf("field view = %+v", fv)
	}
	// Desa, kecamatan, dan kode pos tidak tampil sebagai isian terpisah.
	for _, f := range ProfileFieldViews(SectionAddress, map[string]string{}, nil) {
		if f.Key == "village" || f.Key == "district" || f.Key == "postal_code" {
			t.Errorf("separate %s field still rendered", f.Key)
		}
	}
}
