package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"akra/internal/platform/database"
)

func TestPilotCommand(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if _, err := tx.Exec(ctx, `INSERT INTO schools (name, code) VALUES ('SD Pilot', 'test-pilot')`); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for _, bad := range [][]string{{}, {"-school", "test-pilot", "-from", "2027-02-01"}, {"-school", "test-pilot", "-from", "2027-02-10", "-to", "2027-02-01"},
		{"-school", "test-pilot", "-from", "1-2-2027", "-to", "2027-02-01"}} {
		if code, err := pilot(ctx, tx, bad, &out); code != 2 || err == nil {
			t.Errorf("pilot(%q) = %d, %v", bad, code, err)
		}
	}
	if code, _ := pilot(ctx, tx, []string{"-school", "tidak-ada", "-from", "2027-02-01", "-to", "2027-02-28"}, &out); code != 1 {
		t.Errorf("unknown school = %d", code)
	}
	code, err := pilot(ctx, tx, []string{"-school", "TEST-PILOT", "-from", "2027-02-01", "-to", "2027-02-28", "-tickets", "0"}, &out)
	if code != 0 || err != nil || !strings.Contains(out.String(), "### Pilot SD Pilot (test-pilot), 2027-02-01 – 2027-02-28") ||
		!strings.Contains(out.String(), "0 peserta, 0 attempt") {
		t.Errorf("pilot = %d, %v:\n%s", code, err, out.String())
	}
}
