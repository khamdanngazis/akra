// Package pii mengenkripsi data pribadi (NIK, No KK, no akta, dan
// sejenisnya) sebelum disimpan di database (T85b).
//
// Enkripsi memakai AES-256-GCM di level aplikasi dengan nonce acak per
// nilai. Kunci berasal dari environment variable, terpisah dari database
// dan backup. Pencarian dan cek unik memakai blind index (HMAC-SHA256)
// dengan kunci tersendiri.
//
// Jangan memakai pgcrypto dengan kunci dikirim di query: kunci bisa masuk
// log PostgreSQL dan pg_stat_statements.
package pii

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// KeySize adalah panjang kunci enkripsi dan kunci indeks (byte).
const KeySize = 32

const nonceSize = 12

// ErrDecrypt dikembalikan jika ciphertext rusak, dipindah ke kolom lain,
// atau dienkripsi dengan kunci yang tidak dikenal. Pesan sengaja tidak
// membedakan penyebabnya.
var ErrDecrypt = errors.New("pii: cannot decrypt value")

// Keyring menyimpan kunci enkripsi (bisa beberapa versi, untuk rotasi) dan
// kunci blind index.
type Keyring struct {
	aeads    map[byte]cipher.AEAD
	active   byte
	indexKey []byte
}

// NewKeyring membuat Keyring. encKeys dipetakan per versi (1–255); versi
// tertinggi menjadi kunci aktif untuk enkripsi baru.
func NewKeyring(encKeys map[byte][]byte, indexKey []byte) (*Keyring, error) {
	if len(encKeys) == 0 {
		return nil, errors.New("pii: at least one encryption key is required")
	}
	if len(indexKey) != KeySize {
		return nil, fmt.Errorf("pii: index key must be %d bytes, got %d", KeySize, len(indexKey))
	}
	k := &Keyring{aeads: make(map[byte]cipher.AEAD, len(encKeys)), indexKey: append([]byte(nil), indexKey...)}
	for v, key := range encKeys {
		if v == 0 {
			return nil, errors.New("pii: key version must be 1-255")
		}
		if len(key) != KeySize {
			return nil, fmt.Errorf("pii: encryption key version %d must be %d bytes, got %d", v, KeySize, len(key))
		}
		if hmac.Equal(key, indexKey) {
			return nil, errors.New("pii: index key must differ from encryption keys")
		}
		aead, err := newAEAD(key)
		if err != nil {
			return nil, fmt.Errorf("pii: key version %d: %w", v, err)
		}
		k.aeads[v] = aead
		if v > k.active {
			k.active = v
		}
	}
	return k, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// ParseKeyring membaca format environment variable:
//   - encSpec (PII_ENC_KEYS): "versi:base64" dipisah koma, mis. "1:AAAA…,2:BBBB…"
//   - indexSpec (PII_INDEX_KEY): base64 32 byte
//
// Pesan error tidak pernah memuat isi kunci.
func ParseKeyring(encSpec, indexSpec string) (*Keyring, error) {
	encKeys := map[byte][]byte{}
	for _, part := range strings.Split(encSpec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ver, b64, ok := strings.Cut(part, ":")
		if !ok {
			return nil, errors.New("pii: PII_ENC_KEYS entries must be version:base64key")
		}
		n, err := strconv.ParseUint(strings.TrimSpace(ver), 10, 8)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("pii: PII_ENC_KEYS has invalid version %q (allowed 1-255)", ver)
		}
		if _, dup := encKeys[byte(n)]; dup {
			return nil, fmt.Errorf("pii: PII_ENC_KEYS has duplicate version %d", n)
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return nil, fmt.Errorf("pii: PII_ENC_KEYS version %d is not valid base64", n)
		}
		encKeys[byte(n)] = key
	}
	idx, err := base64.StdEncoding.DecodeString(strings.TrimSpace(indexSpec))
	if err != nil {
		return nil, errors.New("pii: PII_INDEX_KEY is not valid base64")
	}
	return NewKeyring(encKeys, idx)
}

// ActiveVersion mengembalikan versi kunci yang dipakai untuk enkripsi baru.
func (k *Keyring) ActiveVersion() byte { return k.active }

// Versions mengembalikan semua versi kunci yang dikenal, urut naik.
func (k *Keyring) Versions() []byte {
	vs := make([]byte, 0, len(k.aeads))
	for v := range k.aeads {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i] < vs[j] })
	return vs
}

// Encrypt mengenkripsi plain dengan kunci aktif. aad mengikat ciphertext ke
// lokasi penyimpanannya (mis. "student_profiles.nik"), sehingga ciphertext
// yang dipindah ke kolom lain gagal didekripsi.
//
// Format hasil: versi kunci (1 byte) || nonce (12 byte) || ciphertext+tag.
func (k *Keyring) Encrypt(plain, aad string) ([]byte, error) {
	aead := k.aeads[k.active]
	out := make([]byte, 1+nonceSize, 1+nonceSize+len(plain)+aead.Overhead())
	out[0] = k.active
	if _, err := rand.Read(out[1 : 1+nonceSize]); err != nil {
		return nil, fmt.Errorf("pii: generate nonce: %w", err)
	}
	return aead.Seal(out, out[1:1+nonceSize], []byte(plain), []byte(aad)), nil
}

// Decrypt membuka ciphertext hasil Encrypt dengan aad yang sama.
func (k *Keyring) Decrypt(ct []byte, aad string) (string, error) {
	if len(ct) < 1+nonceSize {
		return "", ErrDecrypt
	}
	aead, ok := k.aeads[ct[0]]
	if !ok || len(ct) < 1+nonceSize+aead.Overhead() {
		return "", ErrDecrypt
	}
	plain, err := aead.Open(nil, ct[1:1+nonceSize], ct[1+nonceSize:], []byte(aad))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// NeedsRotation melaporkan apakah ct dienkripsi dengan kunci selain kunci
// aktif.
func (k *Keyring) NeedsRotation(ct []byte) bool {
	return len(ct) > 0 && ct[0] != k.active
}

// BlindIndex menghasilkan HMAC-SHA256 dari nilai yang sudah dinormalisasi.
// kind (mis. "nik", "kk") ikut di-HMAC agar NIK dan No KK yang sama tidak
// menghasilkan indeks yang sama. Hasilnya deterministik, jadi bisa dipakai
// untuk UNIQUE dan pencarian nilai lengkap, tetapi tidak untuk pencarian
// sebagian.
func (k *Keyring) BlindIndex(kind, plain string) []byte {
	m := hmac.New(sha256.New, k.indexKey)
	m.Write([]byte(kind))
	m.Write([]byte{0})
	m.Write([]byte(Normalize(plain)))
	return m.Sum(nil)
}

// Normalize membuang spasi di awal, akhir, dan tengah nilai, lalu
// mengubahnya ke huruf besar. Dipakai sebelum blind index agar "3201 0101"
// dan "32010101" dianggap sama.
func Normalize(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}
