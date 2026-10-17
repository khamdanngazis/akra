package storage

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalRoute adalah path URL tempat driver local menyajikan objek lewat URL
// bertanda tangan.
const LocalRoute = "/storage"

// Local menyimpan objek di disk. Untuk development dan staging tanpa R2;
// config menolaknya di production. URL dari Presign ditandatangani HMAC
// dengan kunci acak per proses (restart membatalkan URL lama) dan
// kedaluwarsa seperti presigned URL R2.
type Local struct {
	dir    string
	secret []byte
	now    func() time.Time
}

// NewLocal membuat driver local di folder dir.
func NewLocal(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create dir %s: %w", dir, err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("storage: generate signing key: %w", err)
	}
	return &Local{dir: dir, secret: secret, now: time.Now}, nil
}

func (l *Local) path(key string) (string, error) {
	if !validKey(key) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	return filepath.Join(l.dir, filepath.FromSlash(key)), nil
}

// Put menyimpan objek lewat file sementara lalu rename (atomik).
func (l *Local) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

// Delete menghapus objek; objek yang tidak ada bukan error.
func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Presign mengembalikan URL relatif /storage/<key>?... yang berlaku ttl.
func (l *Local) Presign(_ context.Context, key string, ttl time.Duration, opts PresignOptions) (string, error) {
	if !validKey(key) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(l.now().Add(ttl).Unix(), 10))
	q.Set("cd", opts.ContentDisposition)
	q.Set("ct", opts.ContentType)
	q.Set("sig", l.sign(key, q))
	return LocalRoute + "/" + key + "?" + q.Encode(), nil
}

func (l *Local) sign(key string, q url.Values) string {
	m := hmac.New(sha256.New, l.secret)
	m.Write([]byte(key + "\n" + q.Get("exp") + "\n" + q.Get("cd") + "\n" + q.Get("ct")))
	return hex.EncodeToString(m.Sum(nil))
}

// Handler menyajikan objek dari URL Presign. Tanda tangan salah atau
// kedaluwarsa ditolak 403, seperti R2.
func (l *Local) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, LocalRoute+"/")
		q := r.URL.Query()
		exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
		if err != nil || !hmac.Equal([]byte(q.Get("sig")), []byte(l.sign(key, q))) || l.now().Unix() > exp {
			http.Error(w, "Request has expired or signature is invalid", http.StatusForbidden)
			return
		}
		p, err := l.path(key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "private, no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		if ct := q.Get("ct"); ct != "" {
			h.Set("Content-Type", ct)
		}
		if cd := q.Get("cd"); cd != "" {
			h.Set("Content-Disposition", cd)
		}
		http.ServeContent(w, r, "", st.ModTime(), f)
	})
}
