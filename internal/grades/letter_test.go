package grades

import (
	"context"
	"errors"
	"testing"

	"akra/internal/platform/settings"
)

func TestLetter_DefaultScale(t *testing.T) {
	scale := DefaultScale()
	if _, err := ValidateScale(scale); err != nil {
		t.Fatalf("default scale invalid: %v", err)
	}
	for score, want := range map[int]string{100: "A", 90: "A", 89: "B", 80: "B", 79: "C", 70: "C", 69: "D", 1: "D", 0: "D"} {
		if got, err := Letter(score, scale); err != nil || got != want {
			t.Errorf("Letter(%d) = %q, %v; want %q", score, got, err, want)
		}
	}
	for _, bad := range []int{-1, 101} {
		if _, err := Letter(bad, scale); !errors.Is(err, ErrInvalidScore) {
			t.Errorf("Letter(%d): %v", bad, err)
		}
	}
}

func TestValidateScale(t *testing.T) {
	// Urutan masukan bebas; hasil diurutkan dari rentang tertinggi.
	sc, err := ValidateScale(Scale{{"Cukup", 0, 74}, {"Sangat Baik", 90, 100}, {"Baik", 75, 89}})
	if err != nil || sc[0].Letter != "Sangat Baik" || sc[2].Letter != "Cukup" {
		t.Fatalf("valid scale: %v %+v", err, sc)
	}
	if got, _ := Letter(75, sc); got != "Baik" {
		t.Errorf("Letter(75) = %q", got)
	}
	if got, _ := Letter(74, sc); got != "Cukup" {
		t.Errorf("Letter(74) = %q", got)
	}
	for _, tc := range []struct {
		name string
		sc   Scale
		want error
	}{
		{"gap 80-84", Scale{{"A", 85, 100}, {"B", 70, 79}, {"C", 0, 69}}, ErrScaleGap},
		{"overlap 80", Scale{{"A", 80, 100}, {"B", 70, 80}, {"C", 0, 69}}, ErrScaleOverlap},
		{"overlap nested", Scale{{"A", 0, 100}, {"B", 50, 60}}, ErrScaleOverlap},
		{"missing 0", Scale{{"A", 90, 100}, {"B", 10, 89}}, ErrScaleCoverage},
		{"missing 100", Scale{{"A", 90, 99}, {"B", 0, 89}}, ErrScaleCoverage},
		{"empty", nil, ErrScaleCoverage},
		{"min above max", Scale{{"A", 100, 90}, {"B", 0, 89}}, ErrInvalidScale},
		{"above 100", Scale{{"A", 90, 101}, {"B", 0, 89}}, ErrInvalidScale},
		{"duplicate letter", Scale{{"A", 90, 100}, {"a", 0, 89}}, ErrInvalidScale},
		{"blank letter", Scale{{" ", 90, 100}, {"B", 0, 89}}, ErrInvalidScale},
		{"letter too long", Scale{{"Sangat Sangat Baik Sekali", 90, 100}, {"B", 0, 89}}, ErrInvalidScale},
	} {
		if _, err := ValidateScale(tc.sc); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// Setiap nilai 0–100 punya tepat satu huruf di skala yang valid.
	for score := 0; score <= 100; score++ {
		n := 0
		for _, b := range sc {
			if score >= b.Min && score <= b.Max {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("score %d is in %d bands", score, n)
		}
	}
}

func TestParseScale(t *testing.T) {
	sc, err := ParseScale(`[{"letter":"A","min":85,"max":100},{"letter":"B","min":0,"max":84}]`)
	if err != nil || len(sc) != 2 {
		t.Fatalf("parse: %v %+v", err, sc)
	}
	if round, err := ParseScale(DefaultScale().JSON()); err != nil || round.JSON() != DefaultScale().JSON() {
		t.Errorf("round trip: %v %s", err, round.JSON())
	}
	for _, bad := range []string{"", "A:90,D:0", `{"letter":"A"}`, `[{"letter":"A","min":90,"max":100,"x":1}]`, `[{"letter":"A","min":"90","max":100}]`,
		`[{"letter":"A","min":90,"max":100}]`} {
		if _, err := ParseScale(bad); err == nil {
			t.Errorf("ParseScale(%q) accepted", bad)
		}
	}
}

func TestLoadScale(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if sc, err := LoadScale(ctx, f.tx, f.schoolA); err != nil || sc.JSON() != DefaultScale().JSON() {
		t.Errorf("default: %v %s", err, sc.JSON())
	}
	custom := Scale{{"A", 85, 100}, {"B", 75, 84}, {"C", 0, 74}}
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.ReportScale, custom.JSON()); err != nil {
		t.Fatal(err)
	}
	if sc, err := LoadScale(ctx, f.tx, f.schoolA); err != nil || sc[0].Min != 85 {
		t.Errorf("stored: %v %+v", err, sc)
	}
	// Skala sekolah lain tidak terpengaruh.
	if sc, _ := LoadScale(ctx, f.tx, f.schoolB); sc[0].Min != 90 {
		t.Errorf("other school: %+v", sc)
	}
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.ReportScale, `[{"letter":"A","min":85,"max":100}]`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadScale(ctx, f.tx, f.schoolA); !errors.Is(err, ErrScaleCoverage) {
		t.Errorf("broken stored scale: %v", err)
	}
}
