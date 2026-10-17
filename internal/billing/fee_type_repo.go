package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/platform/database"
)

const feeTypeColumns = `id, school_id, name, default_amount, coalesce(default_due_day, 0), is_active, created_at, updated_at`

// Semua fungsi di bawah memfilter dengan schoolID dari sesi.

// CreateFeeType membuat jenis tagihan aktif.
func CreateFeeType(ctx context.Context, q database.DBTX, schoolID string, in FeeTypeInput) (FeeType, error) {
	return oneFeeType(ctx, q, "create fee type", `
		INSERT INTO fee_types (school_id, name, default_amount, default_due_day)
		VALUES ($1, $2, $3, $4) RETURNING `+feeTypeColumns,
		schoolID, in.Name, in.DefaultAmount, dueDayArg(in.DefaultDueDay))
}

// UpdateFeeType mengubah nama, nominal default, dan tanggal jatuh tempo
// default. Tagihan yang sudah dibuat tidak ikut berubah karena nominalnya
// tersimpan di item tagihan.
func UpdateFeeType(ctx context.Context, q database.DBTX, schoolID, id string, in FeeTypeInput) (FeeType, error) {
	if !database.IsUUID(id) {
		return FeeType{}, ErrFeeTypeNotFound
	}
	return oneFeeType(ctx, q, "update fee type", `
		UPDATE fee_types SET name = $3, default_amount = $4, default_due_day = $5, updated_at = now()
		WHERE school_id = $1 AND id = $2 RETURNING `+feeTypeColumns,
		schoolID, id, in.Name, in.DefaultAmount, dueDayArg(in.DefaultDueDay))
}

// SetFeeTypeActive mengaktifkan atau menonaktifkan jenis tagihan. Jenis
// tagihan tidak pernah dihapus.
func SetFeeTypeActive(ctx context.Context, q database.DBTX, schoolID, id string, active bool) (FeeType, error) {
	if !database.IsUUID(id) {
		return FeeType{}, ErrFeeTypeNotFound
	}
	return oneFeeType(ctx, q, "set fee type active", `
		UPDATE fee_types SET is_active = $3, updated_at = now()
		WHERE school_id = $1 AND id = $2 RETURNING `+feeTypeColumns, schoolID, id, active)
}

// GetFeeType mengembalikan satu jenis tagihan milik schoolID.
func GetFeeType(ctx context.Context, q database.DBTX, schoolID, id string) (FeeType, error) {
	if !database.IsUUID(id) {
		return FeeType{}, ErrFeeTypeNotFound
	}
	return oneFeeType(ctx, q, "get fee type", `SELECT `+feeTypeColumns+` FROM fee_types WHERE school_id = $1 AND id = $2`, schoolID, id)
}

// ListAllFeeTypes mengembalikan semua jenis tagihan sekolah: aktif dulu,
// lalu urut nama.
func ListAllFeeTypes(ctx context.Context, q database.DBTX, schoolID string) ([]FeeType, error) {
	return listFeeTypes(ctx, q, `SELECT `+feeTypeColumns+` FROM fee_types WHERE school_id = $1
		ORDER BY is_active DESC, lower(name)`, schoolID)
}

// ListActiveFeeTypes mengembalikan jenis tagihan aktif sekolah, urut nama.
// Hanya ini yang boleh dipakai untuk tagihan baru.
func ListActiveFeeTypes(ctx context.Context, q database.DBTX, schoolID string) ([]FeeType, error) {
	return listFeeTypes(ctx, q, `SELECT `+feeTypeColumns+` FROM fee_types WHERE school_id = $1 AND is_active
		ORDER BY lower(name)`, schoolID)
}

func oneFeeType(ctx context.Context, q database.DBTX, op, sql string, args ...any) (FeeType, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return FeeType{}, mapFeeTypeError(op, err)
	}
	f, err := pgx.CollectExactlyOneRow(rows, scanFeeType)
	if err != nil {
		return FeeType{}, mapFeeTypeError(op, err)
	}
	return f, nil
}

func listFeeTypes(ctx context.Context, q database.DBTX, sql string, args ...any) ([]FeeType, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapFeeTypeError("list fee types", err)
	}
	list, err := pgx.CollectRows(rows, scanFeeType)
	if err != nil {
		return nil, mapFeeTypeError("list fee types", err)
	}
	return list, nil
}

func scanFeeType(row pgx.CollectableRow) (FeeType, error) {
	var f FeeType
	err := row.Scan(&f.ID, &f.SchoolID, &f.Name, &f.DefaultAmount, &f.DefaultDueDay, &f.IsActive, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

func dueDayArg(day int) any {
	if day == 0 {
		return nil
	}
	return day
}

func mapFeeTypeError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFeeTypeNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "fee_types_school_id_name_key":
			return ErrDuplicateFeeTypeName
		case pgErr.Code == "23514" && pgErr.ConstraintName == "fee_types_default_amount_check":
			return ErrNegativeAmount
		case pgErr.Code == "23514" && pgErr.ConstraintName == "fee_types_default_due_day_check":
			return ErrInvalidDueDay
		}
		return fmt.Errorf("billing: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("billing: %s: %w", op, err)
}
