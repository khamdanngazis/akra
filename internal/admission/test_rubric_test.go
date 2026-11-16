package admission

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/clock"
	"akra/internal/platform/pii"
)

func TestValidateTestPassScore(t *testing.T) {
	var ve *ValidationError
	if _, err := validateTest(TestForm{Name: "Baca", Weight: "100", MaxScore: "50", PassScore: "60"}, clock.Jakarta); !errors.As(err, &ve) || ve.Fields["pass_score"] == "" {
		t.Errorf("pass above max: %v", err)
	}
	in, err := validateTest(TestForm{Name: "Baca", Weight: "100", PassScore: "70", Rubric: "  Lancar: 80–100  "}, clock.Jakarta)
	if err != nil || in.PassScore == nil || *in.PassScore != 70 || *in.Rubric != "Lancar: 80–100" {
		t.Errorf("valid test = %+v, %v", in, err)
	}
}

func TestRankTrackBelowMinimum(t *testing.T) {
	rows := []Ranked{
		{ApplicantID: "a", Score: 90, Complete: true, Failed: []string{"Baca"}},
		{ApplicantID: "b", Score: 70, Complete: true},
		{ApplicantID: "c", Complete: false},
	}
	got := RankTrack(Track{Method: MethodTest, Quota: 5}, 0, rows)
	if got.Rows[0].ApplicantID != "b" || got.Rows[0].Rank != 1 || got.Rows[1].ApplicantID != "a" || got.Rows[1].Proposal != ApplicantRejected ||
		got.Rows[1].Rank != 0 || got.Rows[2].Proposal != ProposalIncomplete {
		t.Errorf("below-minimum ranking = %+v", got.Rows)
	}
	tests := []SelectionTest{{ID: "t1", Name: "Baca", PassScore: 60}, {ID: "t2", Name: "Hitung"}}
	if f := failedTests(tests, map[string]float64{"t1": 59.5, "t2": 10}); len(f) != 1 || f[0] != "Baca" {
		t.Errorf("failedTests = %v", f)
	}
}

func TestTestSheetPDF(t *testing.T) {
	at := time.Date(2027, 1, 10, 8, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	d := TestSheetData{SchoolName: "SD Contoh", PeriodName: "Gelombang 1", PrintedAt: at,
		Test: SelectionTest{Name: "Tes membaca", MaxScore: 100, PassScore: 60, Rubric: "Lancar: 80-100", ScheduledAt: &at, Location: "Aula"},
		Rows: []TestSheetRow{{RegistrationNumber: "SPMB-2027-0001", FullName: "Budi Santoso", TrackName: "Reguler"}}}
	if err := WriteTestSheetPDF(&buf, d, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"LEMBAR PENILAIAN Tes membaca", "Lancar: 80-100", "SPMB-2027-0001", "Budi Santoso", "minimal lulus 60", "Aula"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("PDF missing %q", want)
		}
	}
}

// TestTestRubricHandlers: rubrik dan nilai minimal lewat form, lembar PDF,
// dan nilai tes di detail pendaftar (T119).
func TestTestRubricHandlers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, _ := e.openPeriod(t)
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	tr, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Tes", Method: MethodTest})
	if err != nil {
		t.Fatal(err)
	}
	path := "/pendaftaran/periode/" + p.ID
	if rec := e.do(http.MethodPost, path+"/tes", "admin_tu", url.Values{"test_name": {"Membaca"}, "weight": {"100"}, "max_score": {"100"},
		"pass_score": {"60"}, "rubric": {"Lancar: 80-100"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add test: %d\n%s", rec.Code, rec.Body.String())
	}
	tests, _ := NewSelectionService(e.tx).Tests(ctx, e.schoolA, p.ID)
	if len(tests) != 1 || tests[0].PassScore != 60 || tests[0].Rubric != "Lancar: 80-100" {
		t.Fatalf("tests = %+v", tests)
	}
	body := e.do(http.MethodGet, path+"/tes", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Minimal lulus 60") || !strings.Contains(body, `class="rubric">Lancar: 80-100`) || !strings.Contains(body, "/lembar.pdf") {
		t.Error("tests page rubric/pass/sheet link missing")
	}
	a := e.submitted(t, apps, p, tr)
	rec := e.do(http.MethodGet, path+"/tes/"+tests[0].ID+"/lembar.pdf", "kepala_sekolah", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("sheet: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := e.do(http.MethodGet, path+"/tes/00000000-0000-0000-0000-000000000000/lembar.pdf", "admin_tu", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown test sheet: %d", rec.Code)
	}
	if rows, _ := testParticipants(ctx, e.tx, e.schoolA, p.ID); len(rows) != 1 || rows[0].FullName != a.FullName {
		t.Errorf("participants = %+v", rows)
	}

	// Nilai di bawah minimal tampil di detail pendaftar.
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
		t.Fatal(err)
	}
	if err := NewSelectionService(e.tx).SaveScores(ctx, e.actor, p.ID, tests[0].ID, map[string]string{a.ID: "55"}); err != nil {
		t.Fatal(err)
	}
	body = e.do(http.MethodGet, "/pendaftaran/"+a.ID, "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Nilai tes") || !strings.Contains(body, "<strong>55</strong>") || !strings.Contains(body, "Di bawah minimal") {
		t.Error("detail test results missing")
	}
	ranking, _ := apps.Ranking(ctx, e.schoolA, p.ID)
	for _, r := range ranking {
		if r.Track.ID == tr.ID && (len(r.Rows) != 1 || r.Rows[0].Proposal != ApplicantRejected) {
			t.Errorf("below-minimum proposal = %+v", r.Rows)
		}
	}
}
