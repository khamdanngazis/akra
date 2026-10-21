package grades

import (
	"errors"
	"fmt"
)

// Aturan nilai akhir (T71). Semua tampilan dan rekap memakai FinalScore.
// Perhitungan hanya memakai bilangan bulat: nilai dalam perseratus (Score)
// dikali bobot persen, sehingga satuan jumlahnya 1/10.000 poin.

var (
	// ErrWeightTotal: total bobot komponen bukan 100.
	ErrWeightTotal = errors.New("grades: component weights must total 100")
	// ErrInvalidWeight: bobot satu komponen di luar 1–100.
	ErrInvalidWeight = errors.New("grades: component weight must be 1-100")
	// ErrInvalidScore: nilai di luar 0–100.
	ErrInvalidScore = errors.New("grades: score must be 0-100")
)

// ComponentScore adalah satu komponen aktif beserta nilai siswa. Score nil
// berarti belum dinilai.
type ComponentScore struct {
	Weight int // persen, 1–100
	Score  *Score
}

// Result adalah nilai akhir satu siswa di satu mapel.
type Result struct {
	// Incomplete true jika ada komponen yang belum dinilai. Score dan Exact
	// lalu bernilai nol dan TIDAK boleh ditampilkan sebagai angka.
	Incomplete bool
	Missing    int   // jumlah komponen yang belum dinilai
	Score      int   // nilai akhir bulat 0–100 (pembulatan RoundFinal)
	Exact      Score // nilai berbobot dalam perseratus, sebelum dibulatkan ke bilangan bulat
}

// FinalScore menghitung nilai akhir dari komponen aktif satu mapel.
//
//   - Bobot setiap komponen harus 1–100 dan totalnya 100 (ErrWeightTotal),
//     diperiksa lebih dulu, bahkan jika ada nilai yang kosong.
//   - Jika ada komponen yang belum dinilai, hasilnya Incomplete tanpa angka
//     parsial.
//   - Nilai akhir = Σ(nilai × bobot) / 100, dibulatkan dengan RoundFinal.
func FinalScore(components []ComponentScore) (Result, error) {
	total := 0
	for _, c := range components {
		if c.Weight < 1 || c.Weight > 100 {
			return Result{}, fmt.Errorf("%w: %d", ErrInvalidWeight, c.Weight)
		}
		total += c.Weight
		if c.Score != nil && (*c.Score < 0 || *c.Score > MaxScore) {
			return Result{}, fmt.Errorf("%w: %d", ErrInvalidScore, *c.Score)
		}
	}
	if total != 100 {
		return Result{}, fmt.Errorf("%w (got %d)", ErrWeightTotal, total)
	}
	var sum int64 // Σ nilai(perseratus) × bobot(persen), satuan 1/10.000 poin
	res := Result{}
	for _, c := range components {
		if c.Score == nil {
			res.Missing++
			continue
		}
		sum += int64(*c.Score) * int64(c.Weight)
	}
	if res.Missing > 0 {
		return Result{Incomplete: true, Missing: res.Missing}, nil
	}
	res.Exact = Score(roundHalfUp(sum, 100))
	res.Score = RoundFinal(sum)
	return res, nil
}

// RoundFinal adalah SATU-SATUNYA tempat aturan pembulatan nilai akhir:
// bilangan bulat, setengah ke atas (78,50 → 79; 78,49 → 78). weighted dalam
// satuan 1/10.000 poin (lihat FinalScore). Jika sekolah memakai aturan lain,
// cukup ubah fungsi ini lalu jalankan ulang final_test.go.
func RoundFinal(weighted int64) int {
	return int(roundHalfUp(weighted, 10000))
}

// roundHalfUp membagi n/d (n >= 0, d > 0) dan membulatkan setengah ke atas.
func roundHalfUp(n, d int64) int64 {
	return (n + d/2) / d
}
