package storage

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestR2Live menguji bucket R2 sungguhan. Hanya berjalan jika
// R2_LIVE_TEST=1, mis.:
//
//	railway run --service akra -- env R2_LIVE_TEST=1 go test -run R2Live -v ./internal/platform/storage
//
// Tidak mencetak kredensial maupun URL bertanda tangan.
func TestR2Live(t *testing.T) {
	if os.Getenv("R2_LIVE_TEST") != "1" {
		t.Skip("R2_LIVE_TEST is not set")
	}
	cfg := R2Config{
		AccountID: os.Getenv("R2_ACCOUNT_ID"), Bucket: os.Getenv("R2_BUCKET"),
		AccessKeyID: os.Getenv("R2_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
	}
	s, err := NewR2(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	key := "schools/live-check/proofs/" + hex.EncodeToString(b[:]) + ".pdf"
	body := "%PDF-1.4\nakra live check\n"
	if err := s.Put(ctx, key, strings.NewReader(body), int64(len(body)), "application/pdf"); err != nil {
		t.Fatalf("put: %v", err)
	}
	t.Logf("put ok: %s", key)
	defer func() {
		if err := s.Delete(ctx, key); err != nil {
			t.Errorf("delete: %v", err)
		}
	}()

	get := func(u string) (int, string, http.Header) {
		t.Helper()
		resp, err := http.Get(u)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}

	// Tanpa tanda tangan: harus ditolak.
	for _, u := range []string{
		"https://" + cfg.AccountID + ".r2.cloudflarestorage.com/" + cfg.Bucket + "/" + key,
		"https://" + cfg.Bucket + "." + cfg.AccountID + ".r2.cloudflarestorage.com/" + key,
	} {
		code, _, _ := get(u)
		t.Logf("unsigned GET (S3 endpoint): %d", code)
		if code == http.StatusOK {
			t.Error("object readable without signature")
		}
	}

	// Presigned 5 menit: bisa dibuka, header sesuai.
	u, err := s.Presign(ctx, key, 5*time.Minute, PresignOptions{ContentDisposition: `attachment; filename="bukti.pdf"`, ContentType: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	code, got, hdr := get(u)
	t.Logf("presigned GET: %d, content-type %q, content-disposition %q", code, hdr.Get("Content-Type"), hdr.Get("Content-Disposition"))
	if code != http.StatusOK || got != body || hdr.Get("Content-Disposition") != `attachment; filename="bukti.pdf"` {
		t.Errorf("presigned GET failed")
	}

	// Presigned kedaluwarsa: ditolak R2.
	short, err := s.Presign(ctx, key, time.Second, PresignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	code, _, _ = get(short)
	t.Logf("expired presigned GET: %d", code)
	if code == http.StatusOK {
		t.Error("expired URL still works")
	}
}
