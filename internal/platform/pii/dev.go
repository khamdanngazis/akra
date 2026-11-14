package pii

import "bytes"

// DevKeyring mengembalikan Keyring dengan kunci tetap untuk development dan
// uji. Kuncinya ada di kode sumber, jadi TIDAK BOLEH dipakai di staging atau
// production; config.Load menolak APP_ENV selain development tanpa
// PII_ENC_KEYS dan PII_INDEX_KEY.
func DevKeyring() *Keyring {
	k, err := NewKeyring(map[byte][]byte{1: bytes.Repeat([]byte{0x11}, KeySize)}, bytes.Repeat([]byte{0x22}, KeySize))
	if err != nil {
		panic(err)
	}
	return k
}
