package grades

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

// ActionReportConfigUpdate adalah aksi audit perubahan pengaturan rapor.
const ActionReportConfigUpdate = "report_config.update"

const (
	// DefaultSignerTitle adalah jabatan penanda tangan jika belum diatur.
	DefaultSignerTitle = "Kepala Sekolah"
	// MaxScaleRows adalah jumlah baris skala di form.
	MaxScaleRows = 8
	maxSignerLen = 80
)

// ReportConfig adalah pengaturan rapor sekolah (T73). Sengaja terbatas:
// format yang benar-benar berbeda adalah keputusan baru, bukan konfigurasi.
type ReportConfig struct {
	// Format: academic.ReportFormatNumeric atau ReportFormatMerdeka. Format
	// Merdeka saat ini hanya mengganti sebutan KKM menjadi KKTP; deskripsi
	// capaian per siswa menyusul (T74b).
	Format        string
	Scale         Scale
	KKMDefault    int
	PrincipalName string
	SignerTitle   string
	// PortalComponents: orang tua melihat nilai per komponen (T75).
	PortalComponents bool
}

// ThresholdLabel mengembalikan "KKM" atau "KKTP" sesuai format.
func (c ReportConfig) ThresholdLabel() string { return academic.ThresholdLabel(c.Format) }

// LoadReportConfig membaca pengaturan rapor dengan default untuk kunci yang
// belum diatur. Nilai tersimpan yang rusak adalah error.
func LoadReportConfig(ctx context.Context, q database.DBTX, schoolID string) (ReportConfig, error) {
	c := ReportConfig{Format: academic.ReportFormatNumeric, SignerTitle: DefaultSignerTitle}
	var err error
	if c.Scale, err = LoadScale(ctx, q, schoolID); err != nil {
		return ReportConfig{}, err
	}
	if c.KKMDefault, _, err = academic.SchoolThreshold(ctx, q, schoolID); err != nil {
		return ReportConfig{}, err
	}
	for key, dst := range map[string]*string{
		settings.ReportFormat:        &c.Format,
		settings.ReportPrincipalName: &c.PrincipalName,
		settings.ReportSignerTitle:   &c.SignerTitle,
	} {
		v, ok, err := settings.Get(ctx, q, schoolID, key)
		if err != nil {
			return ReportConfig{}, err
		}
		if ok {
			*dst = v
		}
	}
	if c.Format != academic.ReportFormatNumeric && c.Format != academic.ReportFormatMerdeka {
		return ReportConfig{}, fmt.Errorf("grades: stored %s is invalid: %q", settings.ReportFormat, c.Format)
	}
	if c.PortalComponents, err = settings.Bool(ctx, q, schoolID, settings.ReportPortalComponents, false); err != nil {
		return ReportConfig{}, err
	}
	return c, nil
}

// BandInput adalah satu baris skala di form apa adanya.
type BandInput struct {
	Letter, Min, Max string
}

// ReportConfigForm adalah isian form pengaturan rapor apa adanya.
type ReportConfigForm struct {
	Format        string
	KKMDefault    string
	PrincipalName string
	SignerTitle   string
	// PortalComponents: isian kotak centang ("on" = dicentang).
	PortalComponents bool
	Bands            []BandInput
}

// FormOf mengisi form dari pengaturan tersimpan.
func FormOf(c ReportConfig) ReportConfigForm {
	f := ReportConfigForm{Format: c.Format, KKMDefault: strconv.Itoa(c.KKMDefault), PrincipalName: c.PrincipalName, SignerTitle: c.SignerTitle,
		PortalComponents: c.PortalComponents}
	for _, b := range c.Scale {
		f.Bands = append(f.Bands, BandInput{b.Letter, strconv.Itoa(b.Min), strconv.Itoa(b.Max)})
	}
	return f
}

// ConfigError berisi pesan per field ("scale" untuk skala secara
// keseluruhan, "band_<i>" untuk satu baris skala).
type ConfigError struct {
	Fields map[string]string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("grades: invalid report config (%d fields)", len(e.Fields))
}

// ValidateReportConfig memeriksa form dan mengubahnya menjadi ReportConfig.
func ValidateReportConfig(f ReportConfigForm) (ReportConfig, error) {
	fields := map[string]string{}
	c := ReportConfig{
		Format:           strings.TrimSpace(f.Format),
		PrincipalName:    strings.Join(strings.Fields(f.PrincipalName), " "),
		SignerTitle:      strings.Join(strings.Fields(f.SignerTitle), " "),
		PortalComponents: f.PortalComponents,
	}
	if c.Format != academic.ReportFormatNumeric && c.Format != academic.ReportFormatMerdeka {
		fields["format"] = "Pilih format rapor."
	}
	n, err := strconv.Atoi(strings.TrimSpace(f.KKMDefault))
	if err != nil || n < 0 || n > 100 {
		fields["kkm_default"] = "Bilangan bulat 0–100."
	}
	c.KKMDefault = n
	if utf8.RuneCountInString(c.PrincipalName) > maxSignerLen {
		fields["principal_name"] = fmt.Sprintf("Maksimal %d karakter.", maxSignerLen)
	}
	switch {
	case c.SignerTitle == "":
		fields["signer_title"] = "Isi jabatan penanda tangan, mis. Kepala Sekolah."
	case utf8.RuneCountInString(c.SignerTitle) > maxSignerLen:
		fields["signer_title"] = fmt.Sprintf("Maksimal %d karakter.", maxSignerLen)
	}

	var sc Scale
	for i, b := range f.Bands {
		letter, lo, hi := strings.TrimSpace(b.Letter), strings.TrimSpace(b.Min), strings.TrimSpace(b.Max)
		if letter == "" && lo == "" && hi == "" {
			continue
		}
		minV, err1 := strconv.Atoi(lo)
		maxV, err2 := strconv.Atoi(hi)
		if letter == "" || err1 != nil || err2 != nil {
			fields["band_"+strconv.Itoa(i)] = "Isi huruf, batas bawah, dan batas atas (bilangan bulat)."
			continue
		}
		sc = append(sc, Band{Letter: letter, Min: minV, Max: maxV})
	}
	if !hasBandErrors(fields) {
		valid, err := ValidateScale(sc)
		switch {
		case errors.Is(err, ErrScaleOverlap):
			fields["scale"] = "Ada rentang yang tumpang tindih. Setiap nilai hanya boleh masuk satu huruf."
		case errors.Is(err, ErrScaleGap):
			fields["scale"] = "Ada celah antar rentang. Batas atas satu huruf harus tepat 1 di bawah batas bawah huruf di atasnya."
		case errors.Is(err, ErrScaleCoverage):
			fields["scale"] = "Skala harus mencakup 0 sampai 100: rentang terendah mulai dari 0 dan tertinggi sampai 100."
		case err != nil:
			fields["scale"] = fmt.Sprintf("Periksa skala: huruf 1–%d karakter dan tidak boleh ganda, batas 0–100, batas bawah tidak lebih besar dari batas atas.", MaxLetterLen)
		}
		c.Scale = valid
	}
	if len(fields) > 0 {
		return ReportConfig{}, &ConfigError{Fields: fields}
	}
	return c, nil
}

func hasBandErrors(fields map[string]string) bool {
	for k := range fields {
		if strings.HasPrefix(k, "band_") {
			return true
		}
	}
	return false
}

// ReportConfigService menyimpan pengaturan rapor.
type ReportConfigService struct {
	db database.DB
}

// NewReportConfigService mengembalikan ReportConfigService.
func NewReportConfigService(db database.DB) *ReportConfigService { return &ReportConfigService{db: db} }

// Save memvalidasi form lalu menyimpan semua kunci dalam satu transaksi
// beserta audit report_config.update (isi sebelum dan sesudah). Tanpa
// perubahan tidak menambah audit. Perubahan langsung berlaku di rekap (T72),
// tanpa deploy ulang.
func (s *ReportConfigService) Save(ctx context.Context, actor school.Actor, f ReportConfigForm) (changed bool, err error) {
	c, err := ValidateReportConfig(f)
	if err != nil {
		return false, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := LoadReportConfig(ctx, tx, actor.SchoolID)
		if err != nil {
			// Pengaturan lama yang rusak boleh ditimpa dengan yang valid.
			before = ReportConfig{}
		}
		if configSnapshot(before) == configSnapshot(c) {
			return nil
		}
		for key, value := range map[string]string{
			settings.ReportFormat:           c.Format,
			settings.ReportScale:            c.Scale.JSON(),
			settings.ReportKKMDefault:       strconv.Itoa(c.KKMDefault),
			settings.ReportPrincipalName:    c.PrincipalName,
			settings.ReportSignerTitle:      c.SignerTitle,
			settings.ReportPortalComponents: strconv.FormatBool(c.PortalComponents),
		} {
			if err := settings.Set(ctx, tx, actor.SchoolID, key, value); err != nil {
				return err
			}
		}
		changed = true
		var beforeVal any
		if before.Format != "" {
			beforeVal = configAudit(before)
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionReportConfigUpdate, EntityType: "school_settings", EntityID: actor.SchoolID,
			Before: beforeVal, After: configAudit(c),
		})
	})
	return changed, err
}

func configSnapshot(c ReportConfig) string {
	return strings.Join([]string{c.Format, c.Scale.JSON(), strconv.Itoa(c.KKMDefault), c.PrincipalName, c.SignerTitle, strconv.FormatBool(c.PortalComponents)}, "\x00")
}

func configAudit(c ReportConfig) map[string]any {
	return map[string]any{"format": c.Format, "scale": c.Scale, "kkm_default": c.KKMDefault,
		"principal_name": c.PrincipalName, "signer_title": c.SignerTitle, "portal_components": c.PortalComponents}
}
