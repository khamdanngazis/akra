package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX dipenuhi oleh *pgxpool.Pool dan pgx.Tx, sehingga fungsi repository
// bisa dipanggil di luar maupun di dalam transaksi.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB adalah DBTX yang juga bisa memulai transaksi. Dipenuhi oleh
// *pgxpool.Pool dan pgx.Tx (Begin pada pgx.Tx membuat savepoint), sehingga
// uji bisa membungkus service dalam satu transaksi yang di-rollback.
type DB interface {
	DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}
