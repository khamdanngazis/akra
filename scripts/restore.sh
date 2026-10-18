#!/bin/sh
# Pulihkan backup dari R2 ke database KOSONG di RESTORE_DATABASE_URL (T62),
# lalu bandingkan jumlah baris tabel utama dengan DATABASE_URL jika diisi.
#   restore.sh                       backup terbaru
#   restore.sh -key backups/2026-10-18/schoolos.dump
# Lihat docs/restore.md.
set -eu
akra-backup restore "$@"
if [ -n "${DATABASE_URL:-}" ]; then
  akra-backup compare
fi
