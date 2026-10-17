package database

// IsUUID melaporkan apakah s berformat UUID (8-4-4-4-12 heksadesimal).
// Dipakai untuk menolak ID dari URL sebelum dikirim ke database, karena
// UUID yang tidak valid menimbulkan error yang membatalkan transaksi.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}
