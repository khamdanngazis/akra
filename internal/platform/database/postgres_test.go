package database

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestConnect_DSNTidakValid(t *testing.T) {
	_, err := Connect(context.Background(), "postgres://user:rahasia@host:notaport/db", 10)
	if err == nil {
		t.Fatal("Connect() error = nil, want error")
	}
	if strings.Contains(err.Error(), "rahasia") {
		t.Errorf("pesan error membocorkan password: %v", err)
	}
}

func TestConnect_DatabaseMati(t *testing.T) {
	// Port 1 hampir pasti tidak dipakai, jadi koneksi langsung ditolak.
	_, err := Connect(context.Background(), "postgres://user:rahasia@127.0.0.1:1/db?connect_timeout=2", 10)
	if err == nil {
		t.Fatal("Connect() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("pesan error tidak menyebut host/port: %v", err)
	}
}

// TestConnect_DatabaseHidup hanya berjalan jika TEST_DATABASE_URL diisi.
func TestConnect_DatabaseHidup(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL tidak diisi")
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
