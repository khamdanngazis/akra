package database

import "testing"

func TestIsUUID(t *testing.T) {
	cases := map[string]bool{
		"6ec5b21d-47d8-4352-9668-57a31ecc15ed":  true,
		"6EC5B21D-47D8-4352-9668-57A31ECC15ED":  true,
		"":                                      false,
		"bukan-uuid":                            false,
		"6ec5b21d47d8435296685 7a31ecc15ed1234": false,
		"6ec5b21d-47d8-4352-9668-57a31ecc15eg":  false,
		"6ec5b21d-47d8-4352-9668-57a31ecc15ed0": false,
		"'; DROP TABLE schools; --------------": false,
	}
	for in, want := range cases {
		if got := IsUUID(in); got != want {
			t.Errorf("IsUUID(%q) = %v, want %v", in, got, want)
		}
	}
}
