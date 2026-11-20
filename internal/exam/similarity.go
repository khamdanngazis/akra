package exam

import "strings"

// Kemiripan jawaban esai (T147): bahan pertimbangan guru, bukan vonis.
// Jawaban dinormalkan (huruf kecil, tanpa tanda baca, spasi dirapatkan,
// harakat Arab disamakan), dipecah menjadi rangkaian 3 kata (shingle), lalu
// dibandingkan dengan indeks Jaccard: |A ∩ B| / |A ∪ B|.

// Ambang kemiripan.
const (
	SimilarMinWords = 8   // jawaban lebih pendek tidak dibandingkan (wajar sama)
	SimilarFlag     = 0.5 // Jaccard ≥ ini ditandai
)

// answerShingles mengembalikan shingle jawaban (shingles, sama dengan cek
// mutu soal T132), atau nil bila jawaban terlalu pendek untuk dibandingkan.
func answerShingles(s string) map[string]bool {
	if len(strings.Fields(normalizeAnswer(s, true))) < SimilarMinWords {
		return nil
	}
	return shingles(s)
}

// SimilarPair adalah dua jawaban yang mirip (indeks ke daftar masukan).
type SimilarPair struct {
	A, B  int
	Score float64 // 0..1
}

// Percent untuk tampilan.
func (p SimilarPair) Percent() int { return int(p.Score*100 + 0.5) }

// FindSimilar mengembalikan pasangan jawaban dengan kemiripan ≥
// SimilarFlag, urut indeks.
func FindSimilar(answers []string) []SimilarPair {
	sets := make([]map[string]bool, len(answers))
	for i, a := range answers {
		sets[i] = answerShingles(a)
	}
	var out []SimilarPair
	for i := range sets {
		if sets[i] == nil {
			continue
		}
		for j := i + 1; j < len(sets); j++ {
			if sets[j] == nil {
				continue
			}
			if s := jaccard(sets[i], sets[j]); s >= SimilarFlag {
				out = append(out, SimilarPair{A: i, B: j, Score: s})
			}
		}
	}
	return out
}
