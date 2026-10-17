package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

func TestValidKey(t *testing.T) {
	for _, k := range []string{"schools/a/proofs/b.jpg", "x.pdf"} {
		if !validKey(k) {
			t.Errorf("%q must be valid", k)
		}
	}
	for _, k := range []string{"", "/etc/passwd", "../x", "a/../../x", "a//b", "a/./b", `a\b`} {
		if validKey(k) {
			t.Errorf("%q must be invalid", k)
		}
	}
}

func TestLocal(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := "schools/s1/proofs/p1.pdf"
	if err := l.Put(ctx, key, strings.NewReader("%PDF-1.4"), 8, "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "schools", "s1", "proofs", "p1.pdf")); string(b) != "%PDF-1.4" {
		t.Errorf("file content = %q", b)
	}
	if err := l.Put(ctx, "../escape.pdf", strings.NewReader("x"), 1, "application/pdf"); err == nil {
		t.Error("path traversal must be rejected")
	}

	get := func(u string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		l.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		return rec
	}
	u, err := l.Presign(ctx, key, 5*time.Minute, PresignOptions{ContentDisposition: `attachment; filename="bukti.pdf"`, ContentType: "application/pdf"})
	if err != nil || !strings.HasPrefix(u, "/storage/"+key+"?") {
		t.Fatalf("Presign = %q, %v", u, err)
	}
	rec := get(u)
	if rec.Code != http.StatusOK || rec.Body.String() != "%PDF-1.4" || rec.Header().Get("Content-Disposition") != `attachment; filename="bukti.pdf"` ||
		rec.Header().Get("Content-Type") != "application/pdf" || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("signed GET: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}

	// Tanpa tanda tangan, tanda tangan diubah, atau header diubah: 403.
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	q.Set("cd", "inline")
	for _, bad := range []string{"/storage/" + key, "/storage/" + key + "?" + q.Encode(), strings.Replace(u, "p1.pdf", "p2.pdf", 1)} {
		if rec := get(bad); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s: status = %d, want 403", bad, rec.Code)
		}
	}
	// Kedaluwarsa.
	l.now = func() time.Time { return time.Now().Add(5*time.Minute + time.Second) }
	if rec := get(u); rec.Code != http.StatusForbidden {
		t.Errorf("expired URL: status = %d, want 403", rec.Code)
	}
	l.now = time.Now

	if err := l.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, key); err != nil {
		t.Errorf("deleting a missing object: %v", err)
	}
	if rec := get(u); rec.Code != http.StatusNotFound {
		t.Errorf("deleted object: status = %d, want 404", rec.Code)
	}
}

type fakeS3 struct {
	mu   sync.Mutex
	reqs []string
	body string
	hdr  http.Header
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
	if r.Method == http.MethodPut {
		b, _ := io.ReadAll(r.Body)
		f.body, f.hdr = string(b), r.Header.Clone()
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestR2(t *testing.T) {
	fake := &fakeS3{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, err := NewR2(R2Config{Endpoint: srv.URL, Bucket: "akra-private", AccessKeyID: "AKID", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	key := "schools/s1/proofs/p1.jpg"
	if err := s.Put(ctx, key, strings.NewReader("jpegdata"), 8, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fake.reqs, ",") != "PUT /akra-private/"+key+",DELETE /akra-private/"+key {
		t.Errorf("requests = %v", fake.reqs)
	}
	if fake.body != "jpegdata" || fake.hdr.Get("Content-Type") != "image/jpeg" || fake.hdr.Get("Cache-Control") != "private, no-store" ||
		!strings.HasPrefix(fake.hdr.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Errorf("PUT body = %q, headers = %v", fake.body, fake.hdr)
	}
	if err := s.Put(ctx, "../x", strings.NewReader(""), 0, "image/jpeg"); err == nil {
		t.Error("invalid key must be rejected")
	}
}

func TestR2_Presign(t *testing.T) {
	s, err := NewR2(R2Config{AccountID: "acc123", Bucket: "akra-private", AccessKeyID: "AKID", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.Presign(ctx, "schools/s1/proofs/p1.pdf", 5*time.Minute, PresignOptions{ContentDisposition: "attachment", ContentType: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "https" || u.Host != "akra-private.acc123.r2.cloudflarestorage.com" || u.Path != "/schools/s1/proofs/p1.pdf" {
		t.Errorf("URL = %s", raw)
	}
	for k, want := range map[string]string{
		"X-Amz-Expires": "300", "X-Amz-Algorithm": "AWS4-HMAC-SHA256",
		"response-content-disposition": "attachment", "response-content-type": "application/pdf",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if q.Get("X-Amz-Signature") == "" || !strings.HasPrefix(q.Get("X-Amz-Credential"), "AKID/") || !strings.Contains(q.Get("X-Amz-Credential"), "/auto/s3/") {
		t.Errorf("signature params = %v", q)
	}
	if _, err := NewR2(R2Config{Bucket: "b", AccessKeyID: "k", SecretAccessKey: "s"}); err == nil {
		t.Error("missing account id must be rejected")
	}
}
