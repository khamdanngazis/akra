package auth

import (
	"fmt"

	"github.com/alexedwards/argon2id"
)

// PasswordParams adalah parameter biaya argon2id yang bisa diatur.
type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// maxConcurrentHashes membatasi hash dan verifikasi argon2id yang berjalan
// bersamaan di satu proses. Setiap operasi memakai MemoryKiB (bawaan 19 MiB),
// jadi tanpa batas ini login serentak (mis. 65 pengguna, uji beban T61)
// memakan >1 GiB dan bisa membuat kontainer kehabisan memori. Pekerjaannya
// CPU-bound, jadi lebih dari jumlah core tidak mempercepat; sisanya antre.
const maxConcurrentHashes = 4

// hashSlots adalah semaphore untuk maxConcurrentHashes.
var hashSlots = make(chan struct{}, maxConcurrentHashes)

func acquireHashSlot() func() {
	hashSlots <- struct{}{}
	return func() { <-hashSlots }
}

// Hasher membuat hash password dengan argon2id memakai parameter tetap.
type Hasher struct {
	params *argon2id.Params
}

// NewHasher mengembalikan Hasher dengan parameter p, salt 16 byte, dan key 32 byte.
func NewHasher(p PasswordParams) *Hasher {
	return &Hasher{params: &argon2id.Params{
		Memory:      p.MemoryKiB,
		Iterations:  p.Iterations,
		Parallelism: p.Parallelism,
		SaltLength:  16,
		KeyLength:   32,
	}}
}

// HashPassword mengembalikan hash argon2id dari plain dalam format PHC
// ("$argon2id$v=19$m=...,t=...,p=...$salt$key").
func (h *Hasher) HashPassword(plain string) (string, error) {
	defer acquireHashSlot()()
	hash, err := argon2id.CreateHash(plain, h.params)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return hash, nil
}

// VerifyPassword memeriksa apakah plain cocok dengan hash. Perbandingan
// berjalan dalam waktu konstan dan memakai parameter yang tersimpan di hash,
// jadi hash dengan parameter lama tetap bisa diverifikasi. Paling banyak
// maxConcurrentHashes berjalan bersamaan; panggilan lain menunggu.
func VerifyPassword(plain, hash string) (bool, error) {
	defer acquireHashSlot()()
	ok, err := argon2id.ComparePasswordAndHash(plain, hash)
	if err != nil {
		return false, fmt.Errorf("auth: verify password: %w", err)
	}
	return ok, nil
}
