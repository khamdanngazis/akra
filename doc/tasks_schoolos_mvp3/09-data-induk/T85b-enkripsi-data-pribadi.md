# T85b — Enkripsi data pribadi di database

**Fase:** 09 Data Induk
**Dependensi:** —
**Estimasi:** 2 hari

## Tujuan
Nomor identitas (NIK, No KK, no akta, NIK orang tua, nomor KIP/KKS) tersimpan terenkripsi di database. Jika database, backup, atau hasil dump bocor, nomor itu tidak bisa dibaca tanpa kunci yang disimpan terpisah.

## Data yang dienkripsi
| Data | Tabel | Perlu dicari/unik? |
|---|---|---|
| NIK siswa | `student_profiles` | Ya (unik per sekolah, pencocokan T96/T101) |
| No KK | `student_profiles` | Ya (dibandingkan dengan NIK) |
| No registrasi akta lahir | `student_profiles` | Tidak |
| Nomor KIP, KKS | `student_profiles` | Tidak |
| NIK orang tua/wali | `guardians` | Ya (pencocokan wali kakak-adik di T96) |
| Field yang sama milik pendaftar | `applicants` | Ya (cek pendaftar ganda) |

NISN **tidak** dienkripsi. NISN adalah nomor pendidikan yang bisa dicek publik di laman NISN, dan dipakai untuk mencocokkan nilai (T102).

## Langkah
1. Paket `internal/platform/crypto` (atau `piicrypt`):
   - `Encrypt(plain string) ([]byte, error)` dengan AES-256-GCM, nonce acak 12 byte per nilai. Format: `versi kunci (1 byte) || nonce || ciphertext`.
   - `Decrypt([]byte) (string, error)` memilih kunci dari byte versi.
   - `BlindIndex(kind, plain string) []byte` dengan HMAC-SHA256 dan kunci terpisah dari kunci enkripsi. `kind` (mis. `"nik"`, `"kk"`) ikut di-HMAC agar NIK dan KK yang sama tidak menghasilkan indeks yang sama.
   - Nilai dinormalisasi sebelum dienkripsi dan di-HMAC (trim, hanya digit).
   - Additional data GCM berisi nama tabel dan kolom, supaya ciphertext tidak bisa dipindah ke kolom lain.
2. Kunci dari environment variable, bukan dari database:
   - `PII_ENC_KEYS` = daftar `versi:base64key` (32 byte). Kunci aktif = versi tertinggi.
   - `PII_INDEX_KEY` = base64 32 byte.
   - Aplikasi menolak start jika kunci tidak ada atau panjangnya salah (pola seperti `RESEND_API_KEY` di `config.go`).
   - Untuk dev/test: kunci tetap di `.env.example` dan `testutil`, dengan peringatan bukan untuk produksi.
3. Bentuk kolom: `nik_enc bytea` + `nik_idx bytea` (hanya untuk data yang perlu dicari). Unik per sekolah lewat `UNIQUE (school_id, nik_idx) WHERE nik_idx IS NOT NULL`.
4. Akibat untuk validasi:
   - Regex panjang digit dan aturan NIK ≠ KK pindah ke service (sebelum enkripsi), karena `CHECK` database tidak bisa membaca ciphertext.
   - Cari NIK hanya bisa dengan nilai lengkap (lewat blind index), bukan sebagian.
5. Tipe `pii.Value` untuk dipakai model: method `Masked()` (`3201********0001`) dan `String()` yang **mengembalikan versi ter-mask**, supaya NIK tidak bocor lewat `fmt`, `slog`, atau template. Nilai utuh hanya lewat method eksplisit `Reveal()`.
6. Rotasi kunci: perintah `cmd/` atau job River yang mendekripsi dengan kunci lama dan mengenkripsi ulang dengan kunci aktif, per batch. Blind index tidak dirotasi (jika perlu ganti, hitung ulang semua indeks).
7. Dokumentasi di `doc/restore.md`:
   - Kunci **wajib** disimpan terpisah dari backup database (mis. password manager tim dan variabel Railway). Backup tanpa kunci tidak bisa dipulihkan datanya.
   - Langkah rotasi kunci dan apa yang terjadi jika kunci hilang.

## File yang disentuh
- `internal/platform/crypto/pii.go`, `pii_test.go`
- `internal/platform/config/config.go`
- `.env.example`, `internal/testutil/`
- `cmd/rotate-pii-key/` (atau job River)
- `doc/restore.md`

## Kriteria selesai
- [ ] Enkripsi nilai yang sama dua kali menghasilkan ciphertext berbeda; blind index-nya sama
- [ ] Ciphertext yang diubah satu byte gagal didekripsi (bukan menghasilkan nilai salah)
- [ ] Ciphertext yang dipindah ke kolom lain gagal didekripsi
- [ ] Data terenkripsi dengan kunci versi 1 tetap terbaca setelah kunci versi 2 aktif; rotasi mengubah semuanya ke versi 2
- [ ] Aplikasi gagal start tanpa `PII_ENC_KEYS` atau `PII_INDEX_KEY`
- [ ] `fmt.Sprint(nilaiPII)` dan `slog` hanya menampilkan versi ter-mask
- [ ] Dump database (`pg_dump`) dari data uji tidak memuat satu pun NIK contoh dalam bentuk teks

## Catatan
- Enkripsi ini melindungi dari kebocoran database dan backup. Tidak melindungi dari pengguna yang sah tapi menyalahgunakan aksesnya; itu tetap diatur lewat peran, mask, dan audit (T86).
- Jangan pakai `pgcrypto` dengan kunci dikirim di query: kunci bisa muncul di log PostgreSQL dan `pg_stat_statements`.

## Catatan implementasi
Paket `internal/platform/pii`. Di development tanpa kunci, aplikasi memakai kunci dev bawaan (dengan peringatan di log); staging/production wajib `PII_ENC_KEYS` dan `PII_INDEX_KEY`. Daftar kolom rotasi ada di `cmd/rotate-pii-key` (`students.PIIColumns`, `admission.PIIColumns`). File dokumen pendaftar (T91) juga dienkripsi; rotasi kunci belum mencakup file (file lama tetap terbaca selama kunci lamanya masih ada di `PII_ENC_KEYS`).
