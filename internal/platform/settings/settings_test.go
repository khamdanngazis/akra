package settings

import (
	"context"
	"os"
	"strings"
	"testing"

	"akra/internal/platform/database"
)

func TestSettings(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var a, b string
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('test-set-a', 'test-set-a') RETURNING id`).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('test-set-b', 'test-set-b') RETURNING id`).Scan(&b); err != nil {
		t.Fatal(err)
	}

	if n, err := Int64(ctx, tx, a, VoidApprovalThreshold, 7); err != nil || n != 7 {
		t.Errorf("unset: %d, %v; want fallback 7", n, err)
	}
	if err := Set(ctx, tx, a, VoidApprovalThreshold, "500000"); err != nil {
		t.Fatal(err)
	}
	if err := Set(ctx, tx, a, VoidApprovalThreshold, "1000000"); err != nil {
		t.Fatal(err)
	}
	if n, err := Int64(ctx, tx, a, VoidApprovalThreshold, 7); err != nil || n != 1000000 {
		t.Errorf("after set: %d, %v", n, err)
	}
	if _, ok, _ := Get(ctx, tx, b, VoidApprovalThreshold); ok {
		t.Error("setting leaked to another school")
	}
	if err := Set(ctx, tx, b, VoidApprovalThreshold, "banyak"); err != nil {
		t.Fatal(err)
	}
	if _, err := Int64(ctx, tx, b, VoidApprovalThreshold, 7); err == nil || !strings.Contains(err.Error(), "not an integer") {
		t.Errorf("non-integer value: error = %v", err)
	}
}
