package exam

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"sync"

	"golang.org/x/image/draw"

	"akra/internal/platform/database"
	"akra/internal/platform/storage"
)

// Pratinjau ringan gambar soal (T142, hemat data). Halaman ujian memuat
// pratinjau (sisi terpanjang ≤ PreviewSide); gambar penuh hanya saat
// peserta memperbesar. srcset sengaja tidak dipakai: HP dengan layar rapat
// (DPR 2,5–3) akan memilih gambar penuh dan justru boros data.

// Batas pratinjau.
const (
	PreviewSide = 960
	// previewKeepBytes: gambar asli sekecil ini dipakai langsung.
	previewKeepBytes = 120 << 10
	// previewPNGBytes: pratinjau PNG (diagram) di atas ini disimpan JPEG.
	previewPNGBytes = 200 << 10
)

// PreparePreview membuat pratinjau gambar asli data (tipe ct). keep true
// bila gambar asli sudah cukup kecil sehingga dipakai apa adanya.
func PreparePreview(data []byte, ct string) (out []byte, outType string, keep bool, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, ErrAssetType
	}
	if ct == "image/gif" || (cfg.Width <= PreviewSide && cfg.Height <= PreviewSide && len(data) <= previewKeepBytes) {
		// GIF bisa animasi: dipakai apa adanya.
		return nil, "", true, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, ErrAssetType
	}
	w, h := cfg.Width, cfg.Height
	scale := min(1, float64(PreviewSide)/float64(max(w, h)))
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// Latar putih: gambar transparan yang disimpan JPEG tidak menjadi hitam.
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	if ct == "image/png" {
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", false, err
		}
		if buf.Len() <= previewPNGBytes {
			return pickSmaller(buf.Bytes(), "image/png", data, ct)
		}
		buf.Reset()
	}
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 72}); err != nil {
		return nil, "", false, err
	}
	return pickSmaller(buf.Bytes(), "image/jpeg", data, ct)
}

// pickSmaller memakai gambar asli bila pratinjau tidak lebih kecil.
func pickSmaller(p []byte, pt string, orig []byte, ot string) ([]byte, string, bool, error) {
	if len(p) >= len(orig) {
		return nil, "", true, nil
	}
	return p, pt, false, nil
}

// previewGroup mencegah banyak peserta membuat pratinjau gambar yang sama
// bersamaan (mis. 40 HP membuka soal yang sama di awal ujian).
var previewGroup = struct {
	sync.Mutex
	running map[string]*previewCall
}{running: map[string]*previewCall{}}

type previewCall struct {
	done chan struct{}
	a    Asset
	err  error
}

// EnsurePreview memastikan pratinjau gambar a tersedia dan mengembalikan
// asset dengan PreviewKey terisi. Gambar lama (sebelum T142) dibuat saat
// pertama diminta.
func EnsurePreview(ctx context.Context, db database.DBTX, store storage.Store, a Asset) (Asset, error) {
	if a.PreviewKey != "" {
		return a, nil
	}
	previewGroup.Lock()
	if c, ok := previewGroup.running[a.ID]; ok {
		previewGroup.Unlock()
		select {
		case <-c.done:
			return c.a, c.err
		case <-ctx.Done():
			return a, ctx.Err()
		}
	}
	c := &previewCall{done: make(chan struct{})}
	previewGroup.running[a.ID] = c
	previewGroup.Unlock()
	c.a, c.err = makePreview(ctx, db, store, a)
	previewGroup.Lock()
	delete(previewGroup.running, a.ID)
	previewGroup.Unlock()
	close(c.done)
	return c.a, c.err
}

func makePreview(ctx context.Context, db database.DBTX, store storage.Store, a Asset) (Asset, error) {
	rc, err := store.Get(ctx, a.StorageKey)
	if err != nil {
		return a, fmt.Errorf("exam: read asset: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxAssetBytes+1))
	rc.Close()
	if err != nil {
		return a, fmt.Errorf("exam: read asset: %w", err)
	}
	return storePreview(ctx, db, store, a, data)
}

// storePreview membuat dan menyimpan pratinjau dari data gambar asli.
func storePreview(ctx context.Context, db database.DBTX, store storage.Store, a Asset, data []byte) (Asset, error) {
	p, pt, keep, err := PreparePreview(data, a.ContentType)
	if err != nil {
		return a, err
	}
	key, size := a.StorageKey, a.Size
	if keep {
		pt = a.ContentType
	} else {
		var rnd [6]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return a, err
		}
		base := strings.TrimSuffix(a.StorageKey, "."+extOf[a.ContentType])
		key = fmt.Sprintf("%s-p%s.%s", base, hex.EncodeToString(rnd[:]), extOf[pt])
		if err := store.Put(ctx, key, bytes.NewReader(p), int64(len(p)), pt); err != nil {
			return a, fmt.Errorf("exam: store preview: %w", err)
		}
		size = len(p)
	}
	// Bila proses lain sudah lebih dulu menyimpan, pakai miliknya.
	var got Asset
	err = db.QueryRow(ctx, `UPDATE exam_assets SET preview_key = coalesce(preview_key, $2), preview_type = coalesce(preview_type, $3),
			preview_bytes = coalesce(preview_bytes, $4)
		WHERE id = $1 RETURNING preview_key, preview_type, preview_bytes`, a.ID, key, pt, size).
		Scan(&got.PreviewKey, &got.PreviewType, &got.PreviewSize)
	if err != nil {
		if key != a.StorageKey {
			_ = store.Delete(ctx, key)
		}
		return a, fmt.Errorf("exam: save preview: %w", err)
	}
	if got.PreviewKey != key && key != a.StorageKey {
		_ = store.Delete(ctx, key)
	}
	a.PreviewKey, a.PreviewType, a.PreviewSize = got.PreviewKey, got.PreviewType, got.PreviewSize
	return a, nil
}
