package admission

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"akra/internal/platform/pii"
)

func TestFinalScore(t *testing.T) {
	tests := []SelectionTest{{ID: "a", Weight: 60, MaxScore: 100}, {ID: "b", Weight: 40, MaxScore: 50}}
	cases := []struct {
		scores   map[string]float64
		want     float64
		complete bool
	}{
		{map[string]float64{"a": 80, "b": 25}, 68, true},        // 48 + 20
		{map[string]float64{"a": 100, "b": 50}, 100, true},      // nilai penuh
		{map[string]float64{"a": 87.5, "b": 33.3}, 79.14, true}, // 52.5 + 26.64
		{map[string]float64{"a": 90}, 54, false},                // tes b belum dinilai
		{map[string]float64{}, 0, false},
	}
	for _, c := range cases {
		got, complete := FinalScore(tests, c.scores)
		if got != c.want || complete != c.complete {
			t.Errorf("FinalScore(%v) = %v, %v; want %v, %v", c.scores, got, complete, c.want, c.complete)
		}
	}
	if _, complete := FinalScore(nil, map[string]float64{}); complete {
		t.Error("no tests must not be complete")
	}
}

func TestSelectionTestsAndScores(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	if _, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, tr.ID, TrackForm{Name: tr.Name, UsesTest: true}); err != nil {
		t.Fatal(err)
	}
	noTest, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Tanpa tes"})
	if err != nil {
		t.Fatal(err)
	}
	sel := NewSelectionService(e.tx)
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)

	var ve *ValidationError
	if _, err := sel.AddTest(ctx, e.actor, p.ID, TestForm{Name: "", Weight: "0", ScheduledAt: "besok"}); !errors.As(err, &ve) || ve.Fields["test_name"] == "" || ve.Fields["weight"] == "" || ve.Fields["scheduled_at"] == "" {
		t.Errorf("invalid test: %v", err)
	}
	written, err := sel.AddTest(ctx, e.actor, p.ID, TestForm{Name: "Tertulis", Weight: "70", ScheduledAt: "2027-01-10T08:00", Location: "Aula"})
	if err != nil {
		t.Fatal(err)
	}
	interview, err := sel.AddTest(ctx, e.actor, p.ID, TestForm{Name: "Wawancara", Weight: "30", MaxScore: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sel.AddTest(ctx, e.actor, p.ID, TestForm{Name: "Tertulis", Weight: "5"}); !errors.As(err, &ve) || ve.Fields["test_name"] == "" {
		t.Errorf("duplicate test name: %v", err)
	}
	tests, _ := sel.Tests(ctx, e.schoolA, p.ID)
	if len(tests) != 2 || WeightsTotal(tests) != 100 || tests[0].ScheduledAt == nil || tests[0].ScheduledAt.UTC().Hour() != 1 {
		t.Fatalf("tests = %+v", tests)
	}

	verified := e.submitted(t, apps, p, tr)
	pending := e.submitted(t, apps, p, tr) // belum diverifikasi
	other := e.submitted(t, apps, p, noTest)
	for _, a := range []Applicant{verified, other} {
		if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
			t.Fatal(err)
		}
	}
	grid, err := sel.Grid(ctx, e.schoolA, p.ID, written.ID)
	if err != nil || len(grid) != 1 || grid[0].ApplicantID != verified.ID {
		t.Fatalf("grid = %+v, %v (only verified applicants on test tracks)", grid, err)
	}

	// Validasi per baris; nilai di luar grid ditolak.
	err = sel.SaveScores(ctx, e.actor, p.ID, written.ID, map[string]string{verified.ID: "101"})
	if !errors.As(err, &ve) || ve.Fields["score_"+verified.ID] == "" {
		t.Errorf("score over max: %v", err)
	}
	for _, bad := range []string{"87.155", "-1", "1e2", "abc"} {
		if err := sel.SaveScores(ctx, e.actor, p.ID, written.ID, map[string]string{verified.ID: bad}); !errors.As(err, &ve) {
			t.Errorf("score %q accepted: %v", bad, err)
		}
	}
	if err := sel.SaveScores(ctx, e.actor, p.ID, written.ID, map[string]string{pending.ID: "80"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("score for unverified applicant: %v", err)
	}
	if err := sel.SaveScores(ctx, e.actor, p.ID, written.ID, map[string]string{other.ID: "80"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("score for track without test: %v", err)
	}
	if err := sel.SaveScores(ctx, e.actor, p.ID, written.ID, map[string]string{verified.ID: "87,5"}); err != nil {
		t.Fatal(err)
	}
	if err := sel.SaveScores(ctx, e.actor, p.ID, interview.ID, map[string]string{verified.ID: "8"}); err != nil {
		t.Fatal(err)
	}
	grid, _ = sel.Grid(ctx, e.schoolA, p.ID, written.ID)
	if grid[0].Score != "87.5" {
		t.Errorf("stored score = %q", grid[0].Score)
	}
	scores, err := sel.Scores(ctx, e.schoolA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final, complete := FinalScore(tests, scores[verified.ID]); final != 85.25 || !complete { // 61.25 + 24
		t.Errorf("final = %v %v", final, complete)
	}
	// Kosong = belum dinilai (NULL), bukan nol.
	if err := sel.SaveScores(ctx, e.actor, p.ID, interview.ID, map[string]string{verified.ID: ""}); err != nil {
		t.Fatal(err)
	}
	scores, _ = sel.Scores(ctx, e.schoolA, p.ID)
	if _, complete := FinalScore(tests, scores[verified.ID]); complete {
		t.Error("cleared score must make the final score incomplete")
	}

	// Nilai maksimal tidak bisa diturunkan di bawah nilai yang ada; tes
	// bernilai tidak bisa dihapus.
	if err := sel.UpdateTest(ctx, e.actor, p.ID, written.ID, TestForm{Name: "Tertulis", Weight: "70", MaxScore: "50"}); !errors.As(err, &ve) || ve.Fields["max_score"] == "" {
		t.Errorf("lower max below existing score: %v", err)
	}
	if err := sel.DeleteTest(ctx, e.actor, p.ID, written.ID); !errors.Is(err, ErrInUse) {
		t.Errorf("delete test with scores: %v", err)
	}
	if err := sel.DeleteTest(ctx, e.actor, p.ID, interview.ID); err != nil {
		t.Errorf("delete test without scores: %v", err)
	}
	// Sekolah lain.
	actorB := e.actor
	actorB.SchoolID, actorB.UserID = e.schoolB, ""
	if _, err := sel.AddTest(ctx, actorB, p.ID, TestForm{Name: "X", Weight: "1"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("add test other school: %v", err)
	}
	if g, _ := sel.Grid(ctx, e.schoolB, p.ID, written.ID); len(g) != 0 {
		t.Error("grid leaks to other school")
	}
}

func TestSelectionHandler(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	if _, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, tr.ID, TrackForm{Name: tr.Name, UsesTest: true}); err != nil {
		t.Fatal(err)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a := e.submitted(t, apps, p, tr)
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
		t.Fatal(err)
	}
	base := "/pendaftaran/periode/" + p.ID + "/tes"
	if rec := e.do(http.MethodPost, base, "admin_tu", url.Values{"test_name": {"Tertulis"}, "weight": {"80"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add test: %d %s", rec.Code, rec.Body.String())
	}
	body := e.do(http.MethodGet, base, "admin_tu", nil).Body.String()
	if !strings.Contains(body, "Tertulis") || !strings.Contains(body, "Jumlah bobot saat ini 80%") {
		t.Error("tests page content")
	}
	tests, _ := NewSelectionService(e.tx).Tests(ctx, e.schoolA, p.ID)
	scoreURL := base + "/" + tests[0].ID + "/nilai"
	if rec := e.do(http.MethodPost, scoreURL, "admin_tu", url.Values{"score_" + a.ID: {"120"}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `value="120"`) {
		t.Errorf("invalid score: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, scoreURL, "admin_tu", url.Values{"score_" + a.ID: {"75"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("save score: %d", rec.Code)
	}
	if body := e.do(http.MethodGet, scoreURL, "kepala_sekolah", nil).Body.String(); !strings.Contains(body, `value="75"`) || strings.Contains(body, "Simpan nilai") {
		t.Error("principal score view")
	}
	if rec := e.do(http.MethodPost, scoreURL, "kepala_sekolah", url.Values{}); rec.Code != http.StatusForbidden {
		t.Errorf("principal save: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, base, "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: %d", rec.Code)
	}
}
