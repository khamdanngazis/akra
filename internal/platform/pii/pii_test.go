package pii

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const sampleNIK = "3201010101010001"

func testKeyring(t *testing.T, versions ...byte) *Keyring {
	t.Helper()
	keys := map[byte][]byte{}
	for _, v := range versions {
		keys[v] = bytes.Repeat([]byte{v}, KeySize)
	}
	k, err := NewKeyring(keys, bytes.Repeat([]byte{0xAA}, KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecrypt(t *testing.T) {
	k := testKeyring(t, 1)
	a, err := k.Encrypt(sampleNIK, "student_profiles.nik")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := k.Encrypt(sampleNIK, "student_profiles.nik")
	if bytes.Equal(a, b) {
		t.Error("encrypting the same value twice must give different ciphertexts")
	}
	if bytes.Contains(a, []byte(sampleNIK)) {
		t.Error("ciphertext contains plaintext")
	}
	if got, err := k.Decrypt(a, "student_profiles.nik"); err != nil || got != sampleNIK {
		t.Errorf("Decrypt = %q, %v", got, err)
	}
	if !bytes.Equal(k.BlindIndex("nik", sampleNIK), k.BlindIndex("nik", sampleNIK)) {
		t.Error("blind index must be deterministic")
	}

	// Kosong tetap bisa dienkripsi dan didekripsi.
	e, _ := k.Encrypt("", "x")
	if got, err := k.Decrypt(e, "x"); err != nil || got != "" {
		t.Errorf("empty roundtrip = %q, %v", got, err)
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	k := testKeyring(t, 1)
	ct, _ := k.Encrypt(sampleNIK, "student_profiles.nik")
	for i := range ct {
		bad := append([]byte(nil), ct...)
		bad[i] ^= 0x01
		if got, err := k.Decrypt(bad, "student_profiles.nik"); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("flipping byte %d: Decrypt = %q, %v; want ErrDecrypt", i, got, err)
		}
	}
	if _, err := k.Decrypt(ct, "guardians.nik"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("ciphertext moved to another column: err = %v, want ErrDecrypt", err)
	}
	for _, short := range [][]byte{nil, {1}, ct[:13], ct[:20]} {
		if _, err := k.Decrypt(short, "student_profiles.nik"); !errors.Is(err, ErrDecrypt) {
			t.Errorf("short ciphertext (%d bytes): err = %v", len(short), err)
		}
	}
	other := testKeyring(t, 1)
	other.aeads[1], _ = newAEAD(bytes.Repeat([]byte{0x77}, KeySize))
	if _, err := other.Decrypt(ct, "student_profiles.nik"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong key: err = %v, want ErrDecrypt", err)
	}
}

func TestBlindIndex(t *testing.T) {
	k := testKeyring(t, 1)
	if !bytes.Equal(k.BlindIndex("nik", " 3201 0101 0101 0001 "), k.BlindIndex("nik", sampleNIK)) {
		t.Error("blind index must ignore whitespace")
	}
	if bytes.Equal(k.BlindIndex("nik", sampleNIK), k.BlindIndex("kk", sampleNIK)) {
		t.Error("the same number as NIK and KK must give different indexes")
	}
	if bytes.Equal(k.BlindIndex("nik", sampleNIK), k.BlindIndex("nik", "3201010101010002")) {
		t.Error("different values must give different indexes")
	}
	// Rotasi kunci enkripsi tidak boleh mengubah indeks.
	k2 := testKeyring(t, 1, 2)
	if !bytes.Equal(k.BlindIndex("nik", sampleNIK), k2.BlindIndex("nik", sampleNIK)) {
		t.Error("blind index must not depend on encryption keys")
	}
}

func TestKeyVersions(t *testing.T) {
	v1 := testKeyring(t, 1)
	old, _ := v1.Encrypt(sampleNIK, "a")

	both := testKeyring(t, 1, 2)
	if both.ActiveVersion() != 2 {
		t.Fatalf("active = %d, want 2", both.ActiveVersion())
	}
	if got, err := both.Decrypt(old, "a"); err != nil || got != sampleNIK {
		t.Errorf("old ciphertext after adding v2: %q, %v", got, err)
	}
	if !both.NeedsRotation(old) {
		t.Error("v1 ciphertext must need rotation when v2 is active")
	}
	fresh, _ := both.Encrypt(sampleNIK, "a")
	if fresh[0] != 2 || both.NeedsRotation(fresh) {
		t.Errorf("new ciphertext version = %d", fresh[0])
	}
	if _, err := v1.Decrypt(fresh, "a"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("unknown key version: err = %v", err)
	}
}

func TestNewKeyringValidation(t *testing.T) {
	good := bytes.Repeat([]byte{1}, KeySize)
	idx := bytes.Repeat([]byte{2}, KeySize)
	cases := []struct {
		name string
		enc  map[byte][]byte
		idx  []byte
	}{
		{"no keys", nil, idx},
		{"short key", map[byte][]byte{1: good[:16]}, idx},
		{"version zero", map[byte][]byte{0: good}, idx},
		{"short index", map[byte][]byte{1: good}, idx[:31]},
		{"index equals key", map[byte][]byte{1: good}, good},
	}
	for _, tc := range cases {
		if _, err := NewKeyring(tc.enc, tc.idx); err == nil {
			t.Errorf("%s: error = nil", tc.name)
		}
	}
}

func TestValueNeverPrintsPlaintext(t *testing.T) {
	v := New(sampleNIK)
	if v.Reveal() != sampleNIK {
		t.Fatal("Reveal must return the full value")
	}
	if v.Masked() != "3201********0001" {
		t.Errorf("Masked = %q", v.Masked())
	}
	var logBuf bytes.Buffer
	slog.New(slog.NewTextHandler(&logBuf, nil)).Info("saved", "nik", v)
	js, _ := json.Marshal(struct{ NIK Value }{v})
	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", v),
		"%s":   fmt.Sprintf("%s", v),
		"%+v":  fmt.Sprintf("%+v", struct{ N Value }{v}),
		"%#v":  fmt.Sprintf("%#v", v),
		"slog": logBuf.String(),
		"json": string(js),
	}
	for name, out := range outputs {
		if strings.Contains(out, sampleNIK) {
			t.Errorf("%s leaks plaintext: %s", name, out)
		}
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"1":                "*",
		"12":               "**",
		"12345":            "***45",
		"12345678":         "******78",
		"123456789":        "*******89",
		"1234567890":       "1234**7890",
		sampleNIK:          "3201********0001",
		"3201-LT-0001-ABC": "3201********-ABC",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseKeyring(t *testing.T) {
	k, err := ParseKeyring(" 2:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI= ,1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	if err != nil {
		t.Fatal(err)
	}
	if k.ActiveVersion() != 2 || fmt.Sprint(k.Versions()) != "[1 2]" {
		t.Errorf("active=%d versions=%v", k.ActiveVersion(), k.Versions())
	}
}
