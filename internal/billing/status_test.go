package billing

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestComputeStatus(t *testing.T) {
	cases := []struct {
		name        string
		total, paid int64
		want        Status
		wantErr     error
	}{
		{"belum bayar", 100_000, 0, StatusUnpaid, nil},
		{"sebagian", 100_000, 50_000, StatusPartial, nil},
		{"sebagian, kurang seribu", 100_000, 99_999, StatusPartial, nil},
		{"lunas", 100_000, 100_000, StatusPaid, nil},
		{"lebih bayar", 100_000, 100_001, "", ErrOverpayment},
		{"tagihan gratis", 0, 0, StatusPaid, nil},
		{"tagihan gratis dibayar", 0, 1, "", ErrOverpayment},
		{"total negatif", -1, 0, "", ErrNegativeAmount},
		{"bayar negatif", 100_000, -1, "", ErrNegativeAmount},
		{"nominal besar", 1_000_000_000_000, 999_999_999_999, StatusPartial, nil},
	}
	for _, tc := range cases {
		got, err := ComputeStatus(tc.total, tc.paid)
		if !errors.Is(err, tc.wantErr) || got != tc.want {
			t.Errorf("%s: ComputeStatus(%d, %d) = %q, %v; want %q, %v", tc.name, tc.total, tc.paid, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestIsOverdue(t *testing.T) {
	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name   string
		status Status
		today  time.Time
		want   bool
	}{
		{"belum jatuh tempo", StatusUnpaid, day(9), false},
		{"jatuh tempo hari ini belum terlambat", StatusUnpaid, day(10), false},
		{"sehari setelah jatuh tempo", StatusUnpaid, day(11), true},
		{"sebagian, lewat jatuh tempo", StatusPartial, day(30), true},
		{"lunas tidak pernah terlambat", StatusPaid, day(30), false},
		{"lunas, jauh setelah jatuh tempo", StatusPaid, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		if got := IsOverdue(tc.status, due, tc.today); got != tc.want {
			t.Errorf("%s: IsOverdue = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Hanya tanggal yang dibandingkan: jam di hari jatuh tempo tidak membuat terlambat.
	lateEvening := time.Date(2026, 9, 10, 23, 59, 0, 0, time.UTC)
	if IsOverdue(StatusUnpaid, due, lateEvening) {
		t.Error("23:59 on the due date counted as overdue")
	}
	// Jatuh tempo dari database (tengah malam UTC) dan today dari clock.Today().
	if !IsOverdue(StatusUnpaid, due.Add(5*time.Hour), day(11)) {
		t.Error("time-of-day on the due date affected the result")
	}
}

// TestStatusFileIsPure memastikan status.go tetap fungsi murni: tanpa
// database, context, atau jam sistem.
func TestStatusFileIsPure(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "status.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path == "context" || strings.Contains(path, "database") || strings.Contains(path, "pgx") || strings.Contains(path, "clock") {
			t.Errorf("status.go imports %q", path)
		}
	}
	body, err := os.ReadFile("status.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "time.Now(") {
		t.Error("status.go calls time.Now(); pass today as a parameter instead")
	}
}
