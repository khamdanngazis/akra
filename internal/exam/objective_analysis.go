package exam

import (
	"math"
	"sort"
	"strings"
)

// Analisis TP (T148): capaian setiap siswa per tujuan pembelajaran dari
// soal bertanda TP yang ia kerjakan. Dengan soal acak per varian, capaian
// dihitung dari soal yang benar-benar diterima siswa itu, bukan dari
// daftar soal paket lain.
//
//   capaian TP siswa = Σ poin diperoleh / Σ poin maksimum soal TP itu
//   TP tuntas bila capaian ≥ KKTP; kelas: persentase siswa yang tuntas.

// ItemOutcome adalah poin satu soal bertanda TP yang dikerjakan siswa.
type ItemOutcome struct {
	QuestionID  string
	ObjectiveID string // kosong: soal tanpa TP
	Earned      int    // satuan 1/10.000 poin
	Points      int
}

// StudentOutcome adalah hasil satu peserta untuk analisis TP.
type StudentOutcome struct {
	Key       string // id peserta unik (siswa atau pendaftar)
	StudentID string
	Name      string
	Number    string
	ClassName string
	ScoreX100 int
	Items     []ItemOutcome
}

// ObjectiveRef adalah identitas TP untuk tampilan.
type ObjectiveRef struct {
	ID, Code, Description string
}

// Label menampilkan kode dan potongan deskripsi TP.
func (o ObjectiveRef) Label() string {
	if o.Description == "" {
		return o.Code
	}
	return o.Code + " — " + Snippet(o.Description, 80)
}

// ObjectiveStat adalah ketuntasan satu TP di kelas.
type ObjectiveStat struct {
	ObjectiveRef
	Questions int // soal berbeda yang memuat TP ini
	Students  int // siswa yang mendapat soal TP ini
	Mastered  int
	Percent   int // persentase siswa tuntas
	Average   int // rata-rata capaian (%)
}

// ObjectiveCell adalah capaian satu siswa untuk satu TP.
type ObjectiveCell struct {
	Has      bool // siswa mendapat soal TP ini
	Percent  int
	Mastered bool
}

// StudentObjectives adalah peta TP satu siswa.
type StudentObjectives struct {
	Key, StudentID, Name, Number, ClassName string
	ScoreX100                               int
	Pass                                    bool
	Cells                                   []ObjectiveCell // sejajar ObjectiveAnalysis.Objectives
	Weak                                    []ObjectiveRef  // TP belum tuntas
}

// Score menampilkan nilai siswa.
func (s StudentObjectives) Score() string { return ScoreLabel(s.ScoreX100) }

// WeakCodes menampilkan kode TP belum tuntas, mis. "TP1, TP3".
func (s StudentObjectives) WeakCodes() string {
	codes := make([]string, len(s.Weak))
	for i, o := range s.Weak {
		codes[i] = o.Code
	}
	return strings.Join(codes, ", ")
}

// ObjectiveAnalysis adalah hasil analisis TP satu ujian.
type ObjectiveAnalysis struct {
	KKTP       int
	Objectives []ObjectiveStat
	Students   []StudentObjectives
	Untagged   int // soal (berbeda) tanpa TP: tidak masuk analisis TP
}

// mastered: earned / (points × satuan) ≥ kktp / 100, tanpa pembulatan.
func mastered(earned, points, kktp int) bool {
	return points > 0 && earned*100 >= kktp*points*PointsUnit
}

// AnalyzeObjectives menghitung ketuntasan per TP dan peta TP per siswa.
// refs memberi urutan dan nama TP; TP yang tidak ada di refs (mis. sudah
// diarsipkan) ditambahkan di akhir dengan kode dari id.
func AnalyzeObjectives(students []StudentOutcome, refs []ObjectiveRef, kktp int) ObjectiveAnalysis {
	a := ObjectiveAnalysis{KKTP: kktp}
	index := map[string]int{}
	known := map[string]ObjectiveRef{}
	for _, r := range refs {
		known[r.ID] = r
	}
	questions := map[string]map[string]bool{}
	untagged := map[string]bool{}
	for _, s := range students {
		for _, it := range s.Items {
			if it.ObjectiveID == "" {
				untagged[it.QuestionID] = true
				continue
			}
			if questions[it.ObjectiveID] == nil {
				questions[it.ObjectiveID] = map[string]bool{}
			}
			questions[it.ObjectiveID][it.QuestionID] = true
		}
	}
	a.Untagged = len(untagged)
	add := func(r ObjectiveRef) {
		if _, ok := questions[r.ID]; ok {
			if _, dup := index[r.ID]; !dup {
				index[r.ID] = len(a.Objectives)
				a.Objectives = append(a.Objectives, ObjectiveStat{ObjectiveRef: r, Questions: len(questions[r.ID])})
			}
		}
	}
	for _, r := range refs {
		add(r)
	}
	var rest []string
	for id := range questions {
		if _, ok := index[id]; !ok {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	for _, id := range rest {
		add(ObjectiveRef{ID: id, Code: "TP " + id[:min(8, len(id))]})
	}
	sums := make([]int, len(a.Objectives)) // Σ capaian (%) untuk rata-rata
	for _, s := range students {
		earned := make([]int, len(a.Objectives))
		points := make([]int, len(a.Objectives))
		for _, it := range s.Items {
			if i, ok := index[it.ObjectiveID]; ok {
				earned[i] += it.Earned
				points[i] += it.Points
			}
		}
		so := StudentObjectives{Key: s.Key, StudentID: s.StudentID, Name: s.Name, Number: s.Number, ClassName: s.ClassName,
			ScoreX100: s.ScoreX100, Pass: s.ScoreX100 >= kktp*100, Cells: make([]ObjectiveCell, len(a.Objectives))}
		for i := range a.Objectives {
			if points[i] == 0 {
				continue
			}
			pct := int(math.Round(float64(earned[i]) * 100 / float64(points[i]*PointsUnit)))
			c := ObjectiveCell{Has: true, Percent: pct, Mastered: mastered(earned[i], points[i], kktp)}
			so.Cells[i] = c
			st := &a.Objectives[i]
			st.Students++
			sums[i] += pct
			if c.Mastered {
				st.Mastered++
			} else {
				so.Weak = append(so.Weak, st.ObjectiveRef)
			}
		}
		a.Students = append(a.Students, so)
	}
	for i := range a.Objectives {
		st := &a.Objectives[i]
		if st.Students > 0 {
			st.Percent = int(math.Round(float64(st.Mastered) * 100 / float64(st.Students)))
			st.Average = int(math.Round(float64(sums[i]) / float64(st.Students)))
		}
	}
	return a
}

// Mastery mengubah peta TP menjadi masukan perencanaan remedial (T137).
func (a ObjectiveAnalysis) Mastery() []StudentMastery {
	var out []StudentMastery
	for _, s := range a.Students {
		if s.StudentID == "" {
			continue
		}
		m := StudentMastery{StudentID: s.StudentID, Score: float64(s.ScoreX100) / 100, Objectives: map[string]float64{}}
		for i, c := range s.Cells {
			if c.Has {
				// Tuntas/tidak mengikuti perbandingan eksak di atas.
				v := float64(c.Percent)
				if c.Mastered {
					v = math.Max(v, float64(a.KKTP))
				} else {
					v = math.Min(v, float64(a.KKTP)-0.01)
				}
				m.Objectives[a.Objectives[i].ID] = v
			}
		}
		out = append(out, m)
	}
	return out
}

// FollowList adalah daftar remedial dan pengayaan.
type FollowList struct {
	Remedial   []StudentObjectives // nilai di bawah KKTP, dengan TP belum tuntas
	Enrichment []StudentObjectives // nilai ≥ KKTP
	Focus      []ObjectiveRef      // gabungan TP belum tuntas siswa remedial
}

// BuildFollowList memisahkan siswa remedial dan pengayaan. TP fokus siswa
// remedial mengikuti PlanRemedial (T137): TP belum tuntas, atau semua TP
// yang ia kerjakan bila semuanya tuntas.
func (a ObjectiveAnalysis) BuildFollowList() FollowList {
	var f FollowList
	plan := PlanRemedial(a.Mastery(), a.KKTP)
	refs := map[string]ObjectiveRef{}
	for _, o := range a.Objectives {
		refs[o.ID] = o.ObjectiveRef
	}
	for _, s := range a.Students {
		if s.Pass {
			f.Enrichment = append(f.Enrichment, s)
			continue
		}
		if weak, ok := plan.PerStudent[s.StudentID]; ok && len(s.Weak) == 0 {
			for _, id := range weak {
				s.Weak = append(s.Weak, refs[id])
			}
		}
		f.Remedial = append(f.Remedial, s)
	}
	for _, o := range a.Objectives {
		for _, id := range plan.Objectives {
			if id == o.ID {
				f.Focus = append(f.Focus, o.ObjectiveRef)
			}
		}
	}
	return f
}

// AchievementSuggestion menyusun saran deskripsi capaian siswa (T150) dari
// peta TP-nya: paling banyak dua TP tuntas dengan capaian tertinggi
// ("Menguasai ...") dan dua TP belum tuntas dengan capaian terendah
// ("perlu ditingkatkan ..."). Hanya saran; guru menyunting sebelum
// menyimpan. Kosong bila siswa tidak punya TP.
func (a ObjectiveAnalysis) AchievementSuggestion(s StudentObjectives) string {
	type tp struct {
		ref ObjectiveRef
		pct int
	}
	var good, weak []tp
	for i, c := range s.Cells {
		if !c.Has {
			continue
		}
		t := tp{a.Objectives[i].ObjectiveRef, c.Percent}
		if c.Mastered {
			good = append(good, t)
		} else {
			weak = append(weak, t)
		}
	}
	sort.SliceStable(good, func(i, j int) bool { return good[i].pct > good[j].pct })
	sort.SliceStable(weak, func(i, j int) bool { return weak[i].pct < weak[j].pct })
	name := func(xs []tp) string {
		parts := make([]string, 0, 2)
		for i, x := range xs {
			if i == 2 {
				break
			}
			p := x.ref.Code
			if d := Snippet(x.ref.Description, 50); d != "" {
				p += " (" + d + ")"
			}
			parts = append(parts, p)
		}
		return strings.Join(parts, " dan ")
	}
	var out []string
	if len(good) > 0 {
		out = append(out, "Menguasai "+name(good))
	}
	if len(weak) > 0 {
		w := "perlu ditingkatkan " + name(weak)
		if len(out) == 0 {
			w = "Perlu ditingkatkan " + name(weak)
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "; ") + "."
}
