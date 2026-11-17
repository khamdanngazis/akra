package exam

import (
	"errors"
	"io/fs"
	"net/url"
	"strings"
	"sync"
	"testing"

	"akra/internal/exam/mathrender"
	"akra/web"
)

var (
	testRendererOnce sync.Once
	testRenderer     *mathrender.Renderer
)

func mr(t *testing.T) *mathrender.Renderer {
	t.Helper()
	testRendererOnce.Do(func() {
		var err error
		static, _ := fs.Sub(web.FS, "static")
		if testRenderer, err = mathrender.New(static); err != nil {
			t.Fatal(err)
		}
	})
	return testRenderer
}

func formOf(kv ...string) QuestionForm {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Add(kv[i], kv[i+1])
	}
	return ParseQuestionForm(v)
}

func fieldErrs(err error) map[string]string {
	var fe *FieldError
	if errors.As(err, &fe) {
		return fe.Fields
	}
	return nil
}

// Keenam jenis bisa dibuat dan divalidasi, dan form bolak-balik
// (FormFromQuestion) menghasilkan soal yang sama.
func TestBuildEveryKind(t *testing.T) {
	r := mr(t)
	cases := map[Kind]QuestionForm{
		KindMC: formOf("kind", "pg", "body", `Nilai \(\frac{3}{4}+\frac{1}{2}\) adalah …`, "choice_0", `\(\frac{5}{4}\)`,
			"choice_1", "1", "choice_2", "2", "correct", "0", "points", "2", "difficulty", "mudah", "cognitive_level", "C3", "tags", "pecahan, Bilangan"),
		KindMulti: formOf("kind", "pg_kompleks", "body", "Bilangan prima:", "choice_0", "2", "choice_1", "4", "choice_2", "5",
			"correct", "0", "correct", "2", "scoring", "semua"),
		KindTF: formOf("kind", "benar_salah", "body", "Tentukan B/S", "statement_0", "Air mendidih 100°C", "truth_0", "B",
			"statement_1", "Bumi datar", "truth_1", "S"),
		KindShort: formOf("kind", "isian", "body", "12 × 4 = …", "accepted", "48\nempat puluh delapan | 48,0", "input", "angka", "tolerance", "0,5"),
		KindMatch: formOf("kind", "menjodohkan", "body", "Jodohkan", "left_0", "Bandung", "right_0", "Jawa Barat",
			"left_1", "Surabaya", "right_1", "Jawa Timur", "distractor_0", "Bali"),
		KindEssay: formOf("kind", "esai", "body", "Jelaskan fotosintesis", "rubric", "Menyebut klorofil (2), cahaya (2)", "points", "10"),
	}
	cases[KindShort] = formOf("kind", "isian", "body", "12 × 4 = …", "accepted", "48 | 48,0", "input", "angka", "tolerance", "0,5")
	for k, f := range cases {
		q, _, err := BuildQuestion(f, r)
		if err != nil {
			t.Errorf("%s: %v", k, fieldErrs(err))
			continue
		}
		if q.Kind != k || q.Status != StatusReady || q.BodyHTML == "" {
			t.Errorf("%s: built %+v", k, q)
		}
		// Bolak-balik: form dari soal lalu dibangun ulang.
		q2, _, err := BuildQuestion(FormFromQuestion(q), r)
		if err != nil {
			t.Errorf("%s round trip: %v", k, fieldErrs(err))
			continue
		}
		if q2.BodySrc != q.BodySrc || len(q2.Options.Choices) != len(q.Options.Choices) || len(q2.Options.Right) != len(q.Options.Right) ||
			strings.Join(q2.Key.Correct, ",") != strings.Join(q.Key.Correct, ",") || len(q2.Key.Pairs) != len(q.Key.Pairs) ||
			len(q2.Key.Accepted) != len(q.Key.Accepted) || q2.Key.Scoring != q.Key.Scoring {
			t.Errorf("%s round trip differs:\n%+v\n%+v", k, q, q2)
		}
	}
	mc, _, _ := BuildQuestion(cases[KindMC], r)
	if mc.Key.Correct[0] != "a" || !strings.Contains(mc.Options.Choices[0].HTML, "katex") || len(mc.Tags) != 2 || mc.Tags[1] != "bilangan" {
		t.Errorf("mc = %+v", mc)
	}
	if m, _, _ := BuildQuestion(cases[KindMatch], r); len(m.Options.Right) != 3 || m.Key.Pairs["l2"] != "r2" {
		t.Errorf("match = %+v", m)
	}
	if s, _, _ := BuildQuestion(cases[KindShort], r); s.Key.Tolerance != 0.5 || len(s.Key.Accepted) != 2 {
		t.Errorf("short = %+v", s.Key)
	}
}

func TestBuildValidationMessages(t *testing.T) {
	r := mr(t)
	cases := []struct {
		form  QuestionForm
		field string
		msg   string
	}{
		{formOf("kind", "pg", "body", "x", "choice_0", "a", "choice_1", "b"), "correct", "Tandai satu opsi yang benar"},
		{formOf("kind", "pg", "body", "x", "choice_0", "a", "choice_1", "", "choice_2", "c", "correct", "0"), "choice_1", "Opsi B kosong"},
		{formOf("kind", "pg", "body", "x", "choice_0", "Dua", "choice_1", "dua", "correct", "0"), "choice_1", "Opsi B sama dengan opsi A"},
		{formOf("kind", "pg", "body", "", "choice_0", "a", "choice_1", "b", "correct", "0"), "body", "Tulis teks soal"},
		{formOf("kind", "isian", "body", "x"), "accepted", "Isian butuh minimal satu jawaban diterima"},
		{formOf("kind", "isian", "body", "x", "input", "angka", "accepted", "dua"), "accepted", "bukan angka"},
		{formOf("kind", "benar_salah", "body", "x", "statement_0", "p"), "statement_0", "Pilih Benar atau Salah"},
		{formOf("kind", "menjodohkan", "body", "x", "left_0", "a", "right_0", "b"), "pairs", "minimal 2 pasangan"},
		{formOf("kind", "pg", "body", `\(\frac{1}{\)`, "choice_0", "a", "choice_1", "b", "correct", "0"), "body", "rumus tidak valid"},
		{formOf("kind", "pg", "body", `<p>Lihat <img data-asset="11111111-1111-4111-8111-111111111111" alt=""></p>`, "choice_0", "a", "choice_1", "b", "correct", "0"), "body", "teks alternatif"},
	}
	for _, c := range cases {
		_, _, err := BuildQuestion(c.form, r)
		msg := fieldErrs(err)[c.field]
		if !strings.Contains(msg, c.msg) {
			t.Errorf("%s: field %s = %q, want %q (all: %v)", c.form.Kind, c.field, msg, c.msg, fieldErrs(err))
		}
	}
	// Draf: isi belum lengkap boleh disimpan, rumus salah tetap ditolak.
	if _, _, err := BuildQuestion(formOf("kind", "pg", "body", "x", "status", "draf"), r); err != nil {
		t.Errorf("draft incomplete: %v", fieldErrs(err))
	}
	if _, _, err := BuildQuestion(formOf("kind", "pg", "body", `\(\frac{\)`, "status", "draf"), r); err == nil {
		t.Error("draft with invalid formula accepted")
	}
	_, assets, err := BuildQuestion(formOf("kind", "esai", "body", `<p><img data-asset="11111111-1111-4111-8111-111111111111" alt="Peta"></p>`), r)
	if err != nil || len(assets) != 1 {
		t.Errorf("assets = %v, %v", assets, err)
	}
}

func TestMatchShortAnswer(t *testing.T) {
	text := AnswerKey{Accepted: []string{"Fotosintesis", "foto sintesis"}}
	for in, want := range map[string]bool{"fotosintesis": true, " FOTOSINTESIS. ": true, "foto  sintesis!": true, "respirasi": false, "": false} {
		if got := MatchShortAnswer(in, "teks", text); got != want {
			t.Errorf("text %q = %v", in, got)
		}
	}
	num := AnswerKey{Accepted: []string{"2,5"}, Tolerance: 0.1}
	for in, want := range map[string]bool{"2.5": true, "2,5": true, "2,59": true, "2,61": false, "dua": false, "2.5.1": false} {
		if got := MatchShortAnswer(in, "angka", num); got != want {
			t.Errorf("number %q = %v", in, got)
		}
	}
	// Isian Arab: dengan opsi normalisasi, berharakat dan tanpa harakat
	// sama-sama diterima (kriteria T128); tanpa opsi, harus persis.
	arab := AnswerKey{Accepted: []string{"الصَّلَاةُ"}, ArabicNormalize: true}
	for _, in := range []string{"الصلاة", "الصَّلَاةُ", "الصلاه"} {
		if !MatchShortAnswer(in, "teks", arab) {
			t.Errorf("arabic normalized %q rejected", in)
		}
	}
	strict := AnswerKey{Accepted: []string{"الصَّلَاةُ"}}
	if MatchShortAnswer("الصلاة", "teks", strict) {
		t.Error("arabic without normalization accepted bare form")
	}
}
