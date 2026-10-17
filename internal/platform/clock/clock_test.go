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
