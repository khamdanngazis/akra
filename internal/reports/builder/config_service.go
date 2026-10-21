package builder

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// Aksi audit konfigurasi laporan.
const (
	ActionConfigCreate = "report_config.create"
	ActionConfigUpdate = "report_config.update"
	ActionConfigShare  = "report_config.share"
	ActionConfigDelete = "report_config.delete"
)

// MaxConfigNameLen adalah panjang maksimal nama konfigurasi.
const MaxConfigNameLen = 80

var (
	// ErrNotOwner: konfigurasi hanya bisa diubah atau dihapus pemiliknya.
	ErrNotOwner = errors.New("builder: only the owner can change this report config")
	// ErrSourceForbidden: peran pengguna tidak boleh memakai sumber itu.
	ErrSourceForbidden = errors.New("builder: source not allowed for these roles")
)

// ConfigError berisi pesan per field untuk form ("name", "source",
// "columns", "filters", "sort").
type ConfigError struct {
	Fields map[string]string
}

func (e *ConfigError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return "builder: invalid report config: " + strings.Join(keys, ", ")
}

// ConfigInput adalah isian simpan konfigurasi.
type ConfigInput struct {
	Name   string
	Config Config
	Shared bool
}

// ConfigService menyimpan konfigurasi laporan.
type ConfigService struct {
	db  database.DB
	reg *Registry
}

// NewConfigService mengembalikan ConfigService untuk registry reg.
func NewConfigService(db database.DB, reg *Registry) *ConfigService {
	return &ConfigService{db: db, reg: reg}
}

// Registry mengembalikan registry yang dipakai service.
func (s *ConfigService) Registry() *Registry { return s.reg }

// canUse melaporkan apakah id boleh memakai sumber konfigurasi c.
func (s *ConfigService) canUse(id auth.Identity, c Config) bool {
	src, ok := s.reg.Source(c.Source)
	return ok && src.Allowed(id.Roles)
}

// Visible mengembalikan konfigurasi milik id dan konfigurasi bersama di
// sekolahnya yang sumbernya boleh dipakai perannya. Konfigurasi sekolah
// lain tidak pernah ikut (school_id dari identitas).
func (s *ConfigService) Visible(ctx context.Context, id auth.Identity) ([]SavedConfig, error) {
	all, err := listConfigs(ctx, s.db, id.SchoolID, id.UserID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, c := range all {
		if c.OwnerID == id.UserID || s.canUse(id, c.Config) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Get mengembalikan satu konfigurasi yang terlihat oleh id (aturan Visible).
func (s *ConfigService) Get(ctx context.Context, id auth.Identity, configID string) (SavedConfig, error) {
	c, err := getConfig(ctx, s.db, id.SchoolID, configID, false)
	if err != nil {
		return SavedConfig{}, err
	}
	if c.OwnerID != id.UserID && (!c.Shared || !s.canUse(id, c.Config)) {
		return SavedConfig{}, ErrConfigNotFound
	}
	return c, nil
}

// Validate memeriksa input dengan aturan builder (T76): sumber dan kolom
// terdaftar, operator sesuai tipe, nilai filter valid, batas jumlah, dan
// sumber boleh dipakai peran pengguna. Konfigurasi yang tidak valid tidak
// pernah disimpan.
func (s *ConfigService) Validate(id auth.Identity, in ConfigInput) (ConfigInput, error) {
	fields := map[string]string{}
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		fields["name"] = "Isi nama konfigurasi."
	case n > MaxConfigNameLen:
		fields["name"] = fmt.Sprintf("Nama maksimal %d karakter.", MaxConfigNameLen)
	}
	src, ok := s.reg.Source(in.Config.Source)
	switch {
	case !ok:
		fields["source"] = "Pilih sumber data dari daftar."
	case !src.Allowed(id.Roles):
		fields["source"] = "Anda tidak punya akses ke sumber data ini."
	default:
		if _, err := Compile(s.reg, id.SchoolID, in.Config, Options{}); err != nil {
			field, msg := compileMessage(err)
			fields[field] = msg
		}
	}
	if len(fields) > 0 {
		return ConfigInput{}, &ConfigError{Fields: fields}
	}
	return in, nil
}

// compileMessage memetakan error Compile ke field dan pesan form.
func compileMessage(err error) (string, string) {
	switch {
	case errors.Is(err, ErrNoColumns):
		return "columns", "Pilih minimal satu kolom."
	case errors.Is(err, ErrTooManyColumns):
		return "columns", fmt.Sprintf("Maksimal %d kolom.", MaxColumns)
	case errors.Is(err, ErrDuplicateColumn):
		return "columns", "Kolom yang sama dipilih dua kali."
	case errors.Is(err, ErrTooManyFilters):
		return "filters", fmt.Sprintf("Maksimal %d filter.", MaxFilters)
	case errors.Is(err, ErrOperatorNotAllowed):
		return "filters", "Operator filter tidak cocok dengan tipe kolom."
	case errors.Is(err, ErrInvalidValue):
		return "filters", "Nilai filter tidak valid: " + strings.TrimPrefix(err.Error(), ErrInvalidValue.Error()+": ")
	case errors.Is(err, ErrTooManyOrder):
		return "sort", fmt.Sprintf("Maksimal %d urutan.", MaxOrder)
	case errors.Is(err, ErrUnknownColumn):
		return "columns", "Ada kolom yang tidak terdaftar untuk sumber ini."
	}
	return "source", "Konfigurasi tidak valid."
}

// Create menyimpan konfigurasi baru milik actor beserta audit
// report_config.create (dan report_config.share jika langsung dibagikan).
func (s *ConfigService) Create(ctx context.Context, actor school.Actor, id auth.Identity, in ConfigInput) (SavedConfig, error) {
	in, err := s.Validate(id, in)
	if err != nil {
		return SavedConfig{}, err
	}
	var saved SavedConfig
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		cid, err := insertConfig(ctx, tx, actor.SchoolID, actor.UserID, in.Name, in.Config, in.Shared)
		if err != nil {
			return err
		}
		if saved, err = getConfig(ctx, tx, actor.SchoolID, cid, false); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, configAudit(actor, ActionConfigCreate, saved, nil)); err != nil {
			return err
		}
		if in.Shared {
			return audit.Record(ctx, tx, shareAudit(actor, saved.ID, false, true))
		}
		return nil
	})
	if err != nil {
		return SavedConfig{}, nameError(err)
	}
	return saved, nil
}

// Update mengubah konfigurasi configID. Hanya pemilik (ErrNotOwner, 403);
// konfigurasi sekolah lain atau yang sudah dihapus ErrConfigNotFound.
// Audit report_config.update jika isi berubah, report_config.share jika
// status berbagi berubah.
func (s *ConfigService) Update(ctx context.Context, actor school.Actor, id auth.Identity, configID string, in ConfigInput) (SavedConfig, error) {
	var saved SavedConfig
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := getConfig(ctx, tx, actor.SchoolID, configID, true)
		if err != nil {
			return err
		}
		if before.OwnerID != actor.UserID {
			return ErrNotOwner
		}
		if in, err = s.Validate(id, in); err != nil {
			return err
		}
		contentChanged := in.Name != before.Name || !sameConfig(in.Config, before.Config)
		if !contentChanged && in.Shared == before.Shared {
			saved = before
			return nil
		}
		if err := updateConfig(ctx, tx, actor.SchoolID, configID, in.Name, in.Config, in.Shared); err != nil {
			return err
		}
		if saved, err = getConfig(ctx, tx, actor.SchoolID, configID, false); err != nil {
			return err
		}
		if contentChanged {
			if err := audit.Record(ctx, tx, configAudit(actor, ActionConfigUpdate, saved, &before)); err != nil {
				return err
			}
		}
		if in.Shared != before.Shared {
			return audit.Record(ctx, tx, shareAudit(actor, configID, before.Shared, in.Shared))
		}
		return nil
	})
	if err != nil {
		return SavedConfig{}, nameError(err)
	}
	return saved, nil
}

// Delete menghapus konfigurasi secara lunak (deleted_at diisi, berbagi
// dimatikan). Hanya pemilik.
func (s *ConfigService) Delete(ctx context.Context, actor school.Actor, configID string) (SavedConfig, error) {
	var before SavedConfig
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if before, err = getConfig(ctx, tx, actor.SchoolID, configID, true); err != nil {
			return err
		}
		if before.OwnerID != actor.UserID {
			return ErrNotOwner
		}
		if err := softDeleteConfig(ctx, tx, actor.SchoolID, configID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionConfigDelete, EntityType: "report_config", EntityID: configID, Before: configSnapshot(before)})
	})
	return before, err
}

func nameError(err error) error {
	if errors.Is(err, ErrDuplicateName) {
		return &ConfigError{Fields: map[string]string{"name": "Anda sudah punya konfigurasi dengan nama ini."}}
	}
	return err
}

func sameConfig(a, b Config) bool {
	ea, fa, sa, _ := encodeConfig(a)
	eb, fb, sb, _ := encodeConfig(b)
	return a.Source == b.Source && string(ea) == string(eb) && string(fa) == string(fb) && string(sa) == string(sb)
}

func configSnapshot(c SavedConfig) map[string]any {
	return map[string]any{"name": c.Name, "source": c.Config.Source, "columns": c.Config.Columns,
		"filters": nonNil(c.Config.Filters), "sort": nonNil(c.Config.Order), "is_shared": c.Shared}
}

func configAudit(actor school.Actor, action string, after SavedConfig, before *SavedConfig) audit.Entry {
	e := audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP, Action: action,
		EntityType: "report_config", EntityID: after.ID, After: configSnapshot(after)}
	if before != nil {
		e.Before = configSnapshot(*before)
	}
	return e
}

func shareAudit(actor school.Actor, id string, before, after bool) audit.Entry {
	return audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP, Action: ActionConfigShare,
		EntityType: "report_config", EntityID: id, Before: map[string]any{"is_shared": before}, After: map[string]any{"is_shared": after}}
}
