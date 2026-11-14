package admission

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // daftarkan dekoder JPEG untuk image.DecodeConfig
	_ "image/png"  // daftarkan dekoder PNG untuk image.DecodeConfig
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/platform/storage"
)

// MaxLogoSize adalah ukuran maksimal file logo sekolah.
const MaxLogoSize = 512 << 10

// maxLogoSide adalah panjang sisi maksimal logo dalam piksel.
const maxLogoSide = 4000

// Error unggah logo.
var (
	ErrLogoTooLarge = errors.New("admission: logo too large")
	ErrLogoType     = errors.New("admission: logo must be png or jpeg")
)

// Logo adalah logo sekolah yang tersimpan.
type Logo struct {
	Key, ContentType string
}

// LogoService menyimpan logo sekolah untuk halaman pendaftaran (T111).
// Logo tampil publik sehingga tidak dienkripsi.
type LogoService struct {
	db    database.DB
	store storage.Store
}

// NewLogoService mengembalikan LogoService.
func NewLogoService(db database.DB, store storage.Store) *LogoService {
	return &LogoService{db: db, store: store}
}

// logoServiceFor memakai storage yang sama dengan dokumen pendaftar.
// documents boleh nil di router uji yang tidak memakai storage.
func logoServiceFor(db database.DB, documents *DocumentService) *LogoService {
	var store storage.Store
	if documents != nil {
		store = documents.store
	}
	return NewLogoService(db, store)
}

// Get mengembalikan logo sekolah; ok false jika belum diunggah.
func (s *LogoService) Get(ctx context.Context, schoolID string) (Logo, bool, error) {
	var key, ct *string
	err := s.db.QueryRow(ctx, `SELECT logo_key, logo_type FROM schools WHERE id = $1`, schoolID).Scan(&key, &ct)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && key == nil) {
		return Logo{}, false, nil
	}
	if err != nil {
		return Logo{}, false, fmt.Errorf("admission: get logo: %w", err)
	}
	return Logo{Key: *key, ContentType: *ct}, true, nil
}

// Open membuka isi file logo; pemanggil wajib menutupnya.
func (s *LogoService) Open(ctx context.Context, l Logo) (io.ReadCloser, error) {
	return s.store.Get(ctx, l.Key)
}

// Save memeriksa file (PNG/JPEG, maksimal MaxLogoSize) lalu menggantikan
// logo lama sekolah.
func (s *LogoService) Save(ctx context.Context, schoolID string, r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, MaxLogoSize+1))
	if err != nil {
		return fmt.Errorf("admission: read logo: %w", err)
	}
	if len(data) > MaxLogoSize {
		return ErrLogoTooLarge
	}
	ct := http.DetectContentType(data)
	if ct != "image/png" && ct != "image/jpeg" {
		return ErrLogoType
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxLogoSide || cfg.Height > maxLogoSide {
		return ErrLogoType
	}
	id, err := newID()
	if err != nil {
		return err
	}
	key := "schools/" + schoolID + "/logo-" + id
	if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
		return err
	}
	var old *string
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT logo_key FROM schools WHERE id = $1 FOR UPDATE`, schoolID).Scan(&old); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE schools SET logo_key = $2, logo_type = $3, updated_at = now() WHERE id = $1`, schoolID, key, ct)
		return err
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		return fmt.Errorf("admission: save logo: %w", err)
	}
	if old != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), *old)
	}
	return nil
}

// Remove menghapus logo sekolah. Tidak error jika belum ada logo.
func (s *LogoService) Remove(ctx context.Context, schoolID string) error {
	var old *string
	err := s.db.QueryRow(ctx, `
		UPDATE schools n SET logo_key = NULL, logo_type = NULL, updated_at = now()
		FROM schools o WHERE n.id = o.id AND n.id = $1 RETURNING o.logo_key`, schoolID).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("admission: remove logo: %w", err)
	}
	if old != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), *old)
	}
	return nil
}

// logoVersion adalah penanda pendek untuk URL logo agar cache browser
// berganti saat logo diganti.
func logoVersion(key string) string {
	if len(key) > 8 {
		return key[len(key)-8:]
	}
	return key
}
