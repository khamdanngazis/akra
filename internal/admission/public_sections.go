package admission

import (
	"fmt"
	"slices"
	"strconv"

	"akra/internal/dapodik/ref"
	"akra/internal/students"
)

// Isian per langkah formulir pendaftaran (T109): bagian keluarga dan
// bantuan dipisah, wali hanya tampil jika perlu, isian orang tua yang
// sudah meninggal disederhanakan, dan isian sekolah asal sesuai jenis
// pendaftar. Tampil/sembunyi memakai Alpine.js; tanpa JavaScript semua
// isian tetap tampil.

// residenceGuardian adalah kode "Tempat tinggal: Wali" (ref.TempatTinggal).
const residenceGuardian = "02"

// guardianDeceasedHidden adalah isian orang tua yang disembunyikan (dan
// dikosongkan saat disimpan) jika pekerjaannya "Meninggal Dunia".
var guardianDeceasedHidden = []string{"phone", "nik", "income", "special_needs"}

// jsBool mengubah bool menjadi literal JavaScript untuk x-data.
func jsBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// addressSections menyusun langkah alamat: alamat, keluarga, dan bantuan
// pemerintah. Usulan PIP diisi sekolah, tidak tampil di sini.
func addressSections(values, errs map[string]string) []publicSection {
	var aid []students.FieldView
	hasKip := values["has_kip"] == "true"
	hasKks := values["kks_number"] != "" || values["has_kks"] == "true"
	for _, f := range students.ProfileFieldViews(students.SectionAssistance, values, errs) {
		switch f.Key {
		case "pip_eligible", "pip_reason":
			continue
		case "has_kip":
			f.Label, f.Model = "Punya Kartu Indonesia Pintar (KIP)", "hasKip"
		case "kip_number", "kip_name":
			f.ShowIf = "hasKip"
		case "kks_number":
			aid = append(aid, students.FieldView{Key: "has_kks", Label: "Punya Kartu Keluarga Sejahtera (KKS) atau kartu PKH", Input: "checkbox",
				Value: jsBool(hasKks), Model: "hasKks"})
			f.Label, f.ShowIf = "Nomor KKS/PKH", "hasKks"
		}
		aid = append(aid, f)
	}
	return []publicSection{
		{Key: students.SectionAddress, Title: "Alamat tempat tinggal", Fields: students.ProfileFieldViews(students.SectionAddress, values, errs)},
		{Key: students.SectionFamily, Title: "Keluarga", Fields: students.ProfileFieldViews(students.SectionFamily, values, errs)},
		{Key: students.SectionAssistance, Title: "Bantuan pemerintah (opsional)", Hint: "Isi jika keluarga menerima kartu bantuan dari pemerintah. Kosongkan jika tidak.",
			Fields: aid, XData: fmt.Sprintf("{ hasKip: %s, hasKks: %s }", jsBool(hasKip), jsBool(hasKks))},
	}
}

// showGuardian melaporkan apakah bagian wali dibuka sejak awal: calon siswa
// tinggal bersama wali, data wali sudah ada, atau ada error di isian wali.
func showGuardian(a Applicant, values, errs map[string]string) bool {
	if a.Values["residence_type"] == residenceGuardian {
		return true
	}
	for _, k := range guardianFields {
		key := students.RelationGuardian + "." + k
		if values[key] != "" || errs[key] != "" {
			return true
		}
	}
	return false
}

// parentSections menyusun langkah orang tua/wali.
func parentSections(values, errs map[string]string) []publicSection {
	titles := map[string]string{students.RelationFather: "Ayah kandung", students.RelationMother: "Ibu kandung", students.RelationGuardian: "Wali"}
	hints := map[string]string{
		students.RelationMother:   "Nama ibu kandung wajib diisi. Hindari gelar (Almh., Hj., Dra.). Jika sudah meninggal, pilih pekerjaan \"Meninggal Dunia\".",
		students.RelationFather:   "Kosongkan jika tidak ada. Jika sudah meninggal, pilih pekerjaan \"Meninggal Dunia\".",
		students.RelationGuardian: "Isi jika calon siswa tinggal bersama wali, mis. kakek/nenek atau pengasuh panti.",
	}
	var out []publicSection
	for _, role := range students.ParentRoles {
		gv := map[string]string{}
		ge := map[string]string{}
		for _, k := range guardianFields {
			gv[k] = values[role+"."+k]
			ge[k] = errs[role+"."+k]
		}
		fields := []students.FieldView{
			{Key: "full_name", Label: "Nama lengkap", Input: "text", Max: maxNameLen, Value: gv["full_name"], Error: ge["full_name"], Required: role == students.RelationMother},
			{Key: "phone", Label: "Nomor HP", Input: "text", InputMode: "tel", Value: gv["phone"], Error: ge["phone"]},
		}
		fields = append(fields, students.GuardianFieldViews(gv, ge)...)
		for i := range fields {
			switch {
			case fields[i].Key == "occupation":
				fields[i].Model = "occ"
			case slices.Contains(guardianDeceasedHidden, fields[i].Key):
				fields[i].ShowIf = "occ !== '" + ref.KodeMeninggal + "'"
			}
			fields[i].Key = role + "." + fields[i].Key
		}
		sec := publicSection{Key: role, Title: titles[role], Hint: hints[role], Fields: fields,
			XData: "{ occ: '" + jsString(gv["occupation"]) + "' }"}
		if role == students.RelationGuardian {
			sec.ShowIf = "showWali"
		}
		out = append(out, sec)
	}
	return out
}

// jsString hanya meloloskan kode referensi (angka/huruf) ke literal
// JavaScript, agar x-data tidak bisa disisipi.
func jsString(s string) string {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return ""
		}
	}
	return s
}

// schoolSections menyusun langkah sekolah asal dan kontak sesuai jenis
// pendaftar. Data kelulusan jenjang sebelumnya (no ujian, ijazah, SKHUN)
// tidak ditanyakan; sekolah melengkapinya di data rinci bila perlu.
func schoolSections(kind string, grade int, track Track, values, errs map[string]string) []publicSection {
	var reg []students.FieldView
	for _, f := range students.ProfileFieldViews(students.SectionRegistration, values, errs) {
		if f.Key != "previous_school" {
			continue
		}
		f.Label = previousSchoolLabel(kind, grade)
		f.Required = kind == KindTransfer
		reg = append(reg, f)
	}
	sec := publicSection{Key: students.SectionRegistration, Title: "Sekolah asal", Fields: reg}
	if kind == KindTransfer {
		sec.Title = "Sekolah asal dan kelas tujuan"
		sec.Hint = "Isi sesuai surat keterangan pindah dari sekolah asal."
		sec.Fields = append(sec.Fields,
			students.FieldView{Key: TransferNPSN, Label: transferLabels[TransferNPSN], Input: "text", InputMode: "numeric", Max: 8,
				Value: values[TransferNPSN], Error: errs[TransferNPSN], Hint: "8 angka, tercantum di surat pindah atau rapor. Kosongkan jika tidak tahu."},
			students.FieldView{Key: TransferLastGrade, Label: transferLabels[TransferLastGrade], Input: "select", Required: true,
				Options: gradeOptions(values[TransferLastGrade]), Value: values[TransferLastGrade], Error: errs[TransferLastGrade]},
			students.FieldView{Key: TransferTargetGrade, Label: transferLabels[TransferTargetGrade], Input: "select", Required: true,
				Options: gradeOptions(values[TransferTargetGrade]), Value: values[TransferTargetGrade], Error: errs[TransferTargetGrade]},
			students.FieldView{Key: TransferReason, Label: transferLabels[TransferReason], Input: "text", Max: 200, Required: true,
				Value: values[TransferReason], Error: errs[TransferReason], Hint: "Mis. ikut orang tua pindah tugas."},
			students.FieldView{Key: TransferEntryDate, Label: transferLabels[TransferEntryDate], Input: "date", Required: true,
				Value: values[TransferEntryDate], Error: errs[TransferEntryDate]},
		)
	} else {
		sec.Hint = "Kosongkan jika calon siswa belum pernah bersekolah."
	}
	out := []publicSection{sec}
	if track.Method == MethodScore {
		// Nilai jalur metode nilai (T118); dokumen bukti diunggah di langkah dokumen.
		out = append(out, publicSection{Key: "nilai_jalur", Title: "Nilai untuk jalur " + track.Name,
			Hint: "Jalur ini diurutkan berdasarkan nilai berikut. Isi sesuai rapor atau sertifikat; sekolah memeriksanya saat verifikasi.",
			Fields: []students.FieldView{{Key: TrackScoreKey, Label: track.ScoreLabel, Input: "text", InputMode: "decimal", Max: 8, Required: true,
				Value: values[TrackScoreKey], Error: errs[TrackScoreKey], Hint: "Angka 0–" + strconv.Itoa(track.ScoreMax) + ", mis. 85,5."}}})
	}
	return append(out,
		publicSection{Key: students.SectionContact, Title: "Kontak calon siswa (opsional)", Fields: students.ProfileFieldViews(students.SectionContact, values, errs)})
}
