package clock

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDateInJayapura(t *testing.T) {
	cases := map[string]string{
		// 15:00 UTC = 00:00 WIT keesokan harinya, padahal masih 22:00 WIB.
		"2026-10-17T15:00:00Z": "2026-10-18",
		"2026-10-17T14:59:59Z": "2026-10-17",
	}
	for in, want := range cases {
		ts, _ := time.Parse(time.RFC3339, in)
		if got := DateIn(ts, Jayapura).Format(time.DateOnly); got != want {
			t.Errorf("DateIn(%s, WIT) = %s, want %s", in, got, want)
		}
		if got := DateIn(ts, Jakarta).Format(time.DateOnly); got != "2026-10-17" {
			t.Errorf("DateIn(%s, WIB) = %s, want 2026-10-17", in, got)
		}
	}
}

// Jadwal 08.00 WIT adalah 06.00 WIB di jam server.
func TestParseScheduleInSchoolZone(t *testing.T) {
	at, err := time.ParseInLocation("2006-01-02T15:04", "2027-02-10T08:00", Jayapura)
	if err != nil {
		t.Fatal(err)
	}
	if got := at.In(Jakarta).Format("15:04"); got != "06:00" {
		t.Errorf("08:00 WIT in WIB = %s, want 06:00", got)
	}
	if got := at.UTC().Format(time.RFC3339); got != "2027-02-09T23:00:00Z" {
		t.Errorf("08:00 WIT in UTC = %s", got)
	}
	if got := FormatDateTimeID(at, Jayapura); got != "10 Februari 2027 08.00 WIT" {
		t.Errorf("FormatDateTimeID = %q", got)
	}
}

func TestLocationAndLabel(t *testing.T) {
	cases := []struct{ name, label string }{
		{"Asia/Jakarta", "WIB"}, {"Asia/Makassar", "WITA"}, {"Asia/Jayapura", "WIT"},
		{"", "WIB"}, {"Europe/London", "WIB"},
	}
	for _, c := range cases {
		if got := ZoneLabel(Location(c.name)); got != c.label {
			t.Errorf("ZoneLabel(Location(%q)) = %s, want %s", c.name, got, c.label)
		}
	}
	if ValidZone("Europe/London") || !ValidZone("Asia/Makassar") {
		t.Error("ValidZone")
	}
}

// Modul ujian wajib memakai zona waktu sekolah; clock.Jakarta tidak boleh
// dipakai di internal/exam (T126).
func TestNoJakartaInExam(t *testing.T) {
	root := filepath.Join("..", "..", "exam")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, banned := range []string{"clock.Jakarta", "clock.Today()", "clock.DateOf("} {
			if strings.Contains(string(b), banned) {
				t.Errorf("%s uses %s; use the school location (auth.Identity.Location)", path, banned)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
