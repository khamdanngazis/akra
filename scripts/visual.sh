#!/usr/bin/env bash
# Pemeriksaan visual (T162): menyimpan halaman utama dengan data demo
# (cmd/server/visual_test.go), lalu memotretnya dengan Chrome headless di
# desktop (1280 px) dan HP (360 px). Di HP juga diperiksa gulir horizontal:
# halaman yang lebih lebar dari 360 px dicatat di overflow.txt dan membuat
# skrip ini gagal.
#
# Pemakaian:
#   TEST_DATABASE_URL=postgres://... scripts/visual.sh [--panduan] [pola]
#
#   VISUAL_OUT  folder hasil (bawaan tmp/visual)
#   CHROME      perintah Chrome (bawaan google-chrome atau chromium)
#   --panduan   salin tangkapan desktop ke doc/panduan-ujian/img
#   pola        hanya halaman yang namanya cocok, mis. 'ujian-*'
#
# Lampirkan tangkapan halaman yang berubah di setiap PR yang mengubah
# tampilan modul ujian.
set -euo pipefail

cd "$(dirname "$0")/.."
: "${TEST_DATABASE_URL:?TEST_DATABASE_URL harus diisi (database uji lokal)}"
out=$(realpath -m "${VISUAL_OUT:-tmp/visual}")
panduan=0
pattern='*'
for arg in "$@"; do
  case "$arg" in
    --panduan) panduan=1 ;;
    *) pattern=$arg ;;
  esac
done
chrome=${CHROME:-$(command -v google-chrome || command -v chromium || command -v chromium-browser || true)}
if [ -z "$chrome" ]; then
  echo "Chrome tidak ditemukan; isi CHROME" >&2
  exit 1
fi

mkdir -p "$out/hp"
rm -f "$out"/*.html "$out"/*.png "$out"/hp/*.html "$out"/hp/*.png "$out/overflow.txt"
VISUAL_OUT="$out" go test ./cmd/server/ -run '^TestVisual$' -count=1

shoot() { # shoot <lebar> <tinggi> <png> <url>
  "$chrome" --headless=new --disable-gpu --no-sandbox --hide-scrollbars \
    --allow-file-access-from-files --window-size="$1,$2" \
    --screenshot="$3" "$4" >/dev/null 2>&1
}

n=0
for f in "$out"/$pattern.html; do
  [ -e "$f" ] || continue
  name=$(basename "$f" .html)
  # Tur singkat tidak ikut dipotret.
  sed -i 's|<script[^>]*js/tour\.js[^>]*></script>||' "$f"
  shoot 1280 860 "$out/$name.png" "file://$f"
  # HP: halaman dimuat di iframe 360 px (jendela Chrome headless tidak bisa
  # sesempit itu); lebar isi halaman dicatat di judul dokumen.
  cat >"$out/hp/$name.html" <<EOF
<!doctype html><html><head><title>-</title><style>body{margin:0}iframe{border:0;width:360px;height:780px}</style></head>
<body><iframe src="../$name.html"></iframe><script>
document.querySelector('iframe').addEventListener('load', function () {
  var d = this.contentDocument, w = d.documentElement.scrollWidth, wide = [];
  // Penyebab: elemen terluar yang melewati 360 px, kecuali isi wadah
  // bergulir sendiri (mis. sub-navigasi modul).
  var scrolls = function (el) {
    for (var p = el.parentElement; p; p = p.parentElement) {
      if (/auto|scroll|hidden/.test(getComputedStyle(p).overflowX)) return true;
    }
    return false;
  };
  if (w > 360) d.querySelectorAll('body *').forEach(function (el) {
    var r = el.getBoundingClientRect(), pr = el.parentElement.getBoundingClientRect();
    if (r.width && r.right > 361 && pr.right <= 361 && wide.length < 3 && !scrolls(el)) {
      wide.push(el.tagName.toLowerCase() + (typeof el.className === 'string' && el.className.trim() ? '.' + el.className.trim().split(/\s+/).join('.') : ''));
    }
  });
  document.title = 'width=' + w + (wide.length ? ' ' + wide.join(' ') : '');
});
</script></body></html>
EOF
  shoot 400 780 "$out/hp/$name.png" "file://$out/hp/$name.html"
  title=$("$chrome" --headless=new --disable-gpu --no-sandbox --allow-file-access-from-files \
    --virtual-time-budget=3000 --dump-dom "file://$out/hp/$name.html" 2>/dev/null | sed -n 's|.*<title>\(.*\)</title>.*|\1|p')
  width=${title#width=}
  width=${width%% *}
  if [ -n "$width" ] && [ "$width" != "-" ] && [ "$width" -gt 360 ]; then
    echo "$name: $title" >>"$out/overflow.txt"
  fi
  n=$((n + 1))
done

if [ "$panduan" = 1 ]; then
  # Nama gambar panduan ← halaman demo.
  for pair in beranda:ujian-beranda bank:ujian-bank bank-contoh:ujian-bank-contoh impor:ujian-impor \
    tp:ujian-tp buat:ujian-buat latihan:ujian-latihan detail:ujian-detail pengawas:ujian-pengawas \
    agenda:ujian-agenda bantuan:ujian-bantuan; do
    cp "$out/${pair#*:}.png" "doc/panduan-ujian/img/${pair%%:*}.png"
  done
  echo "gambar panduan diperbarui di doc/panduan-ujian/img"
fi

echo "$n halaman dipotret ke $out (desktop) dan $out/hp (HP 360 px)"
if [ -s "$out/overflow.txt" ]; then
  echo "gulir horizontal di HP 360 px:" >&2
  cat "$out/overflow.txt" >&2
  exit 1
fi
