package auth

import (
	"strings"
	"testing"
	"time"
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

// Jika semua slot terpakai, hash dan verifikasi menunggu sampai ada slot
// yang dilepas (batas memori saat login serentak, T61).
func TestPassword_ConcurrencyLimit(t *testing.T) {
	h := NewHasher(PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1})
	hash, err := h.HashPassword("rahasia")
	if err != nil {
		t.Fatal(err)
	}
	var releases []func()
	for range maxConcurrentHashes {
		releases = append(releases, acquireHashSlot())
	}
	done := make(chan bool)
	go func() {
		ok, _ := VerifyPassword("rahasia", hash)
		done <- ok
	}()
	select {
	case <-done:
		t.Fatal("verification ran while all slots were taken")
	case <-time.After(100 * time.Millisecond):
	}
	releases[0]()
	select {
	case ok := <-done:
		if !ok {
			t.Error("verification failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("verification did not run after a slot was released")
	}
	for _, r := range releases[1:] {
		r()
	}
}
