# Panduan Operator Lab dan Uji Perangkat Ujian

Untuk operator lab komputer dan tim yang menyiapkan perangkat ujian di sekolah.

## Alamat ujian

- Alamat pendek untuk diketik di setiap komputer: **`<domain>/u`**. Peserta mengetik kode sekolah (mis. `smpn1-contoh`), lalu NIS dan kode di kartu ujian.
- Alamat langsung per sekolah: `<domain>/u/<kode-sekolah>`. QR di kartu ujian sudah berisi alamat ini beserta NIS.
- Cek perangkat sebelum hari ujian: `<domain>/u/cek`. Buka di setiap komputer atau HP dengan jaringan yang sama seperti saat ujian.

## Mode kiosk (komputer lab)

Mode kiosk membuka browser layar penuh tanpa bilah alamat dan tab, sehingga peserta lebih sulit membuka halaman lain.

**Windows (Chrome).** Buat pintasan di desktop dengan target:

```
"C:\Program Files\Google\Chrome\Application\chrome.exe" --kiosk --incognito --no-first-run --disable-translate "https://<domain>/u/<kode-sekolah>"
```

Untuk Edge, ganti dengan `msedge.exe` dan opsi yang sama. Keluar dari kiosk: `Alt+F4`.

**Linux.**

```
google-chrome --kiosk --incognito --no-first-run "https://<domain>/u/<kode-sekolah>"
```

**Chromebook.** Gunakan mode tamu, buka alamat ujian, lalu tekan tombol layar penuh. Chromebook yang dikelola sekolah dapat memakai aplikasi kiosk dari konsol admin Google.

Catatan:

- `--incognito` memastikan tidak ada sisa login peserta sebelumnya. Karena setiap jendela incognito dianggap perangkat baru, **jangan menutup browser** selama ujian berlangsung; bila tertutup, pengawas perlu mengizinkan pindah perangkat.
- Matikan pembaruan otomatis dan *sleep* komputer selama jadwal ujian.
- Bunyi tanda keluar halaman memakai speaker komputer. Atur volume sebelum ujian dan lakukan tes bunyi di halaman konfirmasi.

## Banyak peserta di belakang satu IP (NAT)

Satu lab atau satu WiFi sekolah biasanya keluar ke internet dengan satu IP publik. Batas percobaan masuk dibuat longgar untuk satu IP (3.000 percobaan per 15 menit), sehingga ratusan siswa bisa masuk bersamaan walau sebagian salah ketik kode. Penebakan kode tetap dibatasi per nomor peserta: 5 kali salah mengunci kartu sampai pengawas membuka. Hal ini diuji otomatis untuk 40 siswa satu IP (`TestLabLoginsBehindOneIP`).

## Kebutuhan data

Satu ujian 40 soal dengan 10 gambar memerlukan sekitar **0,8–1 MB** per peserta: halaman ujian, tampilan, font, pratinjau gambar, dan simpan jawaban. Angka ini diuji otomatis (`TestExamDataBudget`, batas 2 MB). Gambar soal dimuat dalam ukuran ringan (sisi terpanjang 960 px); gambar penuh hanya dimuat saat peserta memperbesar gambar. Pada koneksi lambat atau mode hemat data, gambar dimuat saat soalnya dibuka, bukan di awal.

## Browser yang didukung

| Perangkat | Browser | Status |
|---|---|---|
| HP Android | Chrome ≥ 90 | Didukung (disarankan) |
| iPhone/iPad | Safari, iOS ≥ 15 | Didukung |
| Komputer | Chrome/Edge versi dua tahun terakhir, Firefox ESR | Didukung |
| HP Android | Samsung Internet, Mi Browser, Opera, browser bawaan vendor | Bisa dipakai, Chrome disarankan |
| Semua | Opera Mini, UC Browser, Internet Explorer, browser dalam WhatsApp/Instagram/Facebook | Tidak didukung |

Halaman cek perangkat menampilkan penilaian ini untuk browser yang sedang dipakai.

## Uji perangkat nyata (sebelum ujian pertama)

Jalankan skenario di bawah pada **minimal 6 perangkat nyata** dan isi tabel hasil. Uji otomatis di Chrome (lebar 360 px sampai 1920 px, HP lanskap, zoom 200%, teks Arab) sudah lulus, tetapi tidak menggantikan uji di perangkat dan browser bawaan vendor.

Skenario:

1. Buka `/u/cek`: semua butir hijau atau kuning, tes suara terdengar.
2. Masuk dengan kartu uji, tes bunyi, lalu mulai ujian latihan yang berisi pilihan ganda, menjodohkan, isian, esai, gambar, rumus, dan **teks Arab berharakat**.
3. Jawab semua soal. Perbesar satu gambar.
4. Keluar ke aplikasi lain lalu kembali: bunyi (sesi di ruang), getar, dan layar merah tampil.
5. Matikan data/WiFi, jawab 3 soal, muat ulang halaman, nyalakan lagi: semua jawaban sampai ke server.
6. Putar layar ke lanskap, perbesar huruf (A+), aktifkan kontras tinggi.
7. Kumpulkan.

Periksa teks Arab: font Arab dimuat (bukan kotak), harakat tidak bertumpuk, arah kanan-ke-kiri.

| Perangkat | OS/Browser | 1 | 2 | 3 | 4 | 5 | 6 | 7 | Arab | Catatan | Penguji/tanggal |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Android Go 2 GB | Chrome | | | | | | | | | | |
| Android 4 GB | Chrome + browser bawaan | | | | | | | | | | |
| iPhone SE/8 | Safari iOS 15+ | | | | | | | | | | |
| Tablet murah | Chrome | | | | | | | | | | |
| PC Windows 10 | Chrome lama + Edge | | | | | | | | | | |
| Chromebook | Chrome | | | | | | | | | | |
