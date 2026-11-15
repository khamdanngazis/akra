package admission

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/pii"
	"akra/internal/platform/storage"
)

func TestRetentionDue(t *testing.T) {
	closes := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	year := time.Date(2027, 7, 12, 0, 0, 0, 0, time.UTC)
	for _, s := range []string{ApplicantDraft, ApplicantRejected, ApplicantWithdrawn} {
		if due, ok := RetentionDue(s, closes, year); !ok || due.Format(dateLayout) != "2027-05-01" {
			t.Errorf("%s due = %s %v", s, due.Format(dateLayout), ok)
		}
	}
	// Cadangan: paling cepat 30 hari setelah tahun ajaran dimulai.
	if due, ok := RetentionDue(ApplicantReserve, closes, year); !ok || due.Format(dateLayout) != "2027-08-11" {
		t.Errorf("reserve due = %s", due.Format(dateLayout))
	}
	// Periode tutup lebih lambat: 90 hari setelah tutup menang.
	if due, _ := RetentionDue(ApplicantReserve, time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC), year); due.Format(dateLayout) != "2027-09-29" {
		t.Errorf("late reserve due = %s", due.Format(dateLayout))
	}
	for _, s := range []string{ApplicantAccepted, ApplicantSubmitted, ApplicantVerified, ApplicantFix} {
		if _, ok := RetentionDue(s, closes, year); ok {
			t.Errorf("%s must not be erased automatically", s)
		}
	}
}

func TestRetention(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t) // tutup 2027-01-31, tahun ajaran mulai 2027-07-12
	keys := pii.DevKeyring()
	apps := NewApplicantService(e.tx, keys, e.svc)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	docs := NewDocumentService(e.tx, store, keys)
	move := func(a Applicant, to ...string) Applicant {
		t.Helper()
		for _, s := range to {
			if _, err := apps.Transition(ctx, e.actor, a.ID, s, "catatan berisi nama Budi"); err != nil {
				t.Fatalf("transition to %s: %v", s, err)
			}
		}
		got, _ := apps.Load(ctx, e.schoolA, a.ID)
		return got
	}

	draft, _, err := apps.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
	if err != nil {
		t.Fatal(err)
	}
	fill(t, apps, e.schoolA, draft.ID)
	doc, err := docs.Upload(ctx, e.schoolA, draft.ID, "kk", bytes.NewReader(pngBytes(t, "kk")))
	if err != nil {
		t.Fatal(err)
	}
	rejected := move(e.submitted(t, apps, p, tr), ApplicantVerified, ApplicantRejected)
	withdrawn := move(e.submitted(t, apps, p, tr), ApplicantWithdrawn)
	reserve := move(e.submitted(t, apps, p, tr), ApplicantVerified, ApplicantReserve)
	accepted := move(e.submitted(t, apps, p, tr), ApplicantVerified, ApplicantAccepted)

	// Percobaan formulir publik: yang lama dihapus, yang baru dipertahankan.
	if _, err := e.tx.Exec(ctx, `INSERT INTO admission_attempts (school_id, kind, success, attempted_at) VALUES
		($1, 'draft', true, now() - interval '40 days'), ($1, 'draft', true, now() - interval '1 day')`, e.schoolA); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func(day string) int {
		t.Helper()
		d, _ := time.Parse(dateLayout, day)
		n, err := NewRetentionWorker(e.tx, store, func() time.Time { return d }, logger).Run(ctx)
		if err != nil {
			t.Fatalf("run %s: %v", day, err)
		}
		return n
	}
	if n := run("2027-04-30"); n != 0 {
		t.Errorf("before retention ends: %d anonymized", n)
	}
	if n := run("2027-05-01"); n != 3 {
		t.Errorf("2027-05-01: %d anonymized, want 3 (draft, rejected, withdrawn)", n)
	}
	var attempts int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM admission_attempts WHERE school_id = $1`, e.schoolA).Scan(&attempts)
	if attempts != 1 {
		t.Errorf("attempts left = %d, want 1", attempts)
	}

	for _, a := range []Applicant{draft, rejected, withdrawn} {
		var name, phone, reason string
		var profile, guardians []byte
		var nik, kk []byte
		var birth time.Time
		var gender *string
		if err := e.tx.QueryRow(ctx, `SELECT full_name, contact_phone, anonymized_reason, profile::text::bytea, guardians::text::bytea,
			nik_enc, kk_enc, birth_date, gender FROM applicants WHERE id = $1`, a.ID).Scan(&name, &phone, &reason, &profile, &guardians, &nik, &kk, &birth, &gender); err != nil {
			t.Fatal(err)
		}
		if name != anonymizedName || phone != "+00000000" || reason != ReasonRetention || string(profile) != "{}" || string(guardians) != "{}" ||
			nik != nil || kk != nil || birth.Format(dateLayout) != "2019-01-01" || gender == nil {
			t.Errorf("%s not anonymized: name=%q phone=%q profile=%s nik=%v birth=%s", a.Status, name, phone, profile, nik != nil, birth.Format(dateLayout))
		}
		var notes int
		_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM applicant_events WHERE applicant_id = $1 AND note IS NOT NULL`, a.ID).Scan(&notes)
		if notes != 0 {
			t.Errorf("%s keeps %d status notes", a.Status, notes)
		}
	}
	// Nomor pendaftaran dan status tetap ada untuk rekap.
	var number string
	_ = e.tx.QueryRow(ctx, `SELECT registration_number FROM applicants WHERE id = $1`, rejected.ID).Scan(&number)
	if number != rejected.RegistrationNumber {
		t.Errorf("registration number = %q", number)
	}
	// Dokumen: baris dan file terhapus.
	var docRows int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM applicant_documents WHERE applicant_id = $1`, draft.ID).Scan(&docRows)
	if docRows != 0 {
		t.Error("document rows not deleted")
	}
	if _, err := store.Get(ctx, doc.StorageKey); err == nil {
		t.Error("document object not deleted")
	}
	var audits int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = $1 AND school_id = $2 AND user_id IS NULL`, ActionApplicantAnonymize, e.schoolA).Scan(&audits)
	if audits != 3 {
		t.Errorf("anonymize audits = %d", audits)
	}

	// Pendaftar yang sudah dianonimkan: tidak bisa diubah, tidak bisa dicek.
	got, _ := apps.Load(ctx, e.schoolA, draft.ID)
	if got.Editable() || got.AnonymizedAt == nil {
		t.Error("anonymized draft still editable")
	}
	if _, err := apps.Transition(ctx, e.actor, draft.ID, ApplicantWithdrawn, ""); !errors.Is(err, ErrBadTransition) {
		t.Errorf("transition after anonymize: %v", err)
	}
	if _, err := apps.FindForStatus(ctx, e.schoolA, rejected.RegistrationNumber, "2019-05-17"); !errors.Is(err, ErrNotFound) {
		t.Errorf("status check after anonymize: %v", err)
	}
	// Rekap tetap menghitung.
	sum, err := apps.Summarize(ctx, e.schoolA, p.ID, e.svc.Today())
	if err != nil || sum.Total < 4 {
		t.Errorf("summary total = %d, %v", sum.Total, err)
	}

	// Cadangan baru dihapus 30 hari setelah tahun ajaran dimulai; yang
	// diterima tidak pernah dihapus otomatis.
	if n := run("2027-08-10"); n != 0 {
		t.Errorf("reserve anonymized too early: %d", n)
	}
	if n := run("2027-08-11"); n != 1 {
		t.Errorf("reserve not anonymized: %d", n)
	}
	if n := run("2030-01-01"); n != 0 {
		t.Errorf("re-run anonymized %d more (accepted must stay)", n)
	}
	acc, _ := apps.Load(ctx, e.schoolA, accepted.ID)
	if acc.AnonymizedAt != nil || acc.Values["nik"] == "" {
		t.Error("accepted applicant was anonymized")
	}
	_ = reserve
}

func TestEraseOnRequest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	keys := pii.DevKeyring()
	apps := NewApplicantService(e.tx, keys, e.svc)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.router = e.routerWith(t, apps, NewDocumentService(e.tx, store, keys))
	submitted := e.submitted(t, apps, p, tr)
	withdrawn := e.submitted(t, apps, p, tr)
	if _, err := apps.Transition(ctx, e.actor, withdrawn.ID, ApplicantWithdrawn, ""); err != nil {
		t.Fatal(err)
	}

	body := e.do(http.MethodGet, "/pendaftaran/"+withdrawn.ID, "admin_tu", nil).Body.String()
	if !strings.Contains(body, "dihapus otomatis pada") || !strings.Contains(body, "Hapus data pribadi sekarang") {
		t.Error("detail without retention date or erase button")
	}
	if body := e.do(http.MethodGet, "/pendaftaran/"+submitted.ID, "admin_tu", nil).Body.String(); strings.Contains(body, "Hapus data pribadi sekarang") {
		t.Error("erase offered for applicant still in process")
	}
	path := "/pendaftaran/" + withdrawn.ID + "/hapus-data"
	if rec := e.do(http.MethodPost, path, "admin_tu", url.Values{}); rec.Code != http.StatusBadRequest {
		t.Errorf("without confirmation: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path, "kepala_sekolah", url.Values{"confirm": {"on"}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal erase: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/"+submitted.ID+"/hapus-data", "admin_tu", url.Values{"confirm": {"on"}}); rec.Code != http.StatusConflict {
		t.Errorf("erase applicant in process: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path, "admin_tu", url.Values{"confirm": {"on"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("erase: %d %s", rec.Code, rec.Body.String())
	}
	a, _ := apps.Load(ctx, e.schoolA, withdrawn.ID)
	if a.AnonymizedReason != ReasonRequest || a.FullName != anonymizedName || a.Values["nik"] != "" {
		t.Errorf("erased applicant = %q %q", a.AnonymizedReason, a.FullName)
	}
	var actor string
	_ = e.tx.QueryRow(ctx, `SELECT user_id::text FROM audit_logs WHERE action = $1 AND entity_id = $2`, ActionApplicantAnonymize, withdrawn.ID).Scan(&actor)
	if actor != userA {
		t.Errorf("audit actor = %q", actor)
	}
	body = e.do(http.MethodGet, "/pendaftaran/"+withdrawn.ID, "admin_tu", nil).Body.String()
	if !strings.Contains(body, "sudah dihapus") || !strings.Contains(body, "atas permintaan") || strings.Contains(body, "3201") {
		t.Error("detail after erase")
	}
	// Kedua kali: sudah dihapus.
	if rec := e.do(http.MethodPost, path, "admin_tu", url.Values{"confirm": {"on"}}); rec.Code != http.StatusConflict {
		t.Errorf("erase twice: %d", rec.Code)
	}
}
