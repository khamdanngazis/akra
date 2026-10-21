package academic

import (
	"context"
	"fmt"
	"strconv"

	"akra/internal/platform/database"
	"akra/internal/platform/settings"
)

// DefaultKKM berlaku jika sekolah belum mengisi report.kkm_default (T73).
const DefaultKKM = 70

// Format rapor (kunci school_settings report.format).
const (
	ReportFormatNumeric = "angka"   // nilai akhir, huruf, KKM
	ReportFormatMerdeka = "merdeka" // Kurikulum Merdeka: ambang disebut KKTP
)

// ThresholdLabel mengembalikan sebutan ambang ketuntasan untuk format rapor:
// "KKTP" untuk Kurikulum Merdeka, "KKM" untuk lainnya.
func ThresholdLabel(format string) string {
	if format == ReportFormatMerdeka {
		return "KKTP"
	}
	return "KKM"
}

// SchoolThreshold membaca KKM default sekolah dan sebutannya. Nilai
// tersimpan yang rusak adalah error.
func SchoolThreshold(ctx context.Context, q database.DBTX, schoolID string) (kkm int, label string, err error) {
	n, err := settings.Int64(ctx, q, schoolID, settings.ReportKKMDefault, DefaultKKM)
	if err != nil {
		return 0, "", err
	}
	if n < 0 || n > 100 {
		return 0, "", fmt.Errorf("academic: stored %s out of range: %s", settings.ReportKKMDefault, strconv.FormatInt(n, 10))
	}
	format, _, err := settings.Get(ctx, q, schoolID, settings.ReportFormat)
	if err != nil {
		return 0, "", err
	}
	return int(n), ThresholdLabel(format), nil
}

// EffectiveKKM mengembalikan KKM mapel s, atau def jika mapel tidak punya.
func EffectiveKKM(s Subject, def int) int {
	if s.KKM != nil {
		return *s.KKM
	}
	return def
}
