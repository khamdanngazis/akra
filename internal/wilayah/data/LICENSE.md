# Sumber data wilayah

`wilayah.tsv.gz` berisi kode, nama, dan kode pos wilayah administrasi
Indonesia (38 provinsi, 514 kabupaten/kota, 7.285 kecamatan, 83.762
desa/kelurahan). Format: `kode<TAB>nama<TAB>kode_pos` (kode pos hanya untuk
desa/kelurahan).

Dibuat dengan `go run ./cmd/import-wilayah -convert` dari:

| Berkas | Sumber | Keterangan |
|---|---|---|
| `db/wilayah.sql` | https://github.com/cahyadsn/wilayah (branch master, diambil 2026-11-15) | README repo: sesuai Kepmendagri No 300.2.2-2430 Tahun 2025; header berkas masih menyebut 300.2.2-2138 Tahun 2025, terakhir diubah 2026-02-13 |
| `db/wilayah_kodepos.sql` | https://github.com/cahyadsn/wilayah_kodepos (branch main, diambil 2026-11-15) | kode pos per desa/kelurahan, terakhir diubah 2025-06-30 |

Untuk memperbarui: unduh kedua berkas terbaru, jalankan perintah konversi,
lalu commit hasilnya. Server mengimpor ulang otomatis saat start jika isi
berkas berubah.

## Lisensi

Kedua dataset berlisensi MIT.

```
MIT License

Copyright (c) 2017-2025 Cahya DSN (wilayah)
Copyright (c) 2024 Cahya DSN (wilayah_kodepos)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
