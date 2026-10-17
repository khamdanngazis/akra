// Package clock menentukan tanggal "hari ini" menurut zona waktu sekolah.
package clock

import (
	"strconv"
	"time"

	// Data zona waktu ikut di-embed agar Asia/Jakarta tersedia di image
	// mana pun.
	_ "time/tzdata"
)

// Jakarta adalah zona waktu Asia/Jakarta (WIB). Semua penentuan "hari ini"
// di aplikasi memakai zona ini, bukan zona server (Railway memakai UTC).
var Jakarta = mustLoad("Asia/Jakarta")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Today mengembalikan tanggal hari ini di Asia/Jakarta, sebagai tengah malam
// UTC pada tanggal tersebut (bentuk yang sama dengan kolom date dari pgx).
func Today() time.Time {
	return DateOf(time.Now())
}

// DateOf mengembalikan tanggal t di Asia/Jakarta sebagai tengah malam UTC.
func DateOf(t time.Time) time.Time {
	y, m, d := t.In(Jakarta).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

var monthsID = [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember"}

// FormatDateID memformat tanggal dalam bahasa Indonesia, mis. "1 Juli 2026".
func FormatDateID(t time.Time) string {
	return strconv.Itoa(t.Day()) + " " + monthsID[t.Month()-1] + " " + strconv.Itoa(t.Year())
}
