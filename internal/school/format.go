package school

import (
	"strconv"
	"time"
)

var monthsID = [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember"}

// formatDateID memformat tanggal sebagai "1 Juli 2026".
func formatDateID(t time.Time) string {
	return strconv.Itoa(t.Day()) + " " + monthsID[t.Month()-1] + " " + strconv.Itoa(t.Year())
}
