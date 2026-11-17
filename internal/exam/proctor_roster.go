package exam

import (
	"slices"
	"sort"
	"time"
)

// Penjadwalan pengawas PTS/PAS (T134): per ruang per slot per gelombang,
// bergilir dari daftar guru.

// RosterUnit adalah satu tugas mengawasi: satu ruang di satu waktu.
type RosterUnit struct {
	Key        string // room|slot|shift
	RoomID     string
	SlotID     string
	Shift      int
	Start, End time.Time
	// Subjects adalah mapel yang diujikan di ruang itu pada waktu itu.
	Subjects []string
}

// Interval adalah rentang waktu sibuk guru di luar agenda ini.
type Interval struct {
	Start, End time.Time
	Label      string
}

func (i Interval) overlaps(s, e time.Time) bool { return s.Before(i.End) && i.Start.Before(e) }

// RosterTeacher adalah guru calon pengawas.
type RosterTeacher struct {
	ID, Name string
	Subjects []string // mapel yang diampu
	Busy     []Interval
}

// Roster adalah hasil penjadwalan pengawas.
type Roster struct {
	Assign   map[string]string // unit → guru
	Unfilled []string          // unit tanpa pengawas
	Load     map[string]int    // jumlah tugas per guru
}

// BuildRoster membagi pengawas. fixed berisi penugasan yang sudah diatur
// koordinator (dipertahankan). Aturan: guru tidak mengawasi mapel yang ia
// ampu di waktu itu, tidak bentrok dengan tugas lain, dan beban merata
// (guru dengan tugas paling sedikit dipilih lebih dulu, bergilir).
func BuildRoster(units []RosterUnit, teachers []RosterTeacher, fixed map[string]string) Roster {
	r := Roster{Assign: map[string]string{}, Load: map[string]int{}}
	byID := map[string]RosterTeacher{}
	for _, t := range teachers {
		byID[t.ID] = t
	}
	us := append([]RosterUnit(nil), units...)
	sort.SliceStable(us, func(i, j int) bool {
		if !us[i].Start.Equal(us[j].Start) {
			return us[i].Start.Before(us[j].Start)
		}
		return us[i].Key < us[j].Key
	})
	taken := map[string][]Interval{}
	for _, u := range us {
		if t, ok := fixed[u.Key]; ok && t != "" {
			r.Assign[u.Key] = t
			r.Load[t]++
			taken[t] = append(taken[t], Interval{Start: u.Start, End: u.End})
		}
	}
	order := append([]RosterTeacher(nil), teachers...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Name < order[j].Name })
	next := 0
	for _, u := range us {
		if _, ok := r.Assign[u.Key]; ok {
			continue
		}
		var best *RosterTeacher
		bestIdx := 0
		for k := 0; k < len(order); k++ {
			idx := (next + k) % len(order)
			t := order[idx]
			if !canProctor(t, u, taken[t.ID]) {
				continue
			}
			if best == nil || r.Load[t.ID] < r.Load[best.ID] {
				tt := t
				best, bestIdx = &tt, idx
			}
		}
		if best == nil {
			r.Unfilled = append(r.Unfilled, u.Key)
			continue
		}
		r.Assign[u.Key] = best.ID
		r.Load[best.ID]++
		taken[best.ID] = append(taken[best.ID], Interval{Start: u.Start, End: u.End})
		next = (bestIdx + 1) % len(order)
	}
	return r
}

func canProctor(t RosterTeacher, u RosterUnit, taken []Interval) bool {
	for _, s := range u.Subjects {
		if slices.Contains(t.Subjects, s) {
			return false
		}
	}
	for _, i := range append(append([]Interval(nil), taken...), t.Busy...) {
		if i.overlaps(u.Start, u.End) {
			return false
		}
	}
	return true
}

// Jenis masalah penugasan pengawas.
const (
	IssueOwnSubject = "mapel_sendiri"
	IssueClash      = "bentrok"
	IssueUnfilled   = "kosong"
)

// RosterIssue adalah satu masalah penugasan pengawas.
type RosterIssue struct {
	UnitKey   string
	TeacherID string
	Kind      string
	Detail    string
}

// CheckRoster melaporkan masalah penugasan: unit tanpa pengawas, pengawas
// mengawasi mapel yang ia ampu, dan bentrok (dua tugas di waktu yang sama
// atau dengan jadwal lain).
func CheckRoster(units []RosterUnit, teachers []RosterTeacher, assign map[string]string) []RosterIssue {
	byID := map[string]RosterTeacher{}
	for _, t := range teachers {
		byID[t.ID] = t
	}
	var out []RosterIssue
	mine := map[string][]RosterUnit{}
	for _, u := range units {
		tid := assign[u.Key]
		if tid == "" {
			out = append(out, RosterIssue{UnitKey: u.Key, Kind: IssueUnfilled})
			continue
		}
		t := byID[tid]
		for _, s := range u.Subjects {
			if slices.Contains(t.Subjects, s) {
				out = append(out, RosterIssue{UnitKey: u.Key, TeacherID: tid, Kind: IssueOwnSubject, Detail: s})
				break
			}
		}
		for _, b := range t.Busy {
			if b.overlaps(u.Start, u.End) {
				out = append(out, RosterIssue{UnitKey: u.Key, TeacherID: tid, Kind: IssueClash, Detail: b.Label})
			}
		}
		for _, o := range mine[tid] {
			if o.Start.Before(u.End) && u.Start.Before(o.End) {
				out = append(out, RosterIssue{UnitKey: u.Key, TeacherID: tid, Kind: IssueClash, Detail: o.Key})
			}
		}
		mine[tid] = append(mine[tid], u)
	}
	return out
}
