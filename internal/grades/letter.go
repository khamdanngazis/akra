package grades

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"akra/internal/platform/database"
	"akra/internal/platform/settings"
)

// Band adalah satu huruf (atau predikat) dengan rentang nilai akhir bulat
// Min–Max, keduanya inklusif.
type Band struct {
	Letter string `json:"letter"`
	Min    int    `json:"min"`
	Max    int    `json:"max"`
}

// Scale adalah skala huruf, urut dari rentang tertinggi. Skala valid
// mencakup 0–100 tanpa celah dan tanpa tumpang tindih (ValidateScale).
type Scale []Band

// MaxLetterLen adalah panjang maksimal huruf/predikat, mis. "Sangat Baik".
const MaxLetterLen = 20

// DefaultScale berlaku jika sekolah belum mengatur skala: A 90–100,
// B 80–89, C 70–79, D 0–69.
func DefaultScale() Scale {
	return Scale{{"A", 90, 100}, {"B", 80, 89}, {"C", 70, 79}, {"D", 0, 69}}
}

var (
	// ErrInvalidScale: isi skala tidak valid (huruf kosong atau ganda, batas
	// di luar 0–100, min lebih besar dari max).
	ErrInvalidScale = errors.New("grades: invalid letter scale")
	// ErrScaleOverlap: dua rentang tumpang tindih.
	ErrScaleOverlap = errors.New("grades: letter scale ranges overlap")
	// ErrScaleGap: ada nilai di antara dua rentang yang tidak punya huruf.
	ErrScaleGap = errors.New("grades: letter scale has a gap")
	// ErrScaleCoverage: skala tidak dimulai dari 0 atau tidak sampai 100.
	ErrScaleCoverage = errors.New("grades: letter scale must cover 0-100")
)

// ValidateScale mengurutkan skala dari rentang tertinggi lalu memastikan
// setiap nilai bulat 0–100 punya tepat satu huruf.
func ValidateScale(sc Scale) (Scale, error) {
	if len(sc) == 0 {
		return nil, ErrScaleCoverage
	}
	out := slices.Clone(sc)
	seen := map[string]bool{}
	for i, b := range out {
		b.Letter = strings.TrimSpace(b.Letter)
		out[i] = b
		key := strings.ToLower(b.Letter)
		switch {
		case b.Letter == "" || utf8.RuneCountInString(b.Letter) > MaxLetterLen:
			return nil, fmt.Errorf("%w: letter %q", ErrInvalidScale, b.Letter)
		case seen[key]:
			return nil, fmt.Errorf("%w: duplicate letter %q", ErrInvalidScale, b.Letter)
		case b.Min < 0 || b.Max > 100 || b.Min > b.Max:
			return nil, fmt.Errorf("%w: %s %d-%d", ErrInvalidScale, b.Letter, b.Min, b.Max)
		}
		seen[key] = true
	}
	slices.SortStableFunc(out, func(a, b Band) int { return b.Min - a.Min })
	for i := 1; i < len(out); i++ {
		upper, lower := out[i-1], out[i]
		switch {
		case lower.Max >= upper.Min:
			return nil, fmt.Errorf("%w: %s and %s", ErrScaleOverlap, upper.Letter, lower.Letter)
		case lower.Max < upper.Min-1:
			return nil, fmt.Errorf("%w: between %s and %s", ErrScaleGap, lower.Letter, upper.Letter)
		}
	}
	if out[0].Max != 100 || out[len(out)-1].Min != 0 {
		return nil, ErrScaleCoverage
	}
	return out, nil
}

// ParseScale membaca skala JSON kunci school_settings report.scale, mis.
// [{"letter":"A","min":90,"max":100}, ...], lalu memvalidasinya.
func ParseScale(s string) (Scale, error) {
	var sc Scale
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidScale, err)
	}
	return ValidateScale(sc)
}

// JSON memformat skala untuk disimpan.
func (sc Scale) JSON() string {
	b, _ := json.Marshal(sc)
	return string(b)
}

// Letter mengembalikan huruf untuk nilai akhir bulat score (0–100).
func Letter(score int, scale Scale) (string, error) {
	if score < 0 || score > 100 {
		return "", fmt.Errorf("%w: %d", ErrInvalidScore, score)
	}
	for _, b := range scale {
		if score >= b.Min && score <= b.Max {
			return b.Letter, nil
		}
	}
	return "", ErrInvalidScale
}

// LoadScale membaca skala huruf sekolah (report.scale); DefaultScale jika
// belum diatur. Nilai tersimpan yang rusak adalah error, bukan diam-diam
// memakai default.
func LoadScale(ctx context.Context, q database.DBTX, schoolID string) (Scale, error) {
	v, ok, err := settings.Get(ctx, q, schoolID, settings.ReportScale)
	if err != nil {
		return nil, err
	}
	if !ok {
		return DefaultScale(), nil
	}
	return ParseScale(v)
}
