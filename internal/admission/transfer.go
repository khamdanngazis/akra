package admission

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"akra/internal/students"
)

// Isian khusus siswa pindahan (T109), disimpan di applicants.transfer.
const (
	TransferNPSN        = "previous_school_npsn"
	TransferLastGrade   = "last_grade"
	TransferTargetGrade = "target_grade"
	TransferReason      = "transfer_reason"
	TransferEntryDate   = "planned_entry_date"
)

// Kode jenis pendaftaran "Pindahan" (ref.JenisPendaftaran).
const registrationTransfer = "02"

// transferKeys adalah semua isian pindahan, urut tampil.
var transferKeys = []string{TransferNPSN, TransferLastGrade, TransferTargetGrade, TransferReason, TransferEntryDate}

// transferLabels adalah label isian pindahan.
var transferLabels = map[string]string{
	TransferNPSN:        "NPSN sekolah asal",
	TransferLastGrade:   "Kelas terakhir di sekolah asal",
	TransferTargetGrade: "Masuk ke kelas",
	TransferReason:      "Alasan pindah",
	TransferEntryDate:   "Rencana tanggal masuk",
}

// transferRequired adalah isian wajib pindahan selain NPSN.
var transferRequired = []string{TransferLastGrade, TransferTargetGrade, TransferReason, TransferEntryDate}

// publicHiddenFields adalah data rinci yang tidak ditampilkan di formulir
// pendaftaran: data kelulusan jenjang sebelumnya dan usulan PIP (diisi
// sekolah lewat data rinci setelah diterima). Nilai yang sudah ada tidak
// ditimpa saat langkah disimpan.
var publicHiddenFields = []string{"exam_number", "diploma_serial", "skhun_number", "pip_eligible", "pip_reason"}

// registrationFor mengembalikan kode jenis pendaftaran Dapodik untuk jenis
// pendaftar.
func registrationFor(kind string) string {
	if kind == KindTransfer {
		return registrationTransfer
	}
	return registrationNew
}

// validateTransfer memeriksa isian pindahan. previousSchool adalah isian
// "Sekolah asal" (data rinci) yang wajib untuk pindahan.
func validateTransfer(v map[string]string, previousSchool string, today time.Time) (map[string]string, error) {
	out := map[string]string{}
	fields := map[string]string{}
	get := func(k string) string { return strings.TrimSpace(v[k]) }

	if strings.TrimSpace(previousSchool) == "" {
		fields["previous_school"] = "Nama sekolah asal wajib diisi."
	}
	if npsn := get(TransferNPSN); npsn != "" {
		if len(npsn) != 8 || strings.Trim(npsn, "0123456789") != "" {
			fields[TransferNPSN] = "NPSN terdiri dari 8 angka. Kosongkan jika tidak tahu."
		}
		out[TransferNPSN] = npsn
	}
	grade := func(k, msg string) int {
		raw := get(k)
		if raw == "" {
			fields[k] = msg
			return 0
		}
		g, err := strconv.Atoi(raw)
		if err != nil || g < 1 || g > 12 {
			fields[k] = "Pilih kelas 1–12."
			return 0
		}
		out[k] = strconv.Itoa(g)
		return g
	}
	last := grade(TransferLastGrade, "Pilih kelas terakhir di sekolah asal.")
	target := grade(TransferTargetGrade, "Pilih kelas tujuan.")
	if last > 0 && target > 0 && (target < last || target > last+1) {
		fields[TransferTargetGrade] = fmt.Sprintf("Kelas tujuan biasanya kelas %d atau %d.", last, last+1)
	}
	reason := strings.Join(strings.Fields(get(TransferReason)), " ")
	switch {
	case reason == "":
		fields[TransferReason] = "Alasan pindah wajib diisi, mis. ikut orang tua pindah tugas."
	case utf8.RuneCountInString(reason) > 200:
		fields[TransferReason] = "Alasan pindah maksimal 200 karakter."
	}
	out[TransferReason] = reason
	if raw := get(TransferEntryDate); raw == "" {
		fields[TransferEntryDate] = "Rencana tanggal masuk wajib diisi."
	} else if d, err := time.Parse(dateLayout, raw); err != nil || d.Before(today.AddDate(-1, 0, 0)) || d.After(today.AddDate(1, 0, 0)) {
		fields[TransferEntryDate] = "Tanggal masuk tidak valid."
	} else {
		out[TransferEntryDate] = raw
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	return out, nil
}

// missingTransfer mengembalikan isian pindahan yang belum diisi.
func missingTransfer(a Applicant) []students.RequiredField {
	var out []students.RequiredField
	if a.Values["previous_school"] == "" {
		out = append(out, students.RequiredField{Key: "previous_school", Label: "Sekolah asal"})
	}
	for _, k := range transferRequired {
		if a.Transfer[k] == "" {
			out = append(out, students.RequiredField{Key: k, Label: transferLabels[k]})
		}
	}
	return out
}

// isTransferKey melaporkan apakah key adalah isian pindahan.
func isTransferKey(key string) bool {
	for _, k := range transferKeys {
		if k == key {
			return true
		}
	}
	return false
}

// plannedEntry mengembalikan rencana tanggal masuk pindahan, atau ok false.
func plannedEntry(a Applicant) (time.Time, bool) {
	if a.Kind != KindTransfer {
		return time.Time{}, false
	}
	d, err := time.Parse(dateLayout, a.Transfer[TransferEntryDate])
	return d, err == nil
}

// previousSchoolLabel mengembalikan label "Sekolah asal" sesuai jenis dan
// tingkat tujuan: TK/PAUD untuk kelas 1, SD/MI untuk kelas 7, SMP/MTs untuk
// kelas 10.
func previousSchoolLabel(kind string, targetGrade int) string {
	if kind == KindTransfer {
		return "Sekolah asal (sebelum pindah)"
	}
	switch targetGrade {
	case 1:
		return "TK/PAUD asal"
	case 7:
		return "SD/MI asal"
	case 10:
		return "SMP/MTs asal"
	}
	return "Sekolah asal"
}

// gradeOptions mengembalikan pilihan kelas 1–12 untuk select.
func gradeOptions(selected string) []students.Option {
	out := make([]students.Option, 0, 12)
	for g := 1; g <= 12; g++ {
		v := strconv.Itoa(g)
		out = append(out, students.Option{Value: v, Label: "Kelas " + v, Selected: v == selected})
	}
	return out
}

// numberPrefix adalah awalan nomor pendaftaran: SPMB untuk siswa baru,
// PINDAH untuk pindahan (urutan nomornya terpisah).
func numberPrefix(kind string) string {
	if kind == KindTransfer {
		return "PINDAH"
	}
	return "SPMB"
}

// joinValidation menggabungkan error validasi field dari dua sumber.
// Error selain ValidationError dikembalikan apa adanya.
func joinValidation(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	va, okA := a.(*ValidationError)
	vb, okB := b.(*ValidationError)
	if !okA {
		return a
	}
	if !okB {
		return b
	}
	fields := map[string]string{}
	for k, v := range va.Fields {
		fields[k] = v
	}
	for k, v := range vb.Fields {
		fields[k] = v
	}
	return &ValidationError{Fields: fields}
}
