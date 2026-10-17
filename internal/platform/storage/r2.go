package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// R2Config menghubungkan ke bucket Cloudflare R2.
type R2Config struct {
	AccountID       string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// Endpoint mengganti https://<AccountID>.r2.cloudflarestorage.com
	// (untuk uji dengan server S3 palsu); sekaligus memakai path-style.
	Endpoint string
}

// R2 menyimpan objek di bucket R2 privat. Bucket wajib tanpa akses publik
// (public access dan r2.dev dimatikan di dashboard Cloudflare).
type R2 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

// NewR2 membuat client R2.
func NewR2(c R2Config) (*R2, error) {
	if (c.AccountID == "" && c.Endpoint == "") || c.Bucket == "" || c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return nil, errors.New("storage: R2 account id, bucket, and credentials are required")
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = "https://" + c.AccountID + ".r2.cloudflarestorage.com"
	}
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""),
		UsePathStyle: c.Endpoint != "",
		// Checksum hanya bila diwajibkan: kompatibel dengan R2.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &R2{client: client, presign: s3.NewPresignClient(client), bucket: c.Bucket}, nil
}

// Put menyimpan objek. Objek bersifat privat; tidak ada header cache publik.
func (s *R2) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	if !validKey(key) {
		return fmt.Errorf("storage: invalid key %q", key)
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
		CacheControl:  aws.String("private, no-store"),
	})
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

// Delete menghapus objek.
func (s *R2) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return fmt.Errorf("storage: invalid key %q", key)
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

// Presign membuat URL GET bertanda tangan (SigV4) yang kedaluwarsa setelah
// ttl. Header respons diatur lewat parameter response-content-*.
func (s *R2) Presign(ctx context.Context, key string, ttl time.Duration, opts PresignOptions) (string, error) {
	if !validKey(key) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	in := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ResponseCacheControl: aws.String("private, no-store")}
	if opts.ContentDisposition != "" {
		in.ResponseContentDisposition = aws.String(opts.ContentDisposition)
	}
	if opts.ContentType != "" {
		in.ResponseContentType = aws.String(opts.ContentType)
	}
	req, err := s.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presign %s: %w", key, err)
	}
	return req.URL, nil
}
