package exam

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Pembagian peserta PTS/PAS ke ruang dan gelombang (T134).

// Jenis ruang ujian.
const (
	RoomLab   = "lab"   // komputer sekolah
	RoomClass = "kelas" // peserta memakai HP
)

// AllocStudent adalah satu siswa yang dibagi.
type AllocStudent struct {
	ID, Name, NIS string
}

// AllocClass adalah satu kelas beserta siswanya (urut nama).
type AllocClass struct {
	ID, Name string
	Grade    int
	Students []AllocStudent
}

// AllocRoom adalah satu ruang.
type AllocRoom struct {
	ID, Name string
	Kind     string
	Capacity int
}

// Place adalah tempat satu peserta.
type Place struct {
	RoomID string
	Shift  int
	Seat   int
}

// Allocation adalah hasil pembagian.
type Allocation struct {
	Places map[string]Place // per id siswa
	// Unplaced berisi siswa yang tidak mendapat tempat (kapasitas kurang).
	Unplaced []string
	// Split berisi kelas yang terpaksa dipecah ke beberapa ruang/gelombang.
	Split []string
}

type unitKey struct {
	room  string
	shift int
}

type allocUnit struct {
	key       unitKey
	order     int // urutan ruang
	remaining int
}

// Allocate membagi siswa ke ruang dan gelombang. Aturan:
//   - gelombang 1 dulu; lab boleh dibagi beberapa gelombang (maks
//     maxShifts) bila siswa lebih banyak dari komputer, ruang kelas hanya
//     gelombang 1;
//   - satu kelas tetap bersama bila muat di satu ruang (ruang terkecil yang
//     cukup); bila tidak muat dipecah, mengisi ruang tersisa terbesar dulu;
//   - kapasitas tidak pernah dilampaui; yang tidak mendapat tempat
//     dilaporkan;
//   - tempat duduk diurutkan kelas lalu nama di setiap ruang+gelombang.
func Allocate(classes []AllocClass, rooms []AllocRoom, maxShifts int) Allocation {
	if maxShifts < 1 {
		maxShifts = 1
	}
	var units []*allocUnit
	for s := 1; s <= maxShifts; s++ {
		for i, r := range rooms {
			if r.Capacity <= 0 || (s > 1 && r.Kind != RoomLab) {
				continue
			}
			units = append(units, &allocUnit{key: unitKey{r.ID, s}, order: i, remaining: r.Capacity})
		}
	}
	cs := append([]AllocClass(nil), classes...)
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Grade != cs[j].Grade {
			return cs[i].Grade < cs[j].Grade
		}
		return cs[i].Name < cs[j].Name
	})
	out := Allocation{Places: map[string]Place{}}
	members := map[unitKey][]AllocStudent{}
	classOf := map[string]string{}
	for _, c := range cs {
		for _, s := range c.Students {
			classOf[s.ID] = c.Name
		}
		n := len(c.Students)
		if n == 0 {
			continue
		}
		// Utuh: gelombang terendah, lalu ruang terkecil yang cukup.
		var best *allocUnit
		for _, u := range units {
			if u.remaining < n {
				continue
			}
			if best == nil || u.key.shift < best.key.shift ||
				(u.key.shift == best.key.shift && (u.remaining < best.remaining || (u.remaining == best.remaining && u.order < best.order))) {
				best = u
			}
		}
		if best != nil {
			members[best.key] = append(members[best.key], c.Students...)
			best.remaining -= n
			continue
		}
		// Dipecah: gelombang terendah, sisa terbesar dulu.
		out.Split = append(out.Split, c.Name)
		rest := c.Students
		for len(rest) > 0 {
			var pick *allocUnit
			for _, u := range units {
				if u.remaining == 0 {
					continue
				}
				if pick == nil || u.key.shift < pick.key.shift ||
					(u.key.shift == pick.key.shift && (u.remaining > pick.remaining || (u.remaining == pick.remaining && u.order < pick.order))) {
					pick = u
				}
			}
			if pick == nil {
				for _, s := range rest {
					out.Unplaced = append(out.Unplaced, s.ID)
				}
				break
			}
			take := min(pick.remaining, len(rest))
			members[pick.key] = append(members[pick.key], rest[:take]...)
			pick.remaining -= take
			rest = rest[take:]
		}
	}
	for k, ss := range members {
		sort.SliceStable(ss, func(i, j int) bool {
			if classOf[ss[i].ID] != classOf[ss[j].ID] {
				return classOf[ss[i].ID] < classOf[ss[j].ID]
			}
			return ss[i].Name < ss[j].Name
		})
		for i, s := range ss {
			out.Places[s.ID] = Place{RoomID: k.room, Shift: k.shift, Seat: i + 1}
		}
	}
	return out
}

// Occupancy menghitung isi setiap ruang+gelombang dari places.
func Occupancy(places map[string]Place) map[string]int {
	out := map[string]int{}
	for _, p := range places {
		out[p.RoomID+"|"+strconv.Itoa(p.Shift)]++
	}
	return out
}

// ParticipantNumber membuat nomor peserta dari format. Token: {tahun},
// {tingkat} (dua digit), {urut} atau {urut:N} (nol di depan sampai N
// digit), {gelombang}.
func ParticipantNumber(format string, year, grade, seq, shift int) string {
	r := strings.NewReplacer("{tahun}", strconv.Itoa(year), "{tingkat}", fmt.Sprintf("%02d", grade), "{gelombang}", strconv.Itoa(shift))
	out := r.Replace(format)
	for {
		i := strings.Index(out, "{urut")
		if i < 0 {
			break
		}
		j := strings.Index(out[i:], "}")
		if j < 0 {
			break
		}
		tok := out[i : i+j+1]
		width := 1
		if _, w, ok := strings.Cut(strings.Trim(tok, "{}"), ":"); ok {
			if n, err := strconv.Atoi(w); err == nil && n >= 1 && n <= 6 {
				width = n
			}
		}
		out = out[:i] + fmt.Sprintf("%0*d", width, seq) + out[i+j+1:]
	}
	return out
}

// ValidNumberFormat melaporkan apakah format nomor peserta memuat {urut}
// (agar nomor unik).
func ValidNumberFormat(f string) bool {
	return strings.Contains(f, "{urut") && len(f) <= 60
}
