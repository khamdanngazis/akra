package admission

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/pii"
	"akra/internal/platform/storage"
)

func TestStaffEntry(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	// Periode sudah lewat: TU tetap boleh menginput pendaftar susulan.
	e.svc.today = func() time.Time { return time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC) }
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keys := pii.DevKeyring()
	apps := NewApplicantService(e.tx, keys, e.svc)
	e.router = e.routerWith(t, apps, NewDocumentService(e.tx, store, keys))

	body := e.do(http.MethodGet, "/pendaftaran/baru", "admin_tu", nil).Body.String()
	if !strings.Contains(body, "di luar masa pendaftaran") {
		t.Error("closed period not offered to staff")
	}
	form := url.Values{"choice": {p.ID + "|" + tr.ID}, "full_name": {"Citra"}, "gender": {"P"}, "birth_date": {"2019-02-02"}, "phone": {"081211112222"}}
	if rec := e.do(http.MethodPost, "/pendaftaran/baru", "admin_tu", form); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Persetujuan") {
		t.Errorf("without consent: %d", rec.Code)
	}
	form.Set("consent", "on")
	rec := e.do(http.MethodPost, "/pendaftaran/baru", "admin_tu", form)
	if rec.Code != http.StatusSeeOther || !strings.HasSuffix(rec.Header().Get("Location"), "/isi/calon") {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	base := strings.TrimSuffix(rec.Header().Get("Location"), "/isi/calon")
	id := strings.TrimPrefix(base, "/pendaftaran/")

	page := e.do(http.MethodGet, base+"/isi/calon", "admin_tu", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `action="`+base+`/isi/calon"`) || strings.Contains(page.Body.String(), "Kode lanjut Anda") {
		t.Errorf("staff step page: %d", page.Code)
	}
	steps := []struct {
		step string
		form url.Values
	}{
		{"calon", url.Values{"full_name": {"Citra"}, "gender": {"P"}, "birth_date": {"2019-02-02"}, "nik": {"3201010101990001"}, "birth_place": {"Garut"}, "religion": {"01"}}},
		{"alamat", url.Values{"street": {"Jl. Anggrek 2"}, "village": {"Cibiru"}, "district": {"Cileunyi"}, "residence_type": {"01"}, "transport_mode": {"01"}}},
		{"orangtua", url.Values{"ibu.full_name": {"Dewi"}}},
		{"sekolah", url.Values{}},
		{"dokumen", url.Values{}},
	}
	// Simpan draf (T107): tetap di langkah yang sama.
	draft := url.Values{"full_name": {"Citra"}, "gender": {"P"}, "birth_date": {"2019-02-02"}, "go": {"draf"}}
	if rec := e.do(http.MethodPost, base+"/isi/calon", "admin_tu", draft); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != base+"/isi/calon" {
		t.Errorf("staff draft: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// Halaman periksa menandai isian yang kurang per langkah.
	review := e.do(http.MethodGet, base+"/isi/kirim", "admin_tu", nil).Body.String()
	if !strings.Contains(review, base+"/isi/calon?kurang=") {
		t.Error("staff review without fix links")
	}
	for _, s := range steps {
		if rec := e.do(http.MethodPost, base+"/isi/"+s.step, "admin_tu", s.form); rec.Code != http.StatusSeeOther {
			t.Fatalf("step %s: %d\n%s", s.step, rec.Code, rec.Body.String())
		}
	}
	rec = e.do(http.MethodPost, base+"/kirim", "admin_tu", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != base {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}

	a, err := apps.Load(ctx, e.schoolA, id)
	if err != nil || a.Status != ApplicantSubmitted || a.EnteredBy != userA {
		t.Errorf("applicant = %+v, %v", a, err)
	}
	events, _ := apps.Events(ctx, e.schoolA, id)
	if len(events) != 1 || events[0].ActorName != "TU" {
		t.Errorf("submit event = %+v", events)
	}
	var audits int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = $1 AND entity_id = $2`, ActionApplicantCreate, id).Scan(&audits)
	if audits != 1 {
		t.Errorf("create audits = %d", audits)
	}
	// Sudah dikirim: halaman isian mengarah ke detail.
	if rec := e.do(http.MethodGet, base+"/isi/calon", "admin_tu", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != base {
		t.Errorf("edit after submit: %d", rec.Code)
	}
	for _, role := range []string{"kepala_sekolah", "guru"} {
		if rec := e.do(http.MethodGet, "/pendaftaran/baru", role, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s GET new: %d", role, rec.Code)
		}
	}
}
