package admission

import (
	"strings"
)

// flowStep adalah satu titik di garis alur pendaftar (T116).
type flowStep struct {
	Label string
	// State: "done", "current" (langkah yang sedang ditunggu), "warn"
	// (tertahan: perlu perbaikan/cadangan), "stop" (ditolak/mundur), atau
	// "todo".
	State string
}

// linkView adalah tautan aksi di kartu langkah berikutnya.
type linkView struct {
	Label, URL string
	Primary    bool
}

// nextStepView adalah isi kartu "Langkah berikutnya" di detail pendaftar:
// posisi di alur, apa yang ditunggu, dan tombol yang bisa dipakai.
type nextStepView struct {
	Flow  []flowStep
	Tone  string // info, warn, ok, muted
	Title string
	Hint  string
	// Primary adalah aksi utama (tombol besar); Other adalah aksi lain yang
	// membuka isian catatan dulu.
	Primary []option
	Other   []option
	// Blocked berisi alasan aksi utama belum bisa dilakukan; tombolnya
	// tetap tampil tetapi nonaktif.
	Blocked string
	Links   []linkView
	// NoteFor adalah status yang wajib disertai catatan.
	NoteFor string
	// Enroll diisi untuk pendaftar diterima yang belum menjadi siswa:
	// form Jadikan siswa langsung di kartu (T120).
	Enroll *enrollForm
}

// enrollForm adalah isian Jadikan siswa per pendaftar.
type enrollForm struct {
	NextNIS string   // perkiraan NIS otomatis ("" jika tidak bisa ditentukan)
	Classes []option // kelas tahun ajaran pendaftar
	Grade   string   // tingkat tujuan, untuk membantu memilih kelas
	// ReRegistration: sekolah memakai daftar ulang; ReRegistered adalah
	// tanggal tanda daftar ulang ("" jika belum).
	ReRegistration bool
	ReRegistered   string
}

// flowLabels adalah titik alur pendaftar dari kirim sampai jadi siswa.
var flowLabels = []string{"Dikirim", "Diverifikasi", "Diterima", "Jadi siswa"}

// reachedStep mengembalikan indeks titik alur terakhir yang sudah dilalui
// untuk status (-1 = belum dikirim).
func reachedStep(status string) int {
	switch status {
	case ApplicantSubmitted, ApplicantFix:
		return 0
	case ApplicantVerified, ApplicantReserve, ApplicantRejected:
		return 1
	case ApplicantAccepted:
		return 2
	}
	return -1
}

// buildFlow menyusun garis alur. prev adalah status sebelum mundur (untuk
// menempatkan tanda berhenti), enrolled jika sudah menjadi siswa.
func buildFlow(status, prev string, enrolled bool) []flowStep {
	out := make([]flowStep, len(flowLabels))
	for i, l := range flowLabels {
		out[i] = flowStep{Label: l, State: "todo"}
	}
	if enrolled {
		for i := range out {
			out[i].State = "done"
		}
		return out
	}
	reached := reachedStep(status)
	if status == ApplicantWithdrawn {
		reached = reachedStep(prev)
	}
	for i := 0; i <= reached && i < len(out); i++ {
		out[i].State = "done"
	}
	at := reached + 1 // titik yang sedang ditunggu
	switch status {
	case ApplicantFix:
		out[0] = flowStep{Label: StatusLabels[ApplicantFix], State: "warn"}
		return out
	case ApplicantReserve:
		out[2] = flowStep{Label: StatusLabels[ApplicantReserve], State: "warn"}
		return out
	case ApplicantRejected, ApplicantWithdrawn:
		if at < 0 {
			at = 0
		}
		out[at] = flowStep{Label: StatusLabels[status], State: "stop"}
		return out
	}
	if at >= 0 && at < len(out) {
		out[at].State = "current"
	}
	return out
}

// nextStepInput adalah data yang dibutuhkan untuk menyusun langkah berikutnya.
type nextStepInput struct {
	Applicant   Applicant
	Period      Period
	Track       Track
	PrevStatus  string // status sebelum status sekarang (dari riwayat)
	StudentID   string // kosong jika belum menjadi siswa
	MissingDocs []string
	FeeUnpaid   bool // biaya wajib lunas sebelum verifikasi dan belum lunas
	// Enroll adalah isian Jadikan siswa untuk pendaftar diterima.
	Enroll enrollForm
}

// buildNextStep menyusun kartu langkah berikutnya untuk staf.
func buildNextStep(in nextStepInput) nextStepView {
	a := in.Applicant
	v := nextStepView{Flow: buildFlow(a.Status, in.PrevStatus, in.StudentID != ""), Tone: "info", NoteFor: ApplicantFix}
	opt := func(s string) option { return option{Value: s, Label: actionLabels[s]} }
	others := func(primary ...string) {
		for _, s := range NextStatuses(a.Status) {
			isPrimary := false
			for _, p := range primary {
				isPrimary = isPrimary || p == s
			}
			if isPrimary {
				v.Primary = append(v.Primary, opt(s))
			} else {
				v.Other = append(v.Other, opt(s))
			}
		}
	}
	ranking := linkView{Label: "Buka peringkat jalur " + in.Track.Name, URL: "/pendaftaran/periode/" + in.Period.ID + "/hasil"}
	selective := (in.Track.Method != "" && in.Track.Method != MethodOrder) || in.Track.UsesTest || in.Track.Quota > 0

	switch {
	case in.StudentID != "":
		v.Tone, v.Title = "ok", "Sudah menjadi siswa"
		v.Hint = "Data pendaftar sudah dipindahkan ke data siswa. Perubahan berikutnya dilakukan di halaman siswa."
		v.Links = append(v.Links, linkView{Label: "Buka data siswa", URL: "/siswa/" + in.StudentID, Primary: true})
	case a.Status == ApplicantDraft:
		v.Tone, v.Title = "muted", "Formulir belum dikirim"
		v.Hint = "Orang tua belum menekan Kirim. Verifikasi baru bisa dilakukan setelah formulir dikirim. Staf bisa melengkapi lewat Ubah isian."
		others()
	case a.Status == ApplicantSubmitted:
		v.Title = "Menunggu verifikasi"
		v.Hint = "Periksa data dan dokumen di halaman ini. Jika sesuai, klik Verifikasi. Jika ada yang salah atau kurang, pilih Perlu perbaikan dan tulis catatannya untuk orang tua."
		if len(in.MissingDocs) > 0 {
			v.Hint += " Dokumen belum lengkap: " + strings.Join(in.MissingDocs, ", ") + "."
		}
		if in.FeeUnpaid {
			v.Blocked = "Biaya formulir belum lunas. Gelombang ini mewajibkan lunas sebelum verifikasi; pembayaran dicatat di menu Biaya Pendaftaran."
			v.Links = append(v.Links, linkView{Label: "Buka Biaya Pendaftaran", URL: "/pendaftaran/biaya"})
		}
		others(ApplicantVerified)
	case a.Status == ApplicantFix:
		v.Tone, v.Title = "warn", "Menunggu perbaikan dari orang tua"
		v.Hint = "Orang tua sudah mendapat catatan dan kode lanjut. Setelah mereka mengirim ulang, status kembali menjadi Dikirim dan bisa diverifikasi."
		others()
	case a.Status == ApplicantVerified && selective:
		v.Title = "Sudah diverifikasi, tinggal ditetapkan hasilnya"
		v.Hint = "Jalur " + in.Track.Name + " memakai " + selectiveReason(in.Track) + ". Tetapkan hasil lewat halaman peringkat agar urutan dan kuota diperhitungkan. Tombol di bawah tetap bisa dipakai untuk satu pendaftar."
		ranking.Primary = true
		v.Links = append(v.Links, ranking)
		others()
	case a.Status == ApplicantVerified:
		v.Title = "Sudah diverifikasi, tinggal ditetapkan hasilnya"
		v.Hint = "Jalur " + in.Track.Name + " tanpa tes dan tanpa batas kuota, jadi pendaftar bisa langsung diterima."
		others(ApplicantAccepted)
	case a.Status == ApplicantReserve:
		v.Tone, v.Title = "warn", "Cadangan"
		v.Hint = "Terima jika ada kursi kosong, misalnya ada pendaftar diterima yang mundur."
		v.Links = append(v.Links, ranking)
		others(ApplicantAccepted)
	case a.Status == ApplicantAccepted:
		form := in.Enroll
		v.Enroll = &form
		v.Tone = "ok"
		switch {
		case !form.ReRegistration:
			v.Title = "Diterima, tinggal dijadikan siswa"
			v.Hint = "Sekolah tidak memakai daftar ulang, tetapi pendaftar ini diterima sebelum pengaturan itu aktif. Jadikan siswa sekarang; data pendaftar ikut dipindahkan."
		case form.ReRegistered != "":
			v.Title = "Sudah daftar ulang, tinggal dijadikan siswa"
			v.Hint = "Daftar ulang " + form.ReRegistered + ". Jadikan siswa agar mendapat NIS dan kelas; data pendaftar ikut dipindahkan, tidak perlu diinput ulang."
		default:
			v.Title = "Diterima, menunggu daftar ulang"
			v.Hint = "Setelah orang tua daftar ulang, tandai di sini, lalu jadikan siswa agar mendapat NIS dan kelas. Bisa juga langsung dijadikan siswa, atau sekaligus banyak di halaman Jadikan siswa."
		}
		v.Links = append(v.Links, linkView{Label: "Jadikan siswa sekaligus banyak", URL: "/pendaftaran/terima"})
		others()
	case a.Status == ApplicantRejected:
		v.Tone, v.Title = "muted", "Ditolak"
		v.Hint = "Pendaftaran ini sudah selesai."
	case a.Status == ApplicantWithdrawn:
		v.Tone, v.Title = "muted", "Mundur"
		v.Hint = "Pendaftar mengundurkan diri. Pendaftaran ini sudah selesai."
	}
	return v
}

// selectiveReason menjelaskan kenapa jalur perlu halaman peringkat.
func selectiveReason(t Track) string {
	method := map[string]string{MethodTest: "tes seleksi", MethodScore: "urutan " + strings.ToLower(t.ScoreLabel), MethodManual: "penilaian manual"}[t.Method]
	if method == "" && t.UsesTest {
		method = "tes seleksi"
	}
	switch {
	case method != "" && t.Quota > 0:
		return method + " dan kuota"
	case method != "":
		return method
	}
	return "kuota"
}

// actionLabels adalah label tombol untuk setiap status tujuan.
var actionLabels = map[string]string{
	ApplicantVerified:  "Verifikasi",
	ApplicantFix:       "Perlu perbaikan",
	ApplicantAccepted:  "Terima",
	ApplicantReserve:   "Jadikan cadangan",
	ApplicantRejected:  "Tolak",
	ApplicantWithdrawn: "Tandai mundur",
}
