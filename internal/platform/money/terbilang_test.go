package money

import "testing"

func TestTerbilang(t *testing.T) {
	for n, want := range map[int64]string{
		0:                 "nol",
		1:                 "satu",
		10:                "sepuluh",
		11:                "sebelas",
		12:                "dua belas",
		19:                "sembilan belas",
		20:                "dua puluh",
		21:                "dua puluh satu",
		100:               "seratus",
		101:               "seratus satu",
		111:               "seratus sebelas",
		250:               "dua ratus lima puluh",
		1000:              "seribu",
		1001:              "seribu satu",
		1500:              "seribu lima ratus",
		2000:              "dua ribu",
		11000:             "sebelas ribu",
		100000:            "seratus ribu",
		150000:            "seratus lima puluh ribu",
		1_000_000:         "satu juta",
		1_250_500:         "satu juta dua ratus lima puluh ribu lima ratus",
		2_000_001:         "dua juta satu",
		1_000_000_000:     "satu miliar",
		1_000_000_000_000: "satu triliun",
		-5000:             "minus lima ribu",
	} {
		if got := Terbilang(n); got != want {
			t.Errorf("Terbilang(%d) = %q, want %q", n, got, want)
		}
	}
	if got := TerbilangRupiah(150000); got != "Seratus lima puluh ribu rupiah" {
		t.Errorf("TerbilangRupiah = %q", got)
	}
}
