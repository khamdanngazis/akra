package clock

import (
	"testing"
	"time"
)

func TestDateOf(t *testing.T) {
	cases := map[string]string{
		// 17:30 UTC = 00:30 WIB keesokan harinya.
		"2026-10-17T17:30:00Z": "2026-10-18",
		"2026-10-17T16:59:59Z": "2026-10-17",
		"2026-12-31T20:00:00Z": "2027-01-01",
	}
	for in, want := range cases {
		ts, _ := time.Parse(time.RFC3339, in)
		got := DateOf(ts)
		if got.Format("2006-01-02") != want || got.Location() != time.UTC || got.Hour() != 0 {
			t.Errorf("DateOf(%s) = %v, want %s at 00:00 UTC", in, got, want)
		}
	}
}

func TestFormatDateID(t *testing.T) {
	if got := FormatDateID(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); got != "1 Juli 2026" {
		t.Errorf("FormatDateID = %q", got)
	}
	if got := FormatDateID(time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC)); got != "31 Desember 2027" {
		t.Errorf("FormatDateID = %q", got)
	}
}
