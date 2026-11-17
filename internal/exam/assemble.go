package exam

import (
	"math/rand"
	"slices"
	"sort"
	"strings"
)

// Perakitan soal ujian (T133): soal tetap, aturan ambil acak, varian paket
// A/B/C dengan komposisi sama, dan pengacakan per peserta yang menjaga
// kelompok stimulus tetap utuh.

// PoolQuestion adalah data soal yang dibutuhkan perakitan (tanpa isi).
type PoolQuestion struct {
	ID, BankID, StimulusID string
	StimulusOrder          int
	Kind                   Kind
	Difficulty             string
	Cognitive              string
	ObjectiveID            string
	Points                 int
	Status                 string
}

// sig adalah komposisi lengkap soal: jenis, kesulitan, TP, dan level.
func (q PoolQuestion) sig() string {
	return string(q.Kind) + "|" + q.Difficulty + "|" + q.ObjectiveID + "|" + q.Cognitive
}

// looseSig adalah komposisi tanpa level kognitif, dipakai bila padanan
// lengkap tidak ada.
func (q PoolQuestion) looseSig() string {
	return string(q.Kind) + "|" + q.Difficulty + "|" + q.ObjectiveID
}

// Rule adalah aturan ambil acak: Count soal (atau Count kelompok stimulus
// bila Stimulus) dari bank dengan filter yang diisi.
type Rule struct {
	BankID      string
	Kind        Kind
	Difficulty  string
	ObjectiveID string
	Stimulus    bool
	Count       int
}

func (r Rule) matches(q PoolQuestion) bool {
	return (r.Kind == "" || q.Kind == r.Kind) && (r.Difficulty == "" || q.Difficulty == r.Difficulty) &&
		(r.ObjectiveID == "" || q.ObjectiveID == r.ObjectiveID)
}

// AsmItem adalah satu isi bagian ujian: soal tetap atau aturan acak.
type AsmItem struct {
	ID        string
	SectionID string
	Question  *PoolQuestion
	Rule      *Rule
}

// Pool berisi soal yang boleh diambil acak atau dipakai sebagai padanan
// varian, per bank. Hanya soal aktif berstatus siap.
type Pool map[string][]PoolQuestion

// block adalah satu soal tunggal atau satu kelompok stimulus.
type block []PoolQuestion

func (b block) stimulus() string { return b[0].StimulusID }

// blockSig adalah komposisi kelompok: multiset looseSig soalnya.
func (b block) sig() string {
	s := make([]string, len(b))
	for i, q := range b {
		s[i] = q.looseSig()
	}
	sort.Strings(s)
	return strings.Join(s, ";")
}

// singles mengembalikan soal tanpa stimulus di bank.
func (p Pool) singles(bankID string) []PoolQuestion {
	var out []PoolQuestion
	for _, q := range p[bankID] {
		if q.StimulusID == "" {
			out = append(out, q)
		}
	}
	return out
}

// blocks mengembalikan kelompok stimulus di bank, soal berurutan.
func (p Pool) blocks(bankID string) []block {
	idx := map[string]int{}
	var out []block
	for _, q := range p[bankID] {
		if q.StimulusID == "" {
			continue
		}
		i, ok := idx[q.StimulusID]
		if !ok {
			i = len(out)
			idx[q.StimulusID] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], q)
	}
	for _, b := range out {
		sort.SliceStable(b, func(i, j int) bool { return b[i].StimulusOrder < b[j].StimulusOrder })
	}
	return out
}

func (r Rule) blockMatches(b block) bool {
	for _, q := range b {
		if r.matches(q) {
			return true
		}
	}
	return false
}

// Available mengembalikan jumlah soal (atau kelompok stimulus) yang cocok
// dengan aturan r dan belum dipakai (exclude).
func (p Pool) Available(r Rule, exclude map[string]bool) int {
	n := 0
	if r.Stimulus {
		for _, b := range p.blocks(r.BankID) {
			if r.blockMatches(b) && !anyIn(b, exclude) {
				n++
			}
		}
		return n
	}
	for _, q := range p.singles(r.BankID) {
		if r.matches(q) && !exclude[q.ID] {
			n++
		}
	}
	return n
}

func anyIn(b block, set map[string]bool) bool {
	for _, q := range b {
		if set[q.ID] {
			return true
		}
	}
	return false
}

// VariantItem adalah satu soal di satu varian, berurutan per bagian.
type VariantItem struct {
	SectionID  string
	QuestionID string
	StimulusID string
	Points     int
	Q          PoolQuestion
}

// Shortage adalah aturan yang tidak bisa dipenuhi isi bank.
type Shortage struct {
	ItemID    string
	Want, Got int
}

// Assembled adalah hasil rakit satu varian.
type Assembled struct {
	Items []VariantItem
	// Shared: jumlah soal yang sama dengan varian A (tidak ada padanan).
	Shared int
	Short  []Shortage
}

// unit adalah satu hasil ambil di varian A: soal tetap, satu soal acak,
// atau satu kelompok stimulus acak. Varian lain mencari padanannya.
type unit struct {
	section string
	fixed   bool
	rule    *Rule
	b       block
}

// Assemble merakit varian ujian. seeds menentukan banyak varian dan hasil
// acaknya (seed sama + isi bank sama = hasil sama). Varian A diambil acak
// sesuai aturan; varian berikutnya memakai padanan dengan komposisi sama
// (jenis, kesulitan, TP, level) dari bank, dan memakai soal yang sama bila
// padanan tidak ada.
func Assemble(items []AsmItem, pool Pool, seeds []int64) []Assembled {
	if len(seeds) == 0 {
		return nil
	}
	out := make([]Assembled, len(seeds))
	// Soal tetap tidak boleh terambil lagi oleh aturan acak.
	fixed := map[string]bool{}
	for _, it := range items {
		if it.Question != nil {
			fixed[it.Question.ID] = true
		}
	}
	var units []unit
	rng := rand.New(rand.NewSource(seeds[0]))
	used := cloneSet(fixed)
	for _, it := range items {
		switch {
		case it.Question != nil:
			units = append(units, unit{section: it.SectionID, fixed: true, b: block{*it.Question}})
		case it.Rule != nil:
			r := it.Rule
			got := 0
			if r.Stimulus {
				var cands []block
				for _, b := range pool.blocks(r.BankID) {
					if r.blockMatches(b) && !anyIn(b, used) {
						cands = append(cands, b)
					}
				}
				rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
				for _, b := range cands[:min(r.Count, len(cands))] {
					units = append(units, unit{section: it.SectionID, rule: r, b: b})
					markUsed(used, b)
					got++
				}
			} else {
				var cands []PoolQuestion
				for _, q := range pool.singles(r.BankID) {
					if r.matches(q) && !used[q.ID] {
						cands = append(cands, q)
					}
				}
				rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
				for _, q := range cands[:min(r.Count, len(cands))] {
					units = append(units, unit{section: it.SectionID, rule: r, b: block{q}})
					used[q.ID] = true
					got++
				}
			}
			if got < r.Count {
				out[0].Short = append(out[0].Short, Shortage{ItemID: it.ID, Want: r.Count, Got: got})
			}
		}
	}
	for _, u := range units {
		out[0].Items = appendBlock(out[0].Items, u.section, u.b)
	}
	// Varian berikutnya: padanan per unit.
	everUsed := cloneSet(used)
	for v := 1; v < len(seeds); v++ {
		rng := rand.New(rand.NewSource(seeds[v]))
		// Soal tetap tetap dicadangkan agar tidak terambil dua kali bila
		// salah satunya tidak punya padanan.
		mine := cloneSet(fixed)
		out[v].Short = out[0].Short
		for _, u := range units {
			sub := substitute(u, pool, mine, everUsed, rng)
			markUsed(mine, sub)
			markUsed(everUsed, sub)
			if sub[0].ID == u.b[0].ID {
				out[v].Shared += len(sub)
			}
			out[v].Items = appendBlock(out[v].Items, u.section, sub)
		}
	}
	return out
}

// substitute mencari padanan unit u untuk varian lain: kandidat yang belum
// dipakai varian ini (mine), lebih disukai yang belum dipakai varian mana
// pun (everUsed). Tanpa padanan, unit dipakai apa adanya.
func substitute(u unit, pool Pool, mine, everUsed map[string]bool, rng *rand.Rand) block {
	first := u.b[0]
	if u.b.stimulus() != "" {
		if u.fixed {
			// Soal tetap berstimulus: kelompoknya dipilih guru, tidak diganti.
			return u.b
		}
		want := u.b.sig()
		var fresh, reuse []block
		for _, b := range pool.blocks(first.BankID) {
			if b.stimulus() == u.b.stimulus() || anyIn(b, mine) || b.sig() != want || !u.rule.blockMatches(b) {
				continue
			}
			if anyIn(b, everUsed) {
				reuse = append(reuse, b)
			} else {
				fresh = append(fresh, b)
			}
		}
		if b, ok := pick(fresh, reuse, rng); ok {
			return b
		}
		return u.b
	}
	for _, loose := range []bool{false, true} {
		var fresh, reuse []block
		for _, q := range pool.singles(first.BankID) {
			if q.ID == first.ID || mine[q.ID] {
				continue
			}
			if loose && q.looseSig() != first.looseSig() || !loose && q.sig() != first.sig() {
				continue
			}
			if u.rule != nil && !u.rule.matches(q) {
				continue
			}
			if everUsed[q.ID] {
				reuse = append(reuse, block{q})
			} else {
				fresh = append(fresh, block{q})
			}
		}
		if b, ok := pick(fresh, reuse, rng); ok {
			return b
		}
	}
	return u.b
}

func pick(fresh, reuse []block, rng *rand.Rand) (block, bool) {
	if len(fresh) > 0 {
		return fresh[rng.Intn(len(fresh))], true
	}
	if len(reuse) > 0 {
		return reuse[rng.Intn(len(reuse))], true
	}
	return nil, false
}

func appendBlock(items []VariantItem, section string, b block) []VariantItem {
	for _, q := range b {
		items = append(items, VariantItem{SectionID: section, QuestionID: q.ID, StimulusID: q.StimulusID, Points: q.Points, Q: q})
	}
	return items
}

func markUsed(set map[string]bool, b block) {
	for _, q := range b {
		set[q.ID] = true
	}
}

func cloneSet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ShuffleForParticipant mengacak urutan soal satu peserta. Bagian tetap
// berurutan; di dalam bagian, kelompok stimulus diperlakukan sebagai satu
// blok yang diacak di antara soal lain. Soal di dalam blok tetap berurutan
// kecuali inStimulus.
func ShuffleForParticipant(items []VariantItem, seed int64, shuffle, inStimulus bool) []VariantItem {
	if !shuffle && !inStimulus {
		return slices.Clone(items)
	}
	rng := rand.New(rand.NewSource(seed))
	out := make([]VariantItem, 0, len(items))
	for start := 0; start < len(items); {
		end := start
		for end < len(items) && items[end].SectionID == items[start].SectionID {
			end++
		}
		var blocks [][]VariantItem
		for i := start; i < end; {
			j := i + 1
			if s := items[i].StimulusID; s != "" {
				for j < end && items[j].StimulusID == s {
					j++
				}
			}
			blocks = append(blocks, slices.Clone(items[i:j]))
			i = j
		}
		if shuffle {
			rng.Shuffle(len(blocks), func(i, j int) { blocks[i], blocks[j] = blocks[j], blocks[i] })
		}
		for _, b := range blocks {
			if inStimulus && len(b) > 1 && b[0].StimulusID != "" {
				rng.Shuffle(len(b), func(i, j int) { b[i], b[j] = b[j], b[i] })
			}
			out = append(out, b...)
		}
		start = end
	}
	return out
}

// Perkiraan waktu mengerjakan per jenis soal (menit).
var minutesPerKind = map[Kind]float64{
	KindMC: 1, KindMulti: 1.5, KindTF: 1.5, KindShort: 1.5, KindMatch: 2, KindEssay: 5,
}

// Composition adalah ringkasan isi satu varian (panel ringkas wizard).
type Composition struct {
	Count, Points, Essays, Stimuli int
	Minutes                        int
	ByKind                         map[Kind]int
	ByDifficulty                   map[string]int
	ByCognitive                    map[string]int // LOTS/MOTS/HOTS
	ByObjective                    map[string]int // id TP ("" = tanpa TP)
}

// Compose menghitung komposisi item.
func Compose(items []VariantItem) Composition {
	c := Composition{ByKind: map[Kind]int{}, ByDifficulty: map[string]int{}, ByCognitive: map[string]int{}, ByObjective: map[string]int{}}
	stim := map[string]bool{}
	var minutes float64
	for _, it := range items {
		q := it.Q
		c.Count++
		c.Points += it.Points
		c.ByKind[q.Kind]++
		c.ByDifficulty[q.Difficulty]++
		c.ByCognitive[CognitiveGroup(q.Cognitive)]++
		c.ByObjective[q.ObjectiveID]++
		if q.Kind == KindEssay {
			c.Essays++
		}
		if it.StimulusID != "" && !stim[it.StimulusID] {
			stim[it.StimulusID] = true
			c.Stimuli++
			minutes += 2 // membaca teks bersama
		}
		minutes += minutesPerKind[q.Kind]
	}
	c.Minutes = int(minutes + 0.5)
	return c
}

// AssignVariants membagi varian ke n peserta berurutan (nomor/tempat
// duduk) secara bergilir, sehingga peserta bersebelahan mendapat paket
// berbeda. Mengembalikan indeks varian per peserta.
func AssignVariants(n, variants int) []int {
	out := make([]int, n)
	if variants < 1 {
		variants = 1
	}
	for i := range out {
		out[i] = i % variants
	}
	return out
}
