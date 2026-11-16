# Spike T123 — prototipe modul ujian

Prototipe untuk [ADR 0001](../../doc/adr/0001-modul-ujian.md). **Bukan kode produksi.** Folder ini dihapus setelah ADR disetujui; kode final dibuat di T128, T129, dan T139.

Modul Go terpisah (`go.mod` sendiri), jadi tidak ikut `go build ./...` dan `go test ./...` aplikasi.

## Persiapan

```sh
cd spike/mvp4
npm install          # katex, mathlive, puppeteer-core, font Arab (hanya untuk spike)
export KATEX_JS=$PWD/node_modules/katex/dist/katex.min.js
```

## Menjalankan

| Spike | Perintah |
|---|---|
| 1. Render rumus di server | `GOTOOLCHAIN=local go run ./katexbench -katex $KATEX_JS` |
| 3. Impor Word | `GOTOOLCHAIN=local go test -v ./docx` (dokumen contoh sintetis dibuat di `docx/sample.go`) |
| 4. Simpan jawaban | `createdb akra_spike`, lalu `go run ./saveproto -db "$DSN" -clients 5000 -interval 10s -dur 60s -mode token` (atau `-mode session`) |
| 5. Offline | `NODE_PATH=$PWD/node_modules node offline/test.js` (butuh Google Chrome; atur `CHROME` bila path berbeda) |
| 2 & 7. Cetak dan teks Arab | Salin `node_modules/katex/dist/katex.min.css` dan font ke `arabic/page/` (lihat `arabic/genpage.js`), lalu `node arabic/genpage.js && node arabic/shoot.js && node arabic/printshot.js` |

Spike 6 (editor) hanya mengukur ukuran paket: `gzip -9c node_modules/mathlive/mathlive.min.js | wc -c`.

## Menguji dokumen Word sungguhan

Taruh `.docx` guru (data pribadi sudah dihapus) di folder lokal di luar repo, lalu:

```sh
DOCX_DIR=/path/folder-soal GOTOOLCHAIN=local go test -v -run TestRealDocs ./docx
```

Hasilnya ditulis di ADR bagian 3 dan menentukan gerbang T129 (≥ 70% soal sempurna).
