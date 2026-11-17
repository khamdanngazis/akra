package exam

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func makeClasses(grades []int, perGrade, size int) []AllocClass {
	var out []AllocClass
	for _, g := range grades {
		for c := 0; c < perGrade; c++ {
			cl := AllocClass{ID: fmt.Sprintf("c%d%c", g, 'A'+c), Name: fmt.Sprintf("%d%c", g, 'A'+c), Grade: g}
			for s := 0; s < size; s++ {
				cl.Students = append(cl.Students, AllocStudent{ID: fmt.Sprintf("%s-%02d", cl.ID, s), Name: fmt.Sprintf("Siswa %02d", s)})
			}
			out = append(out, cl)
		}
	}
	return out
}

// 600 siswa, 12 kelas, 4 lab × 40 komputer, 8 ruang kelas: pembagian valid
// tanpa melebihi kapasitas.
func TestAllocate600(t *testing.T) {
	classes := makeClasses([]int{7, 8, 9}, 4, 50)
	var rooms []AllocRoom
	for i := 1; i <= 4; i++ {
		rooms = append(rooms, AllocRoom{ID: fmt.Sprintf("lab%d", i), Name: fmt.Sprintf("Lab %d", i), Kind: RoomLab, Capacity: 40})
	}
	for i := 1; i <= 8; i++ {
		rooms = append(rooms, AllocRoom{ID: fmt.Sprintf("r%d", i), Name: fmt.Sprintf("Ruang %d", i), Kind: RoomClass, Capacity: 36})
	}
	a := Allocate(classes, rooms, 3)
	if len(a.Unplaced) != 0 || len(a.Places) != 600 {
		t.Fatalf("placed %d, unplaced %d", len(a.Places), len(a.Unplaced))
	}
	capOf := map[string]int{}
	for _, r := range rooms {
		capOf[r.ID] = r.Capacity
	}
	seats := map[string]bool{}
	for id, p := range a.Places {
		if p.Shift > 1 && p.RoomID[0] != 'l' {
			t.Errorf("%s: classroom used in shift %d", id, p.Shift)
		}
		key := fmt.Sprintf("%s|%d|%d", p.RoomID, p.Shift, p.Seat)
		if seats[key] {
			t.Errorf("seat %s taken twice", key)
		}
		seats[key] = true
		if p.Seat < 1 || p.Seat > capOf[p.RoomID] {
			t.Errorf("%s seat %d out of %d", id, p.Seat, capOf[p.RoomID])
		}
	}
	for k, n := range Occupancy(a.Places) {
		var room string
		fmt.Sscanf(k, "%s", &room)
		if n > 40 {
			t.Errorf("unit %s holds %d", k, n)
		}
	}
	// Kapasitas tidak cukup: sisa dilaporkan, tidak dipaksa masuk.
	a = Allocate(classes, rooms[:4], 2)
	if len(a.Places) != 320 || len(a.Unplaced) != 280 {
		t.Errorf("short capacity: placed %d unplaced %d", len(a.Places), len(a.Unplaced))
	}
}

// Kelas tetap bersama bila muat di satu ruang.
func TestAllocateKeepsClassTogether(t *testing.T) {
	classes := makeClasses([]int{7}, 3, 30)
	rooms := []AllocRoom{{ID: "a", Kind: RoomClass, Capacity: 32}, {ID: "b", Kind: RoomClass, Capacity: 40}, {ID: "c", Kind: RoomLab, Capacity: 30}}
	a := Allocate(classes, rooms, 1)
	if len(a.Split) != 0 || len(a.Unplaced) != 0 {
		t.Fatalf("split %v unplaced %d", a.Split, len(a.Unplaced))
	}
	for _, c := range classes {
		room := a.Places[c.Students[0].ID].RoomID
		for _, s := range c.Students {
			if a.Places[s.ID].RoomID != room {
				t.Errorf("class %s split", c.Name)
			}
		}
	}
}

func TestParticipantNumber(t *testing.T) {
	for _, c := range []struct {
		f    string
		want string
	}{
		{"{tahun}-{tingkat}-{urut:3}", "2026-07-005"},
		{"PAS{tingkat}{urut:4}", "PAS070005"},
		{"{urut}", "5"},
	} {
		if got := ParticipantNumber(c.f, 2026, 7, 5, 1); got != c.want {
			t.Errorf("%s = %s, want %s", c.f, got, c.want)
		}
	}
	if ValidNumberFormat("{tahun}-{tingkat}") {
		t.Error("format without {urut} accepted")
	}
}

// Pengawas tidak mendapat mapelnya sendiri, tidak bentrok, beban merata;
// bentrok penugasan manual dilaporkan.
func TestProctorRoster(t *testing.T) {
	day := time.Date(2026, 12, 1, 0, 30, 0, 0, time.UTC)
	var units []RosterUnit
	for slot := 0; slot < 3; slot++ {
		subj := []string{"mtk", "ipa", "bindo"}[slot]
		for room := 0; room < 4; room++ {
			s := day.Add(time.Duration(slot) * 2 * time.Hour)
			units = append(units, RosterUnit{Key: fmt.Sprintf("r%d|s%d|1", room, slot), RoomID: fmt.Sprintf("r%d", room), SlotID: fmt.Sprintf("s%d", slot),
				Shift: 1, Start: s, End: s.Add(90 * time.Minute), Subjects: []string{subj}})
		}
	}
	teachers := []RosterTeacher{
		{ID: "t1", Name: "Ani", Subjects: []string{"mtk"}},
		{ID: "t2", Name: "Budi", Subjects: []string{"ipa"}},
		{ID: "t3", Name: "Cici", Subjects: []string{"bindo"}},
		{ID: "t4", Name: "Dodi", Subjects: []string{"mtk"}},
		{ID: "t5", Name: "Eka", Subjects: []string{"ipa"}},
		{ID: "t6", Name: "Fajar", Subjects: []string{"pjok"}, Busy: []Interval{{Start: day.Add(4 * time.Hour), End: day.Add(5 * time.Hour), Label: "rapat"}}},
	}
	r := BuildRoster(units, teachers, nil)
	if len(r.Unfilled) != 0 {
		t.Fatalf("unfilled %v", r.Unfilled)
	}
	if issues := CheckRoster(units, teachers, r.Assign); len(issues) != 0 {
		t.Errorf("issues %+v", issues)
	}
	// Beban merata: 12 tugas untuk 6 guru = 2 masing-masing.
	for _, tc := range teachers {
		if r.Load[tc.ID] != 2 {
			t.Errorf("%s load %d", tc.Name, r.Load[tc.ID])
		}
	}
	// Tukar manual: guru matematika di slot matematika + dua ruang sekaligus.
	bad := map[string]string{}
	for k, v := range r.Assign {
		bad[k] = v
	}
	bad["r0|s0|1"] = "t1"
	bad["r1|s0|1"] = "t1"
	issues := CheckRoster(units, teachers, bad)
	var own, clash int
	for _, i := range issues {
		switch i.Kind {
		case IssueOwnSubject:
			own++
		case IssueClash:
			clash++
		}
	}
	if own < 2 || clash < 1 {
		t.Errorf("issues = %+v", issues)
	}
	// Penugasan tetap dipertahankan.
	r = BuildRoster(units, teachers, map[string]string{"r3|s2|1": "t6"})
	if r.Assign["r3|s2|1"] != "t6" {
		t.Error("fixed assignment overwritten")
	}
	// Terlalu sedikit guru: unit tanpa pengawas dilaporkan.
	r = BuildRoster(units, teachers[:1], nil)
	if len(r.Unfilled) == 0 {
		t.Error("no unfilled with one teacher")
	}
}

func TestRoomClashes(t *testing.T) {
	d := time.Date(2026, 12, 1, 0, 30, 0, 0, time.UTC)
	units := []RosterUnit{
		{RoomID: "lab", Start: d, End: d.Add(90 * time.Minute)},
		{RoomID: "lab", Start: d.Add(105 * time.Minute), End: d.Add(195 * time.Minute)}, // gelombang 2
		{RoomID: "lab", Start: d.Add(2 * time.Hour), End: d.Add(210 * time.Minute)},     // jam berikutnya
		{RoomID: "r1", Start: d.Add(2 * time.Hour), End: d.Add(210 * time.Minute)},
	}
	got := roomClashes(units, map[string]string{"lab": "Lab A"}, time.UTC)
	if len(got) != 1 || !strings.HasPrefix(got[0], "Lab A:") {
		t.Errorf("clashes = %v", got)
	}
}
