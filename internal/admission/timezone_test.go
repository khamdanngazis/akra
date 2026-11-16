package admission

import (
	"context"
	"testing"
	"time"
)

// Sekolah WIT: jadwal tes 08.00 diisi dalam WIT, disimpan sebagai 23.00 UTC
// hari sebelumnya (06.00 WIB di jam server), dan tampil kembali 08.00 WIT
// di halaman status publik (T126).
func TestTestScheduleInSchoolTimezone(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.tx.Exec(ctx, `UPDATE schools SET timezone = 'Asia/Jayapura' WHERE id = $1`, e.schoolA); err != nil {
		t.Fatal(err)
	}
	p, _ := e.openPeriod(t)
	sel := NewSelectionService(e.tx)
	test, err := sel.AddTest(ctx, e.actor, p.ID, TestForm{Name: "Tertulis", Weight: "100", ScheduledAt: "2027-02-10T08:00"})
	if err != nil {
		t.Fatal(err)
	}
	if got := test.ScheduledAt.UTC().Format(time.RFC3339); got != "2027-02-09T23:00:00Z" {
		t.Errorf("stored schedule = %s, want 2027-02-09T23:00:00Z", got)
	}
	lines, err := testSchedule(ctx, sel, e.schoolA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "Tertulis: 10 Februari 2027 08.00 WIT" {
		t.Errorf("public schedule = %q", lines)
	}
}
