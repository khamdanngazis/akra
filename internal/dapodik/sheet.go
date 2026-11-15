package dapodik

import (
	"strconv"
	"strings"

	"akra/internal/dapodik/ref"
	"akra/internal/students"
)

// Parent adalah orang tua/wali siswa beserta data rincinya.
type Parent struct {
	Name    string
	Details students.GuardianDetails
}

// Input adalah data satu siswa untuk lembar salin, F-PD, dan ekspor.
type Input struct {
	Student students.Student
	Profile students.Profile
	// Parents berisi ayah kandung, ibu kandung, dan wali (key: ayah, ibu,
	// wali); yang tidak ada dilewati.
	Parents map[string]Parent
}

// SheetField adalah satu baris lembar salin.
type SheetField struct {
	Label string
	Value string // kosong jika belum diisi
	Kind  FieldKind
	Note  string
	// Unsupported berarti AKRA tidak menyimpan field ini.
	Unsupported bool
}

// SheetSection adalah satu bagian lembar salin.
type SheetSection struct {
	Title  string
	Fields []SheetField
}

// Filled mengembalikan jumlah field yang terisi (bisa disalin) di bagian ini.
func (s SheetSection) Filled() int {
	n := 0
	for _, f := range s.Fields {
		if f.Value != "" && !f.Unsupported {
			n++
		}
	}
	return n
}

// Values mengubah data siswa menjadi nilai tampilan per key Layout: pilihan
// menjadi label Dapodik, tanggal dd/mm/yyyy, ya/tidak menjadi "Ya"/"Tidak".
// Nomor identitas dalam bentuk utuh; pemanggil wajib memastikan pengguna
// berhak melihatnya.
func Values(in Input) map[string]string {
	v := map[string]string{
		"full_name": in.Student.FullName,
		"gender":    map[string]string{"L": "Laki-laki", "P": "Perempuan"}[in.Student.Gender],
		"nis":       in.Student.NIS,
	}
	if in.Student.BirthDate != nil {
		v["birth_date"] = in.Student.BirthDate.Format("02/01/2006")
	}
	for _, f := range students.ProfileFields {
		raw := in.Profile.Secret(f.Key).Reveal()
		if raw == "" {
			continue
		}
		switch {
		case f.Ref != nil:
			v[f.Key] = labels(*f.Ref, raw)
		case f.Key == "has_kip" || f.Key == "pip_eligible":
			v[f.Key] = yesNo(raw == "true")
		case f.Key == "entry_date":
			if len(raw) == 10 {
				v[f.Key] = raw[8:10] + "/" + raw[5:7] + "/" + raw[0:4]
			}
		default:
			v[f.Key] = raw
		}
	}
	// Bool yang tidak diisi tetap ditulis "Tidak" agar operator tahu nilainya.
	for _, k := range []string{"has_kip", "pip_eligible"} {
		if v[k] == "" && in.Profile.Exists {
			v[k] = yesNo(false)
		}
	}
	if km := in.Profile.Get("distance_km"); km != "" {
		if x, err := strconv.ParseFloat(km, 64); err == nil {
			if x < 1 {
				v["distance_band"] = "Kurang dari 1 km"
			} else {
				v["distance_band"] = "Lebih dari 1 km"
			}
		}
	}
	if m, err := strconv.Atoi(in.Profile.Get("travel_minutes")); err == nil {
		v["travel_hours"], v["travel_rest_minutes"] = strconv.Itoa(m/60), strconv.Itoa(m%60)
	}
	_, hasWali := in.Parents[students.RelationGuardian]
	v["has_wali"] = yesNo(hasWali)
	for role, p := range in.Parents {
		d := p.Details
		v[role+".full_name"] = p.Name
		v[role+".nik"] = d.NIK.Reveal()
		if d.BirthYear > 0 {
			v[role+".birth_year"] = strconv.Itoa(d.BirthYear)
		}
		v[role+".education"] = labels(ref.Pendidikan, d.Education)
		v[role+".occupation"] = labels(ref.Pekerjaan, d.Occupation)
		v[role+".income"] = labels(ref.Penghasilan, d.Income)
		v[role+".special_needs"] = labels(ref.KebutuhanKhusus, strings.Join(d.SpecialNeeds, ","))
	}
	return v
}

// Sheet menyusun lembar salin sesuai urutan form Dapodik.
func Sheet(in Input) []SheetSection {
	v := Values(in)
	out := make([]SheetSection, 0, len(Layout))
	for _, sec := range Layout {
		ss := SheetSection{Title: sec.Title}
		for _, f := range sec.Fields {
			sf := SheetField{Label: f.Label, Kind: f.Kind, Note: f.Note, Unsupported: f.Key == ""}
			if f.Key != "" {
				sf.Value = v[f.Key]
			}
			ss.Fields = append(ss.Fields, sf)
		}
		out = append(out, ss)
	}
	return out
}

// labels mengubah kode (atau daftar kode dipisah koma) menjadi label.
func labels(l ref.List, codes string) string {
	if codes == "" {
		return ""
	}
	var out []string
	for _, c := range strings.Split(codes, ",") {
		if lbl, err := l.Label(c); err == nil {
			out = append(out, lbl)
		}
	}
	return strings.Join(out, ", ")
}

func yesNo(b bool) string {
	if b {
		return "Ya"
	}
	return "Tidak"
}
