#!/bin/sh
# Backup harian database AKRA ke R2 (T62): pg_dump -Fc ke
# backups/{tanggal WIB}/schoolos.dump, lalu hapus backup di luar retensi
# (BACKUP_RETENTION, bawaan 14). Butuh DATABASE_URL dan R2_* di environment.
# DSN dan kredensial tidak pernah dicetak.
set -eu
exec akra-backup run
