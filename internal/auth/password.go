package auth

import (
	"fmt"

	"github.com/alexedwards/argon2id"
)

// PasswordParams are the tunable argon2id cost parameters.
type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// Hasher hashes passwords with argon2id using fixed parameters.
type Hasher struct {
	params *argon2id.Params
}

// NewHasher returns a Hasher that uses p with a 16-byte salt and 32-byte key.
func NewHasher(p PasswordParams) *Hasher {
	return &Hasher{params: &argon2id.Params{
		Memory:      p.MemoryKiB,
		Iterations:  p.Iterations,
		Parallelism: p.Parallelism,
		SaltLength:  16,
		KeyLength:   32,
	}}
}

// HashPassword returns the PHC-encoded argon2id hash of plain
// ("$argon2id$v=19$m=...,t=...,p=...$salt$key").
func (h *Hasher) HashPassword(plain string) (string, error) {
	hash, err := argon2id.CreateHash(plain, h.params)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return hash, nil
}

// VerifyPassword reports whether plain matches hash. The comparison is
// constant-time and uses the parameters encoded in hash, so hashes created
// with older parameters keep working.
func VerifyPassword(plain, hash string) (bool, error) {
	ok, err := argon2id.ComparePasswordAndHash(plain, hash)
	if err != nil {
		return false, fmt.Errorf("auth: verify password: %w", err)
	}
	return ok, nil
}
