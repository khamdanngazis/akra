// Package storage menyimpan file privat (bukti transfer) di object storage.
// Production memakai bucket Cloudflare R2 privat (S3-compatible); driver
// local menyimpan di disk untuk development dan staging tanpa R2. File tidak
// pernah punya URL publik: akses hanya lewat URL bertanda tangan yang
// kedaluwarsa (Presign).
package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"akra/internal/platform/config"
)

// PresignOptions mengatur header respons saat URL bertanda tangan dibuka.
type PresignOptions struct {
	// ContentDisposition, mis. "inline" untuk gambar atau
	// `attachment; filename="bukti.pdf"` untuk PDF.
	ContentDisposition string
	ContentType        string
}

// Store menyimpan, menghapus, dan memberi akses sementara ke objek.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Delete(ctx context.Context, key string) error
	// Presign mengembalikan URL yang bisa dibuka tanpa login selama ttl.
	// URL ini tidak boleh disimpan di database.
	Presign(ctx context.Context, key string, ttl time.Duration, opts PresignOptions) (string, error)
}

// New memilih driver dari konfigurasi.
func New(cfg config.Config) (Store, error) {
	switch cfg.StorageDriver {
	case "local":
		return NewLocal(cfg.StorageLocalDir)
	case "r2":
		return NewR2(R2Config{
			AccountID: cfg.R2AccountID, Bucket: cfg.R2Bucket,
			AccessKeyID: cfg.R2AccessKeyID, SecretAccessKey: cfg.R2SecretAccessKey,
		})
	default:
		return nil, fmt.Errorf("storage: unknown driver %q", cfg.StorageDriver)
	}
}

// validKey memastikan key relatif dan tidak keluar folder (../).
func validKey(key string) bool {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return false
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
