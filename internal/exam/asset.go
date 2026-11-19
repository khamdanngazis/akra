package exam

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // dekoder GIF untuk image.DecodeConfig
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // dekoder WebP (gambar dikecilkan editor)

	"akra/internal/platform/database"
	"akra/internal/platform/storage"
)

// Batas gambar soal. Editor mengecilkan gambar di browser (sisi terpanjang
// 1600 px, WebP); gambar yang lebih besar dikecilkan server (mis. hasil
// impor Word).
const (
	MaxAssetBytes = 5 << 20
	MaxAssetSide  = 1600
	maxDecodeSide = 8000
)

// Asset adalah gambar soal yang tersimpan.
type Asset struct {
	ID          string
	BankID      string
	StorageKey  string
	ContentType string
	Width       int
	Height      int
	Size        int
	// Pratinjau ringan untuk halaman ujian (T142); kosong = belum dibuat.
	PreviewKey  string
	PreviewType string
	PreviewSize int
}

// ErrAssetType: bukan PNG, JPEG, WebP, atau GIF, atau ukurannya tidak wajar.
var ErrAssetType = errors.New("exam: unsupported image")

// PrepareImage memeriksa gambar dan mengecilkannya bila sisi terpanjang di
// atas MaxAssetSide. Hasil: data siap simpan, tipe, dan ukurannya.
func PrepareImage(data []byte) ([]byte, string, int, int, error) {
	if len(data) == 0 || len(data) > MaxAssetBytes*4 {
		return nil, "", 0, 0, ErrAssetType
	}
	ct := http.DetectContentType(data)
	switch ct {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return nil, "", 0, 0, ErrAssetType
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxDecodeSide || cfg.Height > maxDecodeSide {
		return nil, "", 0, 0, ErrAssetType
	}
	w, h := cfg.Width, cfg.Height
	if (w <= MaxAssetSide && h <= MaxAssetSide && len(data) <= MaxAssetBytes) || ct == "image/gif" {
		if len(data) > MaxAssetBytes {
			return nil, "", 0, 0, ErrAssetType
		}
		return data, ct, w, h, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, ErrAssetType
	}
	scale := float64(MaxAssetSide) / float64(max(w, h))
	if scale > 1 {
		scale = 1
	}
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	// PNG untuk gambar bergaris tegas (diagram), JPEG untuk foto.
	if ct == "image/png" {
		err = png.Encode(&buf, dst)
	} else {
		ct = "image/jpeg"
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85})
	}
	if err != nil || buf.Len() > MaxAssetBytes {
		return nil, "", 0, 0, ErrAssetType
	}
	return buf.Bytes(), ct, nw, nh, nil
}

var extOf = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/gif": "gif"}

// SaveAsset menyimpan gambar untuk bank bankID. Pemanggil sudah memastikan
// sa boleh menulis bank itu.
func SaveAsset(ctx context.Context, db database.DBTX, store storage.Store, sa StaffActor, bankID string, data []byte) (Asset, error) {
	data, ct, w, h, err := PrepareImage(data)
	if err != nil {
		return Asset{}, err
	}
	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return Asset{}, err
	}
	key := fmt.Sprintf("schools/%s/exam/%s/%s.%s", sa.SchoolID, bankID, hex.EncodeToString(rnd[:]), extOf[ct])
	if err := store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ct); err != nil {
		return Asset{}, fmt.Errorf("exam: store asset: %w", err)
	}
	a := Asset{BankID: bankID, StorageKey: key, ContentType: ct, Width: w, Height: h, Size: len(data)}
	if err := db.QueryRow(ctx, `INSERT INTO exam_assets (school_id, bank_id, storage_key, content_type, width, height, size_bytes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, sa.SchoolID, bankID, key, ct, w, h, len(data), sa.UserID).Scan(&a.ID); err != nil {
		_ = store.Delete(ctx, key)
		return Asset{}, fmt.Errorf("exam: save asset: %w", err)
	}
	// Pratinjau dibuat sekarang; bila gagal, dibuat saat pertama diminta.
	if p, err := storePreview(ctx, db, store, a, data); err == nil {
		a = p
	}
	return a, nil
}

// GetAsset mengembalikan gambar milik sekolah, atau ErrNotFound.
func GetAsset(ctx context.Context, q database.DBTX, schoolID, id string) (Asset, error) {
	if !database.IsUUID(id) {
		return Asset{}, ErrNotFound
	}
	var a Asset
	err := q.QueryRow(ctx, `SELECT id, bank_id, storage_key, content_type, width, height, size_bytes,
			coalesce(preview_key, ''), coalesce(preview_type, ''), coalesce(preview_bytes, 0)
		FROM exam_assets WHERE school_id = $1 AND id = $2`,
		schoolID, id).Scan(&a.ID, &a.BankID, &a.StorageKey, &a.ContentType, &a.Width, &a.Height, &a.Size,
		&a.PreviewKey, &a.PreviewType, &a.PreviewSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, fmt.Errorf("exam: get asset: %w", err)
	}
	return a, nil
}

// checkAssets memastikan semua gambar yang dipakai soal milik sekolah.
// Gambar dari bank lain di sekolah yang sama boleh (soal diduplikat atau
// dipindah bank).
func checkAssets(ctx context.Context, q database.DBTX, schoolID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM exam_assets WHERE school_id = $1 AND id = ANY($2)`, schoolID, ids).Scan(&n); err != nil {
		return fmt.Errorf("exam: check assets: %w", err)
	}
	if n != len(ids) {
		return &FieldError{Fields: map[string]string{"body": "Ada gambar yang tidak ditemukan. Unggah ulang gambarnya."}}
	}
	return nil
}

// Draft adalah draf editor yang tersimpan otomatis.
type Draft struct {
	Values    url.Values
	UpdatedAt time.Time
}

// SaveDraft menyimpan draf editor pengguna (menimpa draf sebelumnya).
func SaveDraft(ctx context.Context, db database.DBTX, sa StaffActor, bankID, questionID string, v url.Values) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > 500<<10 {
		return &FieldError{Fields: map[string]string{"draft": "Draf terlalu besar untuk disimpan otomatis."}}
	}
	_, err = db.Exec(ctx, `
		INSERT INTO exam_question_drafts (school_id, user_id, bank_id, question_id, data) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, bank_id, coalesce(question_id, '00000000-0000-0000-0000-000000000000'::uuid))
		DO UPDATE SET data = excluded.data, updated_at = now()`, sa.SchoolID, sa.UserID, bankID, nullable(questionID), data)
	if err != nil {
		return fmt.Errorf("exam: save draft: %w", err)
	}
	return nil
}

// GetDraft mengembalikan draf pengguna untuk soal questionID (kosong =
// soal baru di bank). ok false bila tidak ada.
func GetDraft(ctx context.Context, q database.DBTX, schoolID, userID, bankID, questionID string) (Draft, bool, error) {
	var raw []byte
	var d Draft
	err := q.QueryRow(ctx, `SELECT data, updated_at FROM exam_question_drafts
		WHERE school_id = $1 AND user_id = $2 AND bank_id = $3 AND question_id IS NOT DISTINCT FROM $4`,
		schoolID, userID, bankID, nullable(questionID)).Scan(&raw, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, false, nil
	}
	if err != nil {
		return Draft{}, false, fmt.Errorf("exam: get draft: %w", err)
	}
	if err := json.Unmarshal(raw, &d.Values); err != nil {
		return Draft{}, false, fmt.Errorf("exam: decode draft: %w", err)
	}
	return d, true, nil
}

// DeleteDraft membuang draf pengguna.
func DeleteDraft(ctx context.Context, q database.DBTX, schoolID, userID, bankID, questionID string) error {
	_, err := q.Exec(ctx, `DELETE FROM exam_question_drafts WHERE school_id = $1 AND user_id = $2 AND bank_id = $3 AND question_id IS NOT DISTINCT FROM $4`,
		schoolID, userID, bankID, nullable(questionID))
	if err != nil {
		return fmt.Errorf("exam: delete draft: %w", err)
	}
	return nil
}
