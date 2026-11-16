# T138 — Halaman ujian peserta

**Fase:** 17 Pelaksanaan
**Dependensi:** T124, T133
**Estimasi:** 6 hari

## Tujuan
Siswa SD kelas atas sampai SMA, serta calon siswa, bisa mengerjakan ujian di HP maupun komputer tanpa bingung. Tampilannya mengikuti pola ANBK yang sudah dikenal.

## Alur
1. **Beranda peserta:** ujian yang dibuka untuknya (nama, mapel, waktu, status).
2. **Konfirmasi:**
   - nama, mapel, durasi, jumlah soal, dan aturan singkat ("Jangan keluar dari halaman ini, akan berbunyi");
   - **tes suara** bila sesi memakai bunyi tanda;
   - input **token** bila diwajibkan.
3. **Mulai:**
   - server membuat attempt: varian, urutan soal/opsi hasil acak (disimpan), `deadline_at`, `device_id`;
   - layar penuh diminta.
4. **Mengerjakan.**
5. **Ringkasan sebelum kumpul:** "Terjawab 37 dari 40 · Ragu-ragu 2 · Kosong 1", dengan tautan langsung ke soal yang kosong atau ragu.
6. **Konfirmasi kumpul** (dua langkah, agar tidak terpencet).
7. **Selesai:** "Jawaban sudah diterima pukul 09.42", plus nilai atau pembahasan bila diizinkan.

## Tampilan mengerjakan
- **Header tetap:** nama peserta, hitung mundur (berubah kuning di 10 menit terakhir, merah di 1 menit terakhir), indikator simpan (Tersimpan / Menyimpan… / Offline — aman).
- **Satu soal per layar:** nomor besar; bagian dan instruksinya bisa dilipat.
- **Opsi berupa kartu besar** (tinggi ≥ 48 px), bisa diketuk di mana saja pada kartu; yang dipilih jelas (warna dan ikon centang, tidak hanya warna).
- **Kontrol per jenis soal:**
  - menjodohkan: pilih kiri lalu kanan (bukan seret), karena seret di HP rawan salah;
  - isian: keyboard angka muncul bila jawabannya angka;
  - esai: textarea yang tumbuh, penghitung kata, draf disimpan.
- **Tombol bawah tetap:** ← Sebelumnya · **Ragu-ragu** (kuning) · Berikutnya →.
- **Grid nomor soal:**
  - di HP lewat tombol "Daftar soal" (bottom sheet); di desktop selalu tampil di samping;
  - warna dan ikon: terjawab hijau ✓, ragu kuning ?, kosong abu.
- Gambar bisa diketuk untuk diperbesar (zoom/pinch).
- Rumus dirender dari HTML server, tanpa JS rumus di halaman ini.

## Aksesibilitas dan kenyamanan
- Ukuran huruf A− / A / A+ (disimpan per peserta), mode kontras tinggi, dan dukungan pembaca layar untuk soal teks.
- Bisa dipakai penuh dengan keyboard di lab: angka 1–6 memilih opsi, ←/→ pindah soal, R untuk ragu.
- Tidak ada animasi berat. Total transfer awal < 150 KB di luar gambar.

## Keamanan
- Payload soal dikirim sekali saat halaman dibuka: hanya soal dan opsi dari snapshot, tanpa kunci atau pembahasan.
- Gambar memakai URL presign yang berlaku sampai `deadline_at`.
- Setelah kumpul, halaman soal tidak bisa dibuka lagi. Payload yang tersimpan di cache service worker dihapus.

## Stimulus bersama
1. **Desktop/tablet lanskap:** tampilan dua kolom. Stimulus di kiri dan bisa di-scroll sendiri; soal di kanan. Pindah antar-soal dalam kelompok yang sama tidak mengubah posisi scroll stimulus. Pembatas kolom bisa digeser.
2. **HP:** stimulus tampil di atas soal sebagai kartu yang bisa dilipat ("Teks bacaan · ketuk untuk membaca"). Tombol mengambang **📄 Teks** membuka stimulus di lembar penuh tanpa kehilangan jawaban.
3. Grid nomor soal menandai soal-soal yang berbagi stimulus (garis atau warna kelompok).
4. Stimulus panjang dengan nomor baris tetap bisa dibaca di 360 px, dan ukuran huruf mengikuti pengaturan A−/A/A+.

## Tulisan Arab
- Arah tulisan per paragraf dari soal; font Arab dari ADR T123, dimuat hanya bila ujian memuat teks Arab.
- Opsi Arab tersusun kanan-ke-kiri. Kontrol navigasi dan grid nomor tetap kiri-ke-kanan.

## File yang disentuh
- `db/migrations/0000XX_exam_attempts.up.sql` dan `.down.sql`
- `internal/exam/attempt.go`, `delivery.go` (payload peserta), tes, `take_handler.go`
- `web/templates/exam/take.html`, `take_confirm.html`, `take_done.html`, `participant_home.html`
- `web/static/js/exam-take.js`, CSS halaman ujian

## Kriteria selesai
- [ ] Payload dan HTML peserta tidak memuat kunci atau pembahasan, untuk semua jenis soal (uji otomatis)
- [ ] Muat ulang mempertahankan urutan, varian, jawaban, dan posisi soal
- [ ] Uji pengguna dengan 5 siswa SD kelas 5–6 dan 5 siswa SMP: semua bisa memulai, menandai ragu, dan mengumpulkan tanpa bantuan
- [ ] Lighthouse aksesibilitas ≥ 95; navigasi keyboard penuh di desktop
- [ ] Mulus di HP Android 2 GB RAM (scroll dan pindah soal < 100 ms)
- [ ] Ujian Bahasa Indonesia dengan 3 teks bacaan nyaman dikerjakan di HP 360 px dan desktop (uji pengguna 5 siswa)
- [ ] Ujian PAI dengan teks Arab berharakat tampil benar di Chrome Android dan Safari iOS
