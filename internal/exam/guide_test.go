package exam

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestGuidePDFsOnePage memastikan setiap panduan cetak muat satu halaman
// A4, juga dengan alamat dan kontak yang panjang (T154).
func TestGuidePDFsOnePage(t *testing.T) {
	long := GuideInfo{School: "SMA Negeri 1 Contoh Kabupaten Yang Namanya Cukup Panjang Sekali",
		LoginURL: "https://akra.sekolah.example.id/u/sman1-contoh-kabupaten-panjang", CheckURL: "https://akra.sekolah.example.id/u/cek",
		Support: Support{WhatsApp: "0812-3456-7890", Email: "dukungan@akra.example.id", Hours: "Senin–Jumat 07.00–16.00 WIB"}}
	for _, kind := range []Guide{GuideProctor, GuideParticipant, GuideParent} {
		for _, info := range []GuideInfo{{}, long} {
			var b bytes.Buffer
			if err := WriteGuidePDF(&b, kind, info, time.Unix(1_800_000_000, 0)); err != nil {
				t.Fatalf("%s: %v", kind, err)
			}
			if n := bytes.Count(b.Bytes(), []byte("/Type /Page\n")); n != 1 {
				t.Errorf("%s: %d pages", kind, n)
			}
		}
	}
	if err := WriteGuidePDF(&bytes.Buffer{}, Guide("lain"), GuideInfo{}, time.Now()); err == nil {
		t.Error("unknown guide accepted")
	}
}

// TestGuideText memeriksa isi panduan pengawas (PDF tanpa kompresi).
func TestGuideText(t *testing.T) {
	g := newGuideDoc("x", time.Unix(1_800_000_000, 0))
	g.pdf.SetCompression(false)
	proctorGuide(g, GuideInfo{Support: Support{WhatsApp: "081234567890"}})
	var b bytes.Buffer
	if err := g.pdf.Output(&b); err != nil {
		t.Fatal(err)
	}
	text := pdfText(b.Bytes())
	for _, want := range []string{"Sebelum sesi", "Saat sesi", "Sesudah sesi", "Arti warna", "Offline", "Perlu perhatian",
		"Izinkan pindah perangkat:", "Reset login", "Keterbatasan bunyi", "WhatsApp 081234567890"} {
		if !strings.Contains(text, want) {
			t.Errorf("proctor guide missing %q", want)
		}
	}
}

// pdfText menggabungkan string teks (Tj) di PDF tanpa kompresi.
func pdfText(b []byte) string {
	var out strings.Builder
	for _, m := range regexp.MustCompile(`\((.*?)\) ?Tj`).FindAllSubmatch(b, -1) {
		out.Write(m[1])
		out.WriteByte(' ')
	}
	return out.String()
}

func TestGuideRoutes(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/ujian/panduan/pengawas.pdf", "/ujian/panduan/peserta.pdf", "/ujian/panduan/orang-tua.pdf"} {
		rec := e.get(p, e.a.Teacher)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	rec := e.get("/ujian/bantuan", e.a.Teacher)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Peserta tidak bisa masuk") || !strings.Contains(body, "/u/"+e.a.Code) ||
		!strings.Contains(body, "Hubungi admin TU") {
		t.Errorf("help page = %d", rec.Code)
	}
	if rec := e.get("/ujian/bantuan", e.a.Treasurer); rec.Code != http.StatusForbidden {
		t.Errorf("treasurer help = %d", rec.Code)
	}
}

func TestSupportContact(t *testing.T) {
	cases := map[string]string{"0812-3456-7890": "https://wa.me/6281234567890", "+62 812 3456 7890": "https://wa.me/6281234567890", "123": "", "": ""}
	for in, want := range cases {
		if got := (Support{WhatsApp: in}).WhatsAppURL(); got != want {
			t.Errorf("WhatsAppURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCardsGuideBack: 10 kartu dengan panduan = 2 halaman depan + 2
// halaman belakang, satu panduan per kartu (T154).
func TestCardsGuideBack(t *testing.T) {
	cards := make([]Card, 10)
	for i := range cards {
		cards[i] = Card{Name: fmt.Sprintf("Siswa %d", i+1), Identifier: fmt.Sprint(1000 + i), Code: "ABCD2345", LoginURL: "https://akra.test/u/x?nis=1", Title: "PAS"}
	}
	count := func(opts CardOptions) (int, string) {
		var b bytes.Buffer
		if err := WriteCardsPDFWith(&b, Letterhead{School: "SMP Uji"}, cards, time.Unix(1_800_000_000, 0), false, opts); err != nil {
			t.Fatal(err)
		}
		return bytes.Count(b.Bytes(), []byte("/Type /Page\n")), pdfText(b.Bytes())
	}
	if n, text := count(CardOptions{}); n != 2 || strings.Contains(text, "CARA MENGIKUTI UJIAN") {
		t.Errorf("without guide: %d pages", n)
	}
	n, text := count(CardOptions{Guide: true})
	if n != 4 || strings.Count(text, "CARA MENGIKUTI UJIAN") != 10 {
		t.Errorf("with guide: %d pages, %d guides", n, strings.Count(text, "CARA MENGIKUTI UJIAN"))
	}
}
