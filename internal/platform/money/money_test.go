package money

import (
	"errors"
	"testing"
)

func TestParseRupiah(t *testing.T) {
	ok := map[string]int64{
		"0":            0,
		"1500000":      1500000,
		"1.500.000":    1500000,
		"Rp1.500.000":  1500000,
		"Rp 1.500.000": 1500000,
		" rp 150.000 ": 150000,
		"1.500.000,-":  1500000,
		"1.500.000,00": 1500000,
		"250":          250,
		"007":          7,
	}
	for in, want := range ok {
		got, err := ParseRupiah(in)
		if err != nil || got != want {
			t.Errorf("ParseRupiah(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	bad := map[string]error{
		"":                     ErrInvalidAmount,
		"-1":                   ErrNegativeAmount,
		"Rp -5.000":            ErrNegativeAmount,
		"1,5":                  ErrInvalidAmount,
		"1.500.000,50":         ErrInvalidAmount,
		"1.50.000":             ErrInvalidAmount,
		"15.00":                ErrInvalidAmount,
		"1500.000.":            ErrInvalidAmount,
		"satu juta":            ErrInvalidAmount,
		"1 500 000":            ErrInvalidAmount,
		"1e6":                  ErrInvalidAmount,
		"99999999999999999999": ErrInvalidAmount,
	}
	for in, want := range bad {
		if got, err := ParseRupiah(in); !errors.Is(err, want) {
			t.Errorf("ParseRupiah(%q) = %d, %v; want %v", in, got, err, want)
		}
	}
}

func TestFormatRupiah(t *testing.T) {
	cases := map[int64]string{0: "Rp0", 999: "Rp999", 1000: "Rp1.000", 1500000: "Rp1.500.000", 12345678901: "Rp12.345.678.901", -2500: "-Rp2.500"}
	for in, want := range cases {
		if got := FormatRupiah(in); got != want {
			t.Errorf("FormatRupiah(%d) = %q, want %q", in, got, want)
		}
	}
}
