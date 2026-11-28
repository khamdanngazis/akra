// Package settings menyimpan pengaturan per sekolah dalam tabel
// school_settings (kunci-nilai teks).
package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Kunci pengaturan yang dikenal.
const (
	// VoidApprovalThreshold: pembatalan pembayaran dengan nominal di atas
	// nilai ini (rupiah) perlu persetujuan kepala sekolah.
	VoidApprovalThreshold = "void_approval_threshold"
	// AttendancePerSubject: "true" jika guru boleh mengisi absensi per mapel
	// selain absensi harian (T68). Belum diatur berarti nonaktif.
	AttendancePerSubject = "attendance_per_subject"
	// Pengaturan rapor (T73), dibaca lewat grades.LoadReportConfig.
	//
	// ReportScale: JSON skala huruf nilai akhir, mis.
	// [{"letter":"A","min":90,"max":100}, ...]; harus mencakup 0–100 tanpa
	// celah. Belum diatur berarti grades.DefaultScale.
	ReportScale = "report.scale"
	// ReportKKMDefault: ambang ketuntasan (KKM/KKTP) untuk mapel yang tidak
	// punya KKM sendiri, 0–100.
	ReportKKMDefault = "report.kkm_default"
	// ReportFormat: "angka" (nilai, huruf, KKM) atau "merdeka" (Kurikulum
	// Merdeka: label KKTP; deskripsi capaian menyusul).
	ReportFormat = "report.format"
	// ReportPrincipalName dan ReportSignerTitle: penanda tangan rapor.
	ReportPrincipalName = "report.principal_name"
	// ReportRemedialPolicy: aturan nilai remedial di rapor (T137):
	// "maks_kktp" (nilai remedial paling tinggi sama dengan KKTP) atau
	// "tertinggi" (ambil nilai tertinggi). Keduanya tidak pernah menurunkan.
	ReportRemedialPolicy = "report.remedial_policy"
	ReportSignerTitle    = "report.signer_title"
	// ReportPortalComponents: "true" jika portal orang tua menampilkan nilai
	// per komponen, bukan hanya nilai akhir (T75). Belum diatur = tidak.
	ReportPortalComponents = "report.portal_components"
	// AdmissionNISFormat: pola NIS otomatis untuk pendaftar yang diterima
	// (T96), mis. "{yyyy}{seq:3}". Belum diatur berarti pola bawaan itu.
	AdmissionNISFormat = "admission.nis_format"
	// AdmissionReRegistration: "false" jika pendaftar yang diterima langsung
	// dijadikan siswa tanpa daftar ulang (T120). Belum diatur berarti
	// memakai daftar ulang.
	AdmissionReRegistration = "admission.reregistration"
	// Status modul ujian (dulu exam.enabled dan exam.release) kini di
	// school_modules (T163, internal/platform/module).
)

// Get mengembalikan nilai kunci; ok false jika belum diatur.
func Get(ctx context.Context, q database.DBTX, schoolID, key string) (value string, ok bool, err error) {
	err = q.QueryRow(ctx, `SELECT value FROM school_settings WHERE school_id = $1 AND key = $2`, schoolID, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("settings: get %s: %w", key, err)
	}
	return value, true, nil
}

// Set menyimpan atau mengganti nilai kunci.
func Set(ctx context.Context, q database.DBTX, schoolID, key, value string) error {
	if _, err := q.Exec(ctx, `
		INSERT INTO school_settings (school_id, key, value) VALUES ($1, $2, $3)
		ON CONFLICT (school_id, key) DO UPDATE SET value = excluded.value, updated_at = now()`,
		schoolID, key, value); err != nil {
		return fmt.Errorf("settings: set %s: %w", key, err)
	}
	return nil
}

// Int64 membaca kunci sebagai bilangan bulat; fallback jika belum diatur.
// Nilai tersimpan yang bukan angka dianggap error (bukan fallback) agar
// salah ketik tidak diam-diam melonggarkan aturan.
func Int64(ctx context.Context, q database.DBTX, schoolID, key string, fallback int64) (int64, error) {
	v, ok, err := Get(ctx, q, schoolID, key)
	if err != nil || !ok {
		return fallback, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("settings: %s is not an integer: %q", key, v)
	}
	return n, nil
}

// Bool membaca kunci sebagai "true"/"false"; fallback jika belum diatur.
// Nilai lain dianggap error, sama seperti Int64.
func Bool(ctx context.Context, q database.DBTX, schoolID, key string, fallback bool) (bool, error) {
	v, ok, err := Get(ctx, q, schoolID, key)
	if err != nil || !ok {
		return fallback, err
	}
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("settings: %s is not a boolean: %q", key, v)
}
