#!/bin/sh
# Penjaga deploy dari luar server (T145): bertanya ke /internal/active-exams
# lingkungan tujuan dan gagal bila ada ujian aktif. Dipakai sebelum deploy
# manual (railway up) atau dari CI. Pre-deploy Railway sudah menjalankan
# akra-ops guard di dalam image; skrip ini untuk memeriksa lebih awal.
#
#   AKRA_URL=https://akra-production.up.railway.app OPS_TOKEN=... scripts/deploy-guard.sh
#   scripts/deploy-guard.sh --darurat   # lewati penjagaan (dicatat di keluaran)
set -eu
: "${AKRA_URL:?AKRA_URL is not set}"
: "${OPS_TOKEN:?OPS_TOKEN is not set}"
if [ "${1:-}" = "--darurat" ]; then
	echo "PERINGATAN: deploy darurat, penjaga dilewati." >&2
	exit 0
fi
body=$(curl -fsS --max-time 15 -H "Authorization: Bearer $OPS_TOKEN" "$AKRA_URL/internal/active-exams") || {
	echo "Tidak bisa menghubungi $AKRA_URL/internal/active-exams; deploy dibatalkan." >&2
	exit 1
}
case "$body" in
*'"deploy_ok":true'*)
	echo "Aman untuk deploy: tidak ada ujian aktif."
	;;
*)
	echo "DEPLOY DITOLAK: ada ujian aktif." >&2
	echo "$body" >&2
	exit 3
	;;
esac
