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

func TestParseFormatScore(t *testing.T) {
	for in, want := range map[string]int{"85": 8500, "85,5": 8550, "85.50": 8550, "0": 0, "100,05": 10005} {
		if got, ok := parseScore(in); !ok || got != want {
			t.Errorf("parseScore(%q) = %d, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "-1", "8,555", "abc", ",5", "1e3"} {
		if _, ok := parseScore(bad); ok {
			t.Errorf("parseScore(%q) accepted", bad)
		}
	}
	for in, want := range map[int]string{8550: "85,5", 9000: "90", 8505: "85,05", 0: "0"} {
		if got := formatScore(in); got != want {
			t.Errorf("formatScore(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRankTrackMethods(t *testing.T) {
	rows := []Ranked{
		{ApplicantID: "a", TrackScore: 8000, Complete: true, ManualRank: 2},
		{ApplicantID: "b", TrackScore: 9000, Complete: true, ManualRank: 1},
		{ApplicantID: "c", Complete: false},
	}
	order := func(tr TrackRanking) string {
		var b []string
		for _, r := range tr.Rows {
			b = append(b, r.ApplicantID+":"+r.Proposal)
		}
		return strings.Join(b, " ")
	}
	if got := order(RankTrack(Track{Method: MethodScore, Quota: 1}, 0, append([]Ranked(nil), rows...))); got != "b:diterima a:cadangan c:" {
		t.Errorf("score ranking = %s", got)
	}
	manual := append([]Ranked(nil), rows...)
	manual[0].ManualRank, manual[1].ManualRank = 1, 2
	if got := order(RankTrack(Track{Method: MethodManual}, 0, manual)); got != "a:diterima b:diterima c:" {
		t.Errorf("manual ranking = %s", got)
	}
	// Urutan daftar: semua ikut peringkat meski Complete belum diisi.
	if got := order(RankTrack(Track{Method: MethodOrder}, 0, append([]Ranked(nil), rows...))); strings.Contains(got, "c: ") || strings.HasSuffix(got, "c:") {
		t.Errorf("order ranking excluded applicant: %s", got)
	}
}

func TestValidateTrackMethod(t *testing.T) {
	var ve *ValidationError
	if _, err := validateTrack(TrackForm{Name: "Prestasi", Method: MethodScore}); !errors.As(err, &ve) || ve.Fields["score_label"] == "" || ve.Fields["score_max"] == "" {
		t.Errorf("score track without label/max: %v", err)
	}
	if _, err := validateTrack(TrackForm{Name: "X", Method: "jarak"}); !errors.As(err, &ve) || ve.Fields["method"] == "" {
		t.Errorf("unknown method: %v", err)
	}
	in, err := validateTrack(TrackForm{Name: "Lama", UsesTest: true})
	if err != nil || in.Method != MethodTest {
		t.Errorf("legacy uses_test = %+v, %v", in, err)
	}
}

// TestScoreTrackFlow: jalur metode nilai dari formulir sampai peringkat,
// koreksi TU, urutan manual, dan verifikasi massal (T118).
func TestScoreTrackFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, _ := e.openPeriod(t)
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	tr, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Prestasi", Method: MethodScore, ScoreLabel: "Rata-rata rapor", ScoreMax: "100"})
	if err != nil || tr.Method != MethodScore || tr.UsesTest {
		t.Fatalf("add score track: %+v %v", tr, err)
	}
	draft := func() Applicant {
		a, _, err := apps.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
		if err != nil {
			t.Fatal(err)
		}
		fill(t, apps, e.schoolA, a.ID)
		if _, err := apps.SaveStep(ctx, e.schoolA, a.ID, StepChild, map[string]string{"full_name": a.FullName, "gender": "L", "birth_date": "2019-05-17",
			"nik": "32010101" + digitsOf(a.ID, 8), "kk_number": childKK, "birth_place": "Bandung", "religion": "01"}); err != nil {
			t.Fatal(err)
		}
		got, _ := apps.Load(ctx, e.schoolA, a.ID)
		return got
	}
	a := draft()
	// Nilai belum diisi: kurang saat kirim.
	if m := Missing(a); len(m) == 0 || m[len(m)-1].Key != TrackScoreKey {
		t.Errorf("missing without score = %+v", m)
	}
	var ve *ValidationError
	if _, err := apps.SaveStep(ctx, e.schoolA, a.ID, StepSchool, map[string]string{TrackScoreKey: "120"}); !errors.As(err, &ve) || ve.Fields[TrackScoreKey] == "" {
		t.Errorf("score above max: %v", err)
	}
	if _, err := apps.SaveStep(ctx, e.schoolA, a.ID, StepSchool, map[string]string{TrackScoreKey: "80,5"}); err != nil {
		t.Fatal(err)
	}
	if a, err = apps.Submit(ctx, e.schoolA, a.ID); err != nil || a.ScoreText() != "80,5" {
		t.Fatalf("submit with score: %q %v", a.ScoreText(), err)
	}
	b := draft()
	if _, err := apps.SaveStep(ctx, e.schoolA, b.ID, StepSchool, map[string]string{TrackScoreKey: "92"}); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Submit(ctx, e.schoolA, b.ID); err != nil {
		t.Fatal(err)
	}

	// Detail: nilai tampil dan bisa dikoreksi.
	if body := e.do(http.MethodGet, "/pendaftaran/"+a.ID, "admin_tu", nil).Body.String(); !strings.Contains(body, "<strong>80,5</strong>") || !strings.Contains(body, "/nilai-jalur") {
		t.Error("score card missing on detail")
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/nilai-jalur", "admin_tu", url.Values{TrackScoreKey: {"95"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("correct score: %d", rec.Code)
	}

	// Verifikasi massal dari daftar, lalu peringkat mengikuti nilai.
	if body := e.do(http.MethodGet, "/pendaftaran?jalur="+tr.ID, "admin_tu", nil).Body.String(); !strings.Contains(body, `name="pick" value="`+a.ID+`"`) {
		t.Error("bulk verify checkbox missing")
	}
	rec := e.do(http.MethodPost, "/pendaftaran/verifikasi", "admin_tu", url.Values{"pick": {a.ID, b.ID}, "kembali": {"jalur=" + tr.ID}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/pendaftaran?jalur="+tr.ID {
		t.Fatalf("bulk verify: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := apps.Load(ctx, e.schoolA, b.ID); got.Status != ApplicantVerified {
		t.Errorf("bulk verify status = %s", got.Status)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/verifikasi", "admin_tu", url.Values{"pick": {"00000000-0000-0000-0000-000000000000"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("bulk verify unknown id: %d", rec.Code)
	}
	ranking, err := apps.Ranking(ctx, e.schoolA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range ranking {
		if r.Track.ID == tr.ID && (len(r.Rows) != 2 || r.Rows[0].ApplicantID != a.ID || r.Rows[0].ScoreText() != "95") {
			t.Errorf("score ranking rows = %+v", r.Rows)
		}
	}
	body := e.do(http.MethodGet, "/pendaftaran/periode/"+p.ID+"/hasil", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "rata-rata rapor tertinggi") || !strings.Contains(body, "Nilai rapor/prestasi") {
		t.Error("results page basis missing")
	}
	// Setelah diverifikasi, nilai tetap bisa dikoreksi; setelah diterima tidak.
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantAccepted, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetTrackScore(ctx, e.actor, a.ID, "50"); !errors.Is(err, ErrBadTransition) {
		t.Errorf("correct accepted score: %v", err)
	}

	// Urutan manual: jalur diubah ke metode manual, TU mengisi urutan.
	if _, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, tr.ID, TrackForm{Name: "Prestasi", Method: MethodManual}); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/periode/"+p.ID+"/hasil", "admin_tu", url.Values{"action": {"urutan"}, "rank_" + b.ID: {"0"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid rank: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/periode/"+p.ID+"/hasil", "admin_tu", url.Values{"action": {"urutan"}, "rank_" + b.ID: {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("save ranks: %d", rec.Code)
	}
	if got, _ := apps.Load(ctx, e.schoolA, b.ID); got.ManualRank == nil || *got.ManualRank != 1 {
		t.Errorf("manual rank = %v", got.ManualRank)
	}
	if body := e.do(http.MethodGet, "/pendaftaran/periode/"+p.ID+"/hasil", "admin_tu", nil).Body.String(); !strings.Contains(body, `name="rank_`+b.ID+`" value="1"`) {
		t.Error("manual rank input missing")
	}
	// Ringkasan alur seleksi di detail gelombang.
	if body := e.do(http.MethodGet, "/pendaftaran/periode/"+p.ID, "admin_tu", nil).Body.String(); !strings.Contains(body, "Alur seleksi") || !strings.Contains(body, "Verifikasi berkas") {
		t.Error("selection flow card missing")
	}
}
