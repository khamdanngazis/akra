# T143 — Layar pengawas

**Fase:** 18 Hari-H
**Dependensi:** T125, T139, T141
**Estimasi:** 3 hari

## Tujuan
Pengawas cukup memegang HP untuk memimpin sesi: membacakan token, melihat siapa yang bermasalah, dan menolong peserta tanpa memanggil operator.

## Tampilan (HP lebih dulu)
1. **Masuk:** guru melihat "Sesi saya hari ini" di beranda sebagai kartu besar (ruang, jam, jumlah peserta).
2. **Header sesi:**
   - **token besar** dengan hitung mundur rotasi dan tombol "Tampilkan layar penuh" untuk ditunjukkan ke kelas atau proyektor;
   - status sesi (belum mulai / berlangsung / selesai) dan sisa waktu.
3. **Ringkasan angka:** belum login · mengerjakan · offline · perlu perhatian · selesai. Mengetuk angka memfilter daftar.
4. **Daftar peserta:**
   - urutan: yang perlu perhatian di atas (terkunci, keluar halaman berulang, offline, minta pindah perangkat);
   - per peserta: nama/nomor, progres (terjawab X/Y), sisa waktu, jumlah keluar halaman, status koneksi;
   - **tanda baru** berupa getar dan lencana saat ada kejadian keluar halaman (pelengkap bunyi di perangkat siswa).
5. **Aksi per peserta** (bottom sheet, maks. 2 ketukan):
   - izinkan pindah perangkat;
   - buka kunci;
   - reset login (kode baru tampil di layar untuk didiktekan);
   - tambah waktu;
   - jeda;
   - kumpulkan paksa;
   - catat kejadian (teks, untuk berita acara).
6. **Aksi sesi:** jeda semua, tambah waktu semua (mis. listrik padam), tutup sesi.
7. **Akhir sesi:**
   - berita acara terisi otomatis (hadir, tidak hadir, kejadian, catatan);
   - pengawas menambah catatan lalu **menandatangani** (konfirmasi nama + PIN akun);
   - PDF bisa diunduh (T135).
8. Pembaruan tiap 10 detik lewat polling ringan (satu query agregat per sesi), dengan indikator "diperbarui 3 detik lalu".

## Akses
- Hanya pengawas sesi itu (dalam jendela waktu), koordinator, admin_tu, dan kepala (baca).
- Setiap aksi tercatat di audit dengan pelaku dan alasan.

## File yang disentuh
- `internal/exam/proctor.go`, `proctor_query.go`, tes, `proctor_handler.go`
- `web/templates/exam/proctor.html`, `proctor_token_fullscreen.html`, `my_sessions.html`

## Kriteria selesai
- [ ] Pengawas melakukan reset login dan izin pindah perangkat dalam ≤ 2 ketukan dari daftar
- [ ] Kejadian keluar halaman muncul ≤ 15 detik
- [ ] Query polling 500 peserta < 50 ms (uji dengan data seed)
- [ ] Guru bukan pengawas sesi → 404; pengawas di luar jendela waktu → hanya baca
- [ ] Uji pengguna dengan 3 guru: memimpin sesi simulasi tanpa panduan tertulis
