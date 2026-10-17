package auth

import (
	"context"
	"slices"
)

// Identity adalah identitas pengguna yang sedang login, diambil dari sesi.
// school_id untuk data sekolah selalu diambil dari sini, tidak pernah dari
// URL, query string, atau form.
type Identity struct {
	UserID   string
	SchoolID string
	Roles    []string
}

type identityKey struct{}

func withIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// CurrentUser mengembalikan Identity yang disimpan RequireLogin di ctx.
// ok bernilai false jika request tidak melewati RequireLogin.
func CurrentUser(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// HasRole melaporkan apakah identitas memiliki peran dengan kode code.
func (id Identity) HasRole(code string) bool {
	return slices.Contains(id.Roles, code)
}
