package exam

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func signerWith(t *testing.T, keys ...string) *TokenSigner {
	t.Helper()
	var enc []string
	for _, k := range keys {
		enc = append(enc, base64.StdEncoding.EncodeToString([]byte(k)))
	}
	s, err := ParseTokenSigner(strings.Join(enc, ","))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCardCodeDerivation(t *testing.T) {
	s := signerWith(t, "kunci-pertama-yang-cukup-panjang-32b", "kunci-kedua-yang-juga-cukup-panjang-32")
	nonce := "4b7c9a52-7d0b-4c51-9a8e-3f2d6c1b0e77"
	a, b := s.cardCode(0, nonce), s.cardCode(0, nonce)
	if a != b || len(a) != CodeLength {
		t.Fatalf("code not stable: %q %q", a, b)
	}
	for _, r := range a {
		if !strings.ContainsRune(CodeAlphabet, r) {
			t.Fatalf("code %q has char outside alphabet", a)
		}
	}
	if s.cardCode(1, nonce) == a || s.cardCode(0, "lain") == a {
		t.Errorf("code must depend on key and nonce")
	}
}

func TestApplicantCard(t *testing.T) {
	e := newGradeEnv(t)
	ctx := context.Background()
	if _, err := e.tx.Exec(ctx, `UPDATE exams SET purpose = 'seleksi' WHERE id = (SELECT exam_id FROM exam_sessions WHERE id = $1)`, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, applicant_id, participant_no, variant_id)
		SELECT school_id, session_id, $2, 'P-01', variant_id FROM exam_session_participants WHERE session_id = $1 LIMIT 1`, e.sess.ID, e.a.ApplicantID); err != nil {
		t.Fatal(err)
	}
	old := signerWith(t, "kunci-lama-yang-cukup-panjang-32byte")
	is := NewIssuer().WithCardCodes(old)
	card, err := LoadApplicantCard(ctx, e.tx, is, e.a.ID, e.a.ApplicantID, "https://akra.test", e.real)
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Tests) != 1 || card.Code == "" || !strings.Contains(card.LoginURL, "/u/") || card.Identifier == "" {
		t.Fatalf("card = %+v", card)
	}
	// Dibuka ulang: kode sama, tidak ada kredensial baru.
	var creds int
	count := func() int {
		e.tx.QueryRow(ctx, `SELECT count(*) FROM student_credentials WHERE applicant_id = $1`, e.a.ApplicantID).Scan(&creds)
		return creds
	}
	before := count()
	again, err := LoadApplicantCard(ctx, e.tx, is, e.a.ID, e.a.ApplicantID, "https://akra.test", e.real)
	if err != nil || again.Code != card.Code || count() != before {
		t.Fatalf("reopen: code %q vs %q, creds %d → %d, %v", again.Code, card.Code, before, creds, err)
	}
	// Kode di kartu bisa dipakai login dengan nomor pendaftaran.
	p, err := Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: card.Identifier, Code: strings.ToLower(card.Code), DeviceID: devProctor3, Now: e.real})
	if err != nil || p.ApplicantID != e.a.ApplicantID {
		t.Fatalf("login with card code: %+v, %v", p, err)
	}
	// Rotasi kunci: kunci lama masih dicoba, kode tidak berubah.
	rotated := NewIssuer().WithCardCodes(signerWith(t, "kunci-baru-yang-juga-cukup-panjang-32", "kunci-lama-yang-cukup-panjang-32byte"))
	if c, err := LoadApplicantCard(ctx, e.tx, rotated, e.a.ID, e.a.ApplicantID, "https://akra.test", e.real); err != nil || c.Code != card.Code {
		t.Errorf("after rotation code = %q, want %q (%v)", c.Code, card.Code, err)
	}
	// Kunci lama dibuang: kartu diterbitkan ulang dengan kode baru.
	fresh := NewIssuer().WithCardCodes(signerWith(t, "kunci-baru-yang-juga-cukup-panjang-32"))
	c2, err := LoadApplicantCard(ctx, e.tx, fresh, e.a.ID, e.a.ApplicantID, "https://akra.test", e.real)
	if err != nil || c2.Code == "" || c2.Code == card.Code {
		t.Fatalf("reissue after key drop: %q, %v", c2.Code, err)
	}
	// Kartu cetak TU (IssueCards) memakai kode turunan yang sama dengan
	// yang tampil di halaman status setelahnya.
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	cards, err := IssueCards(ctx, e.tx, sa, fresh, CardScope{ExamID: e.sess.ExamID, SessionID: e.sess.ID}, "Tes", "https://akra.test", true, e.real)
	if err != nil {
		t.Fatal(err)
	}
	printed := ""
	for _, c := range cards {
		if c.Identifier == card.Identifier {
			printed = c.Code
		}
	}
	if c3, err := LoadApplicantCard(ctx, e.tx, fresh, e.a.ID, e.a.ApplicantID, "https://akra.test", e.real); err != nil || printed == "" || c3.Code != printed {
		t.Errorf("printed card %q vs status card %q (%v)", printed, c3.Code, err)
	}
	// Tanpa kunci kode kartu: error, bukan kode acak yang tak bisa
	// ditampilkan ulang.
	if _, err := LoadApplicantCard(ctx, e.tx, NewIssuer(), e.a.ID, e.a.ApplicantID, "", e.real); err != ErrNoCardCodes {
		t.Errorf("issuer without codes = %v", err)
	}
}
