package exam

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestAttemptToken(t *testing.T) {
	key := func(b byte) string {
		return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(rune(b)), 32)))
	}
	s, err := ParseTokenSigner(key('a'))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	c := AttemptClaims{SchoolID: "11111111-1111-1111-1111-111111111111", AttemptID: "22222222-2222-2222-2222-22222222222a",
		DeviceID: "33333333-3333-3333-3333-3333333333bc", Expires: now.Add(time.Hour)}
	tok, err := s.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Verify(tok, now)
	if err != nil || got.SchoolID != c.SchoolID || got.AttemptID != c.AttemptID || got.DeviceID != c.DeviceID || !got.Expires.Equal(c.Expires) {
		t.Fatalf("Verify = %+v, %v", got, err)
	}
	if _, err := s.Verify(tok, now.Add(time.Hour)); err != ErrBadAttemptToken {
		t.Errorf("expired token: %v", err)
	}

	// Setiap karakter yang diubah membuat token ditolak.
	for i := range tok {
		if tok[i] == '.' {
			continue
		}
		b := []byte(tok)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		if _, err := s.Verify(string(b), now); err == nil {
			t.Errorf("tampered token at %d accepted", i)
		}
	}
	for _, bad := range []string{"", ".", "abc", tok + "x", strings.Replace(tok, ".", "", 1)} {
		if _, err := s.Verify(bad, now); err != ErrBadAttemptToken {
			t.Errorf("Verify(%q) = %v", bad, err)
		}
	}

	// Kunci lain menolak; rotasi: kunci lama tetap diterima, kunci baru
	// dipakai untuk menandatangani.
	other, _ := ParseTokenSigner(key('b'))
	if _, err := other.Verify(tok, now); err == nil {
		t.Error("token accepted with another key")
	}
	rotated, err := ParseTokenSigner(key('b') + ", " + key('a'))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.Verify(tok, now); err != nil {
		t.Errorf("old key after rotation: %v", err)
	}
	newTok, _ := rotated.Sign(c)
	if _, err := other.Verify(newTok, now); err != nil {
		t.Errorf("rotated signer must sign with first key: %v", err)
	}
	if _, err := s.Verify(newTok, now); err == nil {
		t.Error("new token accepted by old-only signer")
	}
	if _, err := DevTokenSigner().Verify(tok, now); err == nil {
		t.Error("dev key accepted production token")
	}

	if _, err := s.Sign(AttemptClaims{SchoolID: "x", AttemptID: c.AttemptID, DeviceID: c.DeviceID, Expires: c.Expires}); err == nil {
		t.Error("Sign accepted invalid uuid")
	}
}

func TestParseTokenSigner(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	for _, s := range []string{"", " , ", "not base64!", short} {
		if _, err := ParseTokenSigner(s); err == nil {
			t.Errorf("ParseTokenSigner(%q) = nil error", s)
		} else if strings.Contains(err.Error(), short) {
			t.Errorf("error leaks key material: %v", err)
		}
	}
	long := make([]byte, 32)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if _, err := ParseTokenSigner(enc.EncodeToString(long)); err != nil {
			t.Errorf("valid key rejected: %v", err)
		}
	}
}
