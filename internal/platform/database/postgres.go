// Package database menyiapkan connection pool PostgreSQL.
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

// Connect membuat pool koneksi ke dsn dan memastikan database bisa dijangkau
// dengan Ping. maxConns adalah batas atas koneksi dalam pool.
func Connect(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// Jangan sertakan dsn di pesan error karena bisa berisi password.
		return nil, fmt.Errorf("database: DATABASE_URL tidak valid")
	}

	poolCfg.MaxConns = maxConns
	poolCfg.MinConns = min(defaultMinConns, maxConns)

	addr := net.JoinHostPort(poolCfg.ConnConfig.Host, strconv.Itoa(int(poolCfg.ConnConfig.Port)))

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: gagal membuat pool ke %s: %w", addr, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: tidak bisa terhubung ke %s: %w", addr, err)
	}

	return pool, nil
}
