// Package demo membuat dan menghapus data sekolah untuk demo, wawancara,
// dan uji beban. Hanya untuk database development dan staging.
package demo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// DeleteSchool menghapus sekolah berkode code beserta seluruh datanya. Tabel
// keuangan dan audit append-only (dijaga trigger), jadi trigger dan foreign
// key dimatikan sementara dengan session_replication_role (butuh superuser
// atau peran pemilik database). Tidak melakukan apa pun jika sekolah tidak
// ada.
func DeleteSchool(ctx context.Context, db database.DB, code string) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var sid string
		err := tx.QueryRow(ctx, `SELECT id FROM schools WHERE code = $1`, code).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT c.table_name FROM information_schema.columns c
			JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
			WHERE c.table_schema = 'public' AND c.column_name = 'school_id' AND t.table_type = 'BASE TABLE'`)
		if err != nil {
			return err
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id IN (SELECT id FROM users WHERE school_id = $1)`, sid); err != nil {
			return err
		}
		for _, t := range tables {
			if _, err := tx.Exec(ctx, `DELETE FROM `+pgx.Identifier{t}.Sanitize()+` WHERE school_id = $1`, sid); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM schools WHERE id = $1`, sid)
		return err
	})
}
