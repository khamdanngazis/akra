package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"akra/internal/exam"
	"akra/internal/platform/database"
)

func TestApply(t *testing.T) {
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
	var schoolID string
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('SD Modul', 'test-modul') RETURNING id`).Scan(&schoolID); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := apply(ctx, tx, "test-modul", "", "", &out); err != nil || !strings.Contains(out.String(), "nonaktif, tahap r1") {
		t.Fatalf("show: %v %q", err, out.String())
	}
	out.Reset()
	if err := apply(ctx, tx, " TEST-MODUL ", "on", "r2", &out); err != nil || !strings.Contains(out.String(), "aktif, tahap r2") {
		t.Fatalf("enable: %v %q", err, out.String())
	}
	if m, _ := exam.LoadModule(ctx, tx, schoolID); !m.Has(exam.R2) || m.Has(exam.R3) {
		t.Errorf("module = %+v", m)
	}
	var n int
	tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'module.exam'`, schoolID).Scan(&n)
	if n != 1 {
		t.Errorf("audit = %d, want 1", n)
	}
	// Tanpa perubahan: tidak menambah audit.
	if err := apply(ctx, tx, "test-modul", "on", "r2", &out); err != nil {
		t.Fatal(err)
	}
	tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'module.exam'`, schoolID).Scan(&n)
	if n != 1 {
		t.Errorf("audit after no-op = %d, want 1", n)
	}
	for _, bad := range [][3]string{{"tidak-ada", "on", ""}, {"test-modul", "ya", ""}, {"test-modul", "on", "r4"}} {
		if err := apply(ctx, tx, bad[0], bad[1], bad[2], &out); err == nil {
			t.Errorf("apply(%q) accepted", bad)
		}
	}
	if err := apply(ctx, tx, "test-modul", "off", "", &out); err != nil {
		t.Fatal(err)
	}
	if m, _ := exam.LoadModule(ctx, tx, schoolID); m.Enabled {
		t.Error("module still enabled after off")
	}
}
