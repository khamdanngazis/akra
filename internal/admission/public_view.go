package admission

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/pii"
	"akra/internal/students"
)

// Tampilan halaman pendaftaran publik (T111): kartu periode di halaman
// awal, status tiap langkah, ringkasan "Periksa dan kirim", dan timeline
// langkah berikutnya.

type periodCard struct {
	Name, YearName string
	Grade          int
	Closes         string
	Fee            string
	Tracks         []trackCard
}

type trackCard struct {
	Name     string
	Quota    int
	UsesTest bool
}

type docCard struct {
	Key, Label string
}

// startInfo menyusun kartu periode yang sedang dibuka dan dokumen yang
// perlu disiapkan (gabungan dokumen wajib semua periode, urut Docs).
func startInfo(open []Period) (cards []periodCard, docs []docCard, year string) {
	need := map[string]bool{}
	for _, p := range open {
		c := periodCard{Name: p.Name, YearName: p.AcademicYearName, Grade: p.TargetGrade, Closes: clock.FormatDateID(p.ClosesOn)}
		if p.FeeAmount > 0 {
			c.Fee = money.FormatRupiah(p.FeeAmount)
		}
		for _, t := range p.Tracks {
			c.Tracks = append(c.Tracks, trackCard{Name: t.Name, Quota: t.Quota, UsesTest: t.UsesTest})
		}
		cards = append(cards, c)
		for _, d := range p.RequiredDocs {
			need[d] = true
		}
		if year == "" {
			year = p.AcademicYearName
		}
	}
	for _, d := range Docs {
		if need[d.Key] {
			docs = append(docs, docCard{Key: d.Key, Label: d.Label})
		}
	}
	return cards, docs, year
}

// stepOfField memetakan kunci isian wajib (students.RequiredField) ke
// langkah formulirnya.
func stepOfField(key string) string {
	if key == "mother_name" || strings.Contains(key, ".") {
		return StepParents
	}
	if strings.HasPrefix(key, "doc_") {
		return StepDocument
	}
	if isTransferKey(key) {
		return StepSchool
	}
	for _, f := range students.ProfileFields {
		if f.Key != key {
			continue
		}
		for step, secs := range stepSections {
			if slices.Contains(secs, f.Section) {
				return step
			}
		}
	}
	return StepChild
}

// missingGroup adalah kekurangan satu langkah di halaman periksa.
type missingGroup struct {
	Step, Title string
	Items       []missingItem
	// Query adalah ?kurang=... untuk menandai isian di langkah itu.
	Query string
}

// missingItem adalah satu kekurangan. Doc berisi jenis dokumen jika yang
// kurang adalah dokumen (bisa diunggah langsung dari halaman periksa).
type missingItem struct {
	Text, Key, Doc string
}

// groupMissing mengelompokkan kekurangan per langkah, urut langkah.
func groupMissing(missing []students.RequiredField) []missingGroup {
	var out []missingGroup
	for _, s := range Steps {
		g := missingGroup{Step: s.Key, Title: s.Title}
		var keys []string
		for _, m := range missing {
			if stepOfField(m.Key) != s.Key {
				continue
			}
			if doc, ok := strings.CutPrefix(m.Key, "doc_"); ok {
				g.Items = append(g.Items, missingItem{Text: m.Label + " belum diunggah", Key: m.Key, Doc: doc})
			} else {
				g.Items = append(g.Items, missingItem{Text: m.Label + " belum diisi", Key: m.Key})
				keys = append(keys, m.Key)
			}
		}
		if len(keys) > 0 {
			g.Query = "?kurang=" + url.QueryEscape(strings.Join(keys, ","))
		}
		if len(g.Items) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// stepTouched melaporkan apakah langkah sudah pernah diisi, agar langkah
// yang belum dibuka tidak tampil sebagai "selesai" atau "kurang".
func stepTouched(a Applicant, step string, docs []Document) bool {
	switch step {
	case StepChild:
		return true // diisi sebagian di halaman awal
	case StepParents:
		for _, g := range a.Guardians {
			for _, v := range g {
				if v != "" {
					return true
				}
			}
		}
		return false
	case StepDocument:
		return len(docs) > 0
	case StepSchool:
		if len(a.Transfer) > 0 {
			return true
		}
	}
	for _, f := range students.ProfileFields {
		if slices.Contains(schoolManagedFields, f.Key) {
			continue // diisi otomatis saat draf dibuat
		}
		if slices.Contains(stepSections[step], f.Section) && a.Values[f.Key] != "" && a.Values[f.Key] != "false" {
			return true
		}
	}
	return false
}

// markSteps mengisi nomor dan status (selesai/kurang) penunjuk langkah.
func markSteps(nav []stepNav, a Applicant, missing []students.RequiredField, docs []Document) []stepNav {
	count := map[string]int{}
	for _, m := range missing {
		count[stepOfField(m.Key)]++
	}
	for i := range nav {
		nav[i].Number = i + 1
		if nav[i].Key == StepReview || !stepTouched(a, nav[i].Key, docs) {
			continue
		}
		nav[i].Missing = count[nav[i].Key]
		nav[i].Done = nav[i].Missing == 0
	}
	return nav
}

type summaryRow struct {
	Label, Value string
	Empty        bool
	Required     bool // kosong + wajib: "belum diisi"; kosong + opsional: "tidak diisi"
}

type summaryCard struct {
	Step, Title string
	Missing     int
	Rows        []summaryRow
	Docs        []docRow
}

func row(label, value string) summaryRow {
	value = strings.TrimSpace(value)
	return summaryRow{Label: label, Value: value, Empty: value == "", Required: true}
}

func optionalRow(label, value string) summaryRow {
	r := row(label, value)
	r.Required = false
	return r
}

// joinNonEmpty menggabungkan bagian yang terisi dengan pemisah sep.
func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// reviewSummary menyusun ringkasan data utama per langkah untuk halaman
// "Periksa dan kirim". NIK hanya ditampilkan tersamar.
func reviewSummary(a Applicant, missing []students.RequiredField, docs []docRow) []summaryCard {
	count := map[string]int{}
	for _, m := range missing {
		count[stepOfField(m.Key)]++
	}
	v := a.Values
	birth := clock.FormatDateID(a.BirthDate)
	gender := map[string]string{"L": "Laki-laki", "P": "Perempuan"}[a.Gender]
	rtrw := ""
	if v["rt"] != "" || v["rw"] != "" {
		rtrw = "RT " + v["rt"] + "/RW " + v["rw"]
	}
	g := a.Guardians
	var cards []summaryCard
	for _, s := range Steps {
		c := summaryCard{Step: s.Key, Title: s.Title, Missing: count[s.Key]}
		switch s.Key {
		case StepChild:
			c.Rows = []summaryRow{
				row("Nama", a.FullName),
				row("Jenis kelamin", gender),
				row("Lahir", joinNonEmpty(", ", v["birth_place"], birth)),
				row("NIK", maskNIK(v["nik"])),
			}
		case StepAddress:
			c.Rows = []summaryRow{
				row("Alamat", joinNonEmpty(", ", v["street"], rtrw, v["hamlet"], v["village"], v["district"], v["postal_code"])),
			}
		case StepParents:
			c.Rows = []summaryRow{row("Ibu", g[students.RelationMother]["full_name"])}
			if n := g[students.RelationFather]["full_name"]; n != "" {
				c.Rows = append(c.Rows, row("Ayah", n))
			}
			if n := g[students.RelationGuardian]["full_name"]; n != "" {
				c.Rows = append(c.Rows, row("Wali", n))
			}
		case StepSchool:
			c.Rows = []summaryRow{optionalRow("Sekolah asal", v["previous_school"])}
			if a.Kind == KindTransfer {
				t := a.Transfer
				move := ""
				if t[TransferLastGrade] != "" && t[TransferTargetGrade] != "" {
					move = "Kelas " + t[TransferLastGrade] + " → kelas " + t[TransferTargetGrade]
				}
				entry := ""
				if d, ok := plannedEntry(a); ok {
					entry = clock.FormatDateID(d)
				}
				c.Rows = []summaryRow{row("Sekolah asal", v["previous_school"]), row("Kelas", move), row("Alasan", t[TransferReason]), row("Rencana masuk", entry)}
			}
			if contact := joinNonEmpty(" · ", v["mobile_phone"], v["email"]); contact != "" {
				c.Rows = append(c.Rows, row("Kontak", contact))
			}
		case StepDocument:
			c.Docs = docs
		}
		cards = append(cards, c)
	}
	return cards
}

func maskNIK(nik string) string {
	if nik == "" {
		return ""
	}
	return pii.Mask(nik)
}

// timelineItem adalah satu langkah di timeline halaman selesai.
type timelineItem struct {
	Title, Desc string
	State       string // "done", "current", atau ""
}

// nextSteps menyusun timeline setelah pendaftaran dikirim.
func nextSteps(submittedAt *time.Time, fee string, usesTest bool) []timelineItem {
	sent := timelineItem{Title: "Pendaftaran dikirim", State: "done"}
	if submittedAt != nil {
		at := submittedAt.In(clock.Jakarta)
		sent.Desc = clock.FormatDateID(at) + ", " + at.Format("15.04") + " WIB"
	}
	items := []timelineItem{sent}
	if fee != "" {
		items = append(items, timelineItem{Title: "Bayar biaya formulir " + fee, Desc: "Ikuti petunjuk pembayaran dari sekolah. Bawa bukti pendaftaran."})
	}
	items = append(items, timelineItem{Title: "Pemeriksaan data oleh sekolah", Desc: "Sekolah menghubungi Anda jika ada yang perlu diperbaiki."})
	if usesTest {
		items = append(items, timelineItem{Title: "Tes seleksi", Desc: "Jadwal tes tampil di halaman cek status."})
	}
	items = append(items, timelineItem{Title: "Pengumuman hasil", Desc: "Pantau di halaman cek status dengan nomor pendaftaran."})
	items[1].State = "current"
	return items
}

// statusTone memilih warna kartu status pendaftar.
func statusTone(status string) string {
	switch status {
	case ApplicantAccepted, ApplicantVerified:
		return "success"
	case ApplicantFix, ApplicantReserve:
		return "warning"
	case ApplicantRejected:
		return "danger"
	case ApplicantWithdrawn:
		return "neutral"
	}
	return "info"
}

// historyItem adalah satu baris riwayat status di halaman cek status.
type historyItem struct {
	Label, At string
}

// publicHistory mengubah riwayat status menjadi baris ramah orang tua,
// tanpa nama petugas dan catatan internal.
func publicHistory(events []Event) []historyItem {
	var out []historyItem
	for _, e := range events {
		label, ok := StatusLabels[e.To]
		if !ok || e.To == ApplicantDraft {
			continue
		}
		at := e.At.In(clock.Jakarta)
		out = append(out, historyItem{Label: label, At: clock.FormatDateID(at) + ", " + at.Format("15.04")})
	}
	return out
}

// goDraft adalah nilai go untuk tombol "Simpan draf": simpan lalu tetap di
// langkah yang sama.
const goDraft = "draf"

// draftSavedPublic adalah pesan setelah orang tua menyimpan draf.
const draftSavedPublic = "Draf tersimpan. Untuk melanjutkan nanti, buka halaman pendaftaran, pilih \"Lanjutkan draf yang tersimpan\", lalu masukkan nomor HP dan kode lanjut."

// targetStep menentukan tujuan setelah langkah disimpan: tombol Kembali
// (go=prev), Simpan draf (go=draf), lompat ke langkah lain lewat penunjuk
// langkah (go=<kunci>), atau langkah berikutnya.
func targetStep(step, goTo string) string {
	if goTo == goDraft {
		return step
	}
	if goTo == "prev" {
		if p := prevStep(step); p != "" {
			return p
		}
		return step
	}
	if goTo != "" && goTo != step && validStep(goTo) {
		return goTo
	}
	return nextStep(step)
}

// prevStep mengembalikan langkah sebelum step, atau "" untuk langkah pertama.
func prevStep(step string) string {
	if step == StepReview {
		return Steps[len(Steps)-1].Key
	}
	for i, s := range Steps {
		if s.Key == step && i > 0 {
			return Steps[i-1].Key
		}
	}
	return ""
}

// stepOpt mengubah tampilan langkah setelah disusun.
type stepOpt func(*stepView)

// withDiscard menawarkan "Kembali tanpa menyimpan" ke tujuan go (Kembali
// atau lompat langkah) jika isian halaman ini belum valid.
func withDiscard(step, goTo string) stepOpt {
	return func(v *stepView) {
		if goTo == "" || goTo == goDraft {
			return
		}
		if t := targetStep(step, goTo); t != step {
			v.Discard = t
		}
	}
}

// highlightMissing membaca ?kurang=a,b dari tombol "Lengkapi" di halaman
// periksa (T107) dan menandai isian itu, tanpa pesan "belum disimpan".
func highlightMissing(r *http.Request) (map[string]string, []stepOpt) {
	raw := r.URL.Query().Get("kurang")
	if raw == "" {
		return nil, nil
	}
	errs := map[string]string{}
	for _, k := range strings.Split(raw, ",") {
		k = strings.TrimSpace(k)
		if k == "mother_name" {
			k = students.RelationMother + ".full_name"
		}
		if k != "" && len(errs) < 50 {
			errs[k] = "Wajib diisi agar pendaftaran bisa dikirim."
		}
	}
	return errs, []stepOpt{func(v *stepView) { v.Error, v.Highlight = "", true }}
}

// uploadReturn mengembalikan langkah tujuan setelah unggah dokumen:
// halaman periksa jika diunggah dari sana (?kembali=kirim), selain itu
// langkah Dokumen.
func uploadReturn(r *http.Request) string {
	if r.URL.Query().Get("kembali") == StepReview {
		return StepReview
	}
	return StepDocument
}
