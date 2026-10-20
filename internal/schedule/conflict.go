// Package schedule mengatur jadwal pelajaran mingguan per kelas (T66).
package schedule

import (
	"fmt"
	"strconv"
	"strings"
)

// Minutes adalah jam dalam menit sejak 00.00 (mis. 07.30 = 450).
type Minutes int

// ParseTime membaca "HH:MM" atau "HH.MM" (24 jam).
func ParseTime(s string) (Minutes, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ".", ":")
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(m) != 2 || len(h) < 1 || len(h) > 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return Minutes(hh*60 + mm), true
}

// String memformat jam seperti di Indonesia: "07.30".
func (m Minutes) String() string { return fmt.Sprintf("%02d.%02d", int(m)/60, int(m)%60) }

// Input memformat jam untuk <input type="time">: "07:30".
func (m Minutes) Input() string { return fmt.Sprintf("%02d:%02d", int(m)/60, int(m)%60) }

// Overlaps melaporkan apakah rentang [aStart, aEnd) dan [bStart, bEnd)
// berpotongan. Rentang yang bersebelahan (selesai 08.00, mulai 08.00) tidak
// berpotongan.
func Overlaps(aStart, aEnd, bStart, bEnd Minutes) bool {
	return aStart < bEnd && bStart < aEnd
}

// Hari dalam jadwal: 1 = Senin sampai 6 = Sabtu.
var dayNames = [...]string{"", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}

// DayName mengembalikan nama hari, atau "" jika di luar 1–6.
func DayName(d int) string {
	if d < 1 || d > 6 {
		return ""
	}
	return dayNames[d]
}

// Days adalah daftar hari jadwal, untuk pilihan form dan tampilan mingguan.
func Days() []int { return []int{1, 2, 3, 4, 5, 6} }
