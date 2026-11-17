package exam

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestExcelTemplateImports(t *testing.T) {
	data, err := ExcelTemplate()
	if err != nil {
		t.Fatal(err)
	}
	items, err := ParseExcel(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	qs := questionsOf(items)
	if len(qs) != 7 || len(items) != 8 {
		t.Fatalf("items = %d, questions = %d", len(items), len(qs))
	}
	for _, q := range qs {
		if len(q.Problems) > 0 {
			t.Errorf("%s problems: %v", q.Number, q.Problems)
		}
		if _, _, err := BuildQuestion(q.Form, mr(t)); err != nil {
			t.Errorf("%s: %v", q.Number, fieldErrs(err))
		}
	}
	if qs[6].StimIdx != 0 || items[0].Stim.Title != "Teks untuk soal 7" || qs[0].TPCode != "1.1" || !strings.Contains(qs[0].Form.Body, `\(\frac{3}{4}+\frac{1}{2}\)`) {
		t.Errorf("stimulus %d / tp %q / body %s", qs[6].StimIdx, qs[0].TPCode, qs[0].Form.Body)
	}
}

func TestExcelRowErrors(t *testing.T) {
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "soal")
	rows := [][]any{
		{"jenis", "soal", "opsi_a", "opsi_b", "kunci", "stimulus"},
		{"pg", "Soal tanpa kunci", "a", "b", "", ""},
		{"pg", "Kunci bukan huruf", "a", "b", "Z", ""},
		{"jenis-aneh", "x", "a", "b", "A", ""},
		{"pg", "Stimulus hilang", "a", "b", "A", "S9"},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		f.SetSheetRow("soal", cell, &r)
	}
	var buf bytes.Buffer
	f.Write(&buf)
	items, err := ParseExcel(buf.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"baris 2: kunci tidak ditemukan", `baris 3: kunci "Z" bukan huruf opsi`, `baris 4: jenis "jenis-aneh" tidak dikenal`, `baris 5: kode stimulus "s9"`}
	for i, w := range want {
		if !strings.Contains(strings.Join(items[i].Problems, ";"), w) {
			t.Errorf("row %d problems = %v, want %q", i+2, items[i].Problems, w)
		}
	}
	if _, err := ParseExcel([]byte("bukan excel"), nil); err == nil {
		t.Error("non-xlsx accepted")
	}
}

// Round-trip ekspor lalu impor menghasilkan soal identik (kriteria T131).
func TestExcelRoundTrip(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	sa := e.staff(e.a.Teacher.ID, "guru")
	PasteObjectives(ctx, e.tx, sa, e.a.SubjectID, "7", "TP 1.1\tPecahan", now)
	tpl, _ := ExcelTemplate()
	commit := func(data []byte) {
		imp, err := CreateImport(ctx, e.tx, e.store, sa, e.bank.ID, SourceExcel, "s.xlsx", data, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := ProcessImport(ctx, e.tx, e.store, mr(t), e.a.ID, imp.ID); err != nil {
			t.Fatal(err)
		}
		imp, _ = GetImport(ctx, e.tx, e.a.ID, imp.ID)
		if res, err := CommitImport(ctx, e.tx, mr(t), sa, imp, CommitOptions{}, time.Now()); err != nil || res.Ready != 7 {
			t.Fatalf("commit = %+v, %v", res, err)
		}
	}
	commit(tpl)
	first, _, _ := bankQuestions(ctx, e.tx, e.bank)
	rec := e.getAs("/ujian/bank/"+e.bank.ID+"/ekspor/soal.xlsx", e.a.Teacher.ID, "guru")
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d", rec.Code)
	}
	var audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'exam_bank.export'`, e.a.ID).Scan(&audits)
	if audits != 1 {
		t.Errorf("export audit = %d", audits)
	}
	if rec := e.getAs("/ujian/bank/"+e.bank.ID+"/ekspor/soal.xlsx", e.a.Principal.ID, "kepala_sekolah"); rec.Code != http.StatusForbidden {
		t.Errorf("principal export = %d", rec.Code)
	}
	// Impor ekspor ke bank yang sama: setiap soal muncul dua kali dan identik.
	commit(rec.Body.Bytes())
	all, _, _ := bankQuestions(ctx, e.tx, e.bank)
	if len(all) != 14 {
		t.Fatalf("questions after round trip = %d", len(all))
	}
	sig := func(q Question) string {
		k, _ := jsonMarshal(q.Key)
		o, _ := jsonMarshal(q.Options)
		return strings.Join([]string{string(q.Kind), q.BodySrc, string(o), string(k), q.ExplanationSrc, q.Difficulty, q.CognitiveLevel,
			q.ObjectiveID, strings.Join(q.Tags, ","), map[bool]string{true: "stim", false: ""}[q.StimulusID != ""], itoa(q.Points)}, "|")
	}
	var a, b []string
	for _, q := range first {
		a = append(a, sig(q))
	}
	for _, q := range all {
		b = append(b, sig(q))
	}
	count := map[string]int{}
	for _, s := range b {
		count[s]++
	}
	for _, s := range a {
		if count[s] != 2 {
			t.Errorf("question not identical after round trip:\n%s\n(count %d)", s, count[s])
		}
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
