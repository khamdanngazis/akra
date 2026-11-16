package exam

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateCode(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		c, err := GenerateCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != CodeLength || strings.Trim(c, CodeAlphabet) != "" {
			t.Fatalf("code %q has wrong length or characters", c)
		}
		if strings.ContainsAny(c, "0O1IL") {
			t.Fatalf("code %q contains look-alike characters", c)
		}
		seen[c] = true
	}
	if len(seen) < 199 {
		t.Errorf("only %d distinct codes out of 200", len(seen))
	}
	if got := NormalizeCode(" abcd-efgh "); got != "ABCDEFGH" {
		t.Errorf("NormalizeCode = %q", got)
	}
}

type loginEnv struct {
	*env
	nis, regNumber string
}

func newLoginEnv(t *testing.T) *loginEnv {
	e := &loginEnv{env: newEnv(t)}
	ctx := context.Background()
	if err := e.tx.QueryRow(ctx, `SELECT nis FROM students WHERE id = $1`, e.a.StudentIDs[0]).Scan(&e.nis); err != nil {
		t.Fatal(err)
	}
	if err := e.tx.QueryRow(ctx, `SELECT registration_number FROM applicants WHERE id = $1`, e.a.ApplicantID).Scan(&e.regNumber); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *loginEnv) issue(owner Owner, start, end time.Time) (Credential, string) {
	e.t.Helper()
	c, code, err := NewIssuer().Issue(context.Background(), e.tx, e.a.ID, e.a.Admin.ID, IssueRequest{Owner: owner, Start: start, End: end})
	if err != nil {
		e.t.Fatal(err)
	}
	return c, code
}

func (e *loginEnv) login(id, code, device string, now time.Time) error {
	_, err := Login(context.Background(), e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: id, Code: code, DeviceID: device, Now: now})
	return err
}

const (
	dev1 = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	dev2 = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func TestIssueStoresOnlyHash(t *testing.T) {
	e := newLoginEnv(t)
	start := time.Date(2027, 2, 10, 1, 0, 0, 0, time.UTC)
	c, code := e.issue(Owner{StudentID: e.a.StudentIDs[0]}, start, start.Add(90*time.Minute))
	var hash string
	var from time.Time
	if err := e.tx.QueryRow(context.Background(), `SELECT secret_hash, valid_from FROM student_credentials WHERE id = $1`, c.ID).Scan(&hash, &from); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, code) {
		t.Errorf("secret_hash = %q", hash)
	}
	if !from.Equal(start.Add(-30 * time.Minute)) {
		t.Errorf("valid_from = %v, want 30 min before session", from)
	}
	if _, _, err := NewIssuer().Issue(context.Background(), e.tx, e.a.ID, e.a.Admin.ID, IssueRequest{Owner: Owner{}, Start: start, End: start.Add(time.Hour)}); err == nil {
		t.Error("issued code without owner")
	}
}

func TestLoginWindowAndDevice(t *testing.T) {
	e := newLoginEnv(t)
	ctx := context.Background()
	start := time.Date(2027, 2, 10, 1, 0, 0, 0, time.UTC) // 08.00 WIB
	end := start.Add(90 * time.Minute)
	c, code := e.issue(Owner{StudentID: e.a.StudentIDs[0]}, start, end)

	// Lebih dari 30 menit sebelum sesi: belum berlaku, pesan menyebut jam mulai.
	err := e.login(e.nis, code, dev1, start.Add(-31*time.Minute))
	var nyv *NotYetValidError
	if !errors.As(err, &nyv) || !nyv.SessionStart.Equal(start) {
		t.Fatalf("early login: %v", err)
	}
	if msg, _ := loginMessage(err, time.FixedZone("WIB", 7*3600), start.Add(-31*time.Minute)); !strings.Contains(msg, "sesi mulai pukul 08.00") {
		t.Errorf("message = %q", msg)
	}

	// 30 menit sebelum sesi: berlaku; huruf kecil dan spasi diterima.
	p, err := Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: " " + e.nis + " ", Code: strings.ToLower(code[:4]) + " " + code[4:],
		DeviceID: dev1, Now: start.Add(-30 * time.Minute)})
	if err != nil {
		t.Fatalf("login in window: %v", err)
	}
	if p.Kind != "siswa" || p.StudentID != e.a.StudentIDs[0] || p.ApplicantID != "" || p.ReadOnly || p.CredentialID != c.ID {
		t.Errorf("participant = %+v", p)
	}
	if !p.ExpiresAt.Equal(end.Add(30 * time.Minute)) {
		t.Errorf("ExpiresAt = %v, want end + 30 min", p.ExpiresAt)
	}
	if !CanAttempt(p) {
		t.Error("CanAttempt false during session")
	}

	// Perangkat lain ditolak; perangkat yang sama boleh masuk ulang.
	if err := e.login(e.nis, code, dev2, start.Add(10*time.Minute)); !errors.Is(err, ErrOtherDevice) {
		t.Errorf("other device: %v", err)
	}
	if err := e.login(e.nis, code, dev1, start.Add(10*time.Minute)); err != nil {
		t.Errorf("same device again: %v", err)
	}
	// Pengawas mereset perangkat: perangkat baru diterima.
	if err := ResetDevice(ctx, e.tx, e.a.ID, e.a.Admin.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.login(e.nis, code, dev2, start.Add(20*time.Minute)); err != nil {
		t.Errorf("after device reset: %v", err)
	}
	// Sekolah lain tidak bisa mereset kode ini.
	if err := ResetDevice(ctx, e.tx, e.b.ID, e.b.Admin.ID, c.ID); !errors.Is(err, ErrBadLogin) {
		t.Errorf("other school reset: %v", err)
	}

	// Setelah sesi tanpa masa lihat hasil: kedaluwarsa.
	var exp *ExpiredError
	if err := e.login(e.nis, code, dev2, end.Add(time.Minute)); !errors.As(err, &exp) {
		t.Errorf("after session: %v", err)
	}
	// Hasil dibuka (T151): kode membuka hasil, hanya baca, dari perangkat mana pun.
	if _, err := e.tx.Exec(ctx, `UPDATE student_credentials SET results_until = $2 WHERE id = $1`, c.ID, end.Add(30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	p, err = Login(ctx, e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: e.nis, Code: code, DeviceID: dev1, Now: end.Add(48 * time.Hour)})
	if err != nil || !p.ReadOnly || CanAttempt(p) {
		t.Errorf("read-only login: %+v, %v", p, err)
	}
	if err := e.login(e.nis, code, dev1, end.Add(31*24*time.Hour)); !errors.As(err, &exp) {
		t.Errorf("after results window: %v", err)
	}
}

func TestLoginLockout(t *testing.T) {
	e := newLoginEnv(t)
	ctx := context.Background()
	start := time.Date(2027, 2, 10, 1, 0, 0, 0, time.UTC)
	c, code := e.issue(Owner{StudentID: e.a.StudentIDs[0]}, start, start.Add(time.Hour))
	at := start.Add(5 * time.Minute)
	for i := 1; i < MaxFailures; i++ {
		if err := e.login(e.nis, "ZZZZZZZZ", dev1, at); !errors.Is(err, ErrBadLogin) {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	if err := e.login(e.nis, "ZZZZZZZZ", dev1, at); !errors.Is(err, ErrLocked) {
		t.Fatalf("5th failure: %v, want ErrLocked", err)
	}
	// Kode benar pun ditolak sampai pengawas membuka kunci.
	if err := e.login(e.nis, code, dev1, at); !errors.Is(err, ErrLocked) {
		t.Errorf("correct code while locked: %v", err)
	}
	if err := Unlock(ctx, e.tx, e.a.ID, e.a.Admin.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.login(e.nis, code, dev1, at); err != nil {
		t.Errorf("after unlock: %v", err)
	}
	var n int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'participant.unlock'`, e.a.ID).Scan(&n)
	if n != 1 {
		t.Errorf("unlock audit = %d", n)
	}
	if err := Unlock(ctx, e.tx, e.b.ID, e.b.Admin.ID, c.ID); !errors.Is(err, ErrBadLogin) {
		t.Errorf("unlock from other school: %v", err)
	}
}

func TestLoginApplicantAndUnknown(t *testing.T) {
	e := newLoginEnv(t)
	start := time.Date(2027, 2, 10, 1, 0, 0, 0, time.UTC)
	_, code := e.issue(Owner{ApplicantID: e.a.ApplicantID}, start, start.Add(time.Hour))
	p, err := Login(context.Background(), e.tx, LoginRequest{SchoolID: e.a.ID, Identifier: strings.ToLower(e.regNumber), Code: code, DeviceID: dev1, Now: start})
	if err != nil || p.Kind != "pendaftar" || p.ApplicantID != e.a.ApplicantID || p.StudentID != "" {
		t.Fatalf("applicant login: %+v, %v", p, err)
	}
	for _, id := range []string{"", "9999999", strings.Repeat("1", 41)} {
		if err := e.login(id, code, dev1, start); !errors.Is(err, ErrBadLogin) {
			t.Errorf("identifier %q: %v", id, err)
		}
	}
	// Kode pendaftar sekolah A tidak berlaku di sekolah B.
	if _, err := Login(context.Background(), e.tx, LoginRequest{SchoolID: e.b.ID, Identifier: e.regNumber, Code: code, DeviceID: dev1, Now: start}); !errors.Is(err, ErrBadLogin) {
		t.Errorf("other school: %v", err)
	}
	// Tanpa cookie perangkat: ditolak (tidak bisa diikat).
	if err := e.login(e.regNumber, code, "", start); !errors.Is(err, ErrOtherDevice) {
		t.Errorf("no device: %v", err)
	}
}
