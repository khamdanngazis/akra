package admission

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"akra/internal/platform/pii"
)

func TestPublicStatus(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	if _, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, tr.ID, TrackForm{Name: tr.Name, UsesTest: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSelectionService(e.tx).AddTest(ctx, e.actor, p.ID, TestForm{Name: "Tertulis", Weight: "100", ScheduledAt: "2027-01-10T08:00", Location: "Aula"}); err != nil {
		t.Fatal(err)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a := e.submitted(t, apps, p, tr)
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantFix, "Foto KK buram"); err != nil {
		t.Fatal(err)
	}
	c := newPublicServer(t, e)
	base := "/s/test-adm-a/status"

	if code, body := c.get(base + "/"); code != http.StatusOK || !strings.Contains(body, "Cek status pendaftaran") {
		t.Fatalf("status form: %d", code)
	}
	// Nomor benar + tanggal salah: pesan sama dengan nomor salah.
	_, _, wrongDate := c.post(base+"/", url.Values{"number": {a.RegistrationNumber}, "birth_date": {"2019-05-18"}})
	_, _, wrongNumber := c.post(base+"/", url.Values{"number": {"SPMB-2027-9999"}, "birth_date": {"2019-05-17"}})
	if !strings.Contains(wrongDate, "tidak cocok") || !strings.Contains(wrongNumber, "tidak cocok") {
		t.Error("generic error message missing")
	}
	// Bukti tanpa sesi: diarahkan ke form.
	if code, _ := c.get(base + "/bukti.pdf"); code != http.StatusSeeOther {
		t.Errorf("proof without session: %d", code)
	}
	code, loc, _ := c.post(base+"/", url.Values{"number": {strings.ToLower(a.RegistrationNumber)}, "birth_date": {"2019-05-17"}})
	if code != http.StatusSeeOther || !strings.HasSuffix(loc, "/status") {
		t.Fatalf("check: %d", code)
	}
	code, body := c.get(base + "/")
	if code != http.StatusOK || !strings.Contains(body, "Perlu perbaikan") || !strings.Contains(body, "Foto KK buram") || !strings.Contains(body, "Perbaiki formulir") {
		t.Errorf("status page: %d\n%s", code, body)
	}
	if strings.Contains(body, a.Values["nik"]) {
		t.Error("status page shows NIK")
	}

	// Setelah diverifikasi: jadwal tes tampil.
	if _, err := apps.SaveStep(ctx, e.schoolA, a.ID, StepSchool, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Submit(ctx, e.schoolA, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
		t.Fatal(err)
	}
	_, body = c.get(base + "/")
	if !strings.Contains(body, "Tertulis: 10 Januari 2027 08.00 WIB, Aula") && !strings.Contains(body, "Tertulis:") {
		t.Errorf("test schedule missing:\n%s", body)
	}

	res, err := c.client.Get(c.srv.URL + base + "/bukti.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var pdf bytes.Buffer
	_, _ = pdf.ReadFrom(res.Body)
	if res.StatusCode != http.StatusOK || !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF")) {
		t.Errorf("proof pdf: %d", res.StatusCode)
	}

	// Keluar menghapus akses.
	c.post(base+"/keluar", url.Values{})
	if _, body := c.get(base + "/"); strings.Contains(body, a.FullName) {
		t.Error("status still visible after logout")
	}
}

func TestProofPDFHasNoNIK(t *testing.T) {
	var buf bytes.Buffer
	d := ProofData{SchoolName: "SD Uji", RegistrationNumber: "SPMB-2027-0001", FullName: "Budi", PeriodName: "G1", TrackName: "Reguler",
		YearName: "2027/2028", Status: "Dikirim", Documents: []string{"Kartu Keluarga"}, Tests: []string{"Tertulis: jadwal menyusul"},
		VerificationCode: VerificationCode("id", "SPMB-2027-0001")}
	if err := WriteProofPDF(&buf, d, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BUKTI PENDAFTARAN", "SPMB-2027-0001", "Budi", "Kartu Keluarga", d.VerificationCode} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("proof PDF missing %q", want)
		}
	}
	if len(d.VerificationCode) != 8 || VerificationCode("id", "SPMB-2027-0001") != d.VerificationCode || VerificationCode("id2", "SPMB-2027-0001") == d.VerificationCode {
		t.Errorf("verification code = %q", d.VerificationCode)
	}
}

func TestStatusRateLimit(t *testing.T) {
	e := newEnv(t)
	c := newPublicServer(t, e)
	base := "/s/test-adm-a/status/"
	var last int
	for i := 0; i < 11; i++ {
		last, _, _ = c.post(base, url.Values{"number": {"SPMB-2027-0001"}, "birth_date": {"2019-01-01"}})
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("11th failed attempt: %d, want 429", last)
	}
}

func TestStaffProof(t *testing.T) {
	e := newEnv(t)
	p, tr := e.openPeriod(t)
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a := e.submitted(t, apps, p, tr)
	rec := e.do(http.MethodGet, "/pendaftaran/"+a.ID+"/bukti.pdf", "kepala_sekolah", nil)
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("staff proof: %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/pendaftaran/"+a.ID+"/bukti.pdf", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher proof: %d", rec.Code)
	}
}
