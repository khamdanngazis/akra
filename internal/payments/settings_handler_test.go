package payments

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"akra/internal/billing"
	"akra/internal/platform/clock"
)

func TestFinanceSettingsHandler(t *testing.T) {
	f := newVoidFixture(t)
	c := newClient(t, f.fixture)
	ctx := context.Background()
	form := func(v string) url.Values { return url.Values{"void_approval_threshold": {v}} }

	for _, roles := range []string{"guru", "orang_tua"} {
		if rec := c.do(http.MethodGet, "/pengaturan/keuangan", roles, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s GET: status = %d, want 403", roles, rec.Code)
		}
	}
	for _, roles := range []string{"guru", "orang_tua", "bendahara", "admin_tu"} {
		if rec := c.do(http.MethodPost, "/pengaturan/keuangan", roles, form("1.000.000")); rec.Code != http.StatusForbidden {
			t.Errorf("%s POST: status = %d, want 403", roles, rec.Code)
		}
	}
	for _, roles := range []string{"bendahara", "admin_tu"} {
		body := c.do(http.MethodGet, "/pengaturan/keuangan", roles, nil).Body.String()
		if !strings.Contains(body, "Belum diatur") || strings.Contains(body, `name="void_approval_threshold"`) {
			t.Errorf("%s: page must be read-only", roles)
		}
	}

	c.user = f.kepala.UserID
	if body := c.do(http.MethodGet, "/pengaturan/keuangan", "kepala_sekolah", nil).Body.String(); !strings.Contains(body, `name="void_approval_threshold"`) {
		t.Fatal("principal must see the form")
	}
	for _, bad := range []string{"", "abc", "-5", "1.5", "2.000.000.000.000"} {
		if rec := c.do(http.MethodPost, "/pengaturan/keuangan", "kepala_sekolah", form(bad)); rec.Code != http.StatusBadRequest {
			t.Errorf("threshold %q: status = %d, want 400", bad, rec.Code)
		}
	}
	rec := c.do(http.MethodPost, "/pengaturan/keuangan", "kepala_sekolah", form("200.000"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: status = %d", rec.Code)
	}
	body := c.do(http.MethodGet, "/pengaturan/keuangan", "kepala_sekolah", nil).Body.String()
	if !strings.Contains(body, "diubah menjadi Rp200.000") || !strings.Contains(body, `value="200.000"`) {
		t.Error("page after save")
	}
	c.do(http.MethodPost, "/pengaturan/keuangan", "kepala_sekolah", form("Rp 150.000"))
	s, err := GetFinanceSettings(ctx, f.tx, f.schoolA)
	if err != nil || !s.IsSet || s.VoidApprovalThreshold != 150000 {
		t.Errorf("settings = %+v, %v", s, err)
	}
	if s, _ := GetFinanceSettings(ctx, f.tx, f.schoolB); s.IsSet {
		t.Error("setting leaked to another school")
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE action = 'settings.update' AND school_id = $1 AND user_id = $2`, f.schoolA, f.kepala.UserID); n != 2 {
		t.Errorf("settings.update audits = %d, want 2", n)
	}
	var before, after string
	_ = f.tx.QueryRow(ctx, `SELECT before::text, after::text FROM audit_logs WHERE action = 'settings.update' ORDER BY id DESC LIMIT 1`).Scan(&before, &after)
	if !strings.Contains(before, "200000") || !strings.Contains(after, "150000") {
		t.Errorf("audit before = %s, after = %s", before, after)
	}

	// Ambang Rp150.000: pembatalan Rp150.000 langsung berlaku.
	p := f.pay(t, f.invA.ID, 150000, clock.Today())
	v, err := f.voids.Request(ctx, f.actor, p.ID, "uji ambang")
	if err != nil || v.Status != VoidApproved {
		t.Errorf("void at threshold = %+v, %v; want approved immediately", v, err)
	}
	if st := f.status(t, f.invA.ID); st != billing.StatusUnpaid {
		t.Errorf("status = %s", st)
	}
}
