package module_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/platform/module"
	"akra/internal/testutil"
)

func fixture(t *testing.T) (pgx.Tx, *testutil.School) {
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
	a, _ := testutil.SeedTwoSchools(t, tx, "x")
	t.Cleanup(func() { module.Forget(a.ID) })
	return tx, a
}

// Penjaga saat mematikan modul (T168): ujian dengan sesi berlangsung dan
// pendaftaran dengan periode dibuka ditolak (force tidak berlaku);
// keuangan dengan tagihan belum lunas hanya peringatan (butuh force).
func TestAdminGuards(t *testing.T) {
	tx, a := fixture(t)
	ctx := context.Background()
	admin := module.Admin{By: "uji"}
	var exam string
	if err := tx.QueryRow(ctx, `SELECT id FROM exams WHERE school_id = $1 LIMIT 1`, a.ID).Scan(&exam); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO exam_sessions (school_id, exam_id, starts_at, ends_at, created_by)
		VALUES ($1, $2, now() + interval '2 hours', now() + interval '3 hours', $3)`, a.ID, exam, a.Admin.ID); err != nil {
		t.Fatal(err)
	}
	var g *module.GuardError
	if _, err := admin.Disable(ctx, tx, a.ID, module.Exam, "uji", true); !errors.As(err, &g) || g.Forceable {
		t.Errorf("exam with a session in 2 hours: %v", err)
	}
	if _, err := admin.Disable(ctx, tx, a.ID, module.Admission, "uji", true); !errors.As(err, &g) || g.Module != module.Admission {
		t.Errorf("admission with an open period: %v", err)
	}
	if _, err := admin.Disable(ctx, tx, a.ID, module.Billing, "uji", false); !errors.As(err, &g) || !g.Forceable {
		t.Errorf("billing with unpaid invoices: %v", err)
	}
	res, err := admin.Disable(ctx, tx, a.ID, module.Billing, "kontrak selesai", true)
	if err != nil || len(res.Warnings) != 1 || len(res.Changed) != 1 {
		t.Fatalf("billing with force = %+v, %v", res, err)
	}
	// Audit mencatat catatan dan pelaku.
	var after string
	if err := tx.QueryRow(ctx, `SELECT after::text FROM audit_logs WHERE school_id = $1 AND action = 'module.billing'`, a.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "kontrak selesai") || !strings.Contains(after, `"by": "uji"`) {
		t.Errorf("audit after = %s", after)
	}
	if _, err := admin.Enable(ctx, tx, a.ID, module.Billing, nil, " "); !errors.Is(err, module.ErrNoteRequired) {
		t.Errorf("empty note: %v", err)
	}
	if _, err := admin.Enable(ctx, tx, a.ID, module.Core, nil, "x"); !errors.Is(err, module.ErrCore) {
		t.Errorf("core: %v", err)
	}
}

// Paket (T168): diterapkan dalam satu transaksi. Bila satu modul ditolak
// penjaga, tidak ada yang berubah; bila lolos, modul dan label paket
// sesuai.
func TestAdminApplyPackage(t *testing.T) {
	tx, a := fixture(t)
	ctx := context.Background()
	admin := module.Admin{By: "uji"}
	// Paket Dasar mematikan pendaftaran yang periodenya sedang dibuka.
	if _, err := admin.ApplyPackage(ctx, tx, a.ID, "dasar", "uji", true); err == nil {
		t.Fatal("package applied over an open admission period")
	}
	set, _ := module.Load(ctx, tx, a.ID)
	var pkg string
	tx.QueryRow(ctx, `SELECT package FROM schools WHERE id = $1`, a.ID).Scan(&pkg)
	if !set.Has(module.Billing) || !set.Has(module.Admission) || pkg != "" {
		t.Errorf("rejected package changed modules: %v, package %q", set.IDs(), pkg)
	}
	if _, err := tx.Exec(ctx, `UPDATE admission_periods SET status = 'tutup' WHERE school_id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET cancelled_at = now() WHERE school_id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	res, err := admin.ApplyPackage(ctx, tx, a.ID, "akademik", "paket akademik", true)
	if err != nil {
		t.Fatal(err)
	}
	set, _ = module.Load(ctx, tx, a.ID)
	tx.QueryRow(ctx, `SELECT package FROM schools WHERE id = $1`, a.ID).Scan(&pkg)
	got := strings.Join(set.IDs(), ",")
	if got != "core,attendance,schedule,grades,dapodik,announcements,portal" || pkg != "akademik" || len(res.Changed) != 3 {
		t.Errorf("akademik = %s, package %q, changed %v", got, pkg, res.Changed)
	}
	// Pilihan ujian yang sudah ada dipertahankan saat paket menyalakannya lagi.
	if _, err := admin.ApplyPackage(ctx, tx, a.ID, "lengkap", "lengkap", true); err != nil {
		t.Fatal(err)
	}
	if set, _ := module.Load(ctx, tx, a.ID); set.Option(module.Exam, "release") != "r3" || !set.Has(module.Exam) {
		t.Errorf("lengkap exam release = %q", set.Option(module.Exam, "release"))
	}
	if _, err := admin.ApplyPackage(ctx, tx, a.ID, "premium", "x", false); !errors.Is(err, module.ErrUnknownPackage) {
		t.Errorf("unknown package: %v", err)
	}
	if _, err := admin.Enable(ctx, tx, a.ID, module.Exam, map[string]string{"warna": "biru"}, "x"); !errors.Is(err, module.ErrBadOption) {
		t.Errorf("unknown option: %v", err)
	}
}

// Paket di katalog hanya berisi modul yang dikenal dan memenuhi
// ketergantungan wajib.
func TestPackagesInCatalog(t *testing.T) {
	for _, p := range module.Packages {
		in := module.NewSet(p.Modules...)
		for _, id := range p.Modules {
			info, ok := module.Get(id)
			if !ok || id == module.Core {
				t.Errorf("%s: module %q", p.Code, id)
			}
			for _, r := range info.Requires {
				if !in.Has(r) {
					t.Errorf("%s: %s needs %s", p.Code, id, r)
				}
			}
		}
	}
	if _, ok := module.GetPackage(module.DefaultPackage); !ok {
		t.Error("default package not in catalog")
	}
}

// Satu-satunya jalan mengubah school_modules dan schools.package adalah
// module.Admin (T168): kode aplikasi lain tidak menulisnya (uji, fixture
// uji internal/testutil, dan migrasi boleh).
func TestOnlyAdminWritesModules(t *testing.T) {
	root := "../../.."
	write := regexp.MustCompile(`(?i)(INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+school_modules|UPDATE\s+schools\s+SET\s+package`)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "tmp", "bin", "web", "db", "doc", "spike":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(filepath.ToSlash(p), "internal/platform/module/") ||
			strings.Contains(filepath.ToSlash(p), "internal/testutil/") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if write.Match(b) {
			t.Errorf("%s writes school_modules or schools.package outside module.Admin", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
