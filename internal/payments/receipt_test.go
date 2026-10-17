package payments

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/clock"
)

func TestReceiptPDF(t *testing.T) {
	f := newVoidFixture(t)
	ctx := context.Background()
	if _, err := f.tx.Exec(ctx, `UPDATE schools SET name = 'SD Uji Kuitansi', address = 'Jl. Melati No. 5, Bandung' WHERE id = $1`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	r := cash(f.invA.ID, 150000)
	r.Method, r.BankAccount, r.Reference = MethodTransfer, "BCA 1234567890", "TRX-77"
	p, err := RecordPayment(ctx, f.tx, f.actor, r)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := GetReceipt(ctx, f.tx, f.schoolA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Number != p.ReceiptNumber || rc.SchoolName != "SD Uji Kuitansi" || rc.NIS != "001" || rc.Amount != 150000 || rc.Treasurer != "Bendahara" || rc.Voided {
		t.Errorf("receipt = %+v", rc)
	}
	if _, err := GetReceipt(ctx, f.tx, f.schoolB, p.ID); err != ErrPaymentNotFound {
		t.Errorf("receipt from other school: %v", err)
	}
	if got := ReceiptFilename("KW/2026/000001"); got != "kuitansi-KW-2026-000001.pdf" {
		t.Errorf("filename = %q", got)
	}

	render := func(rc Receipt) string {
		t.Helper()
		rc.PrintedAt = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
		var buf bytes.Buffer
		if err := WriteReceiptPDF(&buf, rc, false); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if !strings.HasPrefix(out, "%PDF-1.") || !strings.Contains(out, "%%EOF") {
			t.Fatal("output is not a PDF")
		}
		return out
	}
	out := render(rc)
	for _, want := range []string{"KUITANSI PEMBAYARAN", "No. " + p.ReceiptNumber, "SD Uji Kuitansi", "Jl. Melati No. 5, Bandung",
		"Rp150.000", "Terbilang: Seratus lima puluh ribu rupiah", "Transfer ke BCA 1234567890 \\(ref. TRX-77\\)", "NIS 001", "Bendahara"} {
		if !strings.Contains(out, want) {
			t.Errorf("PDF missing %q", want)
		}
	}
	if strings.Contains(out, "DIBATALKAN") {
		t.Error("active payment must not be marked as voided")
	}

	f.threshold(t, "1000000")
	if _, err := f.voids.Request(ctx, f.actor, p.ID, "transfer ganda"); err != nil {
		t.Fatal(err)
	}
	if rc, err = GetReceipt(ctx, f.tx, f.schoolA, p.ID); err != nil || !rc.Voided || rc.VoidReason != "transfer ganda" {
		t.Fatalf("voided receipt = %+v, %v", rc, err)
	}
	if out := render(rc); !strings.Contains(out, "DIBATALKAN") || !strings.Contains(out, "Alasan: transfer ganda") {
		t.Error("voided receipt must show the watermark and reason")
	}
}

func TestReceiptHandler(t *testing.T) {
	f := newVoidFixture(t)
	c := newClient(t, f.fixture)
	p := f.pay(t, f.invA.ID, 50000, clock.Today())
	path := "/keuangan/pembayaran/" + p.ID + "/kuitansi"
	for _, roles := range []string{"bendahara", "admin_tu"} {
		rec := c.do(http.MethodGet, path, roles, nil)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" ||
			rec.Header().Get("Content-Disposition") != `attachment; filename="`+ReceiptFilename(p.ReceiptNumber)+`"` ||
			rec.Header().Get("Cache-Control") != "private, no-store" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
			t.Errorf("%s: status = %d, headers = %v", roles, rec.Code, rec.Header())
		}
	}
	for _, roles := range []string{"kepala_sekolah", "guru", "orang_tua"} {
		if rec := c.do(http.MethodGet, path, roles, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", roles, rec.Code)
		}
	}
	// Route pembayaran lain tetap khusus bendahara.
	if rec := c.do(http.MethodGet, "/keuangan/pembayaran/baru", "admin_tu", nil); rec.Code != http.StatusForbidden {
		t.Errorf("admin_tu payment form: status = %d, want 403", rec.Code)
	}
	if rec := c.do(http.MethodGet, "/keuangan/pembayaran/00000000-0000-0000-0000-000000000000/kuitansi", "bendahara", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown payment: status = %d, want 404", rec.Code)
	}
	detail := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "bendahara", nil).Body.String()
	if !strings.Contains(detail, `href="`+path+`"`) || !strings.Contains(detail, p.ReceiptNumber) {
		t.Error("detail page must link to the receipt and show its number")
	}
	if detail := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "kepala_sekolah", nil).Body.String(); strings.Contains(detail, `href="`+path+`"`) {
		t.Error("principal must not get the receipt link")
	}
}
