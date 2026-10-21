package grades

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"akra/internal/platform/database"
	"akra/internal/platform/settings"
)

// Band adalah satu huruf beserta batas bawahnya (inklusif), dalam nilai
// akhir bulat.
type Band struct {
	Letter string
	Min    int
}

// Scale adalah skala huruf, urut batas bawah dari tertinggi; batas bawah
// terakhir selalu 0 sehingga setiap nilai 0–100 punya huruf.
type Scale []Band

// DefaultScale berlaku jika sekolah belum mengatur skala: A ≥ 90, B ≥ 80,
// C ≥ 70, D di bawah 70.
func DefaultScale() Scale {
	return Scale{{"A", 90}, {"B", 80}, {"C", 70}, {"D", 0}}
}

// ErrInvalidScale: teks skala tidak valid.
var ErrInvalidScale = errors.New("grades: invalid letter scale")

// ParseScale membaca skala dari teks "A:90,B:80,C:70,D:0" (format kunci
// school_settings grade_letter_scale). Batas harus turun tegas, 0–100, dan
// yang terakhir 0; huruf 1–3 karakter dan tidak boleh ganda.
func ParseScale(s string) (Scale, error) {
	var sc Scale
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		letter, min, ok := strings.Cut(strings.TrimSpace(part), ":")
		letter = strings.TrimSpace(letter)
		n, err := strconv.Atoi(strings.TrimSpace(min))
		switch {
		case !ok || err != nil:
			return nil, fmt.Errorf("%w: %q", ErrInvalidScale, part)
		case letter == "" || len([]rune(letter)) > 3 || seen[letter]:
			return nil, fmt.Errorf("%w: letter %q", ErrInvalidScale, letter)
		case n < 0 || n > 100 || (len(sc) > 0 && n >= sc[len(sc)-1].Min):
			return nil, fmt.Errorf("%w: bounds must decrease within 0-100", ErrInvalidScale)
		}
		seen[letter] = true
		sc = append(sc, Band{letter, n})
	}
	if len(sc) == 0 || sc[len(sc)-1].Min != 0 {
		return nil, fmt.Errorf("%w: the last bound must be 0", ErrInvalidScale)
	}
	return sc, nil
}

// String memformat skala untuk disimpan: "A:90,B:80,C:70,D:0".
func (sc Scale) String() string {
	parts := make([]string, len(sc))
	for i, b := range sc {
		parts[i] = b.Letter + ":" + strconv.Itoa(b.Min)
	}
	return strings.Join(parts, ",")
}

// Letter mengembalikan huruf untuk nilai akhir bulat score (0–100). Batas
// bawah inklusif: tepat 80 pada skala default = B.
func Letter(score int, scale Scale) (string, error) {
	if score < 0 || score > 100 {
		return "", fmt.Errorf("%w: %d", ErrInvalidScore, score)
	}
	for _, b := range scale {
		if score >= b.Min {
			return b.Letter, nil
		}
	}
	return "", ErrInvalidScale
}

// LoadScale membaca skala huruf sekolah dari school_settings; DefaultScale
// jika belum diatur. Nilai tersimpan yang rusak adalah error, bukan
// diam-diam memakai default.
func LoadScale(ctx context.Context, q database.DBTX, schoolID string) (Scale, error) {
	v, ok, err := settings.Get(ctx, q, schoolID, settings.GradeLetterScale)
	if err != nil {
		return nil, err
	}
	if !ok {
		return DefaultScale(), nil
	}
	return ParseScale(v)
}
