package exam

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// Panduan cetak 1 halaman (T154): pengawas, peserta, dan orang tua calon
// siswa. Bahasa sederhana dengan gambar yang digambar langsung (layar HP,
// warna status), tanpa berkas gambar.

// Guide adalah jenis panduan cetak.
type Guide string

// Jenis panduan.
const (
	GuideProctor     Guide = "pengawas"
	GuideParticipant Guide = "peserta"
	GuideParent      Guide = "orang-tua"
)

// GuideInfo melengkapi panduan dengan data sekolah. Semua boleh kosong.
type GuideInfo struct {
	School   string
	LoginURL string // alamat masuk peserta, mis. https://akra.id/u/sdharapan
	CheckURL string // alamat cek perangkat, mis. https://akra.id/u/cek
	Support  Support
}

// Warna AKRA.
var (
	guideTeal  = [3]int{13, 148, 136}
	guideInk   = [3]int{15, 23, 42}
	guideMuted = [3]int{71, 85, 105}
	guideLine  = [3]int{203, 213, 225}
)

// guideDoc membungkus fpdf dengan bantuan tata letak panduan.
type guideDoc struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
	w   float64 // lebar isi
	m   float64 // margin
	// overflow: isi melewati batas footer (panduan harus satu halaman).
	overflow bool
}

func newGuideDoc(title string, printedAt time.Time) *guideDoc {
	pdf, tr := newDocPDF(title, "P", printedAt, true)
	const m = 14.0
	pdf.SetMargins(m, m, m)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()
	w, _ := pdf.GetPageSize()
	return &guideDoc{pdf: pdf, tr: tr, w: w - 2*m, m: m}
}

func (g *guideDoc) color(c [3]int) { g.pdf.SetTextColor(c[0], c[1], c[2]) }
func (g *guideDoc) fill(c [3]int)  { g.pdf.SetFillColor(c[0], c[1], c[2]) }
func (g *guideDoc) draw(c [3]int)  { g.pdf.SetDrawColor(c[0], c[1], c[2]) }

// header menggambar pita judul.
func (g *guideDoc) header(title, sub, school string) {
	p := g.pdf
	g.fill(guideTeal)
	p.Rect(0, 0, 210, 30, "F")
	p.SetTextColor(255, 255, 255)
	p.SetFont("Helvetica", "B", 9)
	p.SetXY(g.m, 7)
	label := "AKRA · UJIAN ONLINE"
	if school != "" {
		label += " · " + strings.ToUpper(school)
	}
	p.CellFormat(g.w, 4, fitText(p, g.tr, label, g.w), "", 2, "L", false, 0, "")
	p.SetFont("Helvetica", "B", 20)
	p.CellFormat(g.w, 10, g.tr(title), "", 2, "L", false, 0, "")
	p.SetFont("Helvetica", "", 9.5)
	p.CellFormat(g.w, 5, fitText(p, g.tr, sub, g.w), "", 2, "L", false, 0, "")
	p.SetY(36)
}

// heading menulis judul bagian dengan nomor bulat.
func (g *guideDoc) heading(x, w float64, n, title string) {
	p := g.pdf
	y := p.GetY()
	if n != "" {
		g.fill(guideTeal)
		p.Circle(x+3.2, y+3.2, 3.2, "F")
		p.SetTextColor(255, 255, 255)
		p.SetFont("Helvetica", "B", 9)
		p.SetXY(x, y+0.7)
		p.CellFormat(6.4, 5, n, "", 0, "C", false, 0, "")
		x += 8.5
		w -= 8.5
	}
	g.color(guideInk)
	p.SetFont("Helvetica", "B", 11.5)
	p.SetXY(x, y)
	p.CellFormat(w, 6.4, g.tr(title), "", 2, "L", false, 0, "")
	p.SetY(y + 7.5)
}

// bullets menulis daftar poin di kolom x..x+w. Teks di antara ** dicetak
// tebal, di bagian mana pun kalimat.
func (g *guideDoc) bullets(x, w float64, items ...string) {
	p := g.pdf
	pw, _ := p.GetPageSize()
	const size, lh = 10.0, 4.9
	for _, it := range items {
		y := p.GetY()
		g.fill(guideTeal)
		p.Circle(x+1.2, y+2.4, 0.85, "F")
		g.color(guideInk)
		// Margin sementara agar teks dibungkus di dalam kolom.
		p.SetLeftMargin(x + 3.8)
		p.SetRightMargin(pw - (x + w))
		p.SetXY(x+3.8, y)
		for k, seg := range strings.Split(it, "**") {
			if seg == "" {
				continue
			}
			style := ""
			if k%2 == 1 {
				style = "B"
			}
			p.SetFont("Helvetica", style, size)
			p.Write(lh, g.tr(seg))
		}
		p.SetLeftMargin(g.m)
		p.SetRightMargin(g.m)
		p.Ln(lh)
		p.SetY(p.GetY() + 1.3)
	}
}

// phone menggambar HP kecil dengan layar berisi gambar isi.
func (g *guideDoc) phone(x, y, w, h float64, screen func(sx, sy, sw, sh float64)) {
	p := g.pdf
	g.draw(guideInk)
	p.SetLineWidth(0.6)
	p.SetFillColor(255, 255, 255)
	p.RoundedRect(x, y, w, h, 3, "1234", "FD")
	p.SetLineWidth(0.2)
	g.draw(guideLine)
	p.Line(x+w/2-3, y+2.2, x+w/2+3, y+2.2)
	screen(x+2, y+4.5, w-4, h-8)
}

// box menggambar kotak berisi teks (tombol, isian) di layar HP.
func (g *guideDoc) box(x, y, w, h float64, fill [3]int, text string, textColor [3]int, size float64) {
	p := g.pdf
	g.fill(fill)
	g.draw(guideLine)
	p.SetLineWidth(0.2)
	p.RoundedRect(x, y, w, h, 1, "1234", "FD")
	if text != "" {
		g.color(textColor)
		p.SetFont("Helvetica", "B", size)
		p.SetXY(x, y)
		p.CellFormat(w, h, g.tr(text), "", 0, "C", false, 0, "")
	}
}

// footer menulis bantuan di dasar halaman.
func (g *guideDoc) footer(text string, s Support) {
	p := g.pdf
	_, ph := p.GetPageSize()
	y := ph - 18
	if p.GetY() > y {
		g.overflow = true
	}
	g.draw(guideLine)
	p.SetLineWidth(0.3)
	p.Line(g.m, y, g.m+g.w, y)
	g.color(guideMuted)
	p.SetFont("Helvetica", "", 8.5)
	p.SetXY(g.m, y+2)
	if s.Configured() {
		var c []string
		if s.WhatsApp != "" {
			c = append(c, "WhatsApp "+s.WhatsApp)
		}
		if s.Email != "" {
			c = append(c, s.Email)
		}
		text += " Dukungan AKRA: " + strings.Join(c, " · ") + "."
	}
	p.MultiCell(g.w, 4.2, g.tr(text), "", "L", false)
}

// WriteGuidePDF menulis panduan satu halaman jenis kind.
func WriteGuidePDF(w io.Writer, kind Guide, info GuideInfo, printedAt time.Time) error {
	var g *guideDoc
	switch kind {
	case GuideProctor:
		g = newGuideDoc("Panduan Pengawas Ujian", printedAt)
		proctorGuide(g, info)
	case GuideParticipant:
		g = newGuideDoc("Panduan Peserta Ujian", printedAt)
		participantGuide(g, info)
	case GuideParent:
		g = newGuideDoc("Panduan Orang Tua: Tes Seleksi dari Rumah", printedAt)
		parentGuide(g, info)
	default:
		return fmt.Errorf("exam: unknown guide %q", kind)
	}
	if n := g.pdf.PageNo(); n != 1 || g.overflow {
		return fmt.Errorf("exam: guide %s does not fit one page", kind)
	}
	return g.pdf.Output(w)
}

func proctorGuide(g *guideDoc, info GuideInfo) {
	p := g.pdf
	g.header("Panduan Pengawas Ujian", "Sebelum, saat, dan sesudah sesi. Jawaban peserta tersimpan otomatis; tugas Anda memantau dan membantu.", info.School)
	colW := (g.w - 8) / 3
	top := p.GetY()
	cols := []struct {
		n, title string
		items    []string
	}{
		{"1", "Sebelum sesi", []string{
			"**Datang 30 menit lebih awal.** Buka AKRA, menu Ujian, Sesi saya, lalu Layar pengawas.",
			"**Periksa perangkat:** baterai terisi, internet tersambung, volume HP dinaikkan, mode senyap dimatikan.",
			"**Bagikan kartu peserta.** Bila sesi memakai token, tampilkan token layar penuh atau diktekan.",
		}},
		{"2", "Saat sesi", []string{
			"**Pantau warna** di layar pengawas (lihat di bawah). Datangi peserta yang offline atau perlu perhatian.",
			"**Ketuk nama peserta** untuk aksi: izinkan pindah perangkat, reset login, buka kunci, tambah waktu.",
			"**Gangguan lebih dari 5 menit:** jeda sesi. Waktu yang dijeda dikembalikan.",
			"**Catat kejadian.** Catatan masuk berita acara.",
		}},
		{"3", "Sesudah sesi", []string{
			"**Pastikan semua biru (selesai).** Yang belum mengumpulkan dikumpulkan otomatis saat waktu habis.",
			"**Isi dan tandatangani berita acara** di layar pengawas dengan sandi akun Anda. Unduh PDF bila perlu.",
			"**Kumpulkan kartu peserta** dan serahkan ke panitia.",
		}},
	}
	bottom := top
	for i, c := range cols {
		x := g.m + float64(i)*(colW+4)
		p.SetY(top)
		g.heading(x, colW, c.n, c.title)
		g.bullets(x, colW, c.items...)
		bottom = max(bottom, p.GetY())
	}

	// Arti warna: gambar baris peserta seperti di layar pengawas.
	p.SetY(bottom + 4)
	g.heading(g.m, g.w, "", "Arti warna di layar pengawas")
	rows := []struct {
		dot   [3]int
		alert bool
		name  string
		desc  string
	}{
		{[3]int{203, 213, 225}, false, "Belum mulai", "Belum login atau belum menekan Mulai."},
		{[3]int{16, 185, 129}, false, "Mengerjakan", "Normal. Terlihat jumlah soal terjawab dan sisa waktu."},
		{[3]int{245, 158, 11}, false, "Offline", "Sinyal hilang. Jawaban terkirim saat tersambung lagi."},
		{[3]int{37, 99, 235}, false, "Selesai", "Jawaban sudah dikumpulkan."},
		{[3]int{220, 38, 38}, true, "Perlu perhatian", "Baris merah: keluar halaman, terkunci, pindah perangkat."},
	}
	y := p.GetY()
	half := (g.w - 6) / 2
	for i, r := range rows {
		x := g.m + float64(i%2)*(half+6)
		ry := y + float64(i/2)*11
		if r.alert {
			p.SetFillColor(254, 242, 242)
			p.SetDrawColor(252, 165, 165)
		} else {
			p.SetFillColor(255, 255, 255)
			g.draw(guideLine)
		}
		p.SetLineWidth(0.3)
		p.RoundedRect(x, ry, half, 9, 1.5, "1234", "FD")
		g.fill(r.dot)
		p.Circle(x+5, ry+4.5, 2, "F")
		g.color(guideInk)
		p.SetFont("Helvetica", "B", 9.5)
		p.SetXY(x+9, ry+0.8)
		p.CellFormat(half-10, 4, g.tr(r.name), "", 2, "L", false, 0, "")
		p.SetFont("Helvetica", "", 8)
		g.color(guideMuted)
		p.SetX(x + 9)
		p.CellFormat(half-10, 3.8, fitText(p, g.tr, r.desc, half-11), "", 0, "L", false, 0, "")
	}
	p.SetY(y + 3*11 + 2)

	g.heading(g.m, g.w, "", "Aksi yang paling sering dipakai")
	actW := (g.w - 4) / 2
	y = p.GetY()
	p.SetY(y)
	g.bullets(g.m, actW,
		"**Izinkan pindah perangkat:** HP mati, rusak, atau baterai habis. Peserta masuk lagi di perangkat lain dengan kode yang sama.",
		"**Reset login (kode baru):** kartu hilang atau kode salah 5 kali. Diktekan kode baru; kartu lama tidak berlaku untuk sesi ini.",
	)
	left := p.GetY()
	p.SetY(y)
	g.bullets(g.m+actW+4, actW,
		"**Buka kunci:** layar peserta terkunci setelah beberapa kali keluar halaman. Periksa peserta dulu.",
		"**Tambah waktu:** untuk satu peserta atau semua, isi alasannya agar tercatat di berita acara.",
	)
	p.SetY(max(left, p.GetY()) + 2)

	g.heading(g.m, g.w, "", "Keterbatasan bunyi")
	g.bullets(g.m, g.w,
		"Perangkat berbunyi saat peserta keluar dari halaman ujian (sesi di ruang). Volume dan mode senyap HP tidak bisa diatur dari browser.",
		"iPhone/iPad dalam mode senyap bisa tidak berbunyi. **Indikator di layar pengawas adalah acuan utama**, bukan bunyi.",
	)
	g.footer("Butuh bantuan? Buka AKRA, menu Ujian, lalu Bantuan: langkah untuk peserta tidak bisa masuk, HP mati, offline, dan layar terkunci.", info.Support)
}

func participantGuide(g *guideDoc, info GuideInfo) {
	p := g.pdf
	g.header("Panduan Peserta Ujian", "Baca sebelum ujian. Jawabanmu tersimpan otomatis setiap kali memilih atau mengetik.", info.School)
	type step struct {
		n, title string
		items    []string
		screen   func(sx, sy, sw, sh float64)
	}
	small := func(x, y, w float64, s string, c [3]int, size float64) {
		g.color(c)
		p.SetFont("Helvetica", "", size)
		p.SetXY(x, y)
		p.CellFormat(w, 3, g.tr(s), "", 0, "L", false, 0, "")
	}
	white := [3]int{255, 255, 255}
	steps := []step{
		{"1", "Masuk", []string{
			"Buka **alamat di kartu** atau pindai QR-nya.",
			"Ketik NIS (atau nomor pendaftaran) dan kode di kartu, lalu tekan Masuk.",
			"Jangan berikan kodemu kepada orang lain.",
		}, func(sx, sy, sw, sh float64) {
			small(sx+1, sy+1, sw, "NIS", guideMuted, 6)
			g.box(sx+1, sy+4, sw-2, 5, white, "", guideInk, 7)
			small(sx+1, sy+11, sw, "Kode kartu", guideMuted, 6)
			g.box(sx+1, sy+14, sw-2, 5, white, "", guideInk, 7)
			g.box(sx+1, sy+22, sw-2, 6, guideTeal, "Masuk", white, 7)
		}},
		{"2", "Kerjakan", []string{
			"Pilih jawaban. Jawaban tersimpan otomatis.",
			"Ketuk **Daftar soal** untuk pindah nomor. Hijau: sudah dijawab. Abu-abu: belum.",
		}, func(sx, sy, sw, sh float64) {
			for i := 0; i < 9; i++ {
				c := [3]int{241, 245, 249}
				if i < 4 {
					c = [3]int{209, 250, 229}
				}
				cx := sx + 1 + float64(i%3)*(sw-2)/3
				cy := sy + 2 + float64(i/3)*8
				g.box(cx+0.5, cy, (sw-2)/3-1, 6.5, c, fmt.Sprint(i+1), guideInk, 7)
			}
		}},
		{"3", "Ragu-ragu", []string{
			"Belum yakin? Tekan tombol **Ragu-ragu** agar nomornya kuning dan mudah dicek lagi sebelum mengumpulkan.",
		}, func(sx, sy, sw, sh float64) {
			small(sx+1, sy+2, sw, "Soal 5", guideInk, 7)
			g.box(sx+1, sy+7, sw-2, 5, white, "A", guideInk, 6)
			g.box(sx+1, sy+13, sw-2, 5, white, "B", guideInk, 6)
			g.box(sx+1, sy+21, sw-2, 6, [3]int{253, 230, 138}, "Ragu-ragu", guideInk, 7)
		}},
		{"4", "Sinyal hilang", []string{
			"Tetap kerjakan. Jawaban disimpan di perangkat dan terkirim saat sinyal kembali.",
			"**Jangan tutup halaman** dan jangan hapus riwayat browser.",
		}, func(sx, sy, sw, sh float64) {
			g.box(sx+1, sy+2, sw-2, 7, [3]int{254, 243, 199}, "Offline", [3]int{146, 64, 14}, 7)
			small(sx+1, sy+12, sw-2, "Jawaban aman", guideMuted, 6)
			small(sx+1, sy+16, sw-2, "di perangkat", guideMuted, 6)
		}},
		{"5", "Kumpulkan", []string{
			"Cek nomor abu-abu dan kuning, lalu tekan **Kumpulkan**. Setelah dikumpulkan, jawaban tidak bisa diubah.",
			"Waktu habis: jawaban dikumpulkan otomatis.",
		}, func(sx, sy, sw, sh float64) {
			small(sx+1, sy+2, sw, "Terjawab 20/20", guideInk, 6.5)
			g.box(sx+1, sy+20, sw-2, 7, guideTeal, "Kumpulkan", white, 7)
		}},
	}
	// Setiap langkah: HP di kiri, teks di kanan.
	const phoneW, phoneH, rowH = 26.0, 40.0, 52.0
	half := (g.w - 8) / 2
	for i, s := range steps {
		x := g.m + float64(i%2)*(half+8)
		y := 37 + float64(i/2)*rowH
		g.phone(x, y, phoneW, phoneH, s.screen)
		p.SetY(y + 1)
		g.heading(x+phoneW+4, half-phoneW-4, s.n, s.title)
		g.bullets(x+phoneW+4, half-phoneW-4, s.items...)
		if p.GetY() > y+rowH-2 {
			g.overflow = true // teks menabrak langkah di bawahnya
		}
	}
	// Kotak terakhir (sel kosong) untuk aturan penting.
	x := g.m + half + 8
	y := 37 + 2*rowH
	g.fill([3]int{240, 253, 250})
	g.draw(guideTeal)
	p.SetLineWidth(0.4)
	p.RoundedRect(x, y, half, phoneH, 2, "1234", "FD")
	p.SetY(y + 3)
	g.heading(x+3, half-6, "!", "Penting")
	g.bullets(x+3, half-6,
		"**Jangan keluar dari halaman ujian.** Perangkat berbunyi dan kejadian tercatat di layar pengawas.",
		"Ada masalah? **Angkat tangan** dan panggil pengawas. Jawabanmu tidak hilang.",
	)
	p.SetY(37 + 3*rowH + 2)
	g.heading(g.m, g.w, "", "Sebelum hari ujian")
	before := []string{
		"Isi baterai HP dan pastikan kuota/sinyal cukup. Pakai Chrome atau Safari versi terbaru.",
		"Coba **ujian latihan** bila guru membukanya, agar terbiasa dengan halaman ujian.",
	}
	if info.LoginURL != "" {
		before = append([]string{"Alamat masuk ujian: **" + info.LoginURL + "**"}, before...)
	}
	g.bullets(g.m, g.w, before...)
	g.footer("Simpan kartu peserta sampai ujian selesai. Kode di kartu hanya berlaku pada jadwal ujianmu.", Support{})
}

func parentGuide(g *guideDoc, info GuideInfo) {
	p := g.pdf
	g.header("Tes Seleksi dari Rumah", "Panduan untuk orang tua calon siswa. Siapkan perangkat sehari sebelumnya agar tes berjalan lancar.", info.School)
	check := "alamat cek perangkat dari sekolah (…/u/cek)"
	if info.CheckURL != "" {
		check = info.CheckURL
	}
	type step struct {
		n, title string
		items    []string
	}
	steps := []step{
		{"1", "Cek perangkat (H-1)", []string{
			"Buka **" + check + "** di HP atau laptop yang akan dipakai.",
			"Pastikan semua pemeriksaan berwarna hijau. Bila ada yang merah, ikuti saran di layar atau pakai perangkat lain.",
		}},
		{"2", "Siapkan", []string{
			"Baterai penuh atau pengisi daya tersambung, internet stabil dan kuota cukup.",
			"Tempat tenang. Chrome atau Safari versi terbaru. Matikan notifikasi yang mengganggu.",
		}},
		{"3", "Hari tes", []string{
			"Buka **alamat di kartu tes** (di halaman status pendaftaran) atau pindai QR-nya.",
			"Masukkan nomor pendaftaran dan kode kartu. Tes hanya bisa dimulai sesuai jadwal.",
		}},
		{"4", "Selama tes", []string{
			"Anak mengerjakan sendiri. **Jangan pindah aplikasi atau tab**: kejadian tercatat.",
			"Sinyal putus? Jangan tutup halaman. Jawaban tersimpan di perangkat dan terkirim otomatis.",
		}},
		{"5", "Selesai", []string{
			"Tekan **Kumpulkan**. Muncul bukti bahwa jawaban sudah diterima.",
			"Hasil seleksi diumumkan sekolah. Nilai dan pembahasan tes tidak ditampilkan.",
		}},
	}
	// Gambar besar: HP dengan centang hijau (cek perangkat).
	g.phone(g.m+g.w-46, 40, 40, 62, func(sx, sy, sw, sh float64) {
		rows := []string{"Browser", "Internet", "Penyimpanan", "Bunyi"}
		for i, r := range rows {
			ry := sy + 3 + float64(i)*10
			p.SetFillColor(209, 250, 229)
			p.Circle(sx+4, ry+2.5, 2.5, "F")
			p.SetDrawColor(5, 150, 105)
			p.SetLineWidth(0.6)
			p.Line(sx+2.8, ry+2.6, sx+3.7, ry+3.5)
			p.Line(sx+3.7, ry+3.5, sx+5.3, ry+1.5)
			g.color(guideInk)
			p.SetFont("Helvetica", "", 7)
			p.SetXY(sx+8, ry+0.5)
			p.CellFormat(sw-9, 4, g.tr(r), "", 0, "L", false, 0, "")
		}
		g.box(sx+1, sy+sh-10, sw-2, 7, guideTeal, "Siap ujian", [3]int{255, 255, 255}, 7.5)
	})
	textW := g.w - 52
	p.SetY(37)
	for _, s := range steps {
		w := g.w
		if p.GetY() < 106 {
			w = textW
		}
		g.heading(g.m, w, s.n, s.title)
		g.bullets(g.m, w, s.items...)
		p.SetY(p.GetY() + 2)
	}
	p.SetY(p.GetY() + 2)
	g.fill([3]int{240, 253, 250})
	g.draw(guideTeal)
	p.SetLineWidth(0.4)
	y := p.GetY()
	p.RoundedRect(g.m, y, g.w, 22, 2, "1234", "FD")
	p.SetY(y + 3)
	g.heading(g.m+3, g.w-6, "?", "Ada kendala?")
	g.bullets(g.m+3, g.w-6, "Hubungi panitia penerimaan sekolah sebelum jadwal tes berakhir. Sebutkan nama calon siswa, nomor pendaftaran, dan pesan yang muncul di layar.")
	g.footer("Simpan kartu tes. Kode di kartu hanya berlaku pada jadwal tes dan tidak boleh diberikan kepada orang lain.", Support{})
}
