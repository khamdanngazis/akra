// Package money membaca dan memformat nominal rupiah. Nominal selalu
// bilangan bulat rupiah (int64); tidak pernah float.
package money

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

var (
	// ErrInvalidAmount dikembalikan untuk isian yang bukan nominal rupiah bulat.
	ErrInvalidAmount = errors.New("money: invalid rupiah amount")
	// ErrNegativeAmount dikembalikan untuk nominal negatif.
	ErrNegativeAmount = errors.New("money: negative amount")
)

// ParseRupiah membaca nominal rupiah bulat dari isian pengguna. Diterima:
//
//	1500000, 1.500.000, Rp1.500.000, Rp 1.500.000, 1.500.000,-, 1.500.000,00
//
// Titik dianggap pemisah ribuan (format Indonesia) dan harus berkelompok tiga
// digit. Pecahan selain ",00" atau ",-" ditolak, begitu juga angka negatif.
func ParseRupiah(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "Rp"), "rp")
	s = strings.TrimSpace(strings.TrimPrefix(s, "."))
	if strings.HasPrefix(s, "-") {
		return 0, ErrNegativeAmount
	}
	for _, suffix := range []string{",-", ",00", ",0"} {
		if strings.HasSuffix(s, suffix) {
			s = strings.TrimSuffix(s, suffix)
			break
		}
	}
	if s == "" || strings.ContainsAny(s, ", ") {
		return 0, ErrInvalidAmount
	}
	if strings.Contains(s, ".") {
		groups := strings.Split(s, ".")
		if len(groups[0]) == 0 || len(groups[0]) > 3 {
			return 0, ErrInvalidAmount
		}
		for _, g := range groups[1:] {
			if len(g) != 3 {
				return 0, ErrInvalidAmount
			}
		}
		s = strings.Join(groups, "")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, ErrInvalidAmount
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, ErrInvalidAmount
	}
	return n, nil
}

// FormatRupiah memformat n sebagai "Rp1.500.000" (negatif: "-Rp1.500").
func FormatRupiah(n int64) string {
	if n < 0 {
		if n == math.MinInt64 {
			return "-Rp9.223.372.036.854.775.808"
		}
		return "-" + FormatRupiah(-n)
	}
	return "Rp" + Group(n)
}

// Group memformat n dengan titik sebagai pemisah ribuan, tanpa "Rp".
func Group(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}
