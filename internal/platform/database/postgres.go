// Package database sets up the PostgreSQL connection pool.
package database

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMinConns = 2
	pingTimeout     = 5 * time.Second
)

// Connect creates a connection pool for dsn and verifies the database is
// reachable with a Ping. maxConns is the upper bound of pooled connections.
func Connect(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// Do not include the dsn in the error: it may contain a password.
		return nil, fmt.Errorf("database: invalid DATABASE_URL")
	}

	poolCfg.MaxConns = maxConns
	poolCfg.MinConns = min(defaultMinConns, maxConns)

	addr := net.JoinHostPort(poolCfg.ConnConfig.Host, strconv.Itoa(int(poolCfg.ConnConfig.Port)))

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool for %s: %w", addr, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: cannot connect to %s: %w", addr, err)
	}

	return pool, nil
}
