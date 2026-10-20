// Package reports menyusun laporan sekolah (rekap kehadiran, tunggakan)
// untuk layar dan unduhan. Angka selalu dari fungsi modul asalnya, agar
// file sama dengan layar dan sama dengan halaman modul.
package reports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/attendance"
	"akra/internal/platform/database"
)

// MaxSyncRows adalah batas baris ekspor langsung (di request). Di atas itu
// ekspor dijalankan di server (T49).
const MaxSyncRows = 5000

// ErrTooManyRows dikembalikan jika ekspor langsung melebihi MaxSyncRows.
var ErrTooManyRows = errors.New("reports: too many rows for a direct export")

// AttendanceFilter memilih data rekap kehadiran.
type AttendanceFilter struct {
	ClassID  string // kosong = semua kelas tahun ajaran rentang
	From, To time.Time
}

// AttendanceRow adalah rekap satu siswa di satu kelas.
type AttendanceRow struct {
	NIS, FullName, ClassName string
	attendance.Counts
}

// AttendanceReport adalah rekap kehadiran untuk layar dan unduhan.
type AttendanceReport struct {
	YearName  string
	ClassName string // kosong = semua kelas
	From, To  time.Time
	Rows      []AttendanceRow
	Total     attendance.Counts
}

// BuildAttendanceReport menyusun rekap kehadiran sekolah. Rentang wajib di
// dalam satu tahun ajaran (tahun ajaran kelas, atau tahun ajaran yang
// memuat seluruh rentang jika semua kelas). Angka per siswa dari
// attendance.SummaryByStudent (T27), sehingga sama dengan rekap kelas;
// persentase dari attendance.Counts.PresentPercent.
func BuildAttendanceReport(ctx context.Context, q database.DBTX, schoolID string, f AttendanceFilter) (AttendanceReport, error) {
	if f.From.After(f.To) {
		return AttendanceReport{}, attendance.ErrInvalidRange
	}
	r := AttendanceReport{From: f.From, To: f.To}
	type class struct{ id, name string }
	var classes []class
	if f.ClassID != "" {
		cy, err := attendance.GetClassYear(ctx, q, schoolID, f.ClassID)
		if err != nil {
			return AttendanceReport{}, err
		}
		if err := cy.CheckRange(f.From, f.To); err != nil {
			return AttendanceReport{}, err
		}
		r.YearName, r.ClassName = cy.YearName, cy.ClassName
		classes = append(classes, class{cy.ClassID, cy.ClassName})
	} else {
		var yearID string
		err := q.QueryRow(ctx, `SELECT id, name FROM academic_years
			WHERE school_id = $1 AND start_date <= $2 AND end_date >= $3
			ORDER BY start_date DESC LIMIT 1`, schoolID, f.From, f.To).Scan(&yearID, &r.YearName)
		if errors.Is(err, pgx.ErrNoRows) {
			return AttendanceReport{}, attendance.ErrRangeOutsideYear
		}
		if err != nil {
			return AttendanceReport{}, fmt.Errorf("reports: find academic year: %w", err)
		}
		rows, err := q.Query(ctx, `SELECT id, name FROM classes WHERE school_id = $1 AND academic_year_id = $2
			ORDER BY grade_level, lower(name)`, schoolID, yearID)
		if err != nil {
			return AttendanceReport{}, fmt.Errorf("reports: list classes: %w", err)
		}
		classes, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (class, error) {
			var c class
			err := row.Scan(&c.id, &c.name)
			return c, err
		})
		if err != nil {
			return AttendanceReport{}, fmt.Errorf("reports: list classes: %w", err)
		}
	}
	for _, c := range classes {
		list, err := attendance.SummaryByStudent(ctx, q, schoolID, c.id, "", f.From, f.To)
		if err != nil {
			return AttendanceReport{}, err
		}
		for _, s := range list {
			r.Rows = append(r.Rows, AttendanceRow{NIS: s.NIS, FullName: s.FullName, ClassName: c.name, Counts: s.Counts})
			r.Total.Hadir += s.Hadir
			r.Total.Sakit += s.Sakit
			r.Total.Izin += s.Izin
			r.Total.Alpa += s.Alpa
		}
	}
	return r, nil
}
