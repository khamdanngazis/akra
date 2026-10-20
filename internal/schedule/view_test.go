package schedule

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTodayTab(t *testing.T) {
	for in, want := range map[string]int{
		"2026-10-19T08:00:00+07:00": 1, // Senin
		"2026-10-24T08:00:00+07:00": 6, // Sabtu
		"2026-10-25T08:00:00+07:00": 1, // Minggu dibuka sebagai Senin
		"2026-10-20T18:30:00Z":      3, // Selasa 18.30 UTC = Rabu 01.30 WIB
	} {
		ts, _ := time.Parse(time.RFC3339, in)
		if got := TodayTab(ts); got != want {
			t.Errorf("TodayTab(%s) = %d, want %d", in, got, want)
		}
	}
}

// seedWeek membuat jadwal contoh: 1A Senin dan Rabu, 1B Senin (guru1 di
// keduanya), guru2 mengajar IPA di 1A.
func seedWeek(t *testing.T, f *fixture) {
	t.Helper()
	for _, s := range []struct {
		class string
		f     Form
	}{
		{f.class1A, form(f.mtk, "3", "09:00", "09:40")},
		{f.class1A, form(f.mtk, "1", "07:00", "07:40")},
		{f.class1A, form(f.ipa, "1", "07:40", "08:20")},
		{f.class1B, form(f.mtk, "1", "08:20", "09:00")},
	} {
		f.mustCreate(t, s.class, s.f)
	}
}

func times(d DayView) string {
	var s []string
	for _, sl := range d.Slots {
		s = append(s, sl.Start.String()+" "+sl.SubjectName+" "+sl.ClassName)
	}
	return strings.Join(s, ", ")
}

func TestClassAndTeacherWeek(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	seedWeek(t, f)
	week, err := ClassWeek(ctx, f.tx, f.schoolA, f.class1A)
	if err != nil || len(week) != 6 {
		t.Fatalf("class week: %v %d", err, len(week))
	}
	if got := times(week[0]); got != "07.00 Matematika 1A, 07.40 IPA 1A" {
		t.Errorf("Senin: %s", got)
	}
	if got := times(week[2]); got != "09.00 Matematika 1A" || len(week[1].Slots) != 0 {
		t.Errorf("Rabu: %s", got)
	}
	// Guru1: Matematika di 1A dan 1B, tanpa IPA milik guru2.
	mine, err := TeacherWeek(ctx, f.tx, f.schoolA, f.guru1)
	if err != nil {
		t.Fatal(err)
	}
	if got := times(mine[0]); got != "07.00 Matematika 1A, 08.20 Matematika 1B" {
		t.Errorf("guru1 Senin: %s", got)
	}
	if got := times(mine[2]); got != "09.00 Matematika 1A" {
		t.Errorf("guru1 Rabu: %s", got)
	}
	other, _ := TeacherWeek(ctx, f.tx, f.schoolA, f.guru2)
	if got := times(other[0]); got != "07.40 IPA 1A" || (WeekView{Days: other[1:]}).NonEmpty() {
		t.Errorf("guru2: %s", got)
	}
	// Sekolah lain dan id rusak: kosong.
	for _, w := range [][2]string{{f.schoolB, f.guru1}, {f.schoolA, "bukan-uuid"}} {
		days, err := TeacherWeek(ctx, f.tx, w[0], w[1])
		if err != nil || (WeekView{Days: days}).NonEmpty() {
			t.Errorf("TeacherWeek(%s, %s): %v", w[0], w[1], err)
		}
	}
	// Tahun ajaran lain tidak aktif: slot lama tidak masuk jadwal guru.
	if _, err := f.tx.Exec(ctx, `UPDATE academic_years SET is_active = false WHERE school_id = $1`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	if days, _ := TeacherWeek(ctx, f.tx, f.schoolA, f.guru1); (WeekView{Days: days}).NonEmpty() {
		t.Error("teacher week includes an inactive year")
	}
}

var (
	daySection = regexp.MustCompile(`(?s)<section class="card day-card" id="day-(\d)".*?</section>`)
	slotTime   = regexp.MustCompile(`<span class="slot-time">([^<]+)</span>\s*<span class="min-w-0 flex-1"><strong class="block">([^<]+)</strong>`)
)

// rendered mengembalikan isi tiap kartu hari: "07.00–07.40 Matematika, …".
func rendered(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range daySection.FindAllStringSubmatch(body, -1) {
		var s []string
		for _, sl := range slotTime.FindAllStringSubmatch(m[0], -1) {
			s = append(s, sl[1]+" "+sl[2])
		}
		out[m[1]] = strings.Join(s, ", ")
	}
	if len(out) != 6 {
		t.Fatalf("rendered %d day cards, want 6", len(out))
	}
	return out
}

func TestWeekPages(t *testing.T) {
	e := newEnv(t)
	seedWeek(t, e.fixture)
	// Halaman kelas sama dengan data slot.
	body := e.do(http.MethodGet, "/jadwal?kelas="+e.class1A, "kepala_sekolah", nil).Body.String()
	got := rendered(t, body)
	week, _ := ClassWeek(context.Background(), e.tx, e.schoolA, e.class1A)
	for _, d := range week {
		var want []string
		for _, sl := range d.Slots {
			want = append(want, sl.Start.String()+"–"+sl.End.String()+" "+sl.SubjectName)
		}
		if k := string(rune('0' + d.Day)); got[k] != strings.Join(want, ", ") {
			t.Errorf("day %d: page %q, data %q", d.Day, got[k], strings.Join(want, ", "))
		}
	}
	if got["1"] != "07.00–07.40 Matematika, 07.40–08.20 IPA" {
		t.Errorf("Senin 1A: %q", got["1"])
	}
	// Tab hari untuk layar sempit: satu tombol per hari, kartu lain disembunyikan
	// lewat kelas day-off.
	if strings.Count(body, `role="tab"`) != 6 || !strings.Contains(body, `:class="{ 'day-off': day !== 3 }"`) || !strings.Contains(body, `x-data="{ day: `) {
		t.Error("day tabs missing")
	}

	// Guru hanya melihat jadwal mengajarnya sendiri.
	mine := func(user string) map[string]string {
		r := e.doAs(http.MethodGet, "/jadwal/saya", user, "guru", nil)
		if r.Code != http.StatusOK {
			t.Fatalf("GET /jadwal/saya: %d", r.Code)
		}
		return rendered(t, r.Body.String())
	}
	g1 := mine(e.guru1)
	if g1["1"] != "07.00–07.40 Matematika, 08.20–09.00 Matematika" || g1["3"] != "09.00–09.40 Matematika" || strings.Contains(g1["1"], "IPA") {
		t.Errorf("guru1: %v", g1)
	}
	if g2 := mine(e.guru2); g2["1"] != "07.40–08.20 IPA" || g2["3"] != "" {
		t.Errorf("guru2: %v", g2)
	}
	for _, role := range []string{"admin_tu", "kepala_sekolah", "bendahara", "orang_tua"} {
		if r := e.doAs(http.MethodGet, "/jadwal/saya", e.guru1, role, nil); r.Code != http.StatusForbidden {
			t.Errorf("GET /jadwal/saya as %s: %d", role, r.Code)
		}
	}
}

func TestTeacherPage_Empty(t *testing.T) {
	e := newEnv(t)
	r := e.doAs(http.MethodGet, "/jadwal/saya", e.guru2, "guru", nil)
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "Belum ada jadwal mengajar untuk Anda.") {
		t.Errorf("empty teacher page: %d", r.Code)
	}
}
