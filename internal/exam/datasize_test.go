package exam

import (
	"bytes"
	"compress/gzip"
	"context"
	"image"
	"image/jpeg"
	"io/fs"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"akra/web"
)

// photo membuat gambar mirip foto 1600×1200 (gradasi, bentuk, dan derau
// halus) sebagai JPEG kualitas 85, seperti unggahan guru.
func photo(t *testing.T, seed int64) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, 1600, 1200))
	fx, fy := 0.004+rng.Float64()*0.01, 0.003+rng.Float64()*0.01
	for y := 0; y < 1200; y++ {
		for x := 0; x < 1600; x++ {
			v := 0.5 + 0.25*math.Sin(float64(x)*fx) + 0.25*math.Cos(float64(y)*fy)
			n := rng.Float64()*24 - 12
			r := uint8(math.Max(0, math.Min(255, v*200+n)))
			g := uint8(math.Max(0, math.Min(255, (1-v)*180+40+n)))
			b := uint8(math.Max(0, math.Min(255, float64(x*255/1600)*0.6+n)))
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gz(b []byte) int {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	w.Write(b)
	w.Close()
	return buf.Len()
}

var staticRef = regexp.MustCompile(`(?:href|src)="/static/([^"?]+)`)
var fontRef = regexp.MustCompile(`url\(["']?(?:/static/)?([^"')]*latin-wght-normal\.woff2)`)

// Kriteria T142: data satu ujian 40 soal (10 bergambar) < 2 MB, termasuk
// halaman ujian, aset statis, pratinjau gambar, dan simpan jawaban.
func TestExamDataBudget(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	e.students(t)
	var ids []string
	var original int
	for i := 0; i < 40; i++ {
		q := sampleQuestion(KindMC)
		body := "<p>Soal nomor " + itoa(i+1) + ": bacalah pernyataan berikut dengan teliti, lalu pilih jawaban yang paling tepat menurut materi yang sudah dipelajari.</p>"
		if i%4 == 0 {
			data := photo(t, int64(i))
			original += len(data)
			a, err := SaveAsset(ctx, e.tx, e.store, e.owner, e.bank.ID, data)
			if err != nil {
				t.Fatal(err)
			}
			if a.PreviewKey == "" || a.PreviewKey == a.StorageKey || a.PreviewSize >= a.Size {
				t.Fatalf("preview not made: %+v", a)
			}
			body += `<p><img data-asset="` + a.ID + `" alt="Gambar ` + itoa(i) + `" loading="lazy"></p>`
		}
		q.BodySrc, q.BodyHTML = body, body
		q.Options.Choices = []Choice{{ID: "a", Src: "Pilihan pertama yang cukup panjang", HTML: "Pilihan pertama yang cukup panjang"},
			{ID: "b", Src: "Pilihan kedua", HTML: "Pilihan kedua"}, {ID: "c", Src: "Pilihan ketiga", HTML: "Pilihan ketiga"},
			{ID: "d", Src: "Pilihan keempat", HTML: "Pilihan keempat"}}
		created, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, q, now)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	start := now.Add(time.Hour)
	ex := e.scheduledExam(t, start, ids...)
	cards, err := IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now)
	if err != nil {
		t.Fatal(err)
	}
	e.clock = start.Add(time.Minute)
	e.login(t, "hp1", cards[0])
	loc := e.form("hp1", "/u/ujian/"+e.sessionOf(t, ex)+"/mulai", url.Values{}).Header().Get("Location")
	page := e.do("hp1", http.MethodGet, loc, "", "")
	if page.Code != http.StatusOK {
		t.Fatalf("take = %d", page.Code)
	}
	html := page.Body.String()
	total := gz(page.Body.Bytes())
	parts := map[string]int{"halaman": total}

	static, _ := fs.Sub(web.FS, "static")
	seen := map[string]bool{}
	for _, m := range staticRef.FindAllStringSubmatch(html, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		b, err := fs.ReadFile(static, m[1])
		if err != nil {
			t.Fatalf("static %s: %v", m[1], err)
		}
		n := len(b)
		if !strings.HasSuffix(m[1], ".woff2") {
			n = gz(b)
		}
		parts["statis"] += n
		if strings.HasSuffix(m[1], ".css") {
			for _, f := range fontRef.FindAllStringSubmatch(string(b), -1) {
				name := strings.TrimPrefix(f[1], "../")
				if seen[name] {
					continue
				}
				seen[name] = true
				fb, err := fs.ReadFile(static, name)
				if err != nil {
					t.Fatalf("font %s: %v", name, err)
				}
				parts["font"] += len(fb)
			}
		}
	}
	sw, _ := fs.ReadFile(static, "js/exam-sw.js")
	parts["statis"] += gz(sw)
	var previews int
	e.tx.QueryRow(ctx, `SELECT coalesce(sum(preview_bytes), 0) FROM exam_assets WHERE bank_id = $1`, e.bank.ID).Scan(&previews)
	parts["gambar"] = previews
	// Simpan jawaban: ±40 kirim + ping 10 detik selama 60 menit, masing-masing
	// ±600 byte dengan header.
	parts["simpan"] = (40 + 360) * 600
	total = 0
	for _, v := range parts {
		total += v
	}
	t.Logf("data per ujian 40 soal (10 gambar): %d KB %v; gambar asli %d KB", total>>10, parts, original>>10)
	if total >= 2<<20 {
		t.Errorf("exam data %d KB, want < 2048 KB", total>>10)
	}
}

func (e *takeEnv) sessionOf(t *testing.T, ex Exam) string {
	t.Helper()
	ss, err := ListSessions(context.Background(), e.tx, e.a.ID, ex.ID)
	if err != nil || len(ss) != 1 {
		t.Fatalf("sessions = %d, %v", len(ss), err)
	}
	return ss[0].ID
}

func TestPreparePreview(t *testing.T) {
	big := photo(t, 1)
	p, ct, keep, err := PreparePreview(big, "image/jpeg")
	if err != nil || keep || ct != "image/jpeg" {
		t.Fatalf("PreparePreview = %s, keep %v, %v", ct, keep, err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(p))
	if cfg.Width != PreviewSide || cfg.Height != 720 || len(p) >= len(big) {
		t.Errorf("preview %dx%d %d bytes (original %d)", cfg.Width, cfg.Height, len(p), len(big))
	}
	// Gambar kecil dipakai apa adanya.
	small := image.NewRGBA(image.Rect(0, 0, 200, 100))
	var buf bytes.Buffer
	jpeg.Encode(&buf, small, nil)
	if _, _, keep, err := PreparePreview(buf.Bytes(), "image/jpeg"); err != nil || !keep {
		t.Errorf("small image: keep %v, %v", keep, err)
	}
	if _, _, _, err := PreparePreview([]byte("bukan gambar"), "image/png"); err == nil {
		t.Error("garbage accepted")
	}
}

// Kriteria T142: 40 siswa satu lab (satu IP publik) bisa masuk bersamaan
// tanpa terkena batas percobaan, walau sebagian salah ketik kode dulu.
func TestLabLoginsBehindOneIP(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		if _, err := e.tx.Exec(ctx, `WITH s AS (INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id)
			INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) SELECT $1, s.id, $4, $5, '2020-01-01' FROM s`,
			e.a.ID, "LAB-"+itoa(i), "Siswa Lab "+itoa(i), e.a.ClassID, e.a.YearID); err != nil {
			t.Fatal(err)
		}
	}
	start := now.Add(time.Hour)
	ex := e.scheduledExam(t, start, e.qs[0].ID)
	cards, err := IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now)
	if err != nil || len(cards) < 40 {
		t.Fatalf("cards = %d, %v", len(cards), err)
	}
	e.clock = start.Add(-5 * time.Minute)
	for i, c := range cards[:40] {
		dev := "lab-" + itoa(i)
		// Salah ketik dulu, lalu benar. Semua request dari IP yang sama.
		if rec := e.form(dev, "/u/"+e.a.Code, url.Values{"identifier": {c.Identifier}, "code": {"SALAH123"}}); rec.Code == http.StatusSeeOther {
			t.Fatalf("wrong code accepted for %s", c.Identifier)
		}
		rec := e.form(dev, "/u/"+e.a.Code, url.Values{"identifier": {c.Identifier}, "code": {c.Code}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("login %d (%s) from shared IP = %d", i, c.Identifier, rec.Code)
		}
	}
}

// URL pendek: /u/?sekolah=kode mengarah ke halaman masuk sekolah.
func TestParticipantIndexSchoolCode(t *testing.T) {
	e := newTakeEnv(t)
	if rec := e.do("x", http.MethodGet, "/u/?sekolah="+strings.ToUpper(e.a.Code)+"+", "", ""); rec.Code != http.StatusSeeOther ||
		rec.Header().Get("Location") != "/u/"+e.a.Code {
		t.Errorf("school code = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	for _, bad := range []string{"tidak-ada", "../x", "%3Cb%3E"} {
		rec := e.do("x", http.MethodGet, "/u/?sekolah="+bad, "", "")
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Kode sekolah tidak ditemukan") {
			t.Errorf("bad code %q = %d", bad, rec.Code)
		}
	}
	if rec := e.do("x", http.MethodGet, "/u/", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "kode sekolah") {
		t.Errorf("index = %d", rec.Code)
	}
}

// Halaman ujian memuat pratinjau; gambar penuh hanya dengan ?penuh=1.
// Gambar lama (tanpa pratinjau) dibuat pratinjaunya saat pertama diminta.
func TestAttemptImagePreview(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	e.students(t)
	a, err := SaveAsset(ctx, e.tx, e.store, e.owner, e.bank.ID, photo(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	// Seperti gambar sebelum T142.
	if _, err := e.tx.Exec(ctx, `UPDATE exam_assets SET preview_key = NULL, preview_type = NULL, preview_bytes = NULL WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	q := sampleQuestion(KindMC)
	q.BodySrc = `<p>Lihat gambar <img data-asset="` + a.ID + `" alt="foto" loading="lazy"></p>`
	q.BodyHTML = q.BodySrc
	created, err := CreateQuestion(ctx, e.tx, e.owner, e.bank.ID, q, now)
	if err != nil {
		t.Fatal(err)
	}
	start := now.Add(time.Hour)
	ex := e.scheduledExam(t, start, created.ID)
	cards, _ := IssueCards(ctx, e.tx, e.owner, NewIssuer(), CardScope{ExamID: ex.ID}, ex.Name, "", false, now)
	e.clock = start.Add(time.Minute)
	e.login(t, "hp1", cards[0])
	loc := e.form("hp1", "/u/ujian/"+e.sessionOf(t, ex)+"/mulai", url.Values{}).Header().Get("Location")
	img := loc + "/gambar/" + a.ID
	if body := e.do("hp1", http.MethodGet, loc, "", "").Body.String(); !strings.Contains(body, `src="`+img+`"`) {
		t.Fatalf("take page lacks image %s", img)
	}
	rec := e.do("hp1", http.MethodGet, img, "", "")
	got, _ := GetAsset(ctx, e.tx, e.a.ID, a.ID)
	if rec.Code != http.StatusFound || got.PreviewKey == "" || got.PreviewKey == got.StorageKey ||
		!strings.Contains(rec.Header().Get("Location"), got.PreviewKey[strings.LastIndex(got.PreviewKey, "/")+1:]) {
		t.Errorf("preview redirect = %d %s (preview %q)", rec.Code, rec.Header().Get("Location"), got.PreviewKey)
	}
	full := e.do("hp1", http.MethodGet, img+"?penuh=1", "", "")
	if full.Code != http.StatusFound || !strings.Contains(full.Header().Get("Location"), got.StorageKey[strings.LastIndex(got.StorageKey, "/")+1:]) {
		t.Errorf("full redirect = %d %s", full.Code, full.Header().Get("Location"))
	}
}
