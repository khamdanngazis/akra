package admission

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"akra/internal/audit"
	"akra/internal/platform/pii"
)

func TestFees(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.form() // biaya Rp150.000, wajib lunas sebelum verifikasi
	f.RequiredDocs = nil
	p, err := e.svc.Create(ctx, e.actor, f)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Reguler"})
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	fees := NewFeeService(e.tx)
	a := e.submitted(t, apps, p, tr)

	// Biaya di-snapshot saat dikirim: perubahan periode tidak memengaruhi.
	f.FeeAmount = "200.000"
	if _, err := e.svc.Update(ctx, e.actor, p.ID, f); err != nil {
		t.Fatal(err)
	}
	rows, err := fees.List(ctx, e.schoolA, true)
	if err != nil || len(rows) != 1 || rows[0].Amount != 150000 || rows[0].Payment != nil {
		t.Fatalf("unpaid list = %+v, %v", rows, err)
	}

	// Wajib lunas sebelum verifikasi.
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); !errors.Is(err, ErrFeeUnpaid) {
		t.Errorf("verify unpaid: %v", err)
	}

	var ve *ValidationError
	if _, err := fees.Record(ctx, e.actor, a.ID, FeePaymentForm{PaidOn: "2026-11-01", Method: "transfer"}); !errors.As(err, &ve) || ve.Fields["bank_account"] == "" {
		t.Errorf("transfer without account: %v", err)
	}
	if _, err := fees.Record(ctx, e.actor, a.ID, FeePaymentForm{PaidOn: "2099-01-01", Method: "tunai"}); !errors.As(err, &ve) || ve.Fields["paid_on"] == "" {
		t.Errorf("future date: %v", err)
	}
	// Seri nomor kuitansi sama dengan pembayaran siswa.
	if _, err := e.tx.Exec(ctx, `INSERT INTO receipt_counters (school_id, year, last_number) VALUES ($1, 2026, 41)`, e.schoolA); err != nil {
		t.Fatal(err)
	}
	pay, err := fees.Record(ctx, e.actor, a.ID, FeePaymentForm{PaidOn: "2026-11-01", Method: "transfer", BankAccount: "BRI 123", Reference: "TRX9"})
	if err != nil || pay.ReceiptNumber != "KW/2026/000042" || pay.Amount != 150000 || pay.RecordedBy != "TU" {
		t.Fatalf("Record = %+v, %v", pay, err)
	}
	if _, err := fees.Record(ctx, e.actor, a.ID, FeePaymentForm{PaidOn: "2026-11-01", Method: "tunai"}); !errors.Is(err, ErrFeePaid) {
		t.Errorf("double payment: %v", err)
	}
	if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
		t.Errorf("verify after payment: %v", err)
	}

	// Kuitansi.
	rec, err := fees.Receipt(ctx, e.schoolA, a.ID, pay.ID)
	if err != nil || rec.Number != pay.ReceiptNumber || !strings.Contains(rec.Applicant, a.RegistrationNumber) || rec.Voided {
		t.Errorf("Receipt = %+v, %v", rec, err)
	}
	if _, err := fees.Receipt(ctx, e.schoolB, a.ID, pay.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("receipt other school: %v", err)
	}

	// Pembatalan wajib alasan; setelahnya bisa dicatat ulang.
	if err := fees.Void(ctx, e.actor, a.ID, " "); !errors.As(err, &ve) {
		t.Errorf("void without reason: %v", err)
	}
	if err := fees.Void(ctx, e.actor, a.ID, "Salah catat nominal"); err != nil {
		t.Fatal(err)
	}
	if err := fees.Void(ctx, e.actor, a.ID, "lagi"); !errors.Is(err, ErrNoPayment) {
		t.Errorf("void twice: %v", err)
	}
	if rec, _ := fees.Receipt(ctx, e.schoolA, a.ID, pay.ID); !rec.Voided || rec.VoidReason != "Salah catat nominal" {
		t.Errorf("voided receipt = %+v", rec)
	}
	pay2, err := fees.Record(ctx, e.actor, a.ID, FeePaymentForm{PaidOn: "2026-11-02", Method: "tunai"})
	if err != nil || pay2.ReceiptNumber != "KW/2026/000043" {
		t.Errorf("re-record = %+v, %v", pay2, err)
	}
	hist, _ := fees.History(ctx, e.schoolA, a.ID)
	if len(hist) != 2 || hist[1].VoidedAt == nil {
		t.Errorf("history = %+v", hist)
	}
	var n int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE entity_id = $1 AND action = ANY ($2)`, a.ID, []string{audit.ActionAdmissionFeePayment, audit.ActionAdmissionFeeVoid}).Scan(&n)
	if n != 3 {
		t.Errorf("fee audits = %d", n)
	}

	// Pendaftar tanpa biaya.
	free, err := e.svc.Create(ctx, e.actor, PeriodForm{AcademicYearID: e.yearA, Name: "Gratis", TargetGrade: "1", OpensOn: "2026-12-01", ClosesOn: "2027-01-31"})
	if err != nil {
		t.Fatal(err)
	}
	trf, _ := e.svc.AddTrack(ctx, e.actor, free.ID, TrackForm{Name: "R"})
	if _, err := e.svc.SetStatus(ctx, e.actor, free.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	b := e.submitted(t, apps, free, trf)
	if _, err := fees.Record(ctx, e.actor, b.ID, FeePaymentForm{PaidOn: "2026-11-01", Method: "tunai"}); !errors.Is(err, ErrNoFee) {
		t.Errorf("record free applicant: %v", err)
	}
	if _, err := apps.Transition(ctx, e.actor, b.ID, ApplicantVerified, ""); err != nil {
		t.Errorf("verify free applicant: %v", err)
	}
}

func TestFeeHandler(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.form()
	f.RequiredDocs = nil
	p, _ := e.svc.Create(ctx, e.actor, f)
	tr, _ := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Reguler"})
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a := e.submitted(t, apps, p, tr)

	body := e.do(http.MethodGet, "/pendaftaran/biaya", "bendahara", nil).Body.String()
	if !strings.Contains(body, a.FullName) || !strings.Contains(body, "Rp150.000") || !strings.Contains(body, "Catat pembayaran") {
		t.Fatal("fees page for treasurer")
	}
	if body := e.do(http.MethodGet, "/pendaftaran/biaya", "admin_tu", nil).Body.String(); strings.Contains(body, "Catat pembayaran") {
		t.Error("admin sees record button")
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/biaya", "admin_tu", url.Values{"paid_on": {"2026-11-01"}, "method": {"tunai"}}); rec.Code != http.StatusForbidden {
		t.Errorf("admin records fee: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/biaya", "bendahara", url.Values{"paid_on": {"2026-11-01"}, "method": {"transfer"}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Rekening tujuan wajib") {
		t.Errorf("invalid payment: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/pendaftaran/"+a.ID+"/biaya", "bendahara", url.Values{"paid_on": {"2026-11-01"}, "method": {"tunai"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("record: %d %s", rec.Code, rec.Body.String())
	}
	hist, _ := NewFeeService(e.tx).History(ctx, e.schoolA, a.ID)
	rec := e.do(http.MethodGet, "/pendaftaran/"+a.ID+"/biaya/"+hist[0].ID+"/kuitansi", "kepala_sekolah", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("receipt pdf: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if body := e.do(http.MethodGet, "/pendaftaran/"+a.ID, "admin_tu", nil).Body.String(); !strings.Contains(body, "Biaya formulir") || !strings.Contains(body, "Lunas") {
		t.Error("detail page fee status")
	}
	if rec := e.do(http.MethodGet, "/pendaftaran/biaya", "guru", nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher: %d", rec.Code)
	}
}
