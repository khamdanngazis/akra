package dapodik

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"akra/internal/students"
)

// Tingkat masalah.
const (
	LevelError   = "error"      // hampir pasti ditolak Dapodik
	LevelWarning = "peringatan" // perlu diperiksa
)

// Issue adalah satu masalah data yang biasanya muncul di validasi Dapodik.
type Issue struct {
	Field   string
	Level   string
	Message string
}

var (
	digits = regexp.MustCompile(`^[0-9]+$`)
	// titleRe menangkap gelar akademik/sosial yang tidak boleh ada di nama
	// (petunjuk F-PD): Alm., Almh., H., Hj., Dr., Drs., Dra., Ir., S.Pd, dst.
	titleRe = regexp.MustCompile(`(?i)(^|[\s,])(alm|almh|almarhum|almarhumah|hj|h|dr|drs|dra|ir|prof)\.?(\s|$)|(?i)\b[sdm]\.\s?[a-z]{1,4}\.?(\s|,|$)`)
	// nameCharRe: huruf (termasuk huruf beraksen), spasi, apostrof, titik,
	// dan tanda hubung.
	nameCharRe = regexp.MustCompile(`^[\p{L}\s'.\-]+$`)
)

func nameIssues(field, label, name string) []Issue {
	if name == "" {
		return nil
	}
	var out []Issue
	if titleRe.MatchString(name) {
		out = append(out, Issue{field, LevelWarning, label + " tampaknya memuat gelar. Tulis nama sesuai dokumen tanpa gelar (Alm., Hj., Dr., S.Pd, dll.)."})
	}
	if !nameCharRe.MatchString(name) {
		out = append(out, Issue{field, LevelWarning, label + " memuat angka atau simbol. Periksa ejaan sesuai akta."})
	}
	return out
}

// Check memeriksa data satu siswa dengan aturan yang sering menjadi error
// atau peringatan di validasi Dapodik. gradeLevel adalah tingkat kelas
// aktif (0 jika tidak diketahui).
func Check(in Input, gradeLevel int, today time.Time) []Issue {
	var out []Issue
	p := func(k string) string { return in.Profile.Secret(k).Reveal() }
	nik, kk := p("nik"), p("kk_number")
	wni := p("citizenship") != "WNA"
	switch {
	case nik == "":
		out = append(out, Issue{"nik", LevelError, "NIK belum diisi."})
	case wni && (len(nik) != 16 || !digits.MatchString(nik)):
		out = append(out, Issue{"nik", LevelError, "NIK harus 16 digit angka."})
	case wni && strings.HasSuffix(nik, "0000"):
		out = append(out, Issue{"nik", LevelWarning, "NIK berakhiran 0000; periksa kembali, mungkin tertukar dengan nomor lain."})
	}
	if nik != "" && nik == kk {
		out = append(out, Issue{"kk_number", LevelError, "No KK sama dengan NIK; keduanya kemungkinan tertukar."})
	}
	if kk == "" {
		out = append(out, Issue{"kk_number", LevelWarning, "No KK belum diisi."})
	}
	if v := p("nisn"); v != "" && (len(v) != 10 || !digits.MatchString(v)) {
		out = append(out, Issue{"nisn", LevelError, "NISN harus 10 digit angka."})
	}
	out = append(out, nameIssues("full_name", "Nama siswa", in.Student.FullName)...)

	mother, ok := in.Parents[students.RelationMother]
	if !ok || mother.Name == "" {
		out = append(out, Issue{"mother_name", LevelError, "Nama ibu kandung belum diisi."})
	} else {
		out = append(out, nameIssues("mother_name", "Nama ibu kandung", mother.Name)...)
	}
	if f, ok := in.Parents[students.RelationFather]; ok {
		out = append(out, nameIssues("father_name", "Nama ayah", f.Name)...)
	}

	if in.Student.BirthDate != nil {
		age := today.Year() - in.Student.BirthDate.Year()
		if today.YearDay() < in.Student.BirthDate.YearDay() {
			age--
		}
		if gradeLevel > 0 {
			// Usia wajar kira-kira tingkat + 5 sampai tingkat + 7 tahun.
			if age < gradeLevel+4 || age > gradeLevel+9 {
				out = append(out, Issue{"birth_date", LevelWarning, "Usia " + strconv.Itoa(age) + " tahun tidak wajar untuk tingkat " + strconv.Itoa(gradeLevel) + "; periksa tanggal lahir."})
			}
		}
		for role, parent := range in.Parents {
			if y := parent.Details.BirthYear; y > 0 && in.Student.BirthDate.Year()-y < 12 {
				label := map[string]string{students.RelationFather: "ayah", students.RelationMother: "ibu", students.RelationGuardian: "wali"}[role]
				out = append(out, Issue{role + ".birth_year", LevelWarning, "Tahun lahir " + label + " terlalu dekat dengan tahun lahir siswa."})
			}
		}
	} else {
		out = append(out, Issue{"birth_date", LevelError, "Tanggal lahir belum diisi."})
	}
	if v := p("rt"); v != "" && !digits.MatchString(v) {
		out = append(out, Issue{"rt", LevelError, "RT harus angka."})
	}
	if v := p("postal_code"); v != "" && (len(v) != 5 || !digits.MatchString(v)) {
		out = append(out, Issue{"postal_code", LevelError, "Kode pos harus 5 digit."})
	}
	if v := p("latitude"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err != nil || x < -11 || x > 6 {
			out = append(out, Issue{"latitude", LevelError, "Lintang di luar wilayah Indonesia."})
		}
	}
	if v := p("longitude"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err != nil || x < 95 || x > 141 {
			out = append(out, Issue{"longitude", LevelError, "Bujur di luar wilayah Indonesia."})
		}
	}
	return out
}
