package exam

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeSaver mencatat gambar yang disimpan dan memberi ID berurutan.
type fakeSaver struct{ names []string }

func (f *fakeSaver) save(data []byte, name string) (string, error) {
	f.names = append(f.names, name)
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", len(f.names)), nil
}

func parse(t *testing.T, d *docxWriter) ([]ParsedItem, *fakeSaver) {
	t.Helper()
	data, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeSaver{}
	items, err := ParseDocx(data, fs.save)
	if err != nil {
		t.Fatal(err)
	}
	return items, fs
}

func questionsOf(items []ParsedItem) []ParsedItem {
	var out []ParsedItem
	for _, it := range items {
		if it.Kind == "soal" {
			out = append(out, it)
		}
	}
	return out
}

// Template resmi berisi contoh semua jenis terimpor 100% (kriteria T129):
// setiap soal lolos validasi penuh tanpa masalah.
func TestWordTemplateImportsCompletely(t *testing.T) {
	data, err := WordTemplate()
	if err != nil {
		t.Fatal(err)
	}
	items, err := ParseDocx(data, (&fakeSaver{}).save)
	if err != nil {
		t.Fatal(err)
	}
	qs := questionsOf(items)
	if len(qs) != 9 {
		t.Fatalf("questions = %d, want 9", len(qs))
	}
	wantKinds := []Kind{KindMC, KindShort, KindMulti, KindTF, KindMatch, KindEssay, KindMC, KindMC, KindMC}
	for i, q := range qs {
		if Kind(q.Form.Kind) != wantKinds[i] {
			t.Errorf("q%d kind = %s, want %s", i+1, q.Form.Kind, wantKinds[i])
		}
		if len(q.Problems) > 0 || q.Skip {
			t.Errorf("q%d problems: %v", i+1, q.Problems)
		}
		if _, _, err := BuildQuestion(q.Form, mr(t)); err != nil {
			t.Errorf("q%d does not validate: %v\nform: %+v", i+1, fieldErrs(err), q.Form)
		}
	}
	q1 := qs[0]
	if !strings.Contains(q1.Form.Body, `data-tex="\frac{3}{4}+\frac{1}{2}"`) || !strings.Contains(q1.Form.Choices[0], `data-tex="\frac{5}{4}"`) || q1.TPCode != "1.1" || q1.Form.Difficulty != "mudah" {
		t.Errorf("q1 = %+v", q1.Form)
	}
	if strings.Contains(q1.Form.Body, "1.") {
		t.Errorf("q1 body still has its number: %s", q1.Form.Body)
	}
	if !strings.Contains(qs[1].Form.Body, "H<sub>2</sub>O") || !strings.Contains(qs[1].Form.Body, `\sqrt{144}`) {
		t.Errorf("q2 body = %s", qs[1].Form.Body)
	}
	if qs[2].Form.Correct[0] != 0 || qs[2].Form.Correct[1] != 2 {
		t.Errorf("q3 key = %v", qs[2].Form.Correct)
	}
	if qs[3].Form.Truth[0] != "B" || qs[3].Form.Truth[1] != "S" {
		t.Errorf("q4 truth = %v", qs[3].Form.Truth)
	}
	if qs[4].Form.Lefts[2] != "Bali" || qs[4].Form.Rights[2] != "Denpasar" {
		t.Errorf("q5 pairs = %v %v", qs[4].Form.Lefts, qs[4].Form.Rights)
	}
	if qs[5].Form.Points != "10" || !strings.Contains(qs[5].Form.Rubric, "klorofil") {
		t.Errorf("q6 = %+v", qs[5].Form)
	}
	// Stimulus template mengikat soal 7–8 saja.
	var stim ParsedItem
	for _, it := range items {
		if it.Kind == "stimulus" {
			stim = it
		}
	}
	if stim.Stim.Title == "" || !strings.Contains(stim.Stim.Body, "Ani menanam") {
		t.Errorf("stimulus = %+v", stim)
	}
	if qs[6].StimIdx < 0 || qs[7].StimIdx != qs[6].StimIdx || qs[8].StimIdx != -1 || qs[5].StimIdx != -1 {
		t.Errorf("stimulus binding = %d %d %d %d", qs[5].StimIdx, qs[6].StimIdx, qs[7].StimIdx, qs[8].StimIdx)
	}
	// Teks Arab: arah kanan-ke-kiri, harakat utuh.
	if !strings.Contains(qs[8].Form.Body, `<p dir="rtl" lang="ar">ذَهَبَ أَحْمَدُ إِلَى الْمَدْرَسَةِ</p>`) {
		t.Errorf("q9 Arabic = %s", qs[8].Form.Body)
	}
}

// Dua teks bacaan, masing-masing 5 soal, terimpor sebagai dua stimulus
// (pola guru, tanpa penanda template).
func TestTwoReadingPassages(t *testing.T) {
	d := newDocx()
	d.P(wRun("ULANGAN HARIAN BAHASA INDONESIA"))
	d.P(wRun("Bacalah teks berikut untuk menjawab soal nomor 1–5!"))
	d.P(wRun("Pada suatu pagi, Budi pergi ke pasar bersama ibunya."))
	for i := 1; i <= 5; i++ {
		d.P(wRun(fmt.Sprintf("%d. Pertanyaan bacaan satu ke-%d?", i, i)))
		d.P(wRun("A. ya"))
		d.P(wRun("B. tidak"))
		d.P(wRun("Kunci: A"))
	}
	d.P(wRun("Perhatikan tabel berikut untuk soal nomor 6 - 10."))
	d.Table([][]string{{"Nama", "Nilai"}, {"Ani", "80"}, {"Budi", "75"}})
	for i := 6; i <= 10; i++ {
		d.P(wRun(fmt.Sprintf("%d) Pertanyaan tabel ke-%d?", i, i)))
		d.P(wRun("a. benar"))
		d.P(wRun("b. salah"))
		d.P(wRun("Kunci: b"))
	}
	d.P(wRun("11. Soal tanpa bacaan."))
	d.P(wRun("A. satu"))
	d.P(wRun("B. dua"))
	d.P(wRun("Kunci: B"))
	items, _ := parse(t, d)
	var stims []int
	for i, it := range items {
		if it.Kind == "stimulus" {
			stims = append(stims, i)
		}
	}
	if len(stims) != 2 {
		t.Fatalf("stimuli = %d, want 2", len(stims))
	}
	if !strings.Contains(items[stims[0]].Stim.Body, "Budi pergi ke pasar") || !strings.Contains(items[stims[1]].Stim.Body, "<table>") {
		t.Errorf("stimulus bodies: %q / %q", items[stims[0]].Stim.Body, items[stims[1]].Stim.Body)
	}
	count := map[int]int{}
	for _, q := range questionsOf(items) {
		count[q.StimIdx]++
		if len(q.Form.Correct) != 1 {
			t.Errorf("q%s key = %v", q.Number, q.Form.Correct)
		}
	}
	if count[stims[0]] != 5 || count[stims[1]] != 5 || count[-1] != 1 {
		t.Errorf("binding = %v", count)
	}
}

// Kunci di tabel akhir dokumen dan penomoran otomatis Word dikenali.
func TestKeyTableAndAutoNumbering(t *testing.T) {
	d := newDocx()
	for i := 1; i <= 4; i++ {
		d.NP(0, wRun(fmt.Sprintf("Soal bernomor otomatis %d", i)))
		d.NP(1, wRun("pilihan satu"))
		d.NP(1, wRun("pilihan dua"))
		d.NP(1, wRun("pilihan tiga"))
	}
	d.P(wRun("Kunci Jawaban"))
	d.Table([][]string{{"1. B", "2. C"}, {"3. A", "4. B"}})
	items, _ := parse(t, d)
	qs := questionsOf(items)
	if len(qs) != 4 {
		t.Fatalf("questions = %d, want 4", len(qs))
	}
	want := []int{1, 2, 0, 1}
	for i, q := range qs {
		if len(q.Form.Correct) != 1 || q.Form.Correct[0] != want[i] || len(q.Problems) != 0 {
			t.Errorf("q%d correct = %v problems = %v", i+1, q.Form.Correct, q.Problems)
		}
		if q.Form.Choices[2] != "pilihan tiga" || strings.Contains(q.Form.Body, "1.") {
			t.Errorf("q%d body %q choices %v", i+1, q.Form.Body, q.Form.Choices[:3])
		}
	}
}

func TestDocxProblemsAndSafety(t *testing.T) {
	// MathType: pratinjau PNG jadi gambar (perlu dicek), WMF tidak terbaca.
	d := newDocx()
	png := pngBytes(20, 10)
	d.media["preview.png"] = png
	d.rels = append(d.rels, `<Relationship Id="rIdOle1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/preview.png"/>`,
		`<Relationship Id="rIdOle2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/rumus.wmf"/>`,
		`<Relationship Id="rIdExt" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="https://evil.example/x.png" TargetMode="External"/>`)
	d.media["rumus.wmf"] = []byte("wmf")
	ole := func(rid string) string {
		return `<w:r><w:object><v:shape><v:imagedata r:id="` + rid + `"/></v:shape></w:object></w:r>`
	}
	d.P(wRun("1. Nilai "), ole("rIdOle1"), wRun(" adalah"))
	d.P(wRun("A. 1"))
	d.P(wRun("B. 2"))
	d.P(wRun("Kunci: A"))
	d.P(wRun("2. Nilai "), ole("rIdOle2"), wRun(" adalah"))
	d.P(wRun("A. 1"))
	d.P(wRun("B. 2"))
	d.P(wRun("3. Gambar luar "), `<w:r><w:drawing><a:blip r:embed="rIdExt"/></w:drawing></w:r>`)
	d.P(wRun("Jenis: esai"))
	items, fs := parse(t, d)
	qs := questionsOf(items)
	if len(qs) != 3 {
		t.Fatalf("questions = %d", len(qs))
	}
	if !strings.Contains(strings.Join(qs[0].Problems, ";"), "sebagai gambar") || !strings.Contains(qs[0].Form.Body, "data-asset") {
		t.Errorf("q1 = %v %s", qs[0].Problems, qs[0].Form.Body)
	}
	if p := strings.Join(qs[1].Problems, ";"); !strings.Contains(p, "MathType") || !strings.Contains(p, "kunci tidak ditemukan") {
		t.Errorf("q2 problems = %v", qs[1].Problems)
	}
	if len(fs.names) != 1 {
		t.Errorf("saved images = %v (external image must not be fetched)", fs.names)
	}

	// Makro ditolak.
	m := newDocx()
	m.P(wRun("1. Soal"))
	m.extra["word/vbaProject.bin"] = []byte("macro")
	data, _ := m.Bytes()
	if _, err := ParseDocx(data, (&fakeSaver{}).save); !errors.Is(err, ErrDocxMacro) {
		t.Errorf("macro: %v", err)
	}
	// Bukan .docx dan XXE ditolak.
	if _, err := ParseDocx([]byte("bukan zip"), (&fakeSaver{}).save); !errors.Is(err, ErrNotDocx) {
		t.Errorf("not zip: %v", err)
	}
	x := newDocx()
	x.extra["word/document.xml"] = []byte(`<?xml version="1.0"?><!DOCTYPE w [<!ENTITY x SYSTEM "file:///etc/passwd">]><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>&x;</w:t></w:r></w:p></w:body></w:document>`)
	data, _ = x.Bytes()
	if _, err := ParseDocx(data, (&fakeSaver{}).save); !errors.Is(err, ErrNotDocx) {
		t.Errorf("xxe: %v", err)
	}
}
