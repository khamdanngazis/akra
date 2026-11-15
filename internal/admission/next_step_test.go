package admission

import (
	"strings"
	"testing"
)

func flowString(f []flowStep) string {
	var b []string
	for _, s := range f {
		b = append(b, s.Label+":"+s.State)
	}
	return strings.Join(b, " ")
}

func TestBuildFlow(t *testing.T) {
	cases := []struct {
		status, prev string
		enrolled     bool
		want         string
	}{
		{ApplicantDraft, "", false, "Dikirim:current Diverifikasi:todo Diterima:todo Jadi siswa:todo"},
		{ApplicantSubmitted, "", false, "Dikirim:done Diverifikasi:current Diterima:todo Jadi siswa:todo"},
		{ApplicantFix, "", false, "Perlu perbaikan:warn Diverifikasi:todo Diterima:todo Jadi siswa:todo"},
		{ApplicantVerified, "", false, "Dikirim:done Diverifikasi:done Diterima:current Jadi siswa:todo"},
		{ApplicantReserve, "", false, "Dikirim:done Diverifikasi:done Cadangan:warn Jadi siswa:todo"},
		{ApplicantRejected, "", false, "Dikirim:done Diverifikasi:done Ditolak:stop Jadi siswa:todo"},
		{ApplicantAccepted, "", false, "Dikirim:done Diverifikasi:done Diterima:done Jadi siswa:current"},
		{ApplicantAccepted, "", true, "Dikirim:done Diverifikasi:done Diterima:done Jadi siswa:done"},
		{ApplicantWithdrawn, ApplicantSubmitted, false, "Dikirim:done Mundur:stop Diterima:todo Jadi siswa:todo"},
		{ApplicantWithdrawn, ApplicantDraft, false, "Mundur:stop Diverifikasi:todo Diterima:todo Jadi siswa:todo"},
	}
	for _, c := range cases {
		if got := flowString(buildFlow(c.status, c.prev, c.enrolled)); got != c.want {
			t.Errorf("%s/%s: got %s, want %s", c.status, c.prev, got, c.want)
		}
	}
}

func TestBuildNextStep(t *testing.T) {
	values := func(o []option) string {
		var b []string
		for _, x := range o {
			b = append(b, x.Value)
		}
		return strings.Join(b, ",")
	}
	open := Track{ID: "t", Name: "Reguler"}
	quota := Track{ID: "t", Name: "Prestasi", Quota: 10}
	p := Period{ID: "p"}

	v := buildNextStep(nextStepInput{Applicant: Applicant{Status: ApplicantSubmitted}, Period: p, Track: open, MissingDocs: []string{"Akta"}})
	if values(v.Primary) != ApplicantVerified || values(v.Other) != "perlu_perbaikan,mundur" || !strings.Contains(v.Hint, "Akta") || v.Blocked != "" {
		t.Errorf("submitted: %+v", v)
	}
	v = buildNextStep(nextStepInput{Applicant: Applicant{Status: ApplicantSubmitted}, Period: p, Track: open, FeeUnpaid: true})
	if v.Blocked == "" || len(v.Links) != 1 {
		t.Errorf("fee unpaid: %+v", v)
	}
	// Jalur tanpa tes/kuota: terima langsung. Jalur berkuota: ke peringkat.
	v = buildNextStep(nextStepInput{Applicant: Applicant{Status: ApplicantVerified}, Period: p, Track: open})
	if values(v.Primary) != ApplicantAccepted || len(v.Links) != 0 {
		t.Errorf("verified open: %+v", v)
	}
	v = buildNextStep(nextStepInput{Applicant: Applicant{Status: ApplicantVerified}, Period: p, Track: quota})
	if len(v.Primary) != 0 || len(v.Links) != 1 || !v.Links[0].Primary || v.Links[0].URL != "/pendaftaran/periode/p/hasil" || !strings.Contains(v.Hint, "kuota") {
		t.Errorf("verified quota: %+v", v)
	}
	v = buildNextStep(nextStepInput{Applicant: Applicant{Status: ApplicantAccepted}, Period: p, Track: open})
	if len(v.Links) != 1 || v.Links[0].URL != "/pendaftaran/terima" || values(v.Other) != ApplicantWithdrawn {
		t.Errorf("accepted: %+v", v)
	}
	v = buildNextStep(nextStepInput{Applicant: Applicant{Status: ApplicantAccepted}, Period: p, Track: open, StudentID: "s1"})
	if len(v.Other) != 0 || v.Links[0].URL != "/siswa/s1" {
		t.Errorf("enrolled: %+v", v)
	}
	for _, s := range []string{ApplicantRejected, ApplicantWithdrawn} {
		if v := buildNextStep(nextStepInput{Applicant: Applicant{Status: s}, Period: p, Track: open}); len(v.Primary)+len(v.Other)+len(v.Links) != 0 {
			t.Errorf("%s has actions: %+v", s, v)
		}
	}
}
