package exam

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// testPool membuat bank berisi soal tunggal dengan komposisi berulang dan
// beberapa kelompok stimulus.
func testPool() Pool {
	var qs []PoolQuestion
	diffs := []string{"mudah", "sedang", "sulit"}
	levels := []string{"C1", "C3", "C4"}
	for i := 0; i < 60; i++ {
		qs = append(qs, PoolQuestion{ID: fmt.Sprintf("q%02d", i), BankID: "b", Kind: KindMC, Difficulty: diffs[i%3],
			Cognitive: levels[i%3], ObjectiveID: []string{"tp1", "tp2"}[i%2], Points: 1, Status: StatusReady})
	}
	for i := 0; i < 8; i++ {
		qs = append(qs, PoolQuestion{ID: fmt.Sprintf("e%02d", i), BankID: "b", Kind: KindEssay, Difficulty: "sedang", Cognitive: "C4", Points: 5})
	}
	for s := 0; s < 6; s++ {
		for j := 0; j < 3; j++ {
			qs = append(qs, PoolQuestion{ID: fmt.Sprintf("s%d-%d", s, j), BankID: "b", StimulusID: fmt.Sprintf("st%d", s), StimulusOrder: j + 1,
				Kind: KindMC, Difficulty: "sedang", Cognitive: "C3", Points: 1})
		}
	}
	return Pool{"b": qs}
}

func compSig(c Composition) string {
	var parts []string
	for k, v := range c.ByKind {
		parts = append(parts, fmt.Sprintf("k%s=%d", k, v))
	}
	for k, v := range c.ByDifficulty {
		parts = append(parts, fmt.Sprintf("d%s=%d", k, v))
	}
	for k, v := range c.ByCognitive {
		parts = append(parts, fmt.Sprintf("c%s=%d", k, v))
	}
	for k, v := range c.ByObjective {
		parts = append(parts, fmt.Sprintf("o%s=%d", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",") + fmt.Sprintf(" stim=%d pts=%d", c.Stimuli, c.Points)
}

// Varian A/B/C punya komposisi TP/level/kesulitan sama, dengan soal
// berbeda bila bank cukup.
func TestAssembleVariantsSameComposition(t *testing.T) {
	pool := testPool()
	fixed := pool["b"][4] // q04: sedang/C3/tp1
	items := []AsmItem{
		{ID: "i1", SectionID: "s1", Question: &fixed},
		{ID: "i2", SectionID: "s1", Rule: &Rule{BankID: "b", Kind: KindMC, Difficulty: "mudah", Count: 5}},
		{ID: "i3", SectionID: "s1", Rule: &Rule{BankID: "b", Kind: KindMC, Count: 4}},
		{ID: "i4", SectionID: "s2", Rule: &Rule{BankID: "b", Stimulus: true, Count: 2}},
		{ID: "i5", SectionID: "s2", Rule: &Rule{BankID: "b", Kind: KindEssay, Count: 2}},
	}
	for _, seed := range []int64{1, 7, 42, 2026} {
		vs := Assemble(items, pool, []int64{seed, seed + 1, seed + 2})
		if len(vs) != 3 {
			t.Fatalf("variants = %d", len(vs))
		}
		want := compSig(Compose(vs[0].Items))
		seen := map[string]int{}
		for v, a := range vs {
			if len(a.Short) != 0 {
				t.Errorf("seed %d variant %d short %v", seed, v, a.Short)
			}
			if got := compSig(Compose(a.Items)); got != want {
				t.Errorf("seed %d variant %d composition\n got %s\nwant %s", seed, v, got, want)
			}
			ids := map[string]bool{}
			for _, it := range a.Items {
				if ids[it.QuestionID] {
					t.Errorf("seed %d variant %d repeats %s", seed, v, it.QuestionID)
				}
				ids[it.QuestionID] = true
				seen[it.QuestionID]++
			}
			if a.Items[0].SectionID != "s1" || a.Items[len(a.Items)-1].SectionID != "s2" {
				t.Errorf("section order broken")
			}
		}
		// Bank cukup besar: varian B dan C tidak berbagi soal dengan A
		// selain kelompok stimulus (hanya 6 kelompok untuk 3×2).
		if vs[1].Shared != 0 || vs[2].Shared != 0 {
			t.Errorf("seed %d shared = %d, %d", seed, vs[1].Shared, vs[2].Shared)
		}
		// Hasil sama untuk seed sama.
		again := Assemble(items, pool, []int64{seed, seed + 1, seed + 2})
		for v := range vs {
			for i := range vs[v].Items {
				if vs[v].Items[i].QuestionID != again[v].Items[i].QuestionID {
					t.Fatalf("seed %d not deterministic", seed)
				}
			}
		}
	}
}

// Aturan yang melebihi isi bank dilaporkan; Available menghitung sisa.
func TestAssembleShortageAndAvailable(t *testing.T) {
	pool := testPool()
	r := Rule{BankID: "b", Kind: KindEssay, Count: 10}
	if n := pool.Available(r, nil); n != 8 {
		t.Errorf("available essays = %d", n)
	}
	if n := pool.Available(Rule{BankID: "b", Stimulus: true, Count: 1}, map[string]bool{"s0-1": true}); n != 5 {
		t.Errorf("available stimuli = %d", n)
	}
	vs := Assemble([]AsmItem{{ID: "x", SectionID: "s", Rule: &r}}, pool, []int64{1})
	if len(vs[0].Short) != 1 || vs[0].Short[0].Got != 8 {
		t.Errorf("short = %+v", vs[0].Short)
	}
	// Bank kecil: varian B memakai soal yang sama dan dilaporkan.
	small := Pool{"b": pool["b"][:3]}
	vs = Assemble([]AsmItem{{ID: "x", SectionID: "s", Rule: &Rule{BankID: "b", Count: 3}}}, small, []int64{1, 2})
	if vs[1].Shared != 3 {
		t.Errorf("small bank shared = %d", vs[1].Shared)
	}
}

// Kelompok stimulus tidak pernah terpisah dan urutan di dalamnya tetap
// kecuali diizinkan.
func TestShuffleKeepsStimulusBlocks(t *testing.T) {
	pool := testPool()
	items := []AsmItem{
		{ID: "a", SectionID: "s1", Rule: &Rule{BankID: "b", Kind: KindMC, Count: 10}},
		{ID: "b", SectionID: "s1", Rule: &Rule{BankID: "b", Stimulus: true, Count: 3}},
		{ID: "c", SectionID: "s2", Rule: &Rule{BankID: "b", Kind: KindEssay, Count: 3}},
	}
	base := Assemble(items, pool, []int64{5})[0].Items
	moved := false
	for seed := int64(0); seed < 200; seed++ {
		got := ShuffleForParticipant(base, seed, true, false)
		if len(got) != len(base) {
			t.Fatal("length changed")
		}
		checkBlocks(t, got, true)
		if got[0].QuestionID != base[0].QuestionID {
			moved = true
		}
		// Bagian tidak tercampur.
		for i, it := range got {
			if it.SectionID != base[i].SectionID {
				t.Fatalf("seed %d mixes sections", seed)
			}
		}
		// Acak di dalam blok: tetap berkelompok.
		checkBlocks(t, ShuffleForParticipant(base, seed, true, true), false)
	}
	if !moved {
		t.Error("shuffle never changed the order")
	}
	if got := ShuffleForParticipant(base, 1, false, false); got[3].QuestionID != base[3].QuestionID {
		t.Error("no-shuffle changed the order")
	}
}

// checkBlocks memastikan soal satu stimulus berdampingan; ordered juga
// memastikan urutannya tetap.
func checkBlocks(t *testing.T, items []VariantItem, ordered bool) {
	t.Helper()
	last := map[string]int{}
	order := map[string]int{}
	for i, it := range items {
		s := it.StimulusID
		if s == "" {
			continue
		}
		if p, ok := last[s]; ok && p != i-1 {
			t.Fatalf("stimulus %s split at %d", s, i)
		}
		last[s] = i
		if ordered && it.Q.StimulusOrder < order[s] {
			t.Fatalf("stimulus %s reordered", s)
		}
		order[s] = it.Q.StimulusOrder
	}
}

func TestAssignVariants(t *testing.T) {
	got := AssignVariants(5, 2)
	if fmt.Sprint(got) != "[0 1 0 1 0]" {
		t.Errorf("assign = %v", got)
	}
}
