package money

import "strings"

var satuan = [...]string{"", "satu", "dua", "tiga", "empat", "lima", "enam", "tujuh", "delapan", "sembilan", "sepuluh", "sebelas"}

// Terbilang menuliskan n dalam kata bahasa Indonesia, mis. 150000 menjadi
// "seratus lima puluh ribu". 0 menjadi "nol". Negatif diawali "minus".
func Terbilang(n int64) string {
	switch {
	case n == 0:
		return "nol"
	case n < 0:
		if n == -n { // nilai minimum int64 tidak bisa dinegasikan
			return ""
		}
		return "minus " + Terbilang(-n)
	}
	return strings.TrimSpace(terbilang(n))
}

// TerbilangRupiah seperti Terbilang dengan akhiran "rupiah" dan huruf awal
// kapital, untuk kuitansi: "Seratus lima puluh ribu rupiah".
func TerbilangRupiah(n int64) string {
	s := Terbilang(n) + " rupiah"
	return strings.ToUpper(s[:1]) + s[1:]
}

func terbilang(n int64) string {
	switch {
	case n < 12:
		return satuan[n]
	case n < 20:
		return terbilang(n-10) + " belas"
	case n < 100:
		return join(terbilang(n/10)+" puluh", terbilang(n%10))
	case n < 200:
		return join("seratus", terbilang(n-100))
	case n < 1000:
		return join(terbilang(n/100)+" ratus", terbilang(n%100))
	case n < 2000:
		return join("seribu", terbilang(n-1000))
	case n < 1_000_000:
		return join(terbilang(n/1000)+" ribu", terbilang(n%1000))
	case n < 1_000_000_000:
		return join(terbilang(n/1_000_000)+" juta", terbilang(n%1_000_000))
	case n < 1_000_000_000_000:
		return join(terbilang(n/1_000_000_000)+" miliar", terbilang(n%1_000_000_000))
	case n < 1_000_000_000_000_000:
		return join(terbilang(n/1_000_000_000_000)+" triliun", terbilang(n%1_000_000_000_000))
	default:
		return join(terbilang(n/1_000_000_000_000_000)+" kuadriliun", terbilang(n%1_000_000_000_000_000))
	}
}

func join(a, b string) string {
	if b == "" {
		return a
	}
	return a + " " + b
}
