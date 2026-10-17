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

func TestVoidHandler(t *testing.T) {
	f := newVoidFixture(t)
	c := newClient(t, f.fixture)
	ctx := context.Background()
	p := f.pay(t, f.invA.ID, 150000, clock.Today())
	path := "/keuangan/pembayaran/" + p.ID + "/batal"
	reason := url.Values{"reason": {"transfer tercatat dua kali"}}

	for _, roles := range []string{"guru", "orang_tua", "admin_tu", "kepala_sekolah"} {
		if rec := c.do(http.MethodPost, path, roles, reason); rec.Code != http.StatusForbidden {
			t.Errorf("%s request void: status = %d, want 403", roles, rec.Code)
		}
	}
	for _, roles := range []string{"guru", "orang_tua", "admin_tu", "bendahara"} {
		if rec := c.do(http.MethodGet, "/keuangan/pembatalan", roles, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s approvals page: status = %d, want 403", roles, rec.Code)
		}
	}

	detail := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "bendahara", nil).Body.String()
	if !strings.Contains(detail, `action="`+path+`"`) {
		t.Fatal("void form missing for a payment of this month")
	}
	if rec := c.do(http.MethodPost, path, "bendahara", url.Values{"reason": {"  "}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Alasan pembatalan wajib diisi.") {
		t.Errorf("no reason: status = %d", rec.Code)
	}

	// Ambang belum diatur: menunggu persetujuan.
	rec := c.do(http.MethodPost, path, "bendahara", reason)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/keuangan/tagihan/"+f.invA.ID {
		t.Fatalf("request: status = %d", rec.Code)
	}
	detail = c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "bendahara", nil).Body.String()
	for _, want := range []string{"menunggu persetujuan kepala sekolah", "Menunggu persetujuan pembatalan", "transfer tercatat dua kali"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	if strings.Contains(detail, `action="`+path+`"`) {
		t.Error("void form must be hidden while a void is pending")
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusPaid {
		t.Errorf("status while pending = %s", s)
	}

	// Pengaju yang juga kepala sekolah tidak bisa menyetujui.
	pending, _ := f.voids.ListPending(ctx, f.schoolA)
	approve := "/keuangan/pembatalan/" + pending[0].ID + "/setujui"
	page := c.do(http.MethodGet, "/keuangan/pembatalan", "kepala_sekolah", nil).Body.String()
	if !strings.Contains(page, "Anda yang mengajukan") || strings.Contains(page, `action="`+approve+`"`) {
		t.Error("requester must not get approve buttons")
	}
	if rec := c.do(http.MethodPost, approve, "bendahara,kepala_sekolah", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "tidak bisa memutuskan") {
		t.Errorf("self approval: status = %d, want 403", rec.Code)
	}

	// Kepala sekolah lain menyetujui.
	c.user = f.kepala.UserID
	page = c.do(http.MethodGet, "/keuangan/pembatalan", "kepala_sekolah", nil).Body.String()
	if !strings.Contains(page, `action="`+approve+`"`) || !strings.Contains(page, "Rp150.000") {
		t.Fatal("approvals page does not list the request")
	}
	rec = c.do(http.MethodPost, approve, "kepala_sekolah", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("approve: status = %d", rec.Code)
	}
	if s := f.status(t, f.invA.ID); s != billing.StatusUnpaid {
		t.Errorf("status after approval = %s, want belum_bayar", s)
	}
	if page := c.do(http.MethodGet, "/keuangan/pembatalan", "kepala_sekolah", nil).Body.String(); !strings.Contains(page, "Pembatalan disetujui.") || !strings.Contains(page, "Tidak ada pengajuan") {
		t.Error("approvals page after approval")
	}
	if rec := c.do(http.MethodPost, approve, "kepala_sekolah", nil); rec.Code != http.StatusConflict {
		t.Errorf("approve twice: status = %d, want 409", rec.Code)
	}
	if rec := c.do(http.MethodPost, "/keuangan/pembatalan/00000000-0000-0000-0000-000000000000/tolak", "kepala_sekolah", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown void: status = %d, want 404", rec.Code)
	}

	c.user = ""
	detail = c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "bendahara", nil).Body.String()
	if !strings.Contains(detail, "is-voided") || !strings.Contains(detail, ">Dibatalkan<") || !strings.Contains(detail, "Catat pembayaran") {
		t.Error("detail after approval must show the voided payment and allow paying again")
	}
	if rec := c.do(http.MethodPost, path, "bendahara", reason); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sudah dibatalkan") {
		t.Errorf("void twice: status = %d", rec.Code)
	}
}

func TestInvoiceDetailAuditLink(t *testing.T) {
	f := newVoidFixture(t)
	c := newClient(t, f.fixture)
	p := f.pay(t, f.invA.ID, 1000, clock.Today())
	link := `href="/keuangan/audit?entity=` + p.ID + `"`
	for roles, want := range map[string]bool{"kepala_sekolah": true, "admin_tu": true, "bendahara": false} {
		body := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, roles, nil).Body.String()
		if strings.Contains(body, link) != want {
			t.Errorf("%s: audit link shown = %v, want %v", roles, !want, want)
		}
	}
}
