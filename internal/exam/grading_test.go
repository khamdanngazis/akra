package exam

import (
	"encoding/json"
	"math"
	"testing"
)

func choices(ids ...string) []Choice {
	out := make([]Choice, len(ids))
	for i, id := range ids {
		out[i] = Choice{ID: id}
	}
	return out
}

func TestScoreAnswer(t *testing.T) {
	mc := Question{Kind: KindMC, Options: Options{Choices: choices("a", "b", "c", "d")}}
	multi := Question{Kind: KindMulti, Options: Options{Choices: choices("a", "b", "c", "d")}}
	tf := Question{Kind: KindTF, Options: Options{Statements: choices("s1", "s2", "s3", "s4")}}
	tfKey := AnswerKey{Truth: map[string]bool{"s1": true, "s2": false, "s3": true, "s4": false}}
	shortText := Question{Kind: KindShort, Options: Options{Input: "teks"}}
	shortNum := Question{Kind: KindShort, Options: Options{Input: "angka"}}
	match := Question{Kind: KindMatch, Options: Options{Left: choices("l1", "l2", "l3"), Right: choices("r1", "r2", "r3", "r4")}}
	matchKey := AnswerKey{Pairs: map[string]string{"l1": "r1", "l2": "r2", "l3": "r3"}}
	essay := Question{Kind: KindEssay}
	all := AnswerKey{Correct: []string{"a", "c"}, Scoring: ScoringAll}
	prop := AnswerKey{Correct: []string{"a", "c"}, Scoring: ScoringProportional}

	cases := []struct {
		name   string
		q      Question
		key    AnswerKey
		answer string
		want   float64
		manual bool
	}{
		// Pilihan ganda.
		{"pg benar", mc, AnswerKey{Correct: []string{"b"}}, `"b"`, 1, false},
		{"pg salah", mc, AnswerKey{Correct: []string{"b"}}, `"a"`, 0, false},
		{"pg kosong null", mc, AnswerKey{Correct: []string{"b"}}, `null`, 0, false},
		{"pg string kosong", mc, AnswerKey{Correct: []string{"b"}}, `""`, 0, false},
		{"pg berlebih (array)", mc, AnswerKey{Correct: []string{"b"}}, `["b","a"]`, 0, false},
		{"pg format salah", mc, AnswerKey{Correct: []string{"b"}}, `{"x":1}`, 0, false},
		{"pg tanpa kunci", mc, AnswerKey{}, `"b"`, 0, false},
		// PG kompleks semua benar.
		{"kompleks semua tepat", multi, all, `["c","a"]`, 1, false},
		{"kompleks semua kurang", multi, all, `["a"]`, 0, false},
		{"kompleks semua berlebih", multi, all, `["a","c","d"]`, 0, false},
		{"kompleks semua duplikat", multi, all, `["a","a","c"]`, 1, false},
		{"kompleks kosong", multi, all, `[]`, 0, false},
		// PG kompleks proporsional (tanpa negatif).
		{"proporsional tepat", multi, prop, `["a","c"]`, 1, false},
		{"proporsional setengah", multi, prop, `["a"]`, 0.5, false},
		{"proporsional satu benar satu salah", multi, prop, `["a","b"]`, 0, false},
		{"proporsional semua dipilih", multi, prop, `["a","b","c","d"]`, 0, false},
		{"proporsional lebih banyak salah", multi, prop, `["b","d"]`, 0, false},
		{"proporsional id tidak ada", multi, prop, `["a","c","zz"]`, 0.5, false},
		{"proporsional format salah", multi, prop, `"a"`, 0, false},
		// Benar/salah proporsional.
		{"bs semua benar", tf, tfKey, `{"s1":true,"s2":false,"s3":true,"s4":false}`, 1, false},
		{"bs tiga dari empat", tf, tfKey, `{"s1":true,"s2":false,"s3":true,"s4":true}`, 0.75, false},
		{"bs sebagian dijawab", tf, tfKey, `{"s1":true}`, 0.25, false},
		{"bs pernyataan asing diabaikan", tf, tfKey, `{"s1":true,"zz":true}`, 0.25, false},
		{"bs kosong", tf, tfKey, `{}`, 0, false},
		{"bs format salah", tf, tfKey, `["s1"]`, 0, false},
		// Isian teks.
		{"isian teks sama", shortText, AnswerKey{Accepted: []string{"Fotosintesis"}}, `"fotosintesis"`, 1, false},
		{"isian teks spasi dan tanda baca", shortText, AnswerKey{Accepted: []string{"ibu kota"}}, `"  Ibu   kota. "`, 1, false},
		{"isian teks salah", shortText, AnswerKey{Accepted: []string{"ibu kota"}}, `"ibukota negara"`, 0, false},
		{"isian teks alternatif", shortText, AnswerKey{Accepted: []string{"H2O", "air"}}, `"Air"`, 1, false},
		{"isian kosong", shortText, AnswerKey{Accepted: []string{"x"}}, `"   "`, 0, false},
		{"isian arab harakat", shortText, AnswerKey{Accepted: []string{"كِتَابٌ"}, ArabicNormalize: true}, `"كتاب"`, 1, false},
		// Isian angka: 0,5 dan 0.5.
		{"angka koma", shortNum, AnswerKey{Accepted: []string{"0.5"}}, `"0,5"`, 1, false},
		{"angka titik", shortNum, AnswerKey{Accepted: []string{"0,5"}}, `"0.5"`, 1, false},
		{"angka toleransi", shortNum, AnswerKey{Accepted: []string{"3.14"}, Tolerance: 0.01}, `"3,15"`, 1, false},
		{"angka di luar toleransi", shortNum, AnswerKey{Accepted: []string{"3.14"}, Tolerance: 0.001}, `"3,15"`, 0, false},
		{"angka ribuan ambigu", shortNum, AnswerKey{Accepted: []string{"1000"}}, `"1.000,0"`, 0, false},
		{"angka bukan angka", shortNum, AnswerKey{Accepted: []string{"5"}}, `"lima"`, 0, false},
		// Menjodohkan proporsional.
		{"jodoh semua benar", match, matchKey, `{"l1":"r1","l2":"r2","l3":"r3"}`, 1, false},
		{"jodoh dua dari tiga", match, matchKey, `{"l1":"r1","l2":"r2","l3":"r4"}`, 2.0 / 3, false},
		{"jodoh kanan dipakai dua kali", match, matchKey, `{"l1":"r1","l2":"r1","l3":"r1"}`, 1.0 / 3, false},
		{"jodoh sebagian", match, matchKey, `{"l3":"r3"}`, 1.0 / 3, false},
		{"jodoh format salah", match, matchKey, `["r1"]`, 0, false},
		// Esai.
		{"esai berisi manual", essay, AnswerKey{}, `"jawaban panjang"`, 0, true},
		{"esai kosong bernilai 0", essay, AnswerKey{}, `"  "`, 0, false},
		{"esai null", essay, AnswerKey{}, `null`, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, manual := ScoreAnswer(c.q, c.key, json.RawMessage(c.answer))
			if math.Abs(got-c.want) > 1e-9 || manual != c.manual {
				t.Errorf("ScoreAnswer(%s) = %v manual %v, want %v manual %v", c.answer, got, manual, c.want, c.manual)
			}
		})
	}
}

func TestGradeAnswers(t *testing.T) {
	q := func(id string, k Kind) Question {
		return Question{ID: id, Kind: k, Options: Options{Choices: choices("a", "b"), Statements: choices("s1", "s2", "s3"), Input: "teks"}}
	}
	items := []GradeItem{
		{Question: q("q1", KindMC), Key: AnswerKey{Correct: []string{"a"}}, Points: 2},
		{Question: q("q2", KindTF), Key: AnswerKey{Truth: map[string]bool{"s1": true, "s2": true, "s3": true}}, Points: 1},
		{Question: q("q3", KindShort), Key: AnswerKey{Accepted: []string{"x"}}, Points: 3},
		{Question: q("q4", KindEssay), Points: 4},
		{Question: q("q5", KindMC), Key: AnswerKey{Correct: []string{"a"}}, Points: 5, Voided: true},
	}
	answers := map[string]json.RawMessage{
		"q1": json.RawMessage(`"a"`),
		"q2": json.RawMessage(`{"s1":true,"s2":true,"s3":false}`), // 2/3 dari 1 poin
		"q4": json.RawMessage(`"esai"`),
		"q5": json.RawMessage(`"a"`),
	}
	g := GradeAnswers(items, answers, nil)
	// Total 2+1+3+4 = 10 (q5 dibatalkan). Diperoleh 2 + 0,6667 = 2,6667.
	if g.Max != 10 || g.Earned != 26667 || g.Status != ScoreAwaitEssay || g.ScoreX100 != 2667 {
		t.Fatalf("grade = %+v", g)
	}
	if len(g.Answers) != 4 || g.Answers[2].Points != nil {
		t.Errorf("answers = %+v (essay must be pending)", g.Answers)
	}
	// Esai dikoreksi 3 poin: final 5,6667 / 10.
	g2 := GradeAnswers(items, answers, map[string]int{"q4": 30000})
	if g2.Status != ScoreFinal || g2.ScoreX100 != 5667 {
		t.Errorf("after essay = %+v", g2)
	}
	// Idempoten.
	if g3 := GradeAnswers(items, answers, map[string]int{"q4": 30000}); g3.ScoreX100 != g2.ScoreX100 || g3.Earned != g2.Earned {
		t.Errorf("not idempotent: %+v vs %+v", g3, g2)
	}
	// Esai kosong tidak menahan status; poin esai melebihi maksimum dipotong.
	g4 := GradeAnswers(items, map[string]json.RawMessage{"q1": json.RawMessage(`"a"`), "q4": json.RawMessage(`""`)}, nil)
	if g4.Status != ScoreFinal || g4.ScoreX100 != 2000 {
		t.Errorf("empty essay = %+v", g4)
	}
	if g5 := GradeAnswers(items, answers, map[string]int{"q4": 990000}); g5.Earned != 66667 {
		t.Errorf("essay points capped = %+v", g5)
	}
	if ScoreLabel(8750) != "87,50" || ScoreLabel(10000) != "100,00" || ScoreLabel(5) != "0,05" {
		t.Errorf("score labels")
	}
}

func TestValidateKey(t *testing.T) {
	mc := Question{Kind: KindMC, Options: Options{Choices: choices("a", "b")}}
	tf := Question{Kind: KindTF, Options: Options{Statements: choices("s1", "s2")}}
	match := Question{Kind: KindMatch, Options: Options{Left: choices("l1", "l2"), Right: choices("r1", "r2", "r3")}}
	num := Question{Kind: KindShort, Options: Options{Input: "angka"}}
	for _, c := range []struct {
		q    Question
		k    AnswerKey
		want error
	}{
		{mc, AnswerKey{Correct: []string{"b"}}, nil},
		{mc, AnswerKey{Correct: []string{"z"}}, ErrBadKey},
		{mc, AnswerKey{Correct: []string{"a", "b"}}, ErrBadKey},
		{Question{Kind: KindMulti, Options: mc.Options}, AnswerKey{Correct: []string{"a", "b"}, Scoring: ScoringAll}, nil},
		{Question{Kind: KindMulti, Options: mc.Options}, AnswerKey{Correct: []string{"a"}}, ErrBadKey},
		{tf, AnswerKey{Truth: map[string]bool{"s1": true, "s2": false}}, nil},
		{tf, AnswerKey{Truth: map[string]bool{"s1": true}}, ErrBadKey},
		{match, AnswerKey{Pairs: map[string]string{"l1": "r1", "l2": "r3"}}, nil},
		{match, AnswerKey{Pairs: map[string]string{"l1": "r1", "l2": "r1"}}, ErrBadKey},
		{num, AnswerKey{Accepted: []string{"0,5"}}, nil},
		{num, AnswerKey{Accepted: []string{"setengah"}}, ErrBadKey},
		{num, AnswerKey{Accepted: []string{" "}}, ErrBadKey},
		{Question{Kind: KindEssay}, AnswerKey{}, ErrNotObjective},
	} {
		if err := ValidateKey(c.q, c.k); err != c.want {
			t.Errorf("ValidateKey(%s, %+v) = %v, want %v", c.q.Kind, c.k, err, c.want)
		}
	}
}
