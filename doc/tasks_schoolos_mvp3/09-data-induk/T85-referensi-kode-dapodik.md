# T85 — Referensi kode Dapodik

**Fase:** 09 Data Induk
**Dependensi:** —
**Estimasi:** 1 hari

## Tujuan
Semua pilihan yang dipakai Dapodik (agama, pekerjaan, penghasilan, dan lainnya) tersedia di satu paket, dengan kode dan label persis seperti Dapodik. Form, PDF, lembar salin, dan ekspor memakai sumber yang sama.

## Langkah
1. Paket `internal/dapodik/ref` dengan tipe `Option{Code, Label string}` dan satu daftar per referensi. Sumber: formulir F-PD 2027 dan label combobox di `classic/app.js`.
   - Agama: 01 Islam … 07 Kepercayaan kpd Tuhan YME
   - Kebutuhan khusus: 01 Tidak … 18 Autis (bisa lebih dari satu)
   - Tempat tinggal, moda transportasi (termasuk 99 Lainnya)
   - Pendidikan orang tua (01–11), pekerjaan (01–12, plus "Meninggal Dunia"), penghasilan (01–07)
   - Jenis pendaftaran (Siswa Baru, Pindahan, Kembali Bersekolah), alasan keluar, alasan layak/menolak PIP
   - Jenis kesejahteraan, jenis dan tingkat prestasi, jenis beasiswa
2. Fungsi `Label(ref, code)` dan `Valid(ref, code)`. Kode tidak dikenal mengembalikan error, bukan string kosong.
3. Konstanta `Version = "2027"`. Komentar di atas setiap daftar menyebut sumbernya.
4. Kode disimpan di database sebagai `text` dua digit (`"01"`), bukan angka.

## File yang disentuh
- `internal/dapodik/ref/ref.go`, `ref_test.go`

## Kriteria selesai
- [ ] Setiap daftar cocok dengan F-PD 2027 (uji tabel membandingkan kode dan label)
- [ ] `Valid` menolak kode di luar daftar
- [ ] Tidak ada daftar kode Dapodik lain di luar paket ini (`git grep` pada label seperti "Pedagang Kecil")

## Catatan
- Label harus persis sama dengan Dapodik, termasuk ejaan, karena dipakai untuk salin-tempel ke combobox Dapodik (T98).

## Catatan implementasi
Label combobox Dapodik diambil dari server saat aplikasi berjalan, tidak ada di `app.js`; sumber daftar adalah F-PD 2027. Kode "98 Meninggal Dunia" tidak tercetak di F-PD dan perlu dicocokkan di T105. Ada tambahan `List.Code(label)` untuk impor (T101).
