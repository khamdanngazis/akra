package auth

import (
	"strings"
	"testing"
)

// testParams membuat hashing cepat saat pengujian.
var testParams = PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1}

func TestHashAndVerifyPassword(t *testing.T) {
	h := NewHasher(testParams)

	hash, err := h.HashPassword("rahasia-123")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash = %q, want $argon2id$ prefix", hash)
	}
	if strings.Contains(hash, "rahasia-123") {
		t.Error("hash contains the plaintext password")
	}

	ok, err := VerifyPassword("rahasia-123", hash)
	if err != nil || !ok {
		t.Errorf("VerifyPassword(correct) = %v, %v; want true, nil", ok, err)
	}

	ok, err = VerifyPassword("salah", hash)
	if err != nil || ok {
		t.Errorf("VerifyPassword(wrong) = %v, %v; want false, nil", ok, err)
	}
}

func TestHashPassword_UniqueSalt(t *testing.T) {
	h := NewHasher(testParams)
	a, _ := h.HashPassword("same")
	b, _ := h.HashPassword("same")
	if a == b {
		t.Error("two hashes of the same password are identical; salt is not random")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-hash"); err == nil {
		t.Error("VerifyPassword(malformed) error = nil, want error")
	}
}
