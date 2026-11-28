package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/exam"
	"akra/internal/platform/database"
	"akra/internal/platform/module"
	"akra/internal/testutil"
)

func testTx(t *testing.T) pgx.Tx {
	t.Helper()
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
	return tx
}

func mustParse(t *testing.T, args ...string) options {
	t.Helper()
	o, err := parse(args)
	if err != nil {
		t.Fatalf("parse(%v): %v", args, err)
	}
	return o
}

func TestParse(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gelombang.txt")
	if err := os.WriteFile(file, []byte("# gelombang 2\nsd-a\n\nSD-B\nsd-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := mustParse(t, "-file", file, "-school", "sd-c", "-package", "lengkap", "-note", "gelombang 2")
	if strings.Join(o.codes, ",") != "sd-c,sd-a,sd-b" || o.pkg != "lengkap" {
		t.Errorf("wave = %+v", o)
	}
	o = mustParse(t, "-school", "a", "-module", "exam", "-on", "-option", "release=r2", "-note", "pilot")
	if o.module != module.Exam || !o.on || o.opts["release"] != "r2" {
		t.Errorf("module = %+v", o)
	}
	// Bentuk lama T125/T155.
	o = mustParse(t, "-school", "a", "-exam", "on", "-release", "r1", "-note", "pilot")
	if o.module != module.Exam || !o.on || o.opts["release"] != "r1" {
		t.Errorf("legacy = %+v", o)
	}
	if o := mustParse(t, "-list", "-module", "billing"); !o.list || o.module != module.Billing {
		t.Errorf("list = %+v", o)
	}
	for _, bad := range [][]string{
		{"-module", "billing", "-on", "-note", "x"},                               // tanpa sekolah
		{"-school", "a", "-module", "billing", "-on"},                             // tanpa catatan
		{"-school", "a", "-package", "lengkap"},                                   // tanpa catatan
		{"-school", "a", "-module", "keuangan", "-on", "-note", "x"},              // modul tidak dikenal
		{"-school", "a", "-package", "premium", "-note", "x"},                     // paket tidak dikenal
		{"-school", "a", "-module", "billing", "-package", "dasar", "-note", "x"}, // keduanya
		{"-school", "a", "-module", "billing", "-on", "-off", "-note", "x"},
		{"-school", "a", "-module", "billing", "-note", "x"},
		{"-school", "a", "-on", "-note", "x"},
		{"-school", "a", "-module", "exam", "-option", "release", "-note", "x"},
		{"-school", "a", "-exam", "ya"},
	} {
		if _, err := parse(bad); err == nil {
			t.Errorf("parse(%v) accepted", bad)
		}
	}
}

func TestApply(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	var a, b string
	for code, id := range map[string]*string{"test-modul-a": &a, "test-modul-b": &b} {
		if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ($1, $1) RETURNING id`, code).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { module.Forget(a); module.Forget(b) })
	var out bytes.Buffer
	do := func(code string, args ...string) error {
		t.Helper()
		o := mustParse(t, append([]string{"-school", code}, args...)...)
		return apply(ctx, tx, code, o, &out)
	}
	audits := func(school, action string) int {
		var n int
		tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, school, action).Scan(&n)
		return n
	}

	// Status saja.
	if err := do("test-modul-a"); err != nil || !strings.Contains(out.String(), "tanpa paket · status") {
		t.Fatalf("show: %v %q", err, out.String())
	}
	// Modul dengan pilihan, lalu tanpa perubahan: audit sekali.
	for range 2 {
		if err := do("test-modul-a", "-module", "exam", "-on", "-option", "release=r2", "-note", "pilot ulangan"); err != nil {
			t.Fatal(err)
		}
	}
	if m, _ := exam.LoadModule(ctx, tx, a); !m.Has(exam.R2) || m.Has(exam.R3) || audits(a, "module.exam") != 1 {
		t.Errorf("exam r2 = %+v, audits %d", m, audits(a, "module.exam"))
	}
	if !strings.Contains(out.String(), "exam (r2)") {
		t.Errorf("output = %q", out.String())
	}
	// Hanya pilihan: status tetap aktif.
	if err := do("test-modul-a", "-module", "exam", "-option", "release=r3", "-note", "PTS"); err != nil {
		t.Fatal(err)
	}
	if m, _ := exam.LoadModule(ctx, tx, a); !m.Has(exam.R3) {
		t.Errorf("option only = %+v", m)
	}
	// Paket: sekolah B mendapat paket Keuangan (absensi dkk. mati).
	if err := do("test-modul-b", "-package", "keuangan", "-note", "yayasan"); err != nil {
		t.Fatal(err)
	}
	set, _ := module.Load(ctx, tx, b)
	if !set.Has(module.Billing) || set.Has(module.Attendance) || set.Has(module.Exam) || audits(b, "module.package") != 1 {
		t.Errorf("package keuangan = %v", set.IDs())
	}
	// Keuangan dengan tagihan belum lunas (sekolah fixture): butuh -force.
	fx, _ := testutil.SeedTwoSchools(t, tx, "x")
	t.Cleanup(func() { module.Forget(fx.ID) })
	if err := do(fx.Code, "-module", "billing", "-off", "-note", "pindah"); err == nil || !strings.Contains(err.Error(), "use force") {
		t.Errorf("billing off without force: %v", err)
	}
	out.Reset()
	if err := do(fx.Code, "-module", "billing", "-off", "-force", "-note", "pindah ke yayasan"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "peringatan: ") {
		t.Errorf("warning not shown: %q", out.String())
	}

	// -list: sekolah × modul dan paket; -list -module.
	out.Reset()
	if err := listSchools(ctx, tx, "", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "test-modul-b") || !strings.Contains(out.String(), "keuangan") || !strings.Contains(out.String(), "billi") {
		t.Errorf("list = %q", out.String())
	}
	out.Reset()
	if err := listSchools(ctx, tx, module.Exam, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "test-modul-a") || !regexp.MustCompile(`test-modul-a +aktif +r3 `).MatchString(out.String()) || !strings.Contains(out.String(), "sekolah dengan modul exam aktif") {
		t.Errorf("list exam = %q", out.String())
	}
	if err := do("tidak-ada", "-module", "billing", "-on", "-note", "x"); err == nil {
		t.Error("unknown school accepted")
	}
}
