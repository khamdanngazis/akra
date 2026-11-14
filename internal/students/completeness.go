package students

import (
	"context"
	"fmt"
	"strings"

	"akra/internal/platform/database"
)

// RequiredField adalah satu isian yang wajib ada agar data siswa siap
// dimasukkan ke Dapodik (T88).
type RequiredField struct {
	Key     string
	Label   string
	Section string // bagian form data rinci; SectionStudent = form data siswa
	// filled adalah ekspresi SQL boolean dengan alias s (students) dan p
	// (student_profiles, LEFT JOIN).
	filled string
}

// SectionStudent menandai field yang diisi di form data siswa (/siswa/{id}/ubah),
// bukan di form data rinci.
const SectionStudent = "siswa"

// SectionMother adalah bagian ibu kandung di form data rinci.
const SectionMother = "ibu"

// DapodikRequired adalah field wajib Dapodik untuk peserta didik, berurutan
// seperti F-PD. Kelengkapan di halaman detail dan filter "Belum lengkap" di
// daftar siswa memakai definisi yang sama ini.
var DapodikRequired = []RequiredField{
	{"full_name", "Nama lengkap", SectionStudent, "s.full_name <> ''"},
	{"gender", "Jenis kelamin", SectionStudent, "s.gender IS NOT NULL"},
	{"nik", "NIK", SectionIdentity, "p.nik_enc IS NOT NULL"},
	{"birth_place", "Tempat lahir", SectionIdentity, "p.birth_place IS NOT NULL"},
	{"birth_date", "Tanggal lahir", SectionStudent, "s.birth_date IS NOT NULL"},
	{"religion", "Agama", SectionIdentity, "p.religion IS NOT NULL"},
	{"street", "Alamat jalan", SectionAddress, "p.street IS NOT NULL"},
	{"village", "Kelurahan/desa", SectionAddress, "p.village IS NOT NULL"},
	{"district", "Kecamatan", SectionAddress, "p.district IS NOT NULL"},
	{"residence_type", "Tempat tinggal", SectionAddress, "p.residence_type IS NOT NULL"},
	{"transport_mode", "Moda transportasi", SectionAddress, "p.transport_mode IS NOT NULL"},
	{"mother_name", "Nama ibu kandung", SectionMother, `EXISTS (SELECT 1 FROM student_guardians sg
		WHERE sg.school_id = s.school_id AND sg.student_id = s.id AND sg.relationship = 'ibu' AND sg.is_biological)`},
	{"registration_type", "Jenis pendaftaran", SectionRegistration, "p.registration_type IS NOT NULL"},
	{"entry_date", "Tanggal masuk sekolah", SectionRegistration, "p.entry_date IS NOT NULL"},
}

// readinessFilledSQL mengembalikan ekspresi SQL jumlah field wajib yang
// sudah terisi.
func readinessFilledSQL() string {
	parts := make([]string, len(DapodikRequired))
	for i, f := range DapodikRequired {
		parts[i] = "(CASE WHEN " + f.filled + " THEN 1 ELSE 0 END)"
	}
	return "(" + strings.Join(parts, " + ") + ")"
}

// readinessPercentSQL mengembalikan ekspresi persentase kelengkapan
// (dibulatkan ke bawah, jadi 100 hanya jika semua terisi).
func readinessPercentSQL() string {
	return fmt.Sprintf("(%s * 100 / %d)", readinessFilledSQL(), len(DapodikRequired))
}

// Readiness adalah kelengkapan data Dapodik satu siswa.
type Readiness struct {
	Percent int
	Missing []RequiredField
}

// Complete melaporkan apakah semua field wajib terisi.
func (r Readiness) Complete() bool { return len(r.Missing) == 0 }

// GetReadiness menghitung kelengkapan data Dapodik siswa milik schoolID.
func GetReadiness(ctx context.Context, q database.DBTX, schoolID, studentID string) (Readiness, error) {
	if !database.IsUUID(studentID) {
		return Readiness{}, ErrNotFound
	}
	exprs := make([]string, len(DapodikRequired))
	for i, f := range DapodikRequired {
		exprs[i] = "(" + f.filled + ")"
	}
	filled := make([]bool, len(DapodikRequired))
	dest := make([]any, len(filled))
	for i := range filled {
		dest[i] = &filled[i]
	}
	err := q.QueryRow(ctx, `SELECT `+strings.Join(exprs, ", ")+`
		FROM students s LEFT JOIN student_profiles p ON p.school_id = s.school_id AND p.student_id = s.id
		WHERE s.school_id = $1 AND s.id = $2`, schoolID, studentID).Scan(dest...)
	if err != nil {
		return Readiness{}, mapError("get readiness", err)
	}
	var r Readiness
	n := 0
	for i, ok := range filled {
		if ok {
			n++
		} else {
			r.Missing = append(r.Missing, DapodikRequired[i])
		}
	}
	r.Percent = n * 100 / len(DapodikRequired)
	return r, nil
}
