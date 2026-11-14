package admission

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/pii"
)

func TestRankTrack(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 12, 1, 8, m, 0, 0, time.UTC) }
	rows := []Ranked{
		{ApplicantID: "late-high", Score: 90, Complete: true, SubmittedAt: at(30)},
		{ApplicantID: "early-high", Score: 90, Complete: true, SubmittedAt: at(10)},
		{ApplicantID: "incomplete", Score: 99, Complete: false, SubmittedAt: at(1)},
		{ApplicantID: "low", Score: 70, Complete: true, SubmittedAt: at(5)},
		{ApplicantID: "mid", Score: 80, Complete: true, SubmittedAt: at(20)},
	}
	got := RankTrack(Track{UsesTest: true, Quota: 3}, 1, append([]Ranked(nil), rows...))
	var order, proposals []string
	for _, r := range got.Rows {
		order = append(order, r.ApplicantID)
		proposals = append(proposals, r.Proposal)
	}
	// Skor sama: yang kirim lebih awal di atas. Belum lengkap di bawah tanpa
	// peringkat. Kuota 3, sudah diterima 1: sisa 2 diusulkan diterima.
	if strings.Join(order, ",") != "early-high,late-high,mid,low,incomplete" {
		t.Errorf("order = %v", order)
	}
	if strings.Join(proposals, ",") != "diterima,diterima,cadangan,cadangan," || got.Remaining != 2 || got.Rows[4].Rank != 0 {
		t.Errorf("proposals = %v remaining %d", proposals, got.Remaining)
	}

	// Tanpa tes: urut waktu kirim; tanpa kuota: semua diusulkan diterima.
	got = RankTrack(Track{}, 0, append([]Ranked(nil), rows...))
	if got.Rows[0].ApplicantID != "incomplete" || got.Rows[0].Proposal != ProposalAccept || got.Rows[4].Proposal != ProposalAccept {
		t.Errorf("no-test ranking = %+v", got.Rows)
	}
	// Kuota sudah habis: semua cadangan.
	got = RankTrack(Track{Quota: 2}, 5, append([]Ranked(nil), rows...))
	if got.Remaining != 0 || got.Rows[0].Proposal != ProposalReserve {
		t.Errorf("full quota = %+v", got)
	}
}

func TestRankingAndApply(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	if _, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, tr.ID, TrackForm{Name: tr.Name, Quota: "1", UsesTest: true}); err != nil {
		t.Fatal(err)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	sel := NewSelectionService(e.tx)
	test, err := sel.AddTest(ctx, e.actor, p.ID, TestForm{Name: "Tertulis", Weight: "100"})
	if err != nil {
		t.Fatal(err)
	}
	a1 := e.submitted(t, apps, p, tr)
	a2 := e.submitted(t, apps, p, tr)
	for _, a := range []Applicant{a1, a2} {
		if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := sel.SaveScores(ctx, e.actor, p.ID, test.ID, map[string]string{a1.ID: "70", a2.ID: "85"}); err != nil {
		t.Fatal(err)
	}
	ranking, err := apps.Ranking(ctx, e.schoolA, p.ID)
	if err != nil || len(ranking) != 1 || len(ranking[0].Rows) != 2 {
		t.Fatalf("ranking = %+v, %v", ranking, err)
	}
	first, second := ranking[0].Rows[0], ranking[0].Rows[1]
	if first.ApplicantID != a2.ID || first.Proposal != ProposalAccept || second.Proposal != ProposalReserve || first.Score != 85 {
		t.Errorf("ranking rows = %+v", ranking[0].Rows)
	}

	results := map[string]string{a2.ID: ApplicantAccepted, a1.ID: ApplicantReserve}
	changed, over, err := apps.ApplyResults(ctx, e.actor, p.ID, results)
	if err != nil || changed != 2 || over {
		t.Fatalf("ApplyResults = %d %v %v", changed, over, err)
	}
	// Penetapan ulang tanpa perubahan: tidak ada riwayat baru.
	var before, after int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM applicant_events WHERE school_id = $1`, e.schoolA).Scan(&before)
	if changed, _, err := apps.ApplyResults(ctx, e.actor, p.ID, results); err != nil || changed != 0 {
		t.Errorf("second ApplyResults = %d, %v", changed, err)
	}
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM applicant_events WHERE school_id = $1`, e.schoolA).Scan(&after)
	if after != before {
		t.Errorf("events %d -> %d after repeated apply", before, after)
	}
	// Cadangan naik menjadi diterima melebihi kuota: peringatan.
	if _, over, err := apps.ApplyResults(ctx, e.actor, p.ID, map[string]string{a1.ID: ApplicantAccepted}); err != nil || !over {
		t.Errorf("over quota apply = %v %v", over, err)
	}
	// Status tujuan lain ditolak, dan semuanya dibatalkan.
	a3 := e.submitted(t, apps, p, tr)
	if _, _, err := apps.ApplyResults(ctx, e.actor, p.ID, map[string]string{a3.ID: ApplicantFix}); !errors.Is(err, ErrBadTransition) {
		t.Errorf("fix via results: %v", err)
	}
	// Pendaftar periode lain ditolak.
	p2, tr2 := e.openPeriod(t)
	b := e.submitted(t, apps, p2, tr2)
	if _, _, err := apps.ApplyResults(ctx, e.actor, p.ID, map[string]string{b.ID: ApplicantAccepted}); !errors.Is(err, ErrNotFound) {
		t.Errorf("applicant of another period: %v", err)
	}
}

func TestResultsHandler(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a := e.submitted(t, apps, p, tr)
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
		t.Fatal(err)
	}
	base := "/pendaftaran/periode/" + p.ID + "/hasil"
	body := e.do(http.MethodGet, base, "admin_tu", nil).Body.String()
	if !strings.Contains(body, a.RegistrationNumber) || !strings.Contains(body, `value="diterima" selected`) {
		t.Error("results page must propose accepting within unlimited quota")
	}
	if rec := e.do(http.MethodPost, base, "admin_tu", url.Values{"result_" + a.ID: {"diterima"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := apps.Load(ctx, e.schoolA, a.ID)
	if got.Status != ApplicantAccepted {
		t.Errorf("status = %s", got.Status)
	}
	rec := e.do(http.MethodGet, base+".xlsx", "kepala_sekolah", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheet") || rec.Body.Len() < 100 {
		t.Errorf("export: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := e.do(http.MethodPost, base, "kepala_sekolah", url.Values{}); rec.Code != http.StatusForbidden {
		t.Errorf("principal apply: %d", rec.Code)
	}
}
