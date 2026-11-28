# Runbook insiden ujian

Pegangan tim AKRA saat ada masalah selama ujian berlangsung. Tujuannya: **jawaban peserta tidak hilang**, sekolah tahu apa yang terjadi, dan ujian bisa dilanjutkan.

Prinsip yang berlaku di semua kejadian:

- Jawaban ditulis dulu ke perangkat peserta (IndexedDB), lalu dikirim ulang otomatis sampai server menerimanya (T139). Server lambat atau mati sebentar **tidak menghilangkan jawaban**, selama peserta tidak menghapus data browser.
- Peserta tidak perlu menutup atau memuat ulang halaman ujian. Pesan standar ke sekolah: *"Jangan tutup halaman ujian. Jawaban tersimpan di perangkat dan terkirim otomatis begitu layanan pulih."*
- Bila gangguan lebih dari ± 5 menit, koordinator/pengawas **menjeda sesi** (pusat kendali → Jeda) agar waktu peserta tidak berkurang, lalu melanjutkan setelah pulih. Lama jeda otomatis ditambahkan ke waktu peserta.
- Halaman status publik: `/status`. Kejadian yang terbuka otomatis membuat halaman ini menampilkan "gangguan".

## Peran

| Peran | Siapa | Tugas |
|---|---|---|
| Penanggung jawab insiden | engineer yang menerima alert pertama | memimpin penanganan, memutuskan rollback, menulis catatan pascainsiden |
| Kontak sekolah | tim dukungan (atau engineer bila sendirian) | mengirim pesan ke koordinator sekolah, menjawab telepon |
| Cadangan | engineer kedua | bila penanggung jawab tidak merespons alert dalam 10 menit |

Isi nama dan nomor di kanal tim sebelum musim ujian, bukan di dokumen ini.

## Alat

- **Alert**: kanal Telegram tim (dan email `ALERT_EMAIL`). Isinya jenis kejadian, angka, dan tautan halaman status. Kabar "✅ Pulih" dikirim otomatis setelah kondisi baik 5 menit.
- **Dasbor internal**: `/internal/` (Basic auth, kata sandi `OPS_TOKEN`). Isinya jendela deploy, ujian aktif, kesehatan endpoint, kejadian, dan jadwal musim ujian 60 hari.
- **Metrik**: `/metrics` (format Prometheus, token yang sama).
- **Uji kanal alert**: `curl -X POST -H "Authorization: Bearer $OPS_TOKEN" https://<host>/internal/alert-test`.
- **Railway**: log layanan `akra`, metrik CPU/memori, dan riwayat deployment (rollback = *Redeploy* deployment sebelumnya).
- **CLI di container**: `railway ssh --service akra -- akra-ops active` (ujian aktif, JSON), `akra-ops maintenance ...`.

## Aturan alert

| Alert | Ambang | Artinya |
|---|---|---|
| Simpan jawaban gagal > 1% | > 1% dari ≥ 30 simpan dalam 2 menit | server menjawab 5xx: database, bug, atau server kewalahan |
| Simpan jawaban lambat | p95 > 1 detik dari ≥ 30 simpan dalam 2 menit | database lambat, pool penuh, atau CPU penuh |
| Kumpul otomatis tertunda | attempt melewati tenggat + tenggang > 3 menit belum dikumpulkan | job River tidak berjalan (worker mati, database terkunci) |
| Koneksi database > 80% | dua penilaian berturut-turut (± 1 menit) | request menunggu koneksi; akan berujung lambat/timeout |

Ambang dievaluasi di setiap replika tiap 30 detik. Kejadian dibuka sekali lewat database, jadi dua replika tidak mengirim alert ganda.

---

## 1. Server lambat

**Tanda:** alert *simpan lambat* atau *koneksi database > 80%*; pusat kendali sekolah menampilkan "Server lambat atau bermasalah"; sekolah melapor tombol berputar lama.

1. Buka `/internal/`: lihat p95 simpan dan jumlah peserta aktif. Bandingkan dengan kapasitas (ADR 0001: satu replika pool 10 nyaman ± 800 req/dtk).
2. Railway → metrik layanan `akra`: CPU, memori. Railway → Postgres: CPU, koneksi, disk.
3. Bila CPU aplikasi penuh: tambah replika (Railway → Settings → Replicas) atau naikkan resource. Aplikasi siap banyak replika (sesi, rate limit, dan job di database).
4. Bila database penuh beban: cari query lambat (`pg_stat_activity`), terutama ekspor laporan atau impor besar. Hentikan job berat yang tidak penting selama ujian.
5. Bila koneksi pool habis tetapi database santai: naikkan `DB_MAX_CONNS` (perhatikan batas koneksi Postgres × jumlah replika).
6. Bila tidak membaik dalam 10 menit: minta sekolah menjeda sesi.

**Pesan ke sekolah:** *"Server AKRA sedang lambat dan sedang kami tangani. Jawaban peserta aman di perangkat. Bila peserta tidak bisa berpindah soal lebih dari 5 menit, jeda sesi dari pusat kendali; waktu yang dijeda dikembalikan."*

## 2. Database penuh

**Tanda:** alert *simpan gagal > 1%*; log berisi `could not extend file`, `No space left on device`, atau `too many connections`.

1. Railway → Postgres → volume: cek pemakaian disk.
2. Disk penuh: perbesar volume Railway (bisa saat berjalan). Jangan menghapus data ujian. Tabel yang aman dibersihkan: `river_job` yang sudah selesai lama, `rate_limit_attempts` lama.
3. `too many connections`: kurangi `DB_MAX_CONNS` atau jumlah replika agar total ≤ batas Postgres, lalu restart.
4. Setelah pulih, periksa tunggakan kumpul otomatis di `/internal/` dan pastikan menurun ke 0.

**Pesan ke sekolah:** sama dengan bagian 1. Bila sesi berakhir saat gangguan, peserta tetap bisa mengirim jawaban yang tertunda selama tenggang 2 menit; selebihnya tim mengecek antrian per sekolah.

## 3. R2 (penyimpanan gambar) gangguan

**Tanda:** gambar soal tidak muncul, log berisi error R2/presign; simpan jawaban tetap normal.

1. Cek status Cloudflare (cloudflarestatus.com) dan log error R2.
2. Gambar yang sudah dimuat tetap ada di cache service worker perangkat. Peserta yang belum memuat gambar akan melihat gambar kosong.
3. Bila gangguan panjang dan soal bergantung pada gambar: minta pengawas menjeda sesi yang terdampak, atau koordinator mengirim pesan (pusat kendali → Pesan ke layar peserta) agar peserta melewati soal bergambar dulu.
4. Bila soal tidak bisa dikerjakan: koordinator membatalkan soal itu (tab Soal → Batalkan soal) agar poinnya tidak dihitung.

**Pesan ke sekolah:** *"Penyedia penyimpanan gambar sedang gangguan. Peserta boleh melewati soal bergambar dan kembali nanti. Jawaban lain tetap tersimpan."*

## 4. Deploy gagal saat ujian

Deploy **ditolak otomatis** selama ada ujian aktif (bagian "Jendela deploy"). Kejadian ini hanya terjadi pada deploy darurat atau deploy yang mulai sebelum ujian.

1. Railway → Deployments: bila deployment baru gagal health check, Railway tetap memakai deployment lama. Tidak ada tindakan untuk peserta.
2. Bila deployment baru berjalan tetapi rusak (alert error simpan muncul setelah deploy): **rollback** dengan *Redeploy* deployment sebelumnya. Server berhenti bertahap: request simpan yang berjalan diselesaikan, klien mengulang otomatis.
3. Bila migrasi database sudah jalan dan tidak kompatibel ke belakang: jangan rollback aplikasi; perbaiki maju (hotfix) dengan deploy darurat.
4. Tulis catatan pascainsiden: mengapa deploy terjadi saat ujian.

**Pesan ke sekolah:** *"Ada pembaruan yang bermasalah dan sudah kami kembalikan. Jawaban aman; peserta lanjut mengerjakan."*

## 5. Sekolah melapor "tidak bisa login"

Bedakan masalah satu peserta, satu ruang, atau satu sekolah.

1. **Satu peserta**: pengawas membuka layar pengawas → peserta → *Reset login* (kode baru untuk sesi itu) atau *Izinkan pindah perangkat*. Kode terkunci karena salah 5 kali juga dibuka dari sini.
2. **Satu ruang/lab**: biasanya jaringan. Minta pengawas membuka `/u/cek` di satu perangkat (cek koneksi). Bila semua perangkat lab gagal: cek Wi-Fi/akses internet sekolah. Batas login per IP sudah longgar (3.000/15 menit) untuk lab di belakang satu IP.
3. **Seluruh sekolah**: cek `/status` dan `/internal/`. Bila server normal (tidak ada alert), periksa:
   - modul ujian aktif untuk sekolah (`akra-modul -school <kode>`); bila tidak, lihat bagian 7;
   - jadwal sesi (jam mulai/selesai, zona waktu sekolah);
   - kode kartu sudah diterbitkan (tab Dokumen agenda) dan peserta memakai alamat `/u/<kode-sekolah>`.
4. Bila server bermasalah: lihat bagian 1–2.

**Pesan ke sekolah (satu peserta):** *"Pengawas bisa membuat kode baru dari layar pengawas → nama peserta → Reset login, lalu mendiktekan kodenya."*

## 6. Jawaban tiba setelah attempt ditutup

**Tanda:** log `answers arrived after attempt closed` (dengan `attempt_id`), atau `akra-ops pilot` melaporkan jawaban hilang > 0.

Perangkat masih menyimpan perubahan saat attempt sudah dikumpulkan (lewat tenggat dan tenggang 2 menit, atau ditutup pengawas). Perubahan itu tidak masuk nilai. AKRA menyimpan salinannya per soal (`exam_lost_answers`), hanya bila server belum punya versi yang sama atau lebih baru.

1. Lihat soal dan isinya: `SELECT question_id, value, client_seq, reported_at FROM exam_lost_answers WHERE attempt_id = '<id>';`
2. Cari penyebab di kejadian attempt (offline lama, pindah perangkat) dan log save di sekitar tenggat.
3. Sampaikan ke guru pemilik ujian. Guru memutuskan dan, bila perlu, mengubah poin soal itu dengan **nilai manual** di halaman hasil peserta. Nilai manual tercatat di audit dengan alasan.
4. Catat sebagai temuan pilot ([doc/uji-mvp4.md](uji-mvp4.md)). Target pilot: 0.

Salinan ini ikut dihapus oleh job masa simpan data ujian.

## 7. "Modul tidak aktif" dan permintaan aktivasi modul

**Tanda:** pengguna sekolah melihat halaman **"Modul tidak aktif"** ("Modul {nama} tidak aktif untuk sekolah Anda"), menu modul itu tidak ada, atau halaman publik pendaftaran/peserta menjawab "tidak ditemukan".

Halaman ini bukan gangguan: sekolah tidak memakai modul itu (ADR 0002). Data modul yang dimatikan tetap tersimpan.

1. **Cek status sekolah:** `akra-modul -school <kode>` menampilkan paket dan modul aktif. Riwayat perubahan ada di audit (`action LIKE 'module.%'`, lihat [implementator-modul.md](implementator-modul.md) bagian 5).
2. **Modul memang tidak termasuk paket:**
   - teruskan ke tim implementator AKRA sebagai permintaan aktivasi, beserta kode sekolah, modul, dan alasannya;
   - sekolah bisa melihat modulnya sendiri di **Pengaturan → Modul & Paket** (admin TU dan kepala sekolah).
3. **Modul seharusnya aktif** (mis. terlanjur dimatikan): tim implementator menyalakannya dengan catatan, mis. `akra-modul -school <kode> -module exam -on -option release=r2 -note "tiket #123"`. Perubahan terlihat tanpa login ulang, paling lama 30 detik.
4. **Mematikan modul ditolak penjaga** (sesi ujian dalam 24 jam, periode pendaftaran dibuka): jangan dipaksa. Tunggu sesi selesai atau minta sekolah menutup periodenya. Untuk keuangan dengan tagihan belum lunas, `-force` hanya setelah sekolah setuju tagihan tidak lagi terlihat orang tua.

**Pesan ke sekolah:** *"Modul itu belum termasuk paket sekolah Anda. Permintaan sudah kami teruskan ke tim implementator; datanya aman dan akan muncul saat modul diaktifkan."*

---

## Jendela deploy

- Pre-deploy Railway menjalankan `akra-migrate up`, yang lebih dulu menjalankan `akra-ops guard`. Deploy **ditolak** (deployment lama tetap berjalan) bila ada:
  - sesi di ruang yang sedang berlangsung (PTS/PAS, ulangan di kelas);
  - sesi apa pun yang mulai dalam 30 menit;
  - peserta yang sedang mengerjakan (termasuk ulangan jarak jauh).
- Cek lebih awal dari laptop: `AKRA_URL=https://<host> OPS_TOKEN=... scripts/deploy-guard.sh`.
- Rencanakan rilis dengan tabel "Musim ujian 60 hari ke depan" di `/internal/`.
- **Deploy darurat** (perbaikan yang tidak bisa menunggu): set variabel Railway `DEPLOY_EMERGENCY_UNTIL` ke waktu RFC 3339 paling lama 6 jam ke depan, mis. `2026-11-19T12:00:00+07:00`. Mengubah variabel memicu deploy yang lolos penjaga. Hapus variabel setelahnya (sudah tidak berlaku sendiri setelah lewat). Beri tahu koordinator sekolah yang sedang ujian sebelumnya.
- Pemeliharaan terjadwal: `akra-ops maintenance add -start ... -end ... -message "..."`. Tampil di `/status` sejak 24 jam sebelumnya.

## Menguji alert di staging

1. Set `EXAM_FAULT_SAVE_PERCENT=20` di staging (ditolak di production). Sebagian simpan dijawab 500; klien mengulang sehingga jawaban tetap tersimpan.
2. Buka halaman ujian beberapa peserta uji atau jalankan `go run ./cmd/savesim` terhadap staging lokal; tunggu ± 2 menit sampai alert *Simpan jawaban gagal > 1%* masuk.
3. Hapus variabel; kabar "✅ Pulih" masuk ± 5 menit kemudian.

## Setelah insiden

Tulis catatan singkat di kanal tim dalam 2 hari: kronologi, dampak (sekolah, peserta, jawaban hilang = harus 0), penyebab, dan perbaikan agar tidak terulang. Tutup kejadian manual hanya bila kondisi pulih tetapi alert tidak tertutup sendiri (`UPDATE ops_incidents SET resolved_at = now() WHERE id = ...`).
