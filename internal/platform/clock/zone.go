package clock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Zona waktu sekolah yang didukung (T126). Penyimpanan tetap timestamptz
// (UTC); zona hanya dipakai di tepi: parse input dan format tampilan.
var (
	Makassar = mustLoad("Asia/Makassar")
	Jayapura = mustLoad("Asia/Jayapura")
)

// DefaultZone adalah zona bawaan sekolah (kolom schools.timezone).
const DefaultZone = "Asia/Jakarta"

// Zone adalah satu zona waktu sekolah untuk pilihan di profil sekolah.
type Zone struct {
	Name  string // nama IANA, nilai kolom schools.timezone
	Label string // singkatan untuk tampilan jam, mis. "WITA"
	Long  string // keterangan untuk pilihan form
}

// Zones adalah semua zona waktu sekolah yang didukung, urut barat ke timur.
var Zones = []Zone{
	{Name: "Asia/Jakarta", Label: "WIB", Long: "WIB — Waktu Indonesia Barat (Sumatra, Jawa, Kalimantan Barat dan Tengah)"},
	{Name: "Asia/Makassar", Label: "WITA", Long: "WITA — Waktu Indonesia Tengah (Bali, Nusa Tenggara, Kalimantan Selatan, Timur, dan Utara, Sulawesi)"},
	{Name: "Asia/Jayapura", Label: "WIT", Long: "WIT — Waktu Indonesia Timur (Maluku, Papua)"},
}

// ValidZone melaporkan apakah name adalah zona sekolah yang didukung.
func ValidZone(name string) bool {
	for _, z := range Zones {
		if z.Name == name {
			return true
		}
	}
	return false
}

// Location mengembalikan lokasi untuk nama zona sekolah. Nama kosong atau
// tidak dikenal (mis. sesi lama sebelum T126) dianggap Asia/Jakarta.
func Location(name string) *time.Location {
	switch name {
	case "Asia/Makassar":
		return Makassar
	case "Asia/Jayapura":
		return Jayapura
	}
	return Jakarta
}

// ZoneLabel mengembalikan singkatan zona loc ("WIB", "WITA", "WIT").
func ZoneLabel(loc *time.Location) string {
	for _, z := range Zones {
		if z.Name == loc.String() {
			return z.Label
		}
	}
	return "WIB"
}

// TodayIn mengembalikan tanggal hari ini di loc, sebagai tengah malam UTC
// pada tanggal tersebut (bentuk yang sama dengan Today).
func TodayIn(loc *time.Location) time.Time {
	return DateIn(time.Now(), loc)
}

// DateIn mengembalikan tanggal t di loc sebagai tengah malam UTC.
func DateIn(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// FormatDateTimeID memformat waktu t di loc, mis. "1 Juli 2026 08.00 WITA".
func FormatDateTimeID(t time.Time, loc *time.Location) string {
	l := t.In(loc)
	return FormatDateID(l) + " " + l.Format("15.04") + " " + ZoneLabel(loc)
}

// ForSchool membaca zona waktu sekolah dari database. Dipakai di halaman
// tanpa sesi staf (halaman publik, job latar belakang); halaman staf
// memakai auth.Identity.Location.
func ForSchool(ctx context.Context, q database.DBTX, schoolID string) (*time.Location, error) {
	var name string
	err := q.QueryRow(ctx, `SELECT timezone FROM schools WHERE id = $1`, schoolID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("clock: school %s not found", schoolID)
	}
	if err != nil {
		return nil, fmt.Errorf("clock: school timezone: %w", err)
	}
	return Location(name), nil
}
