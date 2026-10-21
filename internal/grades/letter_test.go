package grades

import (
	"context"
	"errors"
	"testing"

	"akra/internal/platform/settings"
)

func TestLetter_DefaultScale(t *testing.T) {
	scale := DefaultScale()
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

func TestParseScale(t *testing.T) {
	sc, err := ParseScale(" A:85, B:75 ,C:65,D:0")
	if err != nil || sc.String() != "A:85,B:75,C:65,D:0" {
		t.Fatalf("parse: %v %s", err, sc)
	}
	if got, _ := Letter(85, sc); got != "A" {
		t.Errorf("custom scale 85 = %s", got)
	}
	if got, _ := Letter(84, sc); got != "B" {
		t.Errorf("custom scale 84 = %s", got)
	}
	if round, err := ParseScale(DefaultScale().String()); err != nil || round.String() != "A:90,B:80,C:70,D:0" {
		t.Errorf("round trip: %v %s", err, round)
	}
	for _, bad := range []string{"", "A:90,B:80", "A:80,B:90,C:0", "A:90,A:80,D:0", "A:90,B:90,D:0", "A:101,D:0", "A90,D:0", "A:x,D:0", ":90,D:0", "ABCD:90,D:0", "A:90,D:-1"} {
		if _, err := ParseScale(bad); !errors.Is(err, ErrInvalidScale) {
			t.Errorf("ParseScale(%q) = %v", bad, err)
		}
	}
}

func TestLoadScale(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if sc, err := LoadScale(ctx, f.tx, f.schoolA); err != nil || sc.String() != DefaultScale().String() {
		t.Errorf("default: %v %s", err, sc)
	}
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.GradeLetterScale, "A:85,B:75,C:65,D:0"); err != nil {
		t.Fatal(err)
	}
	if sc, err := LoadScale(ctx, f.tx, f.schoolA); err != nil || sc[0].Min != 85 {
		t.Errorf("stored: %v %s", err, sc)
	}
	// Skala sekolah lain tidak terpengaruh.
	if sc, _ := LoadScale(ctx, f.tx, f.schoolB); sc[0].Min != 90 {
		t.Errorf("other school: %s", sc)
	}
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.GradeLetterScale, "A:85,B:75"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadScale(ctx, f.tx, f.schoolA); !errors.Is(err, ErrInvalidScale) {
		t.Errorf("broken stored scale: %v", err)
	}
}
