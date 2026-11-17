package exam

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// assetIDRe menemukan ID gambar (data-asset) di sumber teks kaya.
var assetIDRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// parseNumber membaca angka dengan koma atau titik desimal ("2,5" = "2.5").
// Pemisah ribuan tidak didukung agar "1.000" tidak ambigu; ditulis 1000.
func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if strings.Count(s, ",")+strings.Count(s, ".") > 1 {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	return v, err == nil
}

// normalizeAnswer menyeragamkan jawaban isian teks: huruf kecil, tanpa
// tanda baca, spasi dirapatkan; harakat dan variasi huruf Arab
// disamakan bila arabic true.
func normalizeAnswer(s string, arabic bool) string {
	if arabic {
		s = NormalizeArabic(s)
	}
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// MatchShortAnswer melaporkan apakah jawaban isian peserta diterima.
// Teks: abaikan besar/kecil, spasi, dan tanda baca (serta harakat bila
// ArabicNormalize). Angka: koma atau titik desimal, dalam toleransi.
func MatchShortAnswer(input string, mode string, key AnswerKey) bool {
	if strings.TrimSpace(input) == "" {
		return false
	}
	if mode == "angka" {
		got, ok := parseNumber(input)
		if !ok {
			return false
		}
		for _, a := range key.Accepted {
			if want, ok := parseNumber(a); ok {
				d := got - want
				if d < 0 {
					d = -d
				}
				if d <= key.Tolerance+1e-9 {
					return true
				}
			}
		}
		return false
	}
	got := normalizeAnswer(input, key.ArabicNormalize)
	for _, a := range key.Accepted {
		if normalizeAnswer(a, key.ArabicNormalize) == got {
			return true
		}
	}
	return false
}
