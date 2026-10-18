# Uji beban ringan (T61)

Tujuan: memastikan aplikasi tetap responsif saat beberapa pengguna bekerja bersamaan dengan data mendekati kondisi pilot, dan menangkap masalah yang jelas sebelum uji internal (T63). Ini bukan uji produksi.

## Alat

| Alat | Versi |
|---|---|
| k6 | v0.49.0 (commit b5328aa782, go1.21.6, linux/amd64) |
| Data uji | `cmd/loadseed` (repositori ini) |

## Data uji

`cmd/loadseed` mengisi database dengan tiga sekolah (`beban-1` sampai `beban-3`). Setiap sekolah berisi:

- 300 siswa di 12 kelas (1A–6B), masing-masing dengan wali;
- 3 jenis tagihan × 6 bulan (Juli–Desember 2026) = **5.400 tagihan**;
- **3.334 pembayaran** (total 10.002 untuk tiga sekolah), dengan 266 tagihan menunggak per sekolah;
- absensi harian sejak awal tahun ajaran: 21.000 catatan;
- 10 pengumuman;
- akun: `admin@beban-N.uji`, `bendahara@…`, `kepala@…`, `guru1@…` sampai `guru12@…`.

Password semua akun: `Beban-Uji-2026`, bisa diganti dengan `-password`. Daftar akun dan kelas ditulis ke `loadtest/data.json` untuk skrip k6. File itu dan folder `results/` tidak di-commit.

```sh
# Database khusus uji beban (jangan database pilot)
createdb akra_load
DATABASE_URL=postgres://.../akra_load?sslmode=disable make migrate-up
DATABASE_URL=postgres://.../akra_load?sslmode=disable go run ./cmd/loadseed -out loadtest/data.json
# Ulang dari awal: tambahkan -reset (butuh superuser, karena tabel keuangan append-only)
```

## Skenario

[scenarios.js](scenarios.js). Setiap pengguna virtual login sekali lewat form sungguhan (token CSRF, cookie sesi dipertahankan), lalu berulang-ulang:

| Skenario | Pengguna (1x) | Kegiatan | Jeda |
|---|---|---|---|
| `browse` | 50 (admin/TU dan kepala sekolah) | beranda, lalu daftar siswa dengan pencarian | 1–2 dtk |
| `arrears` | 10 (bendahara) | daftar tunggakan dengan filter kelas (30% semua kelas) | 2–4 dtk |
| `export` | 5 (admin/TU) | unduh rekap kehadiran satu kelas, Juli–Oktober (.xlsx) | 5–10 dtk |

Durasi bawaan 5 menit. `LOAD_FACTOR=2` menggandakan jumlah pengguna.

Setiap respons dicek: harus benar-benar halaman aplikasi (bukan redirect ke login) atau file .xlsx. Jumlah respons 5xx dihitung di metrik `server_errors`.

```sh
k6 run -e BASE_URL=http://localhost:8080 loadtest/scenarios.js
k6 run -e BASE_URL=http://localhost:8080 -e LOAD_FACTOR=2 loadtest/scenarios.js
k6 run ... --summary-export=loadtest/results/hasil.json   # simpan ringkasan
```

Threshold di skrip mengikuti kriteria T61: median halaman biasa < 500 ms, p95 < 2 dtk, dan tanpa 5xx. k6 keluar dengan kode 99 jika ada yang terlewati.

## Hasil (lokal, 18 Oktober 2026)

**Lingkungan:**

- satu laptop untuk aplikasi, Postgres, dan k6 sekaligus: Intel i5-1135G7 (8 thread), RAM 7,8 GB, WSL2;
- PostgreSQL 15.12; binary `go build` Go 1.22.2;
- `APP_ENV=development` (HTTP tanpa TLS), dengan setelan bawaan `DB_MAX_CONNS=10`, `RIVER_WORKERS=4`, dan argon2id 19 MiB.

CPU dan memori diambil tiap 5 detik dari `/proc`. CPU 100% = satu core.

### Waktu respons (median / p95)

| Halaman | 1x (65 pengguna) | 2x (130 pengguna) | 4x (260 pengguna, 2 menit) |
|---|---|---|---|
| Beranda | 35 ms / 93 ms | 56 ms / 252 ms | **696 ms** / 2,58 s |
| Daftar siswa + pencarian | 13 ms / 32 ms | 18 ms / 91 ms | 177 ms / 633 ms |
| Daftar tunggakan + filter | 22 ms / 122 ms | 40 ms / 240 ms | 308 ms / 1,00 s |
| Ekspor rekap kehadiran (.xlsx) | 23 ms / 80 ms | 38 ms / 146 ms | 236 ms / 851 ms |
| Login (semua pengguna di awal) | 177 ms / 1,96 s | 223 ms / 2,93 s | 347 ms / 5,65 s |

### Ringkasan

| | 1x | 2x | 4x |
|---|---|---|---|
| Request per detik | 36 | 71 | 106 |
| Error 5xx | 0 | 0 | 0 |
| Cek gagal | 0 dari 11.046 | 0 dari 21.653 | 0 dari 13.522 |
| CPU aplikasi (median / puncak) | 32% / 199% | 89% / 342% | 181% / 350% |
| Memori aplikasi (median / puncak) | 51 MB / 235 MB | 54 MB / 243 MB | 61 MB / 246 MB |
| CPU Postgres (median / puncak) | 36% / 90% | 124% / 213% | 286% / 313% |
| Memori Postgres | ~680 MB | ~716 MB | ~720 MB |

### Kriteria T61

- [x] Median halaman biasa di bawah 500 ms: lulus di 1x dan 2x (tertinggi 56 ms).
- [x] Tidak ada 5xx di skenario dasar: 0 di 1x, 2x, dan 4x.
- [x] Hasil dan versi alat tercatat di file ini.
- [x] Batas kasar kapasitas tercatat (di bawah).

### Temuan dan perbaikan

1. **Lonjakan memori saat login serentak (diperbaiki).** Pada percobaan pertama, memori aplikasi melonjak sampai **2,1 GB**, padahal kondisi stabilnya 68 MB:
   - setiap verifikasi password argon2id memakai 19 MiB, dan 65 login hampir bersamaan berarti sekitar 1,2 GB sekaligus;
   - target heap Go ikut membengkak, sehingga sampah menumpuk sampai sekitar 2,1 GB sebelum GC berjalan;
   - di kontainer dengan batas memori lebih kecil, ini berisiko OOM saat banyak guru login pada jam yang sama.

   Perbaikan: paling banyak 4 hash/verifikasi argon2id berjalan bersamaan dalam satu proses (`maxConcurrentHashes` di [internal/auth/password.go](../internal/auth/password.go)); login lain mengantre. Hasilnya, puncak memori turun ke **235 MB**. p95 login tetap sekitar 2 detik, dan latensi ekor halaman lain membaik (p99 beranda 620 ms → 189 ms).
2. **Tidak ada query lambat yang perlu diperbaiki sebelum T63.** Diukur satu per satu tanpa beban:
   - ringkasan sekolah 1 ms;
   - kehadiran minggu ini 16 ms (satu query per kelas);
   - ringkasan bendahara 33 ms;
   - pembayaran terbaru 10 ms;
   - pengumuman 1 ms.

   Catatan untuk nanti: "pembayaran terbaru" di beranda bendahara membaca semua pembayaran sekolah lalu mengurutkan 5 terbaru (index yang ada `(school_id, paid_on)`). Saat ini masih cepat, tapi biayanya naik seiring riwayat. Index `(school_id, created_at DESC)` bisa ditambahkan jika mulai terasa.

### Batas kasar kapasitas

- Sampai **130 pengguna aktif bersamaan** (2x), semua halaman tetap cepat (median < 60 ms, p95 < 300 ms).
- Di **260 pengguna** (4x) belum ada error, tapi median beranda naik ke ~700 ms. Postgres menjadi leher botol (CPU ~3 core), sedangkan aplikasi sendiri hanya ~1,8 core dengan memori stabil sekitar 60 MB.
- Untuk tiga sekolah pilot (masing-masing sekitar 20–30 staf aktif), kapasitas ini jauh di atas kebutuhan.
- Saat menskalakan, yang perlu dinaikkan lebih dulu adalah CPU database, lalu `DB_MAX_CONNS`. Aplikasi bisa tetap satu instance.
- Login serentak dalam jumlah besar memang lambat secara sengaja (argon2id): sekitar 2–3 detik di p95 saat 130 pengguna login dalam beberapa detik.

Angka ini dari satu mesin yang juga menjalankan k6, jadi lebih pesimis untuk CPU. Uji di staging Railway (CPU dan memori dari dashboard Railway) dicatat terpisah di bawah setelah dijalankan.

## Staging Railway

Belum dijalankan. Langkahnya:

1. Isi database staging dengan `cmd/loadseed` (tiga sekolah `beban-*`; hapus dengan `-reset` setelah selesai).
2. Jalankan `k6 run -e BASE_URL=https://<staging> loadtest/scenarios.js`.
3. Catat CPU dan memori dari dashboard Railway selama uji.
