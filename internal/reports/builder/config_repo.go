package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

var (
	// ErrConfigNotFound: konfigurasi tidak ada, milik sekolah lain, sudah
	// dihapus, atau bukan milik pengguna dan tidak dibagikan kepadanya.
	ErrConfigNotFound = errors.New("builder: report config not found")
	// ErrDuplicateName: pemilik sudah punya konfigurasi dengan nama itu.
	ErrDuplicateName = errors.New("builder: report config name already used")
)

// SavedConfig adalah konfigurasi laporan tersimpan (T77).
type SavedConfig struct {
	ID        string
	OwnerID   string
	OwnerName string
	Name      string
	Config    Config
	Shared    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

const configSelect = `
SELECT rc.id, rc.owner_user_id, u.full_name, rc.name, rc.source, rc.columns, rc.filters, rc.sort, rc.is_shared, rc.created_at, rc.updated_at
FROM report_configs rc
JOIN users u ON u.school_id = rc.school_id AND u.id = rc.owner_user_id`

func scanConfig(row pgx.CollectableRow) (SavedConfig, error) {
	var c SavedConfig
	var cols, filters, sort []byte
	if err := row.Scan(&c.ID, &c.OwnerID, &c.OwnerName, &c.Name, &c.Config.Source, &cols, &filters, &sort, &c.Shared, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return c, err
	}
	for _, part := range []struct {
		raw []byte
		dst any
	}{{cols, &c.Config.Columns}, {filters, &c.Config.Filters}, {sort, &c.Config.Order}} {
		if err := json.Unmarshal(part.raw, part.dst); err != nil {
			return c, fmt.Errorf("builder: decode report config %s: %w", c.ID, err)
		}
	}
	return c, nil
}

// listConfigs mengembalikan konfigurasi sekolah yang belum dihapus: milik
// userID, atau yang dibagikan. Penyaringan peran sumber dilakukan service.
func listConfigs(ctx context.Context, q database.DBTX, schoolID, userID string) ([]SavedConfig, error) {
	rows, err := q.Query(ctx, configSelect+`
		WHERE rc.school_id = $1 AND rc.deleted_at IS NULL AND (rc.owner_user_id = $2 OR rc.is_shared)
		ORDER BY lower(rc.name), rc.created_at, rc.id`, schoolID, userID)
	if err != nil {
		return nil, fmt.Errorf("builder: list report configs: %w", err)
	}
	return pgx.CollectRows(rows, scanConfig)
}

// getConfig mengembalikan konfigurasi sekolah yang belum dihapus, milik
// siapa pun. lock true mengunci barisnya untuk diubah.
func getConfig(ctx context.Context, q database.DBTX, schoolID, id string, lock bool) (SavedConfig, error) {
	if !database.IsUUID(id) {
		return SavedConfig{}, ErrConfigNotFound
	}
	sql := configSelect + ` WHERE rc.school_id = $1 AND rc.id = $2 AND rc.deleted_at IS NULL`
	if lock {
		sql += ` FOR UPDATE OF rc`
	}
	rows, err := q.Query(ctx, sql, schoolID, id)
	if err != nil {
		return SavedConfig{}, fmt.Errorf("builder: get report config: %w", err)
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanConfig)
	if errors.Is(err, pgx.ErrNoRows) {
		return SavedConfig{}, ErrConfigNotFound
	}
	return c, err
}

func encodeConfig(c Config) (cols, filters, sort []byte, err error) {
	if cols, err = json.Marshal(c.Columns); err != nil {
		return
	}
	if filters, err = json.Marshal(nonNil(c.Filters)); err != nil {
		return
	}
	sort, err = json.Marshal(nonNil(c.Order))
	return
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func insertConfig(ctx context.Context, q database.DBTX, schoolID, ownerID, name string, c Config, shared bool) (string, error) {
	cols, filters, sort, err := encodeConfig(c)
	if err != nil {
		return "", err
	}
	var id string
	err = q.QueryRow(ctx, `
		INSERT INTO report_configs (school_id, owner_user_id, name, source, columns, filters, sort, is_shared)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		schoolID, ownerID, name, c.Source, cols, filters, sort, shared).Scan(&id)
	return id, mapConfigError("insert report config", err)
}

func updateConfig(ctx context.Context, q database.DBTX, schoolID, id, name string, c Config, shared bool) error {
	cols, filters, sort, err := encodeConfig(c)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		UPDATE report_configs SET name = $3, source = $4, columns = $5, filters = $6, sort = $7, is_shared = $8, updated_at = now()
		WHERE school_id = $1 AND id = $2 AND deleted_at IS NULL`,
		schoolID, id, name, c.Source, cols, filters, sort, shared)
	return mapConfigError("update report config", err)
}

func softDeleteConfig(ctx context.Context, q database.DBTX, schoolID, id string) error {
	_, err := q.Exec(ctx, `UPDATE report_configs SET deleted_at = now(), is_shared = false, updated_at = now()
		WHERE school_id = $1 AND id = $2 AND deleted_at IS NULL`, schoolID, id)
	return mapConfigError("delete report config", err)
}

func mapConfigError(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && pgErr.ConstraintName == "report_configs_owner_name_key" {
			return ErrDuplicateName
		}
		return fmt.Errorf("builder: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("builder: %s: %w", op, err)
}
