package auth

import (
	"context"
	"slices"
	"time"

	"akra/internal/platform/clock"
)

// Identity adalah identitas pengguna yang sedang login, diambil dari sesi.
// school_id untuk data sekolah selalu diambil dari sini, tidak pernah dari
// URL, query string, atau form.
type Identity struct {
	UserID   string
	SchoolID string
	Roles    []string

	// Hanya untuk tampilan; jangan dipakai untuk otorisasi.
	FullName   string
	SchoolName string

	// Timezone adalah zona waktu sekolah (T126); kosong berarti
	// Asia/Jakarta. Pakai Location untuk konversi.
	Timezone string

	// Duties (tugas tambahan tahun ajaran aktif) diambil saat login dan
	// hanya untuk menu. Jangan dipakai untuk otorisasi; modul ujian membaca
	// ulang dari database (exam.LoadActor). Modul aktif sekolah tidak di
	// sesi: dibaca per request dari module.From (T163).
	Duties []string
}

// Location mengembalikan zona waktu sekolah pengguna. Semua tampilan dan
// input jam/tanggal di halaman staf memakai lokasi ini, bukan clock.Jakarta.
func (id Identity) Location() *time.Location {
	return clock.Location(id.Timezone)
}

// Today mengembalikan tanggal hari ini di zona waktu sekolah pengguna.
func (id Identity) Today() time.Time {
	return clock.TodayIn(id.Location())
}

type identityKey struct{}

func withIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// ContextWithIdentity menyimpan id ke ctx seperti yang dilakukan
// RequireLogin. Hanya untuk uji di paket lain; kode aplikasi selalu
// mendapat Identity dari RequireLogin.
func ContextWithIdentity(ctx context.Context, id Identity) context.Context {
	return withIdentity(ctx, id)
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
