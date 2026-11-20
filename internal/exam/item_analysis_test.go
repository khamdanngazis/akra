package exam

import (
	"math"
	"strings"
	"testing"
)

func TestGroupSize(t *testing.T) {
	for n, want := range map[int]int{0: 0, 1: 0, 2: 1, 3: 1, 4: 1, 7: 2, 10: 3, 37: 10, 600: 162} {
		if got := groupSize(n); got != want {
			t.Errorf("groupSize(%d) = %d, want %d", n, got, want)
		}
	}
}

// mcItem membuat soal PG 4 opsi (a–d) dengan kunci key.
func mcItem(id, key string, points int) GradeItem {
	return GradeItem{Question: Question{ID: id, Kind: KindMC, Options: Options{Choices: choices("a", "b", "c", "d")}},
		Key: AnswerKey{Correct: []string{key}}, Points: points}
}

// responses menyusun jawaban PG: nilai ujian dan opsi dipilih ("" kosong).
func responses(it GradeItem, totals []int, picks []string) []ItemResponse {
	out := make([]ItemResponse, len(totals))
	for i := range totals {
		r := ItemResponse{Total: totals[i] * 100, Blank: picks[i] == ""}
		if picks[i] != "" {
			r.Picked = []string{picks[i]}
			if picks[i] == it.Key.Correct[0] {
				r.Earned = it.Points * PointsUnit
			}
		}
		out[i] = r
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Data contoh dihitung manual (10 peserta, kelompok atas/bawah 27% × 10 =
// 2,7 → 3 orang).
var sampleTotals = []int{95, 90, 85, 80, 70, 65, 60, 50, 40, 30}

func TestAnalyzeItemManualExample(t *testing.T) {
	// Kunci B. Benar: peserta 1, 2, 4, 6 → p = 4/10 = 0,40.
	// Atas (95, 90, 85): B, B, A → 2/3. Bawah (50, 40, 30): C, A, kosong → 0/3.
	// D = 0,667 − 0 = 0,67. Opsi: A 3 (30%), B 4 (40%), C 2 (20%), D 0 (0%)
	// → pengecoh D tidak berfungsi. Kosong 1.
	it := mcItem("q1", "b", 2)
	st := AnalyzeItem(ItemInput{Item: it, Number: 1, Responses: responses(it, sampleTotals,
		[]string{"b", "b", "a", "b", "c", "b", "a", "c", "a", ""})})
	if st.N != 10 || !near(st.P, 0.4) || !st.HasD || st.Group != 3 || !near(st.D, 2.0/3) || st.Blank != 1 {
		t.Fatalf("stat = %+v", st)
	}
	if st.PText() != "0,40" || st.DText() != "0,67" || st.PLabel() != "Sedang" || st.DLabel() != "Sangat baik" || st.Decision() != "Diterima" {
		t.Errorf("labels = %s %s %s %s %s", st.PText(), st.DText(), st.PLabel(), st.DLabel(), st.Decision())
	}
	wantPct := []int{30, 40, 20, 0}
	for i, o := range st.Options {
		if o.Percent != wantPct[i] || o.Key != (i == 1) || o.Weak != (i == 3) {
			t.Errorf("option %s = %+v", o.Letter, o)
		}
	}
	if st.OptionsText() != "A 30% · B* 40% · C 20% · D 0%!" {
		t.Errorf("options text = %q", st.OptionsText())
	}
	if len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "Pengecoh D") || st.NegativeD() || st.Extreme() {
		t.Errorf("warnings = %v", st.Warnings)
	}
}

func TestAnalyzeItemNegativeDiscrimination(t *testing.T) {
	// Kunci tertulis A, padahal kelompok atas memilih C: p = 5/10 = 0,50;
	// atas 0/3, bawah 3/3 → D = −1,00. Pengecoh B dan D 0%.
	it := mcItem("q2", "a", 1)
	st := AnalyzeItem(ItemInput{Item: it, Responses: responses(it, sampleTotals,
		[]string{"c", "c", "c", "c", "a", "c", "a", "a", "a", "a"})})
	if !near(st.P, 0.5) || !near(st.D, -1) || !st.NegativeD() || st.DLabel() != "Negatif" || st.Decision() != "Periksa kunci" {
		t.Fatalf("stat = %+v (%s)", st, st.DLabel())
	}
	if len(st.Warnings) != 2 || !strings.Contains(st.Warnings[0], "periksa kunci") && !strings.Contains(st.Warnings[0], "Periksa kunci") {
		t.Errorf("negative D must be the first warning: %v", st.Warnings)
	}
	if !strings.Contains(st.Warnings[1], "B, D") {
		t.Errorf("weak distractors = %v", st.Warnings)
	}
}

func TestAnalyzeItemExtremesAndPartialCredit(t *testing.T) {
	// Semua benar: p = 1, ditandai.
	it := mcItem("q3", "a", 1)
	st := AnalyzeItem(ItemInput{Item: it, Responses: responses(it, []int{90, 80, 70, 60}, []string{"a", "a", "a", "a"})})
	if !near(st.P, 1) || !st.Extreme() || !strings.Contains(strings.Join(st.Warnings, " "), "p = 1") || st.PLabel() != "Mudah" {
		t.Errorf("p=1 stat = %+v", st)
	}
	// Tidak ada yang benar: p = 0.
	st = AnalyzeItem(ItemInput{Item: it, Responses: responses(it, []int{90, 80, 70, 60}, []string{"b", "c", "d", "b"})})
	if !near(st.P, 0) || !st.Extreme() || !strings.Contains(strings.Join(st.Warnings, " "), "p = 0") || st.PLabel() != "Sukar" {
		t.Errorf("p=0 stat = %+v", st)
	}
	// Esai 4 poin, skor 4, 2, 2, 0: p = 8/16 = 0,50; kelompok 27% × 4 →
	// 1 orang: atas 4/4, bawah 0/4 → D = 1. Tanpa sebaran opsi.
	essay := GradeItem{Question: Question{ID: "q4", Kind: KindEssay}, Points: 4}
	var rs []ItemResponse
	for i, pts := range []int{4, 2, 2, 0} {
		rs = append(rs, ItemResponse{Total: []int{9000, 8000, 6000, 4000}[i], Earned: pts * PointsUnit})
	}
	st = AnalyzeItem(ItemInput{Item: essay, Responses: rs})
	if !near(st.P, 0.5) || !near(st.D, 1) || st.Group != 1 || len(st.Options) != 0 || st.KeyFixable() {
		t.Errorf("essay stat = %+v", st)
	}
	// Satu peserta: tidak ada daya beda. Dibatalkan: tidak dianalisis.
	st = AnalyzeItem(ItemInput{Item: it, Responses: responses(it, []int{90}, []string{"a"})})
	if st.HasD || st.DText() != "—" || st.Decision() != "" {
		t.Errorf("single = %+v", st)
	}
	v := it
	v.Voided = true
	if st = AnalyzeItem(ItemInput{Item: v, Responses: responses(it, []int{90, 10}, []string{"a", "b"})}); st.P != 0 || st.HasD || len(st.Warnings) != 1 {
		t.Errorf("voided = %+v", st)
	}
}

func TestAnalyzeItemMultiDistractors(t *testing.T) {
	// PG kompleks kunci a, c (proporsional dihitung di Earned oleh
	// pemanggil). Opsi d dipilih 1 dari 25 (4%) → tidak berfungsi; b 2/25
	// (8%) tetap.
	it := GradeItem{Question: Question{ID: "q5", Kind: KindMulti, Options: Options{Choices: choices("a", "b", "c", "d")}},
		Key: AnswerKey{Correct: []string{"a", "c"}}, Points: 1}
	var rs []ItemResponse
	for i := 0; i < 25; i++ {
		r := ItemResponse{Total: 10000 - i*100, Picked: []string{"a", "c"}, Earned: PointsUnit}
		switch i {
		case 20:
			r.Picked = []string{"a", "d", "d"} // d dihitung sekali
		case 21, 22:
			r.Picked = []string{"b"}
			r.Earned = 0
		}
		rs = append(rs, r)
	}
	st := AnalyzeItem(ItemInput{Item: it, Responses: rs})
	want := map[string][2]int{"A": {23, 92}, "B": {2, 8}, "C": {22, 88}, "D": {1, 4}}
	for _, o := range st.Options {
		if w := want[o.Letter]; o.Count != w[0] || o.Percent != w[1] || o.Weak != (o.Letter == "D") {
			t.Errorf("option %+v, want %v", o, w)
		}
	}
}

func TestAnalyzeObjectivesWithVariants(t *testing.T) {
	// TP1: q1 (paket A, 2 poin) dan q3 (paket B, 2 poin). TP2: q2 (1 poin,
	// kedua paket). q4 tanpa TP. KKTP 75.
	refs := []ObjectiveRef{{ID: "tp1", Code: "TP1"}, {ID: "tp2", Code: "TP2"}}
	u := PointsUnit
	students := []StudentOutcome{
		// Paket A: TP1 2/2 = 100%, TP2 0/1 = 0%. Nilai 67 → remedial TP2.
		{Key: "a", StudentID: "a", Name: "Ani", ScoreX100: 6700, Items: []ItemOutcome{
			{QuestionID: "q1", ObjectiveID: "tp1", Earned: 2 * u, Points: 2}, {QuestionID: "q2", ObjectiveID: "tp2", Points: 1},
			{QuestionID: "q4", Earned: u, Points: 1}}},
		// Paket B: TP1 hanya dari q3 (1/2 = 50%), bukan dari q1 yang tidak ia
		// terima. TP2 100%. Nilai 80 → pengayaan meski TP1 belum tuntas.
		{Key: "b", StudentID: "b", Name: "Budi", ScoreX100: 8000, Items: []ItemOutcome{
			{QuestionID: "q3", ObjectiveID: "tp1", Earned: u, Points: 2}, {QuestionID: "q2", ObjectiveID: "tp2", Earned: u, Points: 1}}},
		// Paket A: semua tuntas.
		{Key: "c", StudentID: "c", Name: "Citra", ScoreX100: 9000, Items: []ItemOutcome{
			{QuestionID: "q1", ObjectiveID: "tp1", Earned: 2 * u, Points: 2}, {QuestionID: "q2", ObjectiveID: "tp2", Earned: u, Points: 1}}},
		// Nilai 60 tetapi TP yang ia kerjakan tuntas: remedial dengan semua
		// TP-nya (aturan PlanRemedial T137).
		{Key: "d", StudentID: "d", Name: "Dewi", ScoreX100: 6000, Items: []ItemOutcome{
			{QuestionID: "q1", ObjectiveID: "tp1", Earned: 2 * u, Points: 2}}},
		// Tepat di KKTP (3/4 = 75%) dihitung tuntas.
		{Key: "e", StudentID: "e", Name: "Eka", ScoreX100: 7500, Items: []ItemOutcome{
			{QuestionID: "q1", ObjectiveID: "tp1", Earned: u + u/2, Points: 2}}},
	}
	a := AnalyzeObjectives(students, refs, 75)
	if len(a.Objectives) != 2 || a.Untagged != 1 {
		t.Fatalf("objectives = %+v, untagged %d", a.Objectives, a.Untagged)
	}
	// TP1: 5 siswa, tuntas Ani, Citra, Dewi, Eka = 4 → 80%; rata-rata
	// (100+50+100+100+75)/5 = 85. Soal berbeda: q1, q3.
	if o := a.Objectives[0]; o.Code != "TP1" || o.Students != 5 || o.Mastered != 4 || o.Percent != 80 || o.Average != 85 || o.Questions != 2 {
		t.Errorf("TP1 = %+v", o)
	}
	// TP2: 3 siswa (Ani, Budi, Citra), tuntas 2 → 67%; rata-rata 67.
	if o := a.Objectives[1]; o.Students != 3 || o.Mastered != 2 || o.Percent != 67 || o.Average != 67 {
		t.Errorf("TP2 = %+v", o)
	}
	byKey := map[string]StudentObjectives{}
	for _, s := range a.Students {
		byKey[s.Key] = s
	}
	if b := byKey["b"]; b.Cells[0] != (ObjectiveCell{Has: true, Percent: 50}) || !b.Cells[1].Mastered || b.WeakCodes() != "TP1" || !b.Pass {
		t.Errorf("Budi map = %+v", b)
	}
	if d := byKey["d"]; d.Cells[1].Has || !d.Cells[0].Mastered {
		t.Errorf("Dewi map = %+v", d)
	}
	if e := byKey["e"]; !e.Cells[0].Mastered || !e.Pass {
		t.Errorf("Eka at threshold = %+v", e)
	}
	f := a.BuildFollowList()
	if len(f.Remedial) != 2 || len(f.Enrichment) != 3 {
		t.Fatalf("follow = %d remedial, %d enrichment", len(f.Remedial), len(f.Enrichment))
	}
	for _, s := range f.Remedial {
		want := map[string]string{"a": "TP2", "d": "TP1"}[s.Key]
		if s.WeakCodes() != want {
			t.Errorf("remedial %s weak = %q, want %q", s.Name, s.WeakCodes(), want)
		}
	}
	if len(f.Focus) != 2 || f.Focus[0].Code != "TP1" || f.Focus[1].Code != "TP2" {
		t.Errorf("focus = %+v", f.Focus)
	}
}

func TestSummarizeResults(t *testing.T) {
	sc := func(v int) *int { return &v }
	rows := []ParticipantResult{
		{Status: AttemptSubmitted, ScoreStat: ScoreFinal, ScoreX100: sc(10000)},
		{Status: AttemptSubmitted, ScoreStat: ScoreFinal, ScoreX100: sc(7500)},
		{Status: AttemptSubmitted, ScoreStat: ScoreFinal, ScoreX100: sc(7499)},
		{Status: AttemptSubmitted, ScoreStat: ScoreFinal, ScoreX100: sc(950)},
		{Status: AttemptSubmitted, ScoreStat: ScoreAwaitEssay, ScoreX100: sc(4000)},
		{Status: AttemptRunning},
		{},
	}
	s := SummarizeResults(rows, 75)
	// Rata-rata (100 + 75 + 74,99 + 9,5) / 4 = 64,8725 → 64,87.
	if s.Registered != 7 || s.Present != 6 || s.Absent != 1 || s.Working != 1 || s.Awaiting != 1 || s.Final != 4 ||
		s.MeanText() != "64,87" || s.MaxText() != "100,00" || s.MinText() != "9,50" || s.Pass != 2 || s.Fail != 2 || s.PassPercent() != 50 {
		t.Fatalf("summary = %+v", s)
	}
	counts := map[string]int{}
	for _, b := range s.Bins {
		counts[b.Label] = b.Count
	}
	if counts["0–9"] != 1 || counts["70–79"] != 2 || counts["90–100"] != 1 || s.Bins[7].Width != 100 || s.Bins[0].Width != 50 {
		t.Errorf("bins = %+v", s.Bins)
	}
}
