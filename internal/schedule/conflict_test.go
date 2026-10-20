package schedule

import "testing"

func TestParseTime(t *testing.T) {
	for in, want := range map[string]Minutes{"07:00": 420, "7:05": 425, "07.30": 450, " 13:45 ": 825, "00:00": 0, "23:59": 1439} {
		if got, ok := ParseTime(in); !ok || got != want {
			t.Errorf("ParseTime(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "7", "24:00", "07:60", "07:5", "aa:bb", "-1:00", "007:00"} {
		if _, ok := ParseTime(in); ok {
			t.Errorf("ParseTime(%q) accepted", in)
		}
	}
	if m := Minutes(450); m.String() != "07.30" || m.Input() != "07:30" {
		t.Errorf("format: %s %s", m.String(), m.Input())
	}
}

func TestOverlaps(t *testing.T) {
	at := func(s string) Minutes { m, _ := ParseTime(s); return m }
	for _, tc := range []struct {
		a1, a2, b1, b2 string
		want           bool
	}{
		{"07:00", "08:00", "07:30", "08:30", true},  // sebagian
		{"07:00", "08:00", "07:15", "07:45", true},  // di dalam
		{"07:15", "07:45", "07:00", "08:00", true},  // melingkupi
		{"07:00", "08:00", "07:00", "08:00", true},  // sama persis
		{"07:00", "08:00", "08:00", "09:00", false}, // bersebelahan
		{"08:00", "09:00", "07:00", "08:00", false}, // bersebelahan, terbalik
		{"07:00", "07:40", "09:00", "09:40", false}, // terpisah
	} {
		if got := Overlaps(at(tc.a1), at(tc.a2), at(tc.b1), at(tc.b2)); got != tc.want {
			t.Errorf("Overlaps(%s-%s, %s-%s) = %v", tc.a1, tc.a2, tc.b1, tc.b2, got)
		}
	}
}

func TestDayName(t *testing.T) {
	if DayName(1) != "Senin" || DayName(6) != "Sabtu" || DayName(0) != "" || DayName(7) != "" {
		t.Error("day names")
	}
	week := GroupByDay([]Slot{{Day: 1, ID: "a"}, {Day: 3, ID: "b"}, {Day: 1, ID: "c"}})
	if len(week) != 6 || len(week[0].Slots) != 2 || week[0].Slots[1].ID != "c" || len(week[2].Slots) != 1 || len(week[1].Slots) != 0 {
		t.Errorf("GroupByDay: %+v", week)
	}
}
