package payments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/storage"
	"akra/internal/school"
)

// Isi file minimal yang dikenali http.DetectContentType.
func jpeg(n int, seed byte) []byte {
	b := bytes.Repeat([]byte{seed}, n)
	copy(b, "\xff\xd8\xff\xe0\x00\x10JFIF\x00")
	return b
}

var (
	pngFile = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 100)...)
	pdfFile = []byte("%PDF-1.4\n%âãÏÓ\n1 0 obj\n<<>>\nendobj\n")
	exeFile = append([]byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00"), bytes.Repeat([]byte{0}, 200)...)
)

// spyStore mencatat Put dan Delete di atas driver local.
type spyStore struct {
	*storage.Local
	puts, deletes []string
}

func (s *spyStore) Put(ctx context.Context, key string, body io.Reader, size int64, ct string) error {
	s.puts = append(s.puts, key)
	return s.Local.Put(ctx, key, body, size, ct)
}

func (s *spyStore) Delete(ctx context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	return s.Local.Delete(ctx, key)
}

type proofFixture struct {
	*fixture
	dir            string
	store          *spyStore
	proofs         *ProofService
	transfer, cash Payment
}

func newProofFixture(t *testing.T) *proofFixture {
	t.Helper()
	f := &proofFixture{fixture: newFixture(t), dir: t.TempDir()}
	local, err := storage.NewLocal(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	f.store = &spyStore{Local: local}
	f.proofs = NewProofService(f.tx, f.store)
	ctx := context.Background()
	tr := cash(f.invA.ID, 50000)
	tr.Method, tr.BankAccount = MethodTransfer, "BCA 123"
	if f.transfer, err = RecordPayment(ctx, f.tx, f.actor, tr); err != nil {
		t.Fatal(err)
	}
	if f.cash, err = RecordPayment(ctx, f.tx, f.actor, cash(f.invA.ID, 10000)); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *proofFixture) upload(t *testing.T, paymentID string, data []byte) (Proof, error) {
	t.Helper()
	var p Proof
	err := f.sp(t, func(q pgx.Tx) error {
		var err error
		p, err = NewProofService(q, f.store).Upload(context.Background(), f.actor, paymentID, bytes.NewReader(data))
		return err
	})
	return p, err
}

func TestProof_UploadStoresFileUnderSchoolKey(t *testing.T) {
	f := newProofFixture(t)
	ctx := context.Background()
	for _, c := range []struct {
		data []byte
		ct   string
		ext  string
	}{{jpeg(MaxProofSize, 1), "image/jpeg", "jpg"}, {pngFile, "image/png", "png"}, {pdfFile, "application/pdf", "pdf"}} {
		p, err := f.proofs.Upload(ctx, f.actor, f.transfer.ID, bytes.NewReader(c.data))
		if err != nil {
			t.Fatalf("%s: %v", c.ct, err)
		}
		pattern := regexp.MustCompile(`^schools/` + f.schoolA + `/proofs/[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.` + c.ext + `$`)
		if !pattern.MatchString(p.StorageKey) || p.ContentType != c.ct || p.SizeBytes != len(c.data) || !strings.Contains(p.StorageKey, p.ID) {
			t.Errorf("proof = %+v", p)
		}
		if b, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(p.StorageKey))); err != nil || !bytes.Equal(b, c.data) {
			t.Errorf("%s: stored file differs: %v", c.ct, err)
		}
	}
	list, err := ListProofsByInvoice(ctx, f.tx, f.schoolA, f.invA.ID)
	if err != nil || len(list[f.transfer.ID]) != 3 {
		t.Errorf("ListProofsByInvoice = %v, %v", list, err)
	}
	if list, _ := ListProofsByInvoice(ctx, f.tx, f.schoolB, f.invA.ID); len(list) != 0 {
		t.Error("proofs visible from other school")
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE action = 'payment.proof_upload' AND school_id = $1`, f.schoolA); n != 3 {
		t.Errorf("payment.proof_upload audits = %d, want 3", n)
	}
}

func TestProof_Rejections(t *testing.T) {
	f := newProofFixture(t)
	if _, err := f.proofs.Upload(context.Background(), f.actor, f.transfer.ID, bytes.NewReader(jpeg(1000, 7))); err != nil {
		t.Fatal(err)
	}
	f.store.puts = nil
	for _, c := range []struct {
		name      string
		paymentID string
		data      []byte
		want      error
	}{
		{"3 MB", f.transfer.ID, jpeg(3<<20, 2), ErrProofTooLarge},
		{"2 MB + 1 byte", f.transfer.ID, jpeg(MaxProofSize+1, 2), ErrProofTooLarge},
		{"exe renamed to jpg", f.transfer.ID, exeFile, ErrProofType},
		{"plain text", f.transfer.ID, []byte("bukan gambar"), ErrProofType},
		{"empty", f.transfer.ID, nil, ErrProofEmpty},
		{"same file again", f.transfer.ID, jpeg(1000, 7), ErrProofDuplicate},
		{"cash payment", f.cash.ID, jpeg(1000, 8), ErrProofNotTransfer},
		{"other school payment id", "00000000-0000-0000-0000-000000000000", jpeg(1000, 9), ErrPaymentNotFound},
		{"invalid id", "bukan-uuid", jpeg(1000, 9), ErrPaymentNotFound},
	} {
		if _, err := f.upload(t, c.paymentID, c.data); !errors.Is(err, c.want) {
			t.Errorf("%s: error = %v, want %v", c.name, err, c.want)
		}
	}
	if len(f.store.puts) != 0 {
		t.Errorf("rejected uploads reached storage: %v", f.store.puts)
	}
}

func TestProof_DatabaseFailureDeletesObject(t *testing.T) {
	f := newProofFixture(t)
	ghost := f.actor
	ghost.UserID = "11111111-1111-4111-8111-111111111111" // bukan pengguna sekolah: insert gagal (foreign key)
	err := f.sp(t, func(q pgx.Tx) error {
		_, err := NewProofService(q, f.store).Upload(context.Background(), ghost, f.transfer.ID, bytes.NewReader(jpeg(500, 3)))
		return err
	})
	if err == nil {
		t.Fatal("upload by unknown user succeeded")
	}
	if len(f.store.puts) != 1 || len(f.store.deletes) != 1 || f.store.deletes[0] != f.store.puts[0] {
		t.Fatalf("puts = %v, deletes = %v", f.store.puts, f.store.deletes)
	}
	if _, err := os.Stat(filepath.Join(f.dir, filepath.FromSlash(f.store.puts[0]))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan object left in storage: %v", err)
	}
}

func TestProof_DatabaseConstraints(t *testing.T) {
	f := newProofFixture(t)
	raw := func(key string, size int, ct string, sum []byte) error {
		return f.sp(t, func(q pgx.Tx) error {
			_, err := q.Exec(context.Background(), `INSERT INTO payment_proofs (school_id, payment_id, storage_key, sha256, size_bytes, content_type, uploaded_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`, f.schoolA, f.transfer.ID, key, sum, size, ct, f.actor.UserID)
			return err
		})
	}
	key := "schools/" + f.schoolA + "/proofs/x.jpg"
	sum := bytes.Repeat([]byte{1}, 32)
	for _, c := range []struct {
		name, constraint string
		err              error
	}{
		{"3 MB", "payment_proofs_size_check", raw(key, 3<<20, "image/jpeg", sum)},
		{"0 bytes", "payment_proofs_size_check", raw(key, 0, "image/jpeg", sum)},
		{"exe type", "payment_proofs_content_type_check", raw(key, 10, "application/x-msdownload", sum)},
		{"key of other school", "payment_proofs_storage_key_check", raw("schools/"+f.schoolB+"/proofs/x.jpg", 10, "image/jpeg", sum)},
		{"short hash", "payment_proofs_sha256_check", raw(key, 10, "image/jpeg", []byte{1})},
	} {
		var pgErr *pgconn.PgError
		if !errors.As(c.err, &pgErr) || pgErr.ConstraintName != c.constraint {
			t.Errorf("%s: error = %v, want %s", c.name, c.err, c.constraint)
		}
	}
}

func TestProof_SameFileAllowedInOtherSchool(t *testing.T) {
	f := newProofFixture(t)
	ctx := context.Background()
	if _, err := f.proofs.Upload(ctx, f.actor, f.transfer.ID, bytes.NewReader(pdfFile)); err != nil {
		t.Fatal(err)
	}
	actorB := school.Actor{SchoolID: f.schoolB, UserID: f.userB}
	tr := Request{InvoiceID: f.invB.ID, Amount: 1000, PaidOn: time.Now(), Method: MethodTransfer, BankAccount: "BRI"}
	pB, err := RecordPayment(ctx, f.tx, actorB, tr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.proofs.Upload(ctx, actorB, pB.ID, bytes.NewReader(pdfFile)); err != nil {
		t.Errorf("same file in other school: %v", err)
	}
	// Pembayaran sekolah B tidak bisa dilampiri oleh sekolah A.
	if _, err := f.upload(t, pB.ID, jpeg(100, 4)); !errors.Is(err, ErrPaymentNotFound) {
		t.Errorf("payment of other school: error = %v", err)
	}
}

func multipartBody(t *testing.T, field, filename string, data []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if data != nil {
		w, err := mw.CreateFormFile(field, filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func (c *client) upload(path, roles, filename string, data []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	body, ct := multipartBody(c.t, "file", filename, data)
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Roles", roles)
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if cks := rec.Result().Cookies(); len(cks) > 0 {
		c.cookies = cks
	}
	return rec
}

func TestProofHandler(t *testing.T) {
	f := newProofFixture(t)
	c := newClient(t, f.fixture)
	path := "/keuangan/pembayaran/" + f.transfer.ID + "/bukti"

	for _, roles := range []string{"guru", "orang_tua", "admin_tu", "kepala_sekolah"} {
		if rec := c.upload(path, roles, "bukti.jpg", jpeg(100, 1)); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", roles, rec.Code)
		}
	}
	detail := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "bendahara", nil).Body.String()
	if strings.Count(detail, `enctype="multipart/form-data"`) != 1 {
		t.Error("upload form must appear for the transfer payment only")
	}
	if body := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "kepala_sekolah", nil).Body.String(); strings.Contains(body, "multipart/form-data") {
		t.Error("read-only roles must not see the upload form")
	}

	for _, bad := range []struct {
		name, file string
		data       []byte
		msg        string
	}{
		{"exe renamed", "bukti.jpg", exeFile, "File harus berupa foto"},
		{"3 MB", "besar.jpg", jpeg(3<<20, 5), "Maksimal 2 MB"},
		{"no file", "", nil, "Pilih file bukti transfer."},
	} {
		rec := c.upload(path, "bendahara", bad.file, bad.data)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), bad.msg) {
			t.Errorf("%s: status = %d, want 400 with %q", bad.name, rec.Code, bad.msg)
		}
	}

	rec := c.upload(path, "bendahara", "bukti.jpg", jpeg(150*1024, 6))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/keuangan/tagihan/"+f.invA.ID {
		t.Fatalf("upload: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	detail = c.do(http.MethodGet, rec.Header().Get("Location"), "bendahara", nil).Body.String()
	if !strings.Contains(detail, "Bukti transfer tersimpan.") || !strings.Contains(detail, "Bukti: Foto JPG, 150 KB") || !strings.Contains(detail, "Tambah bukti") {
		t.Error("detail page does not show the uploaded proof")
	}
	if rec := c.upload(path, "bendahara", "lagi.jpg", jpeg(150*1024, 6)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "sudah dipakai") {
		t.Errorf("duplicate: status = %d", rec.Code)
	}
	if rec := c.upload("/keuangan/pembayaran/"+f.cash.ID+"/bukti", "bendahara", "x.jpg", jpeg(100, 9)); rec.Code != http.StatusBadRequest {
		t.Errorf("cash payment: status = %d, want 400", rec.Code)
	}
	if rec := c.upload("/keuangan/pembayaran/00000000-0000-0000-0000-000000000000/bukti", "bendahara", "x.jpg", jpeg(100, 9)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown payment: status = %d, want 404", rec.Code)
	}
}

func TestProofAccess(t *testing.T) {
	f := newProofFixture(t)
	ctx := context.Background()
	c := newClient(t, f.fixture, f.store)
	img, err := f.proofs.Upload(ctx, f.actor, f.transfer.ID, bytes.NewReader(pngFile))
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := f.proofs.Upload(ctx, f.actor, f.transfer.ID, bytes.NewReader(pdfFile))
	if err != nil {
		t.Fatal(err)
	}
	// Bukti sekolah B.
	actorB := school.Actor{SchoolID: f.schoolB, UserID: f.userB}
	pB, err := RecordPayment(ctx, f.tx, actorB, Request{InvoiceID: f.invB.ID, Amount: 1000, PaidOn: time.Now(), Method: MethodTransfer, BankAccount: "BRI"})
	if err != nil {
		t.Fatal(err)
	}
	proofB, err := f.proofs.Upload(ctx, actorB, pB.ID, bytes.NewReader(jpeg(300, 4)))
	if err != nil {
		t.Fatal(err)
	}
	views := func() int {
		return f.count(t, `SELECT count(*) FROM audit_logs WHERE action = 'payment.proof_view'`)
	}
	open := func(proofID, roles string) *httptest.ResponseRecorder {
		return c.do(http.MethodGet, "/keuangan/bukti/"+proofID, roles, nil)
	}

	served := 0
	for _, roles := range []string{"bendahara", "admin_tu", "kepala_sekolah"} {
		rec := open(img.ID, roles)
		loc := rec.Header().Get("Location")
		if rec.Code != http.StatusFound || !strings.HasPrefix(loc, "/storage/"+img.StorageKey+"?") ||
			rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s: status = %d, Location = %q, headers = %v", roles, rec.Code, loc, rec.Header())
		}
		served++
		// URL bertanda tangan bisa dibuka tanpa sesi, gambar inline.
		got := httptest.NewRecorder()
		f.store.Handler().ServeHTTP(got, httptest.NewRequest(http.MethodGet, loc, nil))
		if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), pngFile) || got.Header().Get("Content-Disposition") != "inline" ||
			got.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: signed URL: %d %v", roles, got.Code, got.Header())
		}
	}
	rec := open(pdf.ID, "bendahara")
	served++
	got := httptest.NewRecorder()
	f.store.Handler().ServeHTTP(got, httptest.NewRequest(http.MethodGet, rec.Header().Get("Location"), nil))
	if got.Code != http.StatusOK || !strings.HasPrefix(got.Header().Get("Content-Disposition"), `attachment; filename="bukti-transfer-`) {
		t.Errorf("pdf: %d %v", got.Code, got.Header())
	}
	if n := views(); n != served {
		t.Errorf("payment.proof_view audits = %d, want %d (one per access)", n, served)
	}
	var entity string
	_ = f.tx.QueryRow(ctx, `SELECT entity_id FROM audit_logs WHERE action = 'payment.proof_view' ORDER BY id LIMIT 1`).Scan(&entity)
	if entity != img.ID {
		t.Errorf("audit entity_id = %q, want proof id", entity)
	}

	for _, roles := range []string{"orang_tua", "guru"} {
		if rec := open(img.ID, roles); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", roles, rec.Code)
		}
	}
	for _, id := range []string{proofB.ID, "00000000-0000-0000-0000-000000000000", "bukan-uuid"} {
		if rec := open(id, "bendahara"); rec.Code != http.StatusNotFound {
			t.Errorf("proof %s: status = %d, want 404", id, rec.Code)
		}
	}
	if n := views(); n != served {
		t.Errorf("denied access was audited: %d audits, want %d", n, served)
	}

	detail := c.do(http.MethodGet, "/keuangan/tagihan/"+f.invA.ID, "kepala_sekolah", nil).Body.String()
	if !strings.Contains(detail, `href="/keuangan/bukti/`+img.ID+`"`) || strings.Contains(detail, "/storage/") {
		t.Error("detail page must link to the proof route, never to a signed URL")
	}
}
