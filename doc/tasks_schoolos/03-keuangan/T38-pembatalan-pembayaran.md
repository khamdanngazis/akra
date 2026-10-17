# T38 — Pembatalan pembayaran

**Fase:** 03 Keuangan
**Dependensi:** T34
**Estimasi:** 1–2 hari

## Tujuan
Pembayaran yang salah bisa dibatalkan dengan alasan tertulis, tanpa menghapus baris asli. Aturan pembatalan mengikuti keputusan bisnis di rancangan bagian 12.

## Aturan (dari keputusan bisnis)
- Hanya bendahara yang bisa membatalkan.
- Alasan wajib diisi.
- Hanya bisa dibatalkan dalam bulan yang sama dengan `paid_on`.
- Pembatalan dengan nominal di atas ambang perlu persetujuan kepala sekolah.
- Ambang disimpan di tabel `school_settings` dengan kunci `void_approval_threshold`. Nilai awal dikonfirmasi di wawancara.

## Langkah
1. Buat migrasi `000018_payment_voids_and_settings`:
   - `payment_voids`: `id uuid PK`, `payment_id uuid NOT NULL UNIQUE REFERENCES payments(id)`, `reason text NOT NULL`, `requested_by uuid NOT NULL`, `approved_by uuid NULL`, `approved_at timestamptz NULL`, `created_at timestamptz NOT NULL DEFAULT now()`.
   - `school_settings`: `school_id uuid`, `key text`, `value text`, `PRIMARY KEY (school_id, key)`.
2. `UNIQUE (payment_id)` pada `payment_voids` memastikan satu pembayaran hanya bisa dibatalkan sekali.
3. Fungsi `RequestVoid(ctx, schoolID, paymentID, reason, userID)`:
   - Cek peran bendahara.
   - Cek `reason` tidak kosong.
   - Cek `paid_on` berada di bulan yang sama dengan hari ini (Asia/Jakarta). Jika tidak, tolak.
   - Cek nominal. Jika di atas ambang dan belum ada persetujuan, status menunggu persetujuan.
   - Jika tidak di atas ambang, langsung dibatalkan.
4. Fungsi `ApproveVoid` untuk kepala sekolah. Hanya kepala sekolah dari sekolah yang sama yang bisa menyetujui, dan tidak boleh menyetujui pembatalannya sendiri (pengajuan dan persetujuan oleh orang berbeda).
5. Pembatalan tidak mengubah baris `payments`. Status pembayaran dihitung dari keberadaan `payment_voids` yang sudah disetujui.
6. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000018_payment_voids_and_settings.up.sql` dan `.down.sql`
- `internal/payments/void_service.go`, `void_handler.go`
- `internal/platform/settings/settings.go`

## Kriteria selesai
- [ ] Pembatalan tanpa alasan ditolak
- [ ] Pembatalan pembayaran bulan lalu ditolak
- [ ] Pembatalan di atas ambang tidak langsung berlaku sebelum disetujui
- [ ] Kepala sekolah tidak bisa menyetujui pembatalan yang ia ajukan sendiri
- [ ] Pembayaran yang sama tidak bisa dibatalkan dua kali
- [ ] Baris `payments` tidak berubah setelah pembatalan (dicek dengan membandingkan sebelum dan sesudah)

## Catatan
- Status "menunggu persetujuan" perlu ditampilkan di daftar pembayaran agar bendahara tahu statusnya.
- Nilai ambang belum ditetapkan. Jika sekolah tidak mau pembatalan butuh persetujuan, nilainya bisa diisi sangat besar, tetapi aturannya tetap ada.
