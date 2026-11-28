package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/module"
	"akra/internal/testutil"
)

// Modul per sekolah (T163): perubahan status modul terlihat di menu tanpa
// login ulang, paling lama module.CacheTTL (jam palsu).
func TestModuleChangeWithoutRelogin(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	ctx := context.Background()
	clock := time.Now()
	module.Now = func() time.Time { return clock }
	t.Cleanup(func() { module.Now = time.Now; module.Forget(a.ID) })

	cl := newClient(t, app)
	if rec := cl.login(a.Code, a.Admin.Email, testutil.Password); rec.Code != http.StatusSeeOther {
		t.Fatalf("login: %d", rec.Code)
	}
	menu := func() string { return between(cl.get("/").Body.String(), `<nav id="app-nav"`, "</nav>") }
	if m := menu(); !strings.Contains(m, `href="/absensi/rekap"`) || !strings.Contains(m, `href="/ujian"`) {
		t.Fatalf("menu before:\n%s", m)
	}
	// Implementator mematikan absensi dan ujian langsung di database (replika
	// lain): menu replika ini berubah setelah cache habis, tanpa login ulang.
	if _, err := tx.Exec(ctx, `INSERT INTO school_modules (school_id, module, enabled) VALUES ($1, 'attendance', false)
		ON CONFLICT (school_id, module) DO UPDATE SET enabled = false`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE school_modules SET enabled = false WHERE school_id = $1 AND module = 'exam'`, a.ID); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(module.CacheTTL + time.Second)
	m := menu()
	if strings.Contains(m, `href="/absensi/rekap"`) || strings.Contains(m, `href="/ujian"`) {
		t.Errorf("menu still shows disabled modules:\n%s", m)
	}
	if !strings.Contains(m, `href="/siswa"`) || !strings.Contains(m, `href="/keuangan/tunggakan"`) {
		t.Errorf("menu lost core or billing:\n%s", m)
	}
	// Diaktifkan lagi lewat module.Save di replika ini: langsung terlihat.
	if err := module.Save(ctx, tx, a.ID, module.Attendance, module.State{Enabled: true}, "uji", nil); err != nil {
		t.Fatal(err)
	}
	if m := menu(); !strings.Contains(m, `href="/absensi/rekap"`) {
		t.Errorf("menu after re-enable:\n%s", m)
	}
}
