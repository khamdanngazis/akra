package auth

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"
)

// fakeRepo is an in-memory Repository. It ignores the window: every recorded
// attempt counts as recent.
type fakeRepo struct {
	users    map[string]UserCredentials // key: schoolCode + "|" + email
	attempts []LoginAttempt
}

func (f *fakeRepo) FindUserForLogin(_ context.Context, schoolCode, email string) (UserCredentials, error) {
	u, ok := f.users[schoolCode+"|"+email]
	if !ok {
		return UserCredentials{}, ErrNotFound
	}
	return u, nil
}

func (f *fakeRepo) CountRecentFailures(_ context.Context, schoolCode, email string, ip netip.Addr, _ time.Duration) (int, int, error) {
	var byEmail, byIP int
	for _, a := range f.attempts {
		if a.SchoolCode == schoolCode && a.Email == email {
			if a.Success {
				byEmail = 0
			} else {
				byEmail++
			}
		}
		if ip.IsValid() && a.IP == ip && !a.Success {
			byIP++
		}
	}
	return byEmail, byIP, nil
}

func (f *fakeRepo) RecordLoginAttempt(_ context.Context, a LoginAttempt) error {
	f.attempts = append(f.attempts, a)
	return nil
}

var testIP = netip.MustParseAddr("203.0.113.7")

func newTestService(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	h := NewHasher(testParams)
	hash, err := h.HashPassword("benar-123")
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{users: map[string]UserCredentials{
		"sd-harapan|budi@sekolah.id": {
			User:         User{ID: "u1", SchoolID: "s1", Email: "budi@sekolah.id", FullName: "Budi", Roles: []string{"guru"}},
			PasswordHash: hash,
			IsActive:     true,
		},
		"sd-harapan|nonaktif@sekolah.id": {
			User:         User{ID: "u2", SchoolID: "s1", Email: "nonaktif@sekolah.id"},
			PasswordHash: hash,
			IsActive:     false,
		},
	}}
	svc, err := NewService(repo, h)
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo
}

func TestAuthenticate_Success(t *testing.T) {
	svc, repo := newTestService(t)

	u, err := svc.Authenticate(context.Background(), "sd-harapan", "budi@sekolah.id", "benar-123", testIP)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if u.ID != "u1" || u.SchoolID != "s1" || !slices.Equal(u.Roles, []string{"guru"}) {
		t.Errorf("Authenticate() user = %+v", u)
	}
	if n := len(repo.attempts); n != 1 || !repo.attempts[0].Success {
		t.Errorf("attempts = %+v, want one successful attempt", repo.attempts)
	}
}

func TestAuthenticate_NormalizesInput(t *testing.T) {
	svc, repo := newTestService(t)

	_, err := svc.Authenticate(context.Background(), "  SD-Harapan ", "  Budi@Sekolah.ID ", "benar-123", testIP)
	if err != nil {
		t.Fatalf("Authenticate() with mixed-case email error = %v", err)
	}
	if got := repo.attempts[0]; got.Email != "budi@sekolah.id" || got.SchoolCode != "sd-harapan" {
		t.Errorf("recorded attempt = %+v, want normalized school code and email", got)
	}
}

func TestAuthenticate_SameErrorForAllCredentialFailures(t *testing.T) {
	cases := []struct{ name, school, email, password string }{
		{"wrong password", "sd-harapan", "budi@sekolah.id", "salah"},
		{"unknown email", "sd-harapan", "siapa@sekolah.id", "benar-123"},
		{"unknown school", "sd-lain", "budi@sekolah.id", "benar-123"},
		{"inactive user", "sd-harapan", "nonaktif@sekolah.id", "benar-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newTestService(t)

			_, err := svc.Authenticate(context.Background(), tc.school, tc.email, tc.password, testIP)
			if err != ErrInvalidCredentials {
				t.Errorf("error = %v, want exactly ErrInvalidCredentials", err)
			}
			if n := len(repo.attempts); n != 1 || repo.attempts[0].Success {
				t.Errorf("attempts = %+v, want one failed attempt", repo.attempts)
			}
		})
	}
}

func TestAuthenticate_LockoutByEmail(t *testing.T) {
	svc, repo := newTestService(t)
	ctx := context.Background()

	for i := range MaxFailuresPerEmail {
		// Vary the IP so only the email limit applies.
		ip := netip.AddrFrom4([4]byte{198, 51, 100, byte(i)})
		if _, err := svc.Authenticate(ctx, "sd-harapan", "budi@sekolah.id", "salah", ip); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: error = %v, want ErrInvalidCredentials", i+1, err)
		}
	}

	_, err := svc.Authenticate(ctx, "sd-harapan", "budi@sekolah.id", "benar-123", testIP)
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("attempt %d with correct password: error = %v, want ErrTooManyAttempts", MaxFailuresPerEmail+1, err)
	}
	if n := len(repo.attempts); n != MaxFailuresPerEmail {
		t.Errorf("recorded %d attempts, want %d (locked attempts are not recorded)", n, MaxFailuresPerEmail)
	}

	// Another user of the same school is not affected.
	repo.users["sd-harapan|ani@sekolah.id"] = repo.users["sd-harapan|budi@sekolah.id"]
	if _, err := svc.Authenticate(ctx, "sd-harapan", "ani@sekolah.id", "benar-123", testIP); err != nil {
		t.Errorf("other user: error = %v, want nil", err)
	}
}

func TestAuthenticate_LockoutByIP(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	for i := range MaxFailuresPerIP {
		// A different email each time so only the IP limit applies.
		email := string(rune('a'+i%26)) + "@sekolah.id"
		if i >= 26 {
			email = "x" + email
		}
		_, _ = svc.Authenticate(ctx, "sd-harapan", email, "salah", testIP)
	}

	_, err := svc.Authenticate(ctx, "sd-harapan", "budi@sekolah.id", "benar-123", testIP)
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("error = %v, want ErrTooManyAttempts", err)
	}
}

func TestAuthenticate_SuccessResetsEmailCounter(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	for range MaxFailuresPerEmail - 1 {
		_, _ = svc.Authenticate(ctx, "sd-harapan", "budi@sekolah.id", "salah", testIP)
	}
	if _, err := svc.Authenticate(ctx, "sd-harapan", "budi@sekolah.id", "benar-123", testIP); err != nil {
		t.Fatalf("login before limit: error = %v", err)
	}
	if _, err := svc.Authenticate(ctx, "sd-harapan", "budi@sekolah.id", "salah", testIP); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("first failure after success: error = %v, want ErrInvalidCredentials", err)
	}
}
