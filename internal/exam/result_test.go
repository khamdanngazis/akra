package exam

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// gradedEssayEnv: tiga peserta dengan esai sudah dikoreksi (nilai final).
func gradedEssayEnv(t *testing.T) *essayEnv {
	t.Helper()
	e := newEssayEnv(t)
	for i, a := range e.att {
		if rec := e.staffDo(http.MethodPost, e.page+"/"+e.essay+"/"+a.ID, e.a.Admin, url.Values{"skor": {[]string{"2", "1", "0"}[i]}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("grade essay = %d", rec.Code)
		}
	}
	return e
}

func TestResultPagesAndAnalysis(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	base := "/ujian/paket/" + e.ex.ID + "/hasil"
	for _, p := range []string{base, base + "/butir", base + "/tp", base + "/cetak"} {
		rec := e.staffDo(http.MethodGet, p, e.a.Admin, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", p, rec.Code)
		}
	}
	a, err := LoadExamAnalysis(ctx, e.tx, e.ex, ResultFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Summary.Registered != 3 || a.Summary.Final != 3 || len(a.Items) != len(Kinds) {
		t.Fatalf("analysis summary = %+v, items %d", a.Summary, len(a.Items))
	}
	body := e.staffDo(http.MethodGet, base, e.a.Admin, nil).Body.String()
	for _, r := range a.Rows {
		if !strings.Contains(body, r.Name) || !strings.Contains(body, r.Score()) {
			t.Errorf("results page missing %s %s", r.Name, r.Score())
		}
	}
	if !strings.Contains(body, a.Summary.MeanText()) || !strings.Contains(body, "Sebaran nilai") {
		t.Errorf("summary missing on page")
	}
	// Analisis butir tersimpan ke riwayat bank soal, satu baris per soal.
	var n, exams int
	e.tx.QueryRow(ctx, `SELECT count(*), count(DISTINCT exam_id) FROM exam_question_stats WHERE exam_id = $1`, e.ex.ID).Scan(&n, &exams)
	if n == 0 || exams != 1 {
		t.Fatalf("question stats rows = %d", n)
	}
	e.staffDo(http.MethodGet, base+"/butir", e.a.Admin, nil)
	var n2 int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_question_stats WHERE exam_id = $1`, e.ex.ID).Scan(&n2)
	if n2 != n {
		t.Errorf("stats duplicated on reload: %d → %d", n, n2)
	}
	// Kepala sekolah bisa membaca analisis, tidak bisa memberi nilai manual.
	if rec := e.staffDo(http.MethodGet, base+"/butir", e.a.Principal, nil); rec.Code != http.StatusOK {
		t.Errorf("principal analysis = %d", rec.Code)
	}
}

func TestAnalysisExcelMatchesPage(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	base := "/ujian/paket/" + e.ex.ID + "/hasil"
	rec := e.staffDo(http.MethodGet, base+"/analisis.xlsx", e.a.Admin, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), ".xlsx") {
		t.Fatalf("xlsx = %d", rec.Code)
	}
	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a, err := LoadExamAnalysis(ctx, e.tx, e.ex, ResultFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.GetSheetList(); strings.Join(got, "|") != strings.Join([]string{SheetSummary, SheetResults, SheetItems, SheetObjectives, SheetFollow}, "|") {
		t.Fatalf("sheets = %v", got)
	}
	page := e.staffDo(http.MethodGet, base, e.a.Admin, nil).Body.String()
	items := e.staffDo(http.MethodGet, base+"/butir", e.a.Admin, nil).Body.String()
	// Hasil: nama, nilai (angka sama dengan label halaman), status.
	rows, _ := f.GetRows(SheetResults)
	if len(rows) != len(a.Rows)+1 {
		t.Fatalf("result rows = %d", len(rows))
	}
	for i, r := range a.Rows {
		row := rows[i+1]
		raw, _ := f.GetCellValue(SheetResults, "H"+strconv.Itoa(i+2), excelize.Options{RawCellValue: true})
		v, _ := strconv.ParseFloat(raw, 64)
		if row[1] != r.Name || ScoreLabel(int(v*100+0.5)) != r.Score() || row[6] != r.StatusLabel() {
			t.Errorf("excel row %d = %v (raw %s), page %s %s", i, row, raw, r.Name, r.Score())
		}
		if !strings.Contains(page, r.Score()) {
			t.Errorf("page missing score %s", r.Score())
		}
	}
	// Analisis butir: p dan D sama dengan tampilan (koma desimal).
	irows, _ := f.GetRows(SheetItems)
	for i, s := range a.Items {
		if s.N == 0 || s.Voided {
			continue
		}
		raw, _ := f.GetCellValue(SheetItems, "G"+strconv.Itoa(i+2), excelize.Options{RawCellValue: true})
		v, _ := strconv.ParseFloat(raw, 64)
		if decimal2(v) != s.PText() || irows[i+1][7] != s.PLabel() || irows[i+1][10] != s.Decision() {
			t.Errorf("item %d excel p=%s %v, page p=%s", s.Number, raw, irows[i+1], s.PText())
		}
		if !strings.Contains(items, "<strong>"+s.PText()+"</strong>") {
			t.Errorf("item page missing p %s", s.PText())
		}
	}
	// Ringkasan: rata-rata sama.
	srows, _ := f.GetRows(SheetSummary)
	found := false
	for i, r := range srows {
		if len(r) > 0 && r[0] == "Rata-rata" {
			raw, _ := f.GetCellValue(SheetSummary, "B"+strconv.Itoa(i+1), excelize.Options{RawCellValue: true})
			v, _ := strconv.ParseFloat(raw, 64)
			found = ScoreLabel(int(v*100+0.5)) == a.Summary.MeanText()
		}
	}
	if !found {
		t.Errorf("summary mean mismatch")
	}
	// Remedial-Pengayaan: semua siswa bernilai final ada tepat sekali.
	frows, _ := f.GetRows(SheetFollow)
	if len(frows)-1 != len(a.Follow.Remedial)+len(a.Follow.Enrichment) || len(frows)-1 != a.Summary.Final {
		t.Errorf("follow rows = %d, final %d", len(frows)-1, a.Summary.Final)
	}
}

func TestResultDetailAndManualScore(t *testing.T) {
	e := gradedEssayEnv(t)
	ctx := context.Background()
	a0 := e.att[0]
	page := "/ujian/paket/" + e.ex.ID + "/hasil/peserta/" + a0.ID
	rec := e.staffDo(http.MethodGet, page, e.a.Admin, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Fotosintesis adalah") || !strings.Contains(rec.Body.String(), "Log kejadian") {
		t.Fatalf("detail = %d", rec.Code)
	}
	before, _ := e.score(t, a0.ID)
	short := e.items[KindShort].Question.ID // tidak dijawab di env
	post := func(u url.Values, as ...string) int {
		who := e.a.Admin
		if len(as) > 0 {
			who = e.a.Principal
		}
		return e.staffDo(http.MethodPost, page+"/nilai", who, u).Code
	}
	if c := post(url.Values{"qid": {short}, "skor": {"1"}}); c != http.StatusUnprocessableEntity {
		t.Errorf("without reason = %d", c)
	}
	if c := post(url.Values{"qid": {short}, "skor": {"99"}, "alasan": {"x"}}); c != http.StatusUnprocessableEntity {
		t.Errorf("above max = %d", c)
	}
	if c := post(url.Values{"qid": {e.essay}, "skor": {"1"}, "alasan": {"x"}}); c != http.StatusUnprocessableEntity {
		t.Errorf("essay manual = %d", c)
	}
	if c := post(url.Values{"qid": {short}, "skor": {"1"}, "alasan": {"x"}}, "principal"); c != http.StatusForbidden {
		t.Errorf("principal manual = %d", c)
	}
	pts := e.items[KindShort].Points
	if c := post(url.Values{"qid": {short}, "skor": {strconv.Itoa(pts)}, "alasan": {"Jawaban hilang karena listrik padam; dijawab di kertas."}}); c != http.StatusSeeOther {
		t.Fatalf("manual score = %d", c)
	}
	after, st := e.score(t, a0.ID)
	if after <= before || st != ScoreFinal {
		t.Errorf("score %d → %d %s", before, after, st)
	}
	var audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'exam.manual_score' AND entity_id = $1`, a0.ID).Scan(&audits)
	if audits != 1 {
		t.Errorf("manual audits = %d", audits)
	}
	// Bertahan saat nilai ulang (mis. setelah kunci soal lain dibetulkan).
	if _, err := regradeAll(ctx, e.tx, e.ex, nil, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.score(t, a0.ID); again != after {
		t.Errorf("manual points lost on regrade: %d → %d", after, again)
	}
	if b := e.staffDo(http.MethodGet, page, e.a.Admin, nil).Body.String(); !strings.Contains(b, "listrik padam") || !strings.Contains(b, "Nilai manual") {
		t.Errorf("override not shown on detail")
	}
	// Hapus: kembali dinilai otomatis.
	if c := post(url.Values{"qid": {short}, "aksi": {"hapus"}}); c != http.StatusSeeOther {
		t.Fatalf("clear manual = %d", c)
	}
	if back, _ := e.score(t, a0.ID); back != before {
		t.Errorf("after clear = %d, want %d", back, before)
	}
}

func TestFollowUpPrefillFromAnalysis(t *testing.T) {
	e := gradedEssayEnv(t)
	// KKTP 100: semua siswa remedial dan tercentang.
	if _, err := e.tx.Exec(context.Background(), `UPDATE exams SET pass_score = 100 WHERE id = $1`, e.ex.ID); err != nil {
		t.Fatal(err)
	}
	body := e.staffDo(http.MethodGet, "/ujian/paket/"+e.ex.ID+"?tab=lanjut&untuk=remedial", e.a.Admin, nil).Body.String()
	for _, s := range e.students(t) {
		if !strings.Contains(body, `name="student" value="`+s+`" checked`) {
			t.Errorf("student %s not prefilled", s)
		}
	}
	if !strings.Contains(body, "sudah dicentang dari hasil analisis") {
		t.Errorf("prefill notice missing")
	}
	if rec := e.staffDo(http.MethodGet, "/ujian/paket/"+e.ex.ID+"?tab=hasil", e.a.Admin, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("old results tab = %d, want redirect", rec.Code)
	}
}
