package auth

// Kode peran. Nilainya harus sama dengan kolom roles.code di database
// (migrasi 000002).
const (
	RoleAdminTU       = "admin_tu"
	RoleBendahara     = "bendahara"
	RoleGuru          = "guru"
	RoleKepalaSekolah = "kepala_sekolah"
	RoleOrangTua      = "orang_tua"
)

// AllRoles berisi semua kode peran yang dikenal aplikasi.
var AllRoles = []string{RoleAdminTU, RoleBendahara, RoleGuru, RoleKepalaSekolah, RoleOrangTua}

// HasAnyRole melaporkan apakah id memiliki minimal satu dari roles.
func HasAnyRole(id Identity, roles ...string) bool {
	for _, role := range roles {
		if id.HasRole(role) {
			return true
		}
	}
	return false
}
