package exam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// staffGet membuka path staf sebagai sa (router uji, identitas dari header).
func staffGet(e *secEnv, path string, sa StaffActor) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Test-User", sa.UserID+"|"+sa.Roles[0])
	rec := httptest.NewRecorder()
	e.examEnv.router.ServeHTTP(rec, req)
	return rec
}

// Tombol utama per status ujian (T160).
func TestPrimaryAction(t *testing.T) {
	e := Exam{ID: "e1", WizardStep: "aturan"}
	for _, c := range []struct {
		status string
		s      frameState
		label  string
		href   string
	}{
		{ExamDraft, frameState{CanWrite: true}, "Lanjutkan menyusun", examStepPath("e1", "aturan")},
		{ExamDraft, frameState{}, "", ""},
		{"terjadwal", frameState{CanWrite: true}, "Cetak kartu", "/ujian/paket/e1?tab=dokumen"},
		{"terjadwal", frameState{}, "Lihat jadwal", "/ujian/paket/e1?tab=sesi"},
		{"berlangsung", frameState{CanWrite: true}, "Pantau sesi", "/ujian/paket/e1?tab=sesi"},
		{"selesai", frameState{CanWrite: true, Awaiting: 3, Release: true, ToReport: true}, "Koreksi esai", "/ujian/paket/e1/esai"},
		{"selesai", frameState{CanWrite: true, Release: true, ToReport: true}, "Buka hasil untuk siswa", "/ujian/paket/e1/hasil"},
		{"selesai", frameState{CanWrite: true, ToReport: true}, "Kirim ke rapor", "/ujian/paket/e1/rapor"},
		{"selesai", frameState{CanWrite: true, ToReport: true, Sent: true, Stale: 2}, "Kirim ke rapor", "/ujian/paket/e1/rapor"},
		{"selesai", frameState{CanWrite: true, ToReport: true, Sent: true}, "Lihat hasil", "/ujian/paket/e1/hasil"},
		{"selesai", frameState{Awaiting: 3, Release: true}, "Lihat hasil", "/ujian/paket/e1/hasil"},
		{"arsip", frameState{CanWrite: true}, "", ""},
	} {
		e.Status = c.status
		got := PrimaryAction(e, c.s)
		if got.Label != c.label || got.Href != c.href {
			t.Errorf("%s %+v = %+v, want %q %q", c.status, c.s, got, c.label, c.href)
		}
	}
}

// Detail, hasil, analisis, koreksi esai, dan kirim rapor memakai kerangka
// yang sama: satu tombol utama di header, tab utama dengan Hasil aktif di
// halaman hasil, dan aksi lama tetap tercapai (T160).
func TestExamFrameAcrossPages(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	var examID string
	var ends time.Time
	if err := e.tx.QueryRow(ctx, `SELECT exam_id::text, ends_at FROM exam_sessions WHERE id = $1`, e.sess).Scan(&examID, &ends); err != nil {
		t.Fatal(err)
	}
	header := func(body string) string {
		i := strings.Index(body, `<div class="page-header">`)
		j := strings.Index(body, `<nav class="tabs mt-4" aria-label="Bagian ujian">`)
		if i < 0 || j < i {
			return ""
		}
		return body[i:j]
	}
	for _, c := range []struct {
		path, tab string
		hasilSub  string // sub-tab aktif bila di tab Hasil
	}{
		{"/ujian/paket/" + examID, "Ringkasan", ""},
		{"/ujian/paket/" + examID + "?tab=soal", "Soal", ""},
		{"/ujian/paket/" + examID + "/hasil", "Hasil", "Nilai"},
		{"/ujian/paket/" + examID + "/hasil/butir", "Hasil", "Analisis butir"},
		{"/ujian/paket/" + examID + "/hasil/tp", "Hasil", "Analisis TP"},
		{"/ujian/paket/" + examID + "/esai", "Hasil", "Koreksi esai"},
		{"/ujian/paket/" + examID + "/rapor", "Hasil", "Kirim ke rapor"},
	} {
		rec := staffGet(e, c.path, e.owner)
		body := rec.Body.String()
		if rec.Code != 200 {
			t.Errorf("%s = %d", c.path, rec.Code)
			continue
		}
		h := header(body)
		if h == "" || strings.Count(h, "btn-primary") > 1 || !strings.Contains(h, "Lainnya") {
			t.Errorf("%s: header without frame or with several primary buttons", c.path)
		}
		if strings.Count(body, "<h1") != 1 {
			t.Errorf("%s: %d <h1>", c.path, strings.Count(body, "<h1"))
		}
		if !strings.Contains(body, `aria-current="page">`+c.tab+`</a>`) {
			t.Errorf("%s: tab %s not active", c.path, c.tab)
		}
		if c.hasilSub != "" && !strings.Contains(body, `aria-current="page">`+c.hasilSub) {
			t.Errorf("%s: sub-tab %s not active", c.path, c.hasilSub)
		}
	}
	// Semua aksi lama halaman hasil tetap tercapai dari header, tab, atau
	// menu "Lainnya" (ujian selesai; arsip tidak tersedia saat berlangsung).
	if _, err := e.tx.Exec(ctx, `UPDATE exams SET status = 'selesai' WHERE id = $1`, examID); err != nil {
		t.Fatal(err)
	}
	body := staffGet(e, "/ujian/paket/"+examID+"/hasil", e.owner).Body.String()
	for _, want := range []string{"/hasil/analisis.xlsx", "/hasil/cetak", "/esai", "/salin", "/arsip", "/naskah", "/kunci"} {
		if !strings.Contains(body, `/ujian/paket/`+examID+want) {
			t.Errorf("results page lacks %s", want)
		}
	}
	// Ujian berlangsung: tombol utama "Pantau sesi".
	if _, err := e.tx.Exec(ctx, `UPDATE exams SET status = 'berlangsung' WHERE id = $1`, examID); err != nil {
		t.Fatal(err)
	}
	if h := header(staffGet(e, "/ujian/paket/"+examID, e.owner).Body.String()); !strings.Contains(h, `btn btn-primary" href="/ujian/paket/`+examID+`?tab=sesi">Pantau sesi`) {
		t.Errorf("running exam primary action:\n%s", h)
	}
}
