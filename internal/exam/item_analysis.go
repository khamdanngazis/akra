package exam

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Analisis butir klasik (T148), dengan rumus yang dikenal guru:
//
//   - Tingkat kesukaran p = rata-rata porsi poin yang diperoleh peserta.
//     Untuk soal benar/salah murni sama dengan (jumlah benar / jumlah
//     peserta); untuk soal berskor sebagian (esai, menjodohkan) memakai
//     rata-rata skor / skor maksimum.
//   - Daya beda D = p kelompok atas − p kelompok bawah, masing-masing 27%
//     peserta dengan nilai ujian tertinggi dan terendah.
//   - Efektivitas pengecoh: opsi salah yang dipilih kurang dari 5% peserta
//     ditandai tidak berfungsi.
//
// Dengan soal acak per varian, setiap soal dianalisis atas peserta yang
// mendapat soal itu saja (kelompok atas/bawah juga dari mereka).

// Ambang analisis butir.
const (
	upperLowerShare   = 0.27 // proporsi kelompok atas dan bawah
	weakDistractorPct = 5    // pengecoh dipilih < 5% peserta ditandai
)

// ItemResponse adalah jawaban satu peserta untuk satu soal.
type ItemResponse struct {
	Total  int      // nilai ujian peserta (×100), untuk kelompok atas/bawah
	Earned int      // poin diperoleh, satuan 1/10.000 poin
	Picked []string // opsi dipilih (pilihan ganda dan PG kompleks)
	Blank  bool     // tidak dijawab
}

// ItemInput adalah satu soal dengan jawaban semua peserta yang
// mendapatkannya.
type ItemInput struct {
	Item          GradeItem
	Number        int // nomor soal (urutan paket pertama yang memuatnya)
	ObjectiveCode string
	Responses     []ItemResponse
}

// OptionStat adalah sebaran pilihan satu opsi.
type OptionStat struct {
	Letter  string
	Count   int
	Percent int
	Key     bool
	Weak    bool // pengecoh dipilih < 5% peserta
}

// ItemStat adalah hasil analisis satu soal.
type ItemStat struct {
	QuestionID    string
	Version       int
	Number        int
	Kind          Kind
	Snippet       string
	ObjectiveID   string
	ObjectiveCode string
	Points        int
	Voided        bool
	N             int     // peserta yang mendapat soal ini
	P             float64 // tingkat kesukaran 0..1
	D             float64 // daya beda −1..1
	HasD          bool    // kelompok atas dan bawah terbentuk (N ≥ 2)
	Group         int     // ukuran kelompok atas (= bawah)
	Options       []OptionStat
	Blank         int
	Warnings      []string
}

// round2 membulatkan ke dua desimal (tampilan p dan D).
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// groupSize mengembalikan ukuran kelompok atas/bawah: 27% dibulatkan,
// paling sedikit 1 bila ada minimal dua peserta, dan tidak lebih dari
// separuh peserta.
func groupSize(n int) int {
	if n < 2 {
		return 0
	}
	g := int(math.Round(upperLowerShare * float64(n)))
	return max(1, min(g, n/2))
}

// AnalyzeItem menghitung p, D, dan sebaran opsi satu soal.
func AnalyzeItem(in ItemInput) ItemStat {
	q := in.Item.Question
	st := ItemStat{QuestionID: q.ID, Version: q.Version, Number: in.Number, Kind: q.Kind, Snippet: Snippet(PlainText(q.BodySrc), 90),
		ObjectiveID: q.ObjectiveID, ObjectiveCode: in.ObjectiveCode, Points: in.Item.Points, Voided: in.Item.Voided, N: len(in.Responses)}
	if st.Voided {
		st.Warnings = append(st.Warnings, "Soal dibatalkan: tidak dihitung di nilai dan analisis.")
		return st
	}
	if st.N == 0 || st.Points <= 0 {
		return st
	}
	full := float64(st.Points * PointsUnit)
	sum := 0
	for _, r := range in.Responses {
		sum += r.Earned
		if r.Blank {
			st.Blank++
		}
	}
	st.P = float64(sum) / (float64(st.N) * full)
	if g := groupSize(st.N); g > 0 {
		// Urut nilai ujian tertinggi; nilai sama mempertahankan urutan
		// masukan (urutan peserta) agar hasil stabil.
		rs := append([]ItemResponse(nil), in.Responses...)
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].Total > rs[j].Total })
		up, low := 0, 0
		for i := 0; i < g; i++ {
			up += rs[i].Earned
			low += rs[len(rs)-1-i].Earned
		}
		st.D = float64(up-low) / (float64(g) * full)
		st.HasD, st.Group = true, g
	}
	if q.Kind == KindMC || q.Kind == KindMulti {
		key := setOf(in.Item.Key.Correct)
		counts := map[string]int{}
		for _, r := range in.Responses {
			seen := map[string]bool{}
			for _, id := range r.Picked {
				if !seen[id] {
					seen[id] = true
					counts[id]++
				}
			}
		}
		var weak []string
		for i, c := range q.Options.Choices {
			o := OptionStat{Letter: letter(i), Count: counts[c.ID], Key: key[c.ID]}
			o.Percent = int(math.Round(float64(o.Count) * 100 / float64(st.N)))
			if !o.Key && o.Count*100 < weakDistractorPct*st.N {
				o.Weak = true
				weak = append(weak, o.Letter)
			}
			st.Options = append(st.Options, o)
		}
		if len(weak) > 0 {
			st.Warnings = append(st.Warnings, "Pengecoh "+strings.Join(weak, ", ")+" dipilih kurang dari 5% peserta: tidak berfungsi, perlu diganti.")
		}
	}
	switch p := round2(st.P); {
	case p == 0:
		st.Warnings = append(st.Warnings, "Tidak ada yang menjawab benar (p = 0): periksa kunci atau rumusan soal.")
	case p == 1:
		st.Warnings = append(st.Warnings, "Semua menjawab benar (p = 1): soal tidak membedakan peserta.")
	}
	if st.HasD && round2(st.D) < 0 {
		st.Warnings = append([]string{"Daya beda negatif: kelompok bawah lebih banyak benar. Periksa kunci."}, st.Warnings...)
	}
	return st
}

// NegativeD melaporkan daya beda negatif (kemungkinan kunci salah).
func (s ItemStat) NegativeD() bool { return s.HasD && round2(s.D) < 0 }

// Extreme melaporkan p = 0 atau p = 1.
func (s ItemStat) Extreme() bool { p := round2(s.P); return s.N > 0 && (p == 0 || p == 1) }

// KeyFixable: kunci soal bisa dibetulkan (bukan esai).
func (s ItemStat) KeyFixable() bool { return s.Kind != KindEssay && !s.Voided }

// PText menampilkan p dengan koma desimal, mis. "0,53".
func (s ItemStat) PText() string { return decimal2(s.P) }

// DText menampilkan daya beda.
func (s ItemStat) DText() string {
	if !s.HasD {
		return "—"
	}
	return decimal2(s.D)
}

// decimal2 menampilkan pecahan dua desimal dengan koma.
func decimal2(v float64) string {
	return strings.Replace(strconv.FormatFloat(round2(v), 'f', 2, 64), ".", ",", 1)
}

// PLabel mengembalikan kategori tingkat kesukaran.
func (s ItemStat) PLabel() string {
	if s.N == 0 || s.Voided {
		return ""
	}
	switch p := round2(s.P); {
	case p < 0.30:
		return "Sukar"
	case p <= 0.70:
		return "Sedang"
	}
	return "Mudah"
}

// DLabel mengembalikan kategori daya beda.
func (s ItemStat) DLabel() string {
	if !s.HasD || s.Voided {
		return ""
	}
	switch d := round2(s.D); {
	case d < 0:
		return "Negatif"
	case d < 0.20:
		return "Jelek"
	case d < 0.30:
		return "Cukup"
	case d < 0.40:
		return "Baik"
	}
	return "Sangat baik"
}

// Decision mengembalikan saran tindak lanjut soal untuk bank soal.
func (s ItemStat) Decision() string {
	if !s.HasD || s.Voided {
		return ""
	}
	switch d := round2(s.D); {
	case d < 0:
		return "Periksa kunci"
	case d < 0.20:
		return "Ganti"
	case d < 0.30 || s.Extreme():
		return "Revisi"
	}
	return "Diterima"
}

// OptionsText meringkas sebaran opsi, mis. "A 10% · B* 60% · C 2%!".
func (s ItemStat) OptionsText() string {
	parts := make([]string, len(s.Options))
	for i, o := range s.Options {
		p := o.Letter
		if o.Key {
			p += "*"
		}
		p += " " + strconv.Itoa(o.Percent) + "%"
		if o.Weak {
			p += "!"
		}
		parts[i] = p
	}
	return strings.Join(parts, " · ")
}

// StoreQuestionStats menyimpan p dan D soal ujian e ke exam_question_stats
// (riwayat butir di bank soal, T127): satu baris per soal per ujian,
// diperbarui hanya bila berubah. Soal dibatalkan atau tanpa peserta
// dihapus dari riwayat ujian ini.
func StoreQuestionStats(ctx context.Context, q database.DB, e Exam, stats []ItemStat, now time.Time) error {
	var ids []string
	var vers, ns []int
	var ps, ds []string
	for _, s := range stats {
		if s.Voided || s.N == 0 {
			continue
		}
		ids = append(ids, s.QuestionID)
		vers = append(vers, max(s.Version, 1))
		ns = append(ns, s.N)
		ps = append(ps, strconv.FormatFloat(math.Round(s.P*1000)/1000, 'f', 3, 64))
		d := ""
		if s.HasD {
			d = strconv.FormatFloat(math.Round(s.D*1000)/1000, 'f', 3, 64)
		}
		ds = append(ds, d)
	}
	return pgx.BeginFunc(ctx, q, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM exam_question_stats WHERE school_id = $1 AND exam_id = $2 AND NOT (question_id = ANY($3::uuid[]))`,
			e.SchoolID, e.ID, ids); err != nil {
			return fmt.Errorf("exam: prune question stats: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO exam_question_stats (school_id, question_id, version, exam_id, participants, difficulty_index, discrimination, computed_at)
			SELECT $1, u.q, u.v, $2, u.n, u.p::numeric, nullif(u.d, '')::numeric, $8
			FROM unnest($3::uuid[], $4::int[], $5::int[], $6::text[], $7::text[]) AS u(q, v, n, p, d)
			ON CONFLICT (exam_id, question_id) DO UPDATE SET version = excluded.version, participants = excluded.participants,
				difficulty_index = excluded.difficulty_index, discrimination = excluded.discrimination, computed_at = excluded.computed_at
			WHERE (exam_question_stats.version, exam_question_stats.participants, exam_question_stats.difficulty_index)
			      IS DISTINCT FROM (excluded.version, excluded.participants, excluded.difficulty_index)
			   OR exam_question_stats.discrimination IS DISTINCT FROM excluded.discrimination`,
			e.SchoolID, e.ID, ids, vers, ns, ps, ds, now); err != nil {
			return fmt.Errorf("exam: store question stats: %w", err)
		}
		return nil
	})
}
