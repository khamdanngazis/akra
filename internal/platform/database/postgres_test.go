package database

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestConnect_InvalidDSN(t *testing.T) {
	_, err := Connect(context.Background(), "postgres://user:secret@host:notaport/db", 10)
	if err == nil {
		t.Fatal("Connect() error = nil, want error")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error message leaks the password: %v", err)
	}
}

func TestConnect_DatabaseDown(t *testing.T) {
	// Port 1 is almost certainly unused, so the connection is refused immediately.
	_, err := Connect(context.Background(), "postgres://user:secret@127.0.0.1:1/db?connect_timeout=2", 10)
	if err == nil {
		t.Fatal("Connect() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error message does not mention host/port: %v", err)
	}
}

// TestConnect_DatabaseUp runs only when TEST_DATABASE_URL is set.
func TestConnect_DatabaseUp(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	pool, err := Connect(context.Background(), dsn, 4)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	if got := pool.Config().MaxConns; got != 4 {
		t.Errorf("MaxConns = %d, want 4", got)
	}
	if got := pool.Config().MinConns; got != 2 {
		t.Errorf("MinConns = %d, want 2", got)
	}
}
