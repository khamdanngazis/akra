package grades

import (
	"errors"
	"math/big"
	"math/rand"
	"testing"
)

func sc(v Score) *Score { return &v }

// pts mengubah nilai bulat (mis. 80) ke Score perseratus.
func pts(n int) *Score { return sc(Score(n * 100)) }

func TestFinalScore_Cases(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    []ComponentScore
		want  int
		exact Score
	}{
		{"30/30/40 with 80/90/70", []ComponentScore{{30, pts(80)}, {30, pts(90)}, {40, pts(70)}}, 79, 7900},
		{"half rounds up: 78,50", []ComponentScore{{50, pts(78)}, {50, pts(79)}}, 79, 7850},
		{"below half rounds down: 78,49", []ComponentScore{{100, sc(7849)}}, 78, 7849},
		{"exactly half from decimals", []ComponentScore{{100, sc(8450)}}, 85, 8450},
		{"weighted 82,45 rounds down", []ComponentScore{{30, sc(8150)}, {70, sc(8285)}}, 82, 8245},
		{"all 100", []ComponentScore{{25, pts(100)}, {75, pts(100)}}, 100, 10000},
		{"all 0 is a real zero", []ComponentScore{{60, pts(0)}, {40, pts(0)}}, 0, 0},
		{"33/33/34", []ComponentScore{{33, pts(70)}, {33, pts(80)}, {34, pts(91)}}, 80, 8044},
	} {
		got, err := FinalScore(tc.in)
		if err != nil || got.Incomplete || got.Score != tc.want || got.Exact != tc.exact {
			t.Errorf("%s: %+v, %v; want %d (exact %d)", tc.name, got, err, tc.want, tc.exact)
		}
	}
}

func TestFinalScore_Incomplete(t *testing.T) {
	for _, in := range [][]ComponentScore{
		{{30, pts(80)}, {30, nil}, {40, pts(70)}},
		{{30, nil}, {30, nil}, {40, nil}},
		{{100, nil}},
		{{50, pts(0)}, {50, nil}}, // nol tetap dihitung dinilai; yang kosong tidak
	} {
		got, err := FinalScore(in)
		missing := 0
		for _, c := range in {
			if c.Score == nil {
				missing++
			}
		}
		// Tidak ada angka parsial sama sekali.
		if err != nil || !got.Incomplete || got.Missing != missing || got.Score != 0 || got.Exact != 0 {
			t.Errorf("%v: %+v, %v", in, got, err)
		}
	}
}

func TestFinalScore_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []ComponentScore
		want error
	}{
		{"total 90", []ComponentScore{{30, pts(80)}, {60, pts(80)}}, ErrWeightTotal},
		{"total 110", []ComponentScore{{50, pts(80)}, {60, pts(80)}}, ErrWeightTotal},
		{"total 90 with empty score", []ComponentScore{{30, nil}, {60, pts(80)}}, ErrWeightTotal},
		{"no components", nil, ErrWeightTotal},
		{"weight 0", []ComponentScore{{0, pts(80)}, {100, pts(80)}}, ErrInvalidWeight},
		{"weight 101", []ComponentScore{{101, pts(80)}}, ErrInvalidWeight},
		{"score above 100", []ComponentScore{{100, sc(10001)}}, ErrInvalidScore},
		{"negative score", []ComponentScore{{100, sc(-1)}}, ErrInvalidScore},
	} {
		if _, err := FinalScore(tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// TestFinalScore_Deterministic: input yang sama, 1000 kali, hasil identik.
func TestFinalScore_Deterministic(t *testing.T) {
	in := []ComponentScore{{20, sc(8725)}, {30, sc(9150)}, {50, sc(6633)}}
	first, err := FinalScore(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 1000 {
		if got, err := FinalScore(in); err != nil || got != first {
			t.Fatalf("run %d: %+v, %v; first %+v", i, got, err, first)
		}
	}
}

// TestRoundFinal_MatchesExactArithmetic membandingkan pembulatan dengan
// perhitungan rasional eksak (math/big, hanya di uji) untuk input acak.
func TestRoundFinal_MatchesExactArithmetic(t *testing.T) {
	r := rand.New(rand.NewSource(71))
	half := big.NewRat(1, 2)
	for i := range 20000 {
		n := 1 + r.Intn(5)
		weights := make([]int, n)
		left := 100
		for j := range n - 1 {
			w := 1 + r.Intn(left-(n-1-j))
			weights[j], left = w, left-w
		}
		weights[n-1] = left
		in := make([]ComponentScore, n)
		exact := new(big.Rat)
		for j, w := range weights {
			s := Score(r.Intn(int(MaxScore) + 1))
			in[j] = ComponentScore{w, sc(s)}
			exact.Add(exact, new(big.Rat).SetFrac64(int64(s)*int64(w), 10000))
		}
		got, err := FinalScore(in)
		if err != nil {
			t.Fatal(err)
		}
		// floor(exact + 1/2)
		v := new(big.Rat).Add(exact, half)
		want := new(big.Int).Quo(v.Num(), v.Denom()).Int64()
		if int64(got.Score) != want {
			t.Fatalf("case %d %v: got %d, want %d (exact %s)", i, in, got.Score, want, exact.FloatString(4))
		}
	}
}

// TestRoundFinal_Boundaries memeriksa setiap perseratus di sekitar batas
// pembulatan.
func TestRoundFinal_Boundaries(t *testing.T) {
	for whole := range 100 {
		for frac := range 100 {
			s := Score(whole*100 + frac)
			got, _ := FinalScore([]ComponentScore{{100, sc(s)}})
			want := whole
			if frac >= 50 {
				want++
			}
			if got.Score != want {
				t.Fatalf("%s: got %d, want %d", s, got.Score, want)
			}
		}
	}
}
