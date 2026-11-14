package admission

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"akra/internal/platform/pii"
)

func TestSummary(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	if _, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, tr.ID, TrackForm{Name: tr.Name, Quota: "5"}); err != nil {
		t.Fatal(err)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a1 := e.submitted(t, apps, p, tr)
	e.submitted(t, apps, p, tr)
	if _, _, err := apps.CreateDraft(ctx, e.schoolA, draftForm(p, tr), ""); err != nil { // draf tidak dihitung
		t.Fatal(err)
	}
	for _, to := range []string{ApplicantVerified, ApplicantAccepted} {
		if _, err := apps.Transition(ctx, e.actor, a1.ID, to, ""); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := apps.Summarize(ctx, e.schoolA, p.ID, e.svc.Today())
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]int{}
	for _, c := range sum.ByStatus {
		status[c.Label] = c.N
	}
	total := 0
	for _, n := range status {
		total += n
	}
	if sum.Total != 2 || sum.Drafts != 1 || status["Diterima"] != 1 || status["Dikirim"] != 1 || total != sum.Total {
		t.Errorf("summary = %+v (status %v)", sum, status)
	}
	if len(sum.ByTrack) != 1 || sum.ByTrack[0].Submitted != 2 || sum.ByTrack[0].Accepted != 1 || sum.ByTrack[0].Remaining != 4 {
		t.Errorf("tracks = %+v", sum.ByTrack)
	}
	if len(sum.BySchool) != 1 || sum.BySchool[0].Label != "TK Melati" || sum.BySchool[0].N != 2 {
		t.Errorf("schools = %+v", sum.BySchool)
	}
	// Daftar pendaftar untuk filter yang sama berisi jumlah yang sama.
	rows, _ := apps.List(ctx, e.schoolA, ListFilter{PeriodID: p.ID})
	if len(rows) != sum.Total {
		t.Errorf("list rows = %d, summary total = %d", len(rows), sum.Total)
	}
	if other, _ := apps.Summarize(ctx, e.schoolB, "", e.svc.Today()); other.Total != 0 {
		t.Error("summary leaks to other school")
	}

	body := e.do(http.MethodGet, "/pendaftaran/rekap?periode="+p.ID, "kepala_sekolah", nil).Body.String()
	if !strings.Contains(body, "Rekap pendaftaran") || !strings.Contains(body, "4 dari 5") {
		t.Error("summary page content")
	}
	rec := e.do(http.MethodGet, "/pendaftaran/rekap/pendaftar.xlsx?periode="+p.ID, "admin_tu", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() < 100 {
		t.Errorf("export: %d", rec.Code)
	}
	var audits int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, e.schoolA, ActionApplicantExport).Scan(&audits)
	if audits != 1 {
		t.Errorf("export audits = %d", audits)
	}
	if rec := e.do(http.MethodGet, "/pendaftaran/rekap", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: %d", rec.Code)
	}
}
