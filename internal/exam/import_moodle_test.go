package exam

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func moodleSample() string {
	img := base64.StdEncoding.EncodeToString(pngBytes(8, 8))
	return `<?xml version="1.0" encoding="UTF-8"?>
<quiz>
<question type="category"><category><text>$course$/top/Matematika/Pecahan</text></category></question>
<question type="multichoice"><name><text>Pecahan 1</text></name>
<questiontext format="html"><text><![CDATA[<p>Hasil $$\frac{1}{2}+\frac{1}{4}$$ adalah <img src="@@PLUGINFILE@@/gambar%201.png" alt="Diagram"></p>]]></text>
<file name="gambar 1.png" path="/" encoding="base64">` + img + `</file></questiontext>
<generalfeedback format="html"><text><![CDATA[<p>Samakan penyebut.</p>]]></text></generalfeedback>
<defaultgrade>2.0000000</defaultgrade><single>true</single>
<answer fraction="100" format="html"><text><![CDATA[<p>\(\frac{3}{4}\)</p>]]></text></answer>
<answer fraction="0" format="html"><text>1</text></answer>
<answer fraction="0" format="html"><text>2</text></answer>
</question>
<question type="multichoice"><name><text>Prima</text></name><questiontext format="html"><text>Pilih bilangan prima</text></questiontext>
<single>false</single>
<answer fraction="50"><text>2</text></answer><answer fraction="-50"><text>4</text></answer><answer fraction="50"><text>5</text></answer></question>
<question type="truefalse"><name><text>Air</text></name><questiontext format="html"><text>Air mendidih 100 °C</text></questiontext>
<answer fraction="100"><text>true</text></answer><answer fraction="0"><text>false</text></answer></question>
<question type="shortanswer"><name><text>Ibu kota</text></name><questiontext format="html"><text>Ibu kota Indonesia?</text></questiontext>
<answer fraction="100"><text>Jakarta</text></answer><answer fraction="50"><text>Batavia</text></answer></question>
<question type="numerical"><name><text>Kali</text></name><questiontext format="html"><text>12 x 4 =</text></questiontext>
<answer fraction="100"><text>48</text><tolerance>0.5</tolerance></answer></question>
<question type="description"><name><text>Teks bacaan</text></name><questiontext format="html"><text><![CDATA[<p>Ani menanam pohon.</p>]]></text></questiontext></question>
<question type="matching"><name><text>Jodoh</text></name><questiontext format="html"><text>Jodohkan</text></questiontext>
<subquestion format="html"><text>Jawa Barat</text><answer><text>Bandung</text></answer></subquestion>
<subquestion format="html"><text>Jawa Timur</text><answer><text>Surabaya</text></answer></subquestion>
<subquestion format="html"><text></text><answer><text>Medan</text></answer></subquestion></question>
<question type="essay"><name><text>Esai</text></name><questiontext format="html"><text>Apa yang ditanam Ani?</text></questiontext>
<graderinfo format="html"><text>Pohon (5)</text></graderinfo></question>
<question type="cloze"><name><text>Cloze</text></name><questiontext format="html"><text>{1:SA:=x}</text></questiontext></question>
</quiz>`
}

func TestParseMoodleXML(t *testing.T) {
	fs := &fakeSaver{}
	items, err := ParseMoodleXML([]byte(moodleSample()), fs.save)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 9 {
		t.Fatalf("items = %d, want 9", len(items))
	}
	byKind := map[string]ParsedItem{}
	for _, it := range items {
		if it.Kind == "stimulus" {
			byKind["stimulus"] = it
		} else if !it.Skip {
			byKind[it.Form.Kind] = it
		} else {
			byKind["skip"] = it
		}
	}
	mc := byKind["pg"]
	if !strings.Contains(mc.Form.Body, `\(\frac{1}{2}+\frac{1}{4}\)`) || !strings.Contains(mc.Form.Body, `data-asset="00000000-0000-4000-8000-000000000001" alt="Diagram"`) ||
		mc.Form.Points != "2" || mc.Form.Tags != "pecahan" || len(mc.Form.Correct) != 1 || mc.Form.Correct[0] != 0 || len(fs.names) != 1 || fs.names[0] != "gambar 1.png" {
		t.Errorf("multichoice = %+v (images %v)", mc.Form, fs.names)
	}
	if m := byKind["pg_kompleks"]; len(m.Form.Correct) != 2 {
		t.Errorf("multi = %v", m.Form.Correct)
	}
	if tf := byKind["benar_salah"]; tf.Form.Truth[0] != "B" || !strings.Contains(tf.Form.Statements[0], "Air mendidih") {
		t.Errorf("truefalse = %+v", tf.Form)
	}
	if s := byKind["isian"]; s.Form.Accepted == "" {
		t.Errorf("short = %+v", s.Form)
	}
	if mt := byKind["menjodohkan"]; mt.Form.Lefts[1] != "Jawa Timur" || mt.Form.Distractors[0] != "Medan" {
		t.Errorf("matching = %v %v", mt.Form.Lefts, mt.Form.Distractors)
	}
	if e := byKind["esai"]; e.StimIdx < 0 || !strings.Contains(e.Form.Rubric, "Pohon") {
		t.Errorf("essay after description should be offered under the stimulus: %+v", e)
	}
	if sk := byKind["skip"]; !strings.Contains(strings.Join(sk.Problems, ""), "cloze") {
		t.Errorf("cloze not skipped with reason: %v", sk.Problems)
	}
	for _, it := range items {
		if it.Kind == "soal" && !it.Skip {
			if _, _, err := BuildQuestion(it.Form, mr(t)); err != nil {
				t.Errorf("%s does not validate: %v", it.Form.Kind, fieldErrs(err))
			}
		}
	}
	// Numerik: angka dengan toleransi.
	for _, it := range items {
		if it.Origin != "" && strings.HasPrefix(it.Origin, "Kali") && (it.Form.Input != "angka" || it.Form.Tolerance != "0.5") {
			t.Errorf("numerical = %+v", it.Form)
		}
	}
	// XXE ditolak.
	xxe := `<?xml version="1.0"?><!DOCTYPE q [<!ENTITY x SYSTEM "file:///etc/passwd">]><quiz><question type="essay"><questiontext><text>&x;</text></questiontext></question></quiz>`
	if _, err := ParseMoodleXML([]byte(xxe), fs.save); !errors.Is(err, ErrImportFile) {
		t.Errorf("xxe: %v", err)
	}
}

func TestParseGIFT(t *testing.T) {
	src := "// komentar\n$CATEGORY: $course$/IPA\n\n" +
		"::Air:: Air mendidih pada suhu ... {=100 °C ~50 °C ~0 °C}\n\n" +
		"::BS:: Matahari terbit di timur. {T}\n\n" +
		"::Isian:: Ibu kota Jawa Barat adalah {=Bandung =bandung}\n\n" +
		"::Angka:: 2 + 2,5 = {#4.5:0.1}\n\n" +
		"::Jodoh:: Jodohkan {=Bandung -> Jawa Barat =Surabaya -> Jawa Timur}\n\n" +
		"::Prima:: Pilih prima {~%50%2 ~%-50%4 ~%50%5}\n\n" +
		"Bacalah teks: Ani menanam pohon.\n\n" +
		"::Esai:: Apa yang ditanam? {}\n"
	items, err := ParseGIFT([]byte(src), (&fakeSaver{}).save)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, it := range items {
		if it.Kind == "stimulus" {
			kinds = append(kinds, "stimulus")
			continue
		}
		kinds = append(kinds, it.Form.Kind)
		if _, _, err := BuildQuestion(it.Form, mr(t)); err != nil {
			t.Errorf("%s: %v", it.Form.Kind, fieldErrs(err))
		}
	}
	if got := strings.Join(kinds, ","); got != "pg,benar_salah,isian,isian,menjodohkan,pg_kompleks,stimulus,esai" {
		t.Errorf("kinds = %s", got)
	}
	if items[0].Form.Correct[0] != 0 || items[0].Form.Tags != "ipa" || items[3].Form.Input != "angka" || items[3].Form.Tolerance != "0.1" || items[7].StimIdx != 6 {
		t.Errorf("details: %+v / %+v / stim %d", items[0].Form, items[3].Form, items[7].StimIdx)
	}
}

// Ekspor AKRA lalu impor lagi menghasilkan soal yang sama, termasuk
// stimulus sebagai "description" tepat sebelum soal-soalnya.
func TestMoodleRoundTrip(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	sa := e.staff(e.a.Teacher.ID, "guru")
	tpl, _ := WordTemplate()
	imp, _ := CreateImport(ctx, e.tx, e.store, sa, e.bank.ID, SourceWord, "t.docx", tpl, time.Now())
	if err := ProcessImport(ctx, e.tx, e.store, mr(t), e.a.ID, imp.ID); err != nil {
		t.Fatal(err)
	}
	imp, _ = GetImport(ctx, e.tx, e.a.ID, imp.ID)
	if _, err := CommitImport(ctx, e.tx, mr(t), sa, imp, CommitOptions{IncludeCheck: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	qs, stims, err := bankQuestions(ctx, e.tx, e.bank)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{db: e.tx, store: e.store}
	xmlOut := ExportMoodleXML("Matematika", stims, qs, h.exportImages(ctx, e.a.ID))
	if !strings.Contains(string(xmlOut), `type="description"`) || !strings.Contains(string(xmlOut), `\(\frac{3}{4}+\frac{1}{2}\)`) {
		t.Fatalf("export missing description or TeX")
	}
	items, err := ParseMoodleXML(xmlOut, (&fakeSaver{}).save)
	if err != nil {
		t.Fatal(err)
	}
	// Benar/salah dengan 2 pernyataan diekspor sebagai 2 soal Moodle.
	var back []Question
	stimBefore := -1
	for i, it := range items {
		if it.Kind == "stimulus" {
			stimBefore = i
			continue
		}
		q, _, err := BuildQuestion(it.Form, mr(t))
		if err != nil {
			t.Fatalf("reimported %s: %v", it.Form.Kind, fieldErrs(err))
		}
		back = append(back, q)
	}
	if stimBefore < 0 {
		t.Error("description lost")
	}
	want := map[Kind]int{}
	for _, q := range qs {
		if q.Kind == KindTF {
			want[KindTF] += len(q.Options.Statements)
		} else {
			want[q.Kind]++
		}
	}
	got := map[Kind]int{}
	for _, q := range back {
		got[q.Kind]++
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("kind %s: %d after round trip, want %d", k, got[k], n)
		}
	}
	// Isi dan kunci PG pertama sama.
	if PlainText(back[0].BodySrc) != PlainText(qs[0].BodySrc) || strings.Join(back[0].Key.Correct, ",") != strings.Join(qs[0].Key.Correct, ",") {
		t.Errorf("first question differs:\n%s %v\n%s %v", back[0].BodySrc, back[0].Key.Correct, qs[0].BodySrc, qs[0].Key.Correct)
	}
}
