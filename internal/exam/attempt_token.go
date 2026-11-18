package exam

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token attempt (ADR 0001 bagian 4, T139): tanda bukti bahwa perangkat
// boleh menyimpan jawaban sebuah attempt. Endpoint simpan memverifikasinya
// murni dengan CPU, tanpa membaca sesi atau database.
//
// Format: base64url(sekolah ‖ attempt ‖ perangkat ‖ kedaluwarsa) "."
// base64url(HMAC-SHA256). Dikirim di header X-Attempt-Token, tidak pernah
// lewat cookie, sehingga tidak butuh CSRF.

// AttemptTokenHeader adalah header pembawa token attempt.
const AttemptTokenHeader = "X-Attempt-Token"

// attemptTokenTTL adalah masa berlaku token setelah tenggat attempt. Batas
// sebenarnya tetap diperiksa di SQL (status, perangkat, tenggat); masa
// berlaku ini hanya batas atas untuk token bocor, dan memberi ruang bila
// pengawas menambah waktu (T143/T144).
const attemptTokenTTL = 6 * time.Hour

// minTokenKeyBytes adalah panjang minimum kunci HMAC.
const minTokenKeyBytes = 32

// ErrBadAttemptToken: token kosong, rusak, tanda tangan salah, atau
// kedaluwarsa.
var ErrBadAttemptToken = errors.New("exam: invalid attempt token")

// AttemptClaims adalah isi token attempt.
type AttemptClaims struct {
	SchoolID  string
	AttemptID string
	DeviceID  string
	Expires   time.Time
}

// TokenSigner menandatangani dan memverifikasi token attempt. Kunci
// pertama dipakai untuk menandatangani; semua kunci diterima saat
// verifikasi (rotasi: kunci baru di depan, kunci lama di belakang sampai
// token lama kedaluwarsa).
type TokenSigner struct {
	keys [][]byte
}

// ParseTokenSigner membaca EXAM_TOKEN_KEYS: daftar kunci base64 (standar
// atau URL, boleh tanpa padding) dipisah koma, masing-masing minimal 32
// byte.
func ParseTokenSigner(s string) (*TokenSigner, error) {
	var keys [][]byte
	for i, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, err := decodeKey(part)
		if err != nil {
			return nil, fmt.Errorf("exam: EXAM_TOKEN_KEYS key %d is not valid base64", i+1)
		}
		if len(k) < minTokenKeyBytes {
			return nil, fmt.Errorf("exam: EXAM_TOKEN_KEYS key %d must be at least %d bytes, got %d", i+1, minTokenKeyBytes, len(k))
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, errors.New("exam: EXAM_TOKEN_KEYS is empty")
	}
	return &TokenSigner{keys: keys}, nil
}

func decodeKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if k, err := enc.DecodeString(s); err == nil {
			return k, nil
		}
	}
	return nil, errors.New("invalid base64")
}

// DevTokenSigner mengembalikan signer dengan kunci tetap untuk development
// dan uji. Jangan dipakai di staging/production.
func DevTokenSigner() *TokenSigner {
	return &TokenSigner{keys: [][]byte{[]byte("akra-dev-exam-token-key-not-secret!!")}}
}

const tokenPayloadLen = 16*3 + 8

// Sign membuat token untuk claims.
func (s *TokenSigner) Sign(c AttemptClaims) (string, error) {
	payload := make([]byte, 0, tokenPayloadLen)
	for _, id := range []string{c.SchoolID, c.AttemptID, c.DeviceID} {
		u, err := uuidBytes(id)
		if err != nil {
			return "", fmt.Errorf("exam: sign attempt token: %w", err)
		}
		payload = append(payload, u...)
	}
	payload = binary.BigEndian.AppendUint64(payload, uint64(c.Expires.Unix()))
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(s.keys[0], payload)), nil
}

// Verify memeriksa tanda tangan dan masa berlaku token.
func (s *TokenSigner) Verify(tok string, now time.Time) (AttemptClaims, error) {
	// Strict: bit sisa di karakter terakhir harus nol, sehingga satu token
	// hanya punya satu bentuk teks.
	enc := base64.RawURLEncoding.Strict()
	p, sig, ok := strings.Cut(tok, ".")
	if !ok || len(p) != enc.EncodedLen(tokenPayloadLen) || len(sig) != enc.EncodedLen(sha256.Size) {
		return AttemptClaims{}, ErrBadAttemptToken
	}
	payload, err1 := enc.DecodeString(p)
	got, err2 := enc.DecodeString(sig)
	if err1 != nil || err2 != nil {
		return AttemptClaims{}, ErrBadAttemptToken
	}
	valid := false
	for _, k := range s.keys {
		if hmac.Equal(got, mac(k, payload)) {
			valid = true
			break
		}
	}
	if !valid {
		return AttemptClaims{}, ErrBadAttemptToken
	}
	c := AttemptClaims{
		SchoolID:  uuidString(payload[0:16]),
		AttemptID: uuidString(payload[16:32]),
		DeviceID:  uuidString(payload[32:48]),
		Expires:   time.Unix(int64(binary.BigEndian.Uint64(payload[48:56])), 0),
	}
	if !now.Before(c.Expires) {
		return AttemptClaims{}, ErrBadAttemptToken
	}
	return c, nil
}

// TokenFor membuat token untuk attempt yang berjalan di perangkatnya.
func (s *TokenSigner) TokenFor(a Attempt) (string, error) {
	return s.Sign(AttemptClaims{SchoolID: a.SchoolID, AttemptID: a.ID, DeviceID: a.DeviceID,
		Expires: a.DeadlineAt.Add(SaveGrace + attemptTokenTTL)})
}

func mac(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("akra.exam.attempt.v1\x00"))
	h.Write(payload)
	return h.Sum(nil)
}

// uuidBytes mengubah UUID teks (36 karakter) menjadi 16 byte.
func uuidBytes(s string) ([]byte, error) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return nil, fmt.Errorf("invalid uuid %q", s)
	}
	b, err := hex.DecodeString(s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36])
	if err != nil {
		return nil, fmt.Errorf("invalid uuid %q", s)
	}
	return b, nil
}

// uuidString memformat 16 byte sebagai UUID teks huruf kecil.
func uuidString(b []byte) string {
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
