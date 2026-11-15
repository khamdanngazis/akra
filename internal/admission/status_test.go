package admission

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"akra/internal/platform/pii"
	"akra/internal/platform/storage"
)

func TestCanTransition(t *testing.T) {
	allowed := map[string][]string{
		ApplicantSubmitted: {ApplicantFix, ApplicantVerified, ApplicantWithdrawn},
		ApplicantFix:       {ApplicantWithdrawn},
		ApplicantVerified:  {ApplicantAccepted, ApplicantReserve, ApplicantRejected, ApplicantWithdrawn},
		ApplicantReserve:   {ApplicantAccepted, ApplicantRejected, ApplicantWithdrawn},
		ApplicantAccepted:  {ApplicantWithdrawn},
		ApplicantDraft:     {ApplicantWithdrawn},
		ApplicantRejected:  nil,
		ApplicantWithdrawn: nil,
	}
	all := []string{ApplicantDraft, ApplicantSubmitted, ApplicantFix, ApplicantVerified, ApplicantAccepted, ApplicantReserve, ApplicantRejected, ApplicantWithdrawn}
	for from, tos := range allowed {
		for _, to := range all {
			want := false
			for _, x := range tos {
				want = want || x == to
			}
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

// submitted membuat pendaftar yang sudah dikirim.
func (e *env) submitted(t *testing.T, svc *ApplicantService, p Period, tr Track) Applicant {
	t.Helper()
	ctx := context.Background()
	a, _, err := svc.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
	if err != nil {
		t.Fatal(err)
	}
	fill(t, svc, e.schoolA, a.ID)
	// NIK unik per pendaftar, diturunkan dari ID-nya.
	_, err = svc.SaveStep(ctx, e.schoolA, a.ID, StepChild, map[string]string{"full_name": a.FullName, "gender": "L", "birth_date": "2019-05-17",
		"nik": "32010101" + digitsOf(a.ID, 8), "kk_number": childKK, "birth_place": "Bandung", "religion": "01"})
	if err != nil {
		t.Fatal(err)
	}
	a, err = svc.Submit(ctx, e.schoolA, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func digitsOf(s string, n int) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() == n {
			break
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte(byte('0' + r%10))
		}
	}
	return b.String()
}

func TestTransition(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	svc := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a := e.submitted(t, svc, p, tr)
	number := a.RegistrationNumber

	var ve *ValidationError
	if _, err := svc.Transition(ctx, e.actor, a.ID, ApplicantFix, "  "); !errors.As(err, &ve) || ve.Fields["note"] == "" {
		t.Errorf("fix without note: %v", err)
	}
	if _, err := svc.Transition(ctx, e.actor, a.ID, ApplicantAccepted, ""); !errors.Is(err, ErrBadTransition) {
		t.Errorf("submitted -> accepted: %v", err)
	}
	res, err := svc.Transition(ctx, e.actor, a.ID, ApplicantFix, "Foto KK buram, unggah ulang.")
	if err != nil || len(res.ResumeCode) != 6 || res.Applicant.Status != ApplicantFix {
		t.Fatalf("fix = %+v, %v", res, err)
	}
	if note, _ := svc.LastNote(ctx, e.schoolA, a.ID); note != "Foto KK buram, unggah ulang." {
		t.Errorf("LastNote = %q", note)
	}

	// Orang tua melanjutkan dengan kode baru, memperbaiki, lalu kirim ulang.
	got, err := svc.Resume(ctx, e.schoolA, "081234567890", res.ResumeCode)
	if err != nil || got.ID != a.ID {
		t.Fatalf("resume for fix: %v %v", got.ID, err)
	}
	if _, err := svc.SaveStep(ctx, e.schoolA, a.ID, StepSchool, map[string]string{"previous_school": "TK Mawar"}); err != nil {
		t.Fatal(err)
	}
	again, err := svc.Submit(ctx, e.schoolA, a.ID)
	if err != nil || again.Status != ApplicantSubmitted || again.RegistrationNumber != number {
		t.Errorf("resubmit = %+v, %v (number %s)", again, err, number)
	}
	if _, err := svc.Resume(ctx, e.schoolA, "081234567890", res.ResumeCode); !errors.Is(err, ErrNotFound) {
		t.Errorf("resume code after resubmit: %v", err)
	}

	for _, to := range []string{ApplicantVerified, ApplicantReserve, ApplicantAccepted} {
		if _, err := svc.Transition(ctx, e.actor, a.ID, to, ""); err != nil {
			t.Fatalf("-> %s: %v", to, err)
		}
	}
	events, err := svc.Events(ctx, e.schoolA, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, ev := range events {
		seq = append(seq, ev.To)
	}
	if strings.Join(seq, ",") != "dikirim,perlu_perbaikan,dikirim,diverifikasi,cadangan,diterima" {
		t.Errorf("events = %v", seq)
	}
	if events[0].ActorName != "" || events[1].ActorName != "TU" {
		t.Errorf("event actors = %q, %q", events[0].ActorName, events[1].ActorName)
	}
	var audits int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = $1 AND entity_id = $2`, ActionApplicantStatus, a.ID).Scan(&audits)
	if audits != 4 {
		t.Errorf("status audits = %d", audits)
	}

	// Sudah menjadi siswa: status terkunci.
	var studentID string
	if err := e.tx.QueryRow(ctx, `INSERT INTO students (school_id, nis, full_name) VALUES ($1, '9001', 'Budi') RETURNING id`, e.schoolA).Scan(&studentID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `UPDATE applicants SET student_id = $2 WHERE id = $1`, a.ID, studentID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, e.actor, a.ID, ApplicantWithdrawn, ""); !errors.Is(err, ErrBadTransition) {
		t.Errorf("withdraw enrolled applicant: %v", err)
	}

	// Sekolah lain.
	actorB := e.actor
	actorB.SchoolID, actorB.UserID = e.schoolB, ""
	if _, err := svc.Transition(ctx, actorB, a.ID, ApplicantWithdrawn, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("other school: %v", err)
	}
}

func TestTransitionQuotaAndConcurrency(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	if _, err := e.svc.UpdateTrack(ctx, e.actor, p.ID, tr.ID, TrackForm{Name: tr.Name, Quota: "1"}); err != nil {
		t.Fatal(err)
	}
	svc := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a1 := e.submitted(t, svc, p, tr)
	a2 := e.submitted(t, svc, p, tr)
	for _, a := range []Applicant{a1, a2} {
		if _, err := svc.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
			t.Fatal(err)
		}
	}
	if res, err := svc.Transition(ctx, e.actor, a1.ID, ApplicantAccepted, ""); err != nil || res.OverQuota {
		t.Errorf("first accept: %+v %v", res.OverQuota, err)
	}
	if res, err := svc.Transition(ctx, e.actor, a2.ID, ApplicantAccepted, ""); err != nil || !res.OverQuota {
		t.Errorf("second accept must warn over quota: %+v %v", res.OverQuota, err)
	}

	// Perubahan kedua yang didasarkan pada status lama ditolak (baris
	// dikunci FOR UPDATE, jadi petugas kedua melihat status terbaru).
	a3 := e.submitted(t, svc, p, tr)
	if _, err := svc.Transition(ctx, e.actor, a3.ID, ApplicantVerified, ""); err != nil {
		t.Fatal(err)
	}
	results := map[string]error{}
	for _, to := range []string{ApplicantRejected, ApplicantReserve} {
		_, results[to] = svc.Transition(ctx, e.actor, a3.ID, to, "")
	}
	if results[ApplicantRejected] != nil || !errors.Is(results[ApplicantReserve], ErrBadTransition) {
		t.Errorf("second change after rejection: %v", results)
	}
}

func TestApplicantHandler(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	keys := pii.DevKeyring()
	svc := NewApplicantService(e.tx, keys, e.svc)
	a := e.submitted(t, svc, p, tr)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	docs := NewDocumentService(e.tx, store, keys)
	// Dokumen diunggah lewat status perlu perbaikan agar boleh diubah.
	if _, err := svc.Transition(ctx, e.actor, a.ID, ApplicantFix, "Lengkapi KK"); err != nil {
		t.Fatal(err)
	}
	doc, err := docs.Upload(ctx, e.schoolA, a.ID, "kk", bytes.NewReader(pngBytes(t, "isi-kk")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(ctx, e.schoolA, a.ID); err != nil {
		t.Fatal(err)
	}
	e.router = e.routerWith(t, svc, docs)

	rec := e.do(http.MethodGet, "/pendaftaran", "admin_tu", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), a.FullName) || !strings.Contains(rec.Body.String(), a.RegistrationNumber) {
		t.Fatalf("list: %d", rec.Code)
	}
	if body := e.do(http.MethodGet, "/pendaftaran?status=ditolak", "admin_tu", nil).Body.String(); strings.Contains(body, a.RegistrationNumber) {
		t.Error("status filter ignored")
	}
	rec = e.do(http.MethodGet, "/pendaftaran/"+a.ID, "admin_tu", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, a.Values["nik"]) || !strings.Contains(body, "Islam") || !strings.Contains(body, "Diverifikasi") || !strings.Contains(body, "Kartu Keluarga") {
		t.Errorf("detail: %d", rec.Code)
	}

	// Dokumen dialirkan setelah didekripsi.
	rec = e.do(http.MethodGet, "/pendaftaran/"+a.ID+"/dokumen/"+doc.ID, "kepala_sekolah", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.HasSuffix(rec.Body.Bytes(), []byte("isi-kk")) || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("open document: %d %v", rec.Code, rec.Header())
	}

	// Navigasi kembali (T121): tombol ke daftar (dipulihkan filternya oleh
	// nav.js) dan jejak dengan nama pendaftar sebagai halaman sekarang.
	if !strings.Contains(body, `class="back-btn" href="/pendaftaran" data-restore`) || !strings.Contains(body, `<span aria-current="page">`+a.FullName+`</span>`) {
		t.Error("back navigation missing")
	}
	// Kartu langkah berikutnya (T116) ada di atas data pendaftar.
	if i, j := strings.Index(body, `class="card next-step`), strings.Index(body, "Data pendaftar"); i < 0 || j < i {
		t.Error("next-step card missing or below applicant data")
	}

	// Status: perlu perbaikan menampilkan kode lanjut sekali. Catatan
	// kosong ditolak dan isian catatan terbuka lagi.
	if rec := e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/status", "admin_tu", url.Values{"status": {"perlu_perbaikan"}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bagian yang perlu diperbaiki") ||
		!strings.Contains(rec.Body.String(), `pick: 'perlu_perbaikan'`) {
		t.Errorf("fix without note: %d", rec.Code)
	}
	rec = e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/status", "admin_tu", url.Values{"status": {"perlu_perbaikan"}, "note": {"Akta buram"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("fix: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/status", "admin_tu", url.Values{"status": {"diterima"}}); rec.Code != http.StatusConflict {
		t.Errorf("invalid transition: %d", rec.Code)
	}

	// Kepala sekolah baca saja; guru ditolak.
	if body := e.do(http.MethodGet, "/pendaftaran/"+a.ID, "kepala_sekolah", nil).Body.String(); strings.Contains(body, "Langkah berikutnya") || strings.Contains(body, `name="status"`) {
		t.Error("principal sees actions")
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/status", "kepala_sekolah", url.Values{"status": {"mundur"}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal POST: %d", rec.Code)
	}
	for _, p := range []string{"/pendaftaran", "/pendaftaran/" + a.ID, "/pendaftaran/" + a.ID + "/dokumen/" + doc.ID} {
		if rec := e.do(http.MethodGet, p, "guru", nil); rec.Code != http.StatusForbidden {
			t.Errorf("teacher GET %s: %d", p, rec.Code)
		}
	}
}
