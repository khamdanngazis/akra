package exam

import (
	"bytes"
	"html/template"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"net/http"
	"sync"

	"akra/internal/exam/mathrender"
)

// Cek perangkat untuk peserta (T136): halaman publik /u/cek yang bisa
// dibagikan ke siswa H-7. Tanpa login dan tanpa data pribadi.

// WithMath memberi renderer rumus untuk contoh rumus di cek perangkat.
func (h *ParticipantHandler) WithMath(m *mathrender.Renderer) *ParticipantHandler {
	h.math = m
	return h
}

type deviceCheckView struct {
	Verdict DeviceVerdict
	Math    template.HTML
}

// sampleTeX adalah rumus contoh: harus tampil sebagai pecahan dan akar.
const sampleTeX = `\frac{1}{2} + \sqrt{x^2 + 1}`

func (h *ParticipantHandler) deviceCheck(w http.ResponseWriter, r *http.Request) {
	v := deviceCheckView{Verdict: ClassifyUA(r.UserAgent())}
	if h.math != nil {
		if out, err := h.math.Render(sampleTeX, false); err == nil {
			v.Math = template.HTML(out) //nolint:gosec // keluaran KaTeX dari TeX tetap milik server
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	h.render.Render(w, r, http.StatusOK, "exam/device_check", v)
}

// devicePing adalah respons kecil untuk mengukur waktu tempuh.
func (h *ParticipantHandler) devicePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

var (
	sampleOnce sync.Once
	sampleJPEG []byte
)

// sampleImage membuat gambar contoh ± 150 KB (seukuran gambar soal) untuk
// mengukur kecepatan unduh. Isinya acak tetapi tetap (seed tetap).
func sampleImage() []byte {
	sampleOnce.Do(func() {
		img := image.NewRGBA(image.Rect(0, 0, 900, 600))
		rng := rand.New(rand.NewSource(7))
		for y := 0; y < 600; y++ {
			for x := 0; x < 900; x++ {
				n := uint8(rng.Intn(64))
				img.Set(x, y, color.RGBA{R: uint8(x/4) + n, G: uint8(y/3) + n, B: 160 + n/2, A: 255})
			}
		}
		var buf bytes.Buffer
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
		sampleJPEG = buf.Bytes()
	})
	return sampleJPEG
}

func (h *ParticipantHandler) deviceSample(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(sampleImage())
}
