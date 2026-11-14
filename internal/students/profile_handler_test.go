package students

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// withUser membuat pengguna uji (ID tetap dari middleware uji) agar
// updated_by dan audit punya rujukan yang valid.
func (e *hEnv) withUser() {
	e.t.Helper()
	if _, err := e.tx.Exec(context.Background(), `INSERT INTO users (id, school_id, email, password_hash, full_name)
		VALUES ('55555555-5555-5555-5555-555555555555', $1, 'uji@a.test', 'x', 'Uji')`, e.schoolA); err != nil {
		e.t.Fatal(err)
	}
}

func TestProfileHandler(t *testing.T) {
	e := newHEnv(t)
	e.withUser()
	id := e.createStudent("001", "Budi", "1A")
	path := "/siswa/" + id + "/data-rinci"

	rec := e.do(http.MethodGet, path, admin, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Kelengkapan data Dapodik") || !strings.Contains(body, "Nama ibu kandung") {
		t.Fatalf("GET: status = %d", rec.Code)
	}

	// Simpan bagian identitas.
	rec = e.do(http.MethodPost, path, admin, url.Values{"section": {"identitas"}, "nik": {nikA}, "religion": {"01"}, "birth_place": {"Bandung"}, "special_needs": {"01"}})
	if rec.Code != http.StatusSeeOther || !strings.HasSuffix(rec.Header().Get("Location"), "#identitas") {
		t.Fatalf("POST identitas: status = %d\n%s", rec.Code, rec.Body.String())
	}
	// Bagian lain tidak terhapus saat menyimpan alamat.
	rec = e.do(http.MethodPost, path, admin, url.Values{"section": {"alamat"}, "street": {"Jl. Melati 5"}, "village": {"Sukajadi"}, "district": {"Sukasari"}, "residence_type": {"01"}, "transport_mode": {"01"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST alamat: status = %d\n%s", rec.Code, rec.Body.String())
	}
	body = e.do(http.MethodGet, path, admin, nil).Body.String()
	if !strings.Contains(body, `value="`+nikA+`"`) || !strings.Contains(body, `value="Jl. Melati 5"`) {
		t.Error("saved values not shown")
	}

	// Error validasi: isian dikembalikan, data lama tetap.
	rec = e.do(http.MethodPost, path, admin, url.Values{"section": {"identitas"}, "nik": {"123"}, "kk_number": {"abc"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "NIK harus 16 digit") || !strings.Contains(rec.Body.String(), `value="123"`) {
		t.Errorf("invalid NIK: status = %d", rec.Code)
	}

	// Ibu kandung belum ada: nama wajib, lalu dibuat dan ditautkan.
	rec = e.do(http.MethodPost, path, admin, url.Values{"section": {"ibu"}, "nik": {"3201010101010002"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Nama wajib diisi") {
		t.Errorf("mother without name: status = %d", rec.Code)
	}
	rec = e.do(http.MethodPost, path, admin, url.Values{"section": {"ibu"}, "full_name": {"Sari"}, "nik": {"3201010101010002"}, "birth_year": {"1985"}, "occupation": {"07"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST ibu: status = %d\n%s", rec.Code, rec.Body.String())
	}
	list, _ := NewGuardianService(e.tx).ListByStudent(context.Background(), e.schoolA, id)
	if len(list) != 1 || list[0].FullName != "Sari" || list[0].Relationship != RelationMother || !list[0].IsBiological || !list[0].IsPrimary {
		t.Fatalf("guardians = %+v", list)
	}
	// Simpan lagi: wali yang sama diperbarui, tidak membuat wali baru.
	if rec := e.do(http.MethodPost, path, admin, url.Values{"section": {"ibu"}, "full_name": {"Nama Lain"}, "nik": {"3201010101010002"}, "birth_year": {"1986"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST ibu again: status = %d", rec.Code)
	}
	list, _ = NewGuardianService(e.tx).ListByStudent(context.Background(), e.schoolA, id)
	if len(list) != 1 || list[0].FullName != "Sari" {
		t.Errorf("second save created/renamed guardian: %+v", list)
	}

	// Kelengkapan: tinggal jenis pendaftaran dan tanggal masuk.
	r, err := GetReadiness(context.Background(), e.tx, e.schoolA, id)
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, f := range r.Missing {
		missing = append(missing, f.Key)
	}
	if strings.Join(missing, ",") != "registration_type,entry_date" {
		t.Errorf("missing = %v (percent %d)", missing, r.Percent)
	}
	if rec := e.do(http.MethodPost, path, admin, url.Values{"section": {"registrasi"}, "registration_type": {"01"}, "entry_date": {"2026-07-13"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST registrasi: %d", rec.Code)
	}
	r, _ = GetReadiness(context.Background(), e.tx, e.schoolA, id)
	if !r.Complete() || r.Percent != 100 {
		t.Errorf("readiness = %+v", r)
	}

	// Daftar siswa: filter "belum lengkap".
	other := e.createStudent("002", "Ani", "1A")
	body = e.do(http.MethodGet, "/siswa?dapodik=belum", admin, nil).Body.String()
	if strings.Contains(body, "/siswa/"+id+`"`) || !strings.Contains(body, "/siswa/"+other+`"`) {
		t.Error("incomplete filter shows complete student or hides incomplete one")
	}
	body = e.do(http.MethodGet, "/siswa", admin, nil).Body.String()
	if !strings.Contains(body, "Lengkap") {
		t.Error("list does not show complete badge")
	}
	if !strings.Contains(e.do(http.MethodGet, "/siswa/"+id, admin, nil).Body.String(), "100% lengkap") {
		t.Error("detail page does not show readiness")
	}

	// Kepala sekolah: baca saja. Guru dan orang tua: 403.
	rec = e.do(http.MethodGet, path, principal, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Simpan ") || !strings.Contains(rec.Body.String(), nikA) {
		t.Errorf("principal GET: status = %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path, principal, url.Values{"section": {"alamat"}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal POST: %d", rec.Code)
	}
	for _, who := range []string{teacher, parent} {
		if rec := e.do(http.MethodGet, path, who, nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s GET: %d", who, rec.Code)
		}
	}

	// Sekolah lain dan bagian tidak dikenal.
	if rec := e.do(http.MethodGet, "/siswa/"+e.studentB+"/data-rinci", admin, nil); rec.Code != http.StatusNotFound {
		t.Errorf("other school GET: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/siswa/"+e.studentB+"/data-rinci", admin, url.Values{"section": {"alamat"}, "street": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("other school POST: %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path, admin, url.Values{"section": {"rahasia"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown section: %d", rec.Code)
	}
}
