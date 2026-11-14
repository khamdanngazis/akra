package admission

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/web"
)

const (
	childNIK  = "3201010101100001"
	childKK   = "3201010101100099"
	motherNIK = "3201010101800002"
)

// openPeriod membuat periode yang sedang dibuka dengan satu jalur.
func (e *env) openPeriod(t *testing.T) (Period, Track) {
	t.Helper()
	ctx := context.Background()
	f := e.form()
	f.Name = "Gelombang " + fmt.Sprint(time.Now().UnixNano())
	f.RequiredDocs = nil // dokumen wajib diuji di TestDocuments
	p, err := e.svc.Create(ctx, e.actor, f)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Reguler"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	return p, tr
}

func draftForm(p Period, tr Track) DraftForm {
	return DraftForm{PeriodID: p.ID, TrackID: tr.ID, FullName: " Budi  Santoso ", Gender: "L", BirthDate: "2019-05-17", Phone: "0812-3456-7890", Consent: true}
}

// fill mengisi semua langkah dengan data lengkap.
func fill(t *testing.T, svc *ApplicantService, schoolID, id string) Applicant {
	t.Helper()
	ctx := context.Background()
	steps := []struct {
		step string
		v    map[string]string
	}{
		{StepChild, map[string]string{"full_name": "Budi Santoso", "gender": "L", "birth_date": "2019-05-17", "nik": childNIK, "kk_number": childKK,
			"birth_place": "Bandung", "religion": "01", "birth_cert_number": "3273-LT-01"}},
		{StepAddress, map[string]string{"street": "Jl. Melati 5", "village": "Sukajadi", "district": "Sukasari", "residence_type": "01", "transport_mode": "02"}},
		{StepParents, map[string]string{"ibu.full_name": "Sari", "ibu.nik": motherNIK, "ibu.phone": "081299990000", "ibu.occupation": "07"}},
		{StepSchool, map[string]string{"previous_school": "TK Melati"}},
	}
	var a Applicant
	for _, s := range steps {
		var err error
		if a, err = svc.SaveStep(ctx, schoolID, id, s.step, s.v); err != nil {
			t.Fatalf("SaveStep %s: %v", s.step, err)
		}
	}
	return a
}

func TestApplicantDraftAndSubmit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	keys := pii.DevKeyring()
	svc := NewApplicantService(e.tx, keys, e.svc)

	// Validasi langkah awal.
	bad := DraftForm{PeriodID: p.ID, TrackID: tr.ID}
	_, _, err := svc.CreateDraft(ctx, e.schoolA, bad, "")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields["consent"] == "" || ve.Fields["full_name"] == "" || ve.Fields["phone"] == "" || ve.Fields["birth_date"] == "" {
		t.Fatalf("empty draft form: %v", err)
	}

	a, code, err := svc.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != ApplicantDraft || a.FullName != "Budi Santoso" || a.ContactPhone != "+6281234567890" || a.RegistrationNumber != "" || len(code) != 6 {
		t.Fatalf("draft = %+v code %q", a, code)
	}
	if a.Values["registration_type"] != "01" {
		t.Errorf("registration_type = %q", a.Values["registration_type"])
	}

	// Belum lengkap: tidak bisa dikirim.
	if _, err := svc.Submit(ctx, e.schoolA, a.ID); !errors.As(err, &ve) {
		t.Errorf("submit incomplete: %v", err)
	}
	if m := Missing(a); len(m) == 0 {
		t.Error("Missing must list required fields")
	}

	// Error validasi di langkah.
	if _, err := svc.SaveStep(ctx, e.schoolA, a.ID, StepChild, map[string]string{"full_name": "Budi", "gender": "L", "birth_date": "2019-05-17", "nik": "123"}); !errors.As(err, &ve) || ve.Fields["nik"] == "" {
		t.Errorf("invalid NIK step: %v", err)
	}
	if _, err := svc.SaveStep(ctx, e.schoolA, a.ID, StepParents, map[string]string{"ayah.full_name": "Ayah"}); !errors.As(err, &ve) || ve.Fields["ibu.full_name"] == "" {
		t.Errorf("mother name required: %v", err)
	}

	a = fill(t, svc, e.schoolA, a.ID)
	if a.Values["nik"] != childNIK || a.Guardians["ibu"]["nik"] != motherNIK || a.Guardians["ibu"]["phone"] != "+6281299990000" {
		t.Errorf("values not saved: nik %q mother %v", a.Values["nik"], a.Guardians["ibu"])
	}
	if len(Missing(a)) != 0 {
		t.Errorf("Missing = %v", Missing(a))
	}

	// Data sensitif tidak ada di jsonb maupun di baris dalam bentuk teks.
	var profile, guardians string
	var nikEnc, nikIdx []byte
	if err := e.tx.QueryRow(ctx, `SELECT profile::text, guardians::text, nik_enc, nik_idx FROM applicants WHERE id = $1`, a.ID).Scan(&profile, &guardians, &nikEnc, &nikIdx); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{childNIK, childKK, motherNIK, "3273-LT-01"} {
		if strings.Contains(profile, secret) || strings.Contains(guardians, secret) {
			t.Errorf("jsonb contains %s", secret)
		}
	}
	var leaked bool
	if err := e.tx.QueryRow(ctx, `SELECT row_to_json(a)::text LIKE '%' || $2 || '%' OR row_to_json(a)::text LIKE '%' || $3 || '%' FROM applicants a WHERE id = $1`, a.ID, childNIK, motherNIK).Scan(&leaked); err != nil || leaked {
		t.Errorf("row leaks NIK: %v %v", leaked, err)
	}
	if bytes.Contains(nikEnc, []byte(childNIK)) || !bytes.Equal(nikIdx, keys.BlindIndex("nik", childNIK)) {
		t.Error("NIK not encrypted or wrong blind index")
	}

	done, err := svc.Submit(ctx, e.schoolA, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != ApplicantSubmitted || !regexp.MustCompile(`^SPMB-2027-\d{4}$`).MatchString(done.RegistrationNumber) || done.SubmittedAt == nil {
		t.Errorf("submitted = %+v", done)
	}
	if _, err := svc.SaveStep(ctx, e.schoolA, a.ID, StepAddress, map[string]string{"street": "x"}); !errors.Is(err, ErrNotEditable) {
		t.Errorf("edit after submit: %v", err)
	}
	if _, err := svc.Submit(ctx, e.schoolA, a.ID); !errors.Is(err, ErrNotEditable) {
		t.Errorf("submit twice: %v", err)
	}

	// NIK yang sama di periode yang sama ditolak saat dikirim.
	b, _, err := svc.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
	if err != nil {
		t.Fatal(err)
	}
	fill(t, svc, e.schoolA, b.ID)
	if _, err := svc.Submit(ctx, e.schoolA, b.ID); !errors.Is(err, ErrDuplicateNIK) {
		t.Errorf("duplicate NIK submit: %v", err)
	}

	// Sekolah lain tidak bisa membaca.
	if _, err := svc.Load(ctx, e.schoolB, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load other school: %v", err)
	}
}

func TestApplicantClosedAndTrack(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	svc := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)

	// Di luar tanggal: ditolak di server.
	e.svc.today = func() time.Time { return time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC) }
	if _, _, err := svc.CreateDraft(ctx, e.schoolA, draftForm(p, tr), ""); !errors.Is(err, ErrClosed) {
		t.Errorf("closed period: %v", err)
	}
	// TU boleh menginput susulan (T92).
	if _, _, err := svc.CreateDraft(ctx, e.schoolA, draftForm(p, tr), userA); err != nil {
		t.Errorf("TU late entry: %v", err)
	}
	e.svc.today = func() time.Time { return time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC) }

	// Jalur dari periode lain ditolak.
	p2, tr2 := e.openPeriod(t)
	f := draftForm(p, tr)
	f.TrackID = tr2.ID
	var ve *ValidationError
	if _, _, err := svc.CreateDraft(ctx, e.schoolA, f, ""); !errors.As(err, &ve) || ve.Fields["track_id"] == "" {
		t.Errorf("track of another period: %v", err)
	}
	_ = p2
	// Periode sekolah lain.
	if _, _, err := svc.CreateDraft(ctx, e.schoolB, draftForm(p, tr), ""); !errors.As(err, &ve) {
		t.Errorf("other school's period: %v", err)
	}
	// Periode dengan pendaftar tidak bisa dihapus.
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusDraft); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(ctx, e.actor, p.ID); !errors.Is(err, ErrInUse) {
		t.Errorf("delete period with applicants: %v", err)
	}
}

func TestApplicantResume(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	svc := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	a, code, err := svc.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Resume(ctx, e.schoolA, "+62 812 3456 7890", strings.ToLower(code)); err != nil || got.ID != a.ID {
		t.Fatalf("Resume = %v, %v", got.ID, err)
	}
	if _, err := svc.Resume(ctx, e.schoolB, "081234567890", code); !errors.Is(err, ErrNotFound) {
		t.Errorf("resume at other school: %v", err)
	}
	wrong := "AAAAAA"
	if wrong == code {
		wrong = "BBBBBB"
	}
	for i := 0; i < maxResumeFails; i++ {
		if _, err := svc.Resume(ctx, e.schoolA, "081234567890", wrong); !errors.Is(err, ErrNotFound) {
			t.Fatalf("wrong code %d: %v", i, err)
		}
	}
	if _, err := svc.Resume(ctx, e.schoolA, "081234567890", code); !errors.Is(err, ErrResumeLocked) {
		t.Errorf("after %d failures: %v, want locked", maxResumeFails, err)
	}
	svc.now = func() time.Time { return time.Now().Add(resumeLockout + time.Minute) }
	if _, err := svc.Resume(ctx, e.schoolA, "081234567890", code); err != nil {
		t.Errorf("after lockout expires: %v", err)
	}
	// Setelah dikirim, kode lanjut tidak berlaku lagi.
	fill(t, svc, e.schoolA, a.ID)
	if _, err := svc.Submit(ctx, e.schoolA, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resume(ctx, e.schoolA, "081234567890", code); !errors.Is(err, ErrNotFound) {
		t.Errorf("resume after submit: %v", err)
	}
}

// TestSubmitConcurrentNumbers: 50 kiriman bersamaan tidak boleh mendapat
// nomor pendaftaran ganda. Memakai koneksi sungguhan (bukan satu transaksi
// uji), lalu membersihkan datanya.
func TestSubmitConcurrentNumbers(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	code := fmt.Sprintf("test-adm-c%d", time.Now().UnixNano()%1e9)
	var schoolID, yearID, periodID, trackID string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('C', $1) RETURNING id`, code).Scan(&schoolID))
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM applicants WHERE school_id = $1`, `DELETE FROM admission_counters WHERE school_id = $1`,
			`DELETE FROM admission_tracks WHERE school_id = $1`, `DELETE FROM admission_periods WHERE school_id = $1`,
			`DELETE FROM academic_years WHERE school_id = $1`, `DELETE FROM users WHERE school_id = $1`, `DELETE FROM schools WHERE id = $1`} {
			if _, err := pool.Exec(ctx, q, schoolID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	var userID string
	must(pool.QueryRow(ctx, `INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'c@c.test', 'x', 'C') RETURNING id`, schoolID).Scan(&userID))
	must(pool.QueryRow(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, '2027/2028', '2027-07-12', '2028-06-20') RETURNING id`, schoolID).Scan(&yearID))
	must(pool.QueryRow(ctx, `INSERT INTO admission_periods (school_id, academic_year_id, name, target_grade, opens_on, closes_on, status, created_by)
		VALUES ($1, $2, 'G1', 1, '2000-01-01', '2100-01-01', 'buka', $3) RETURNING id`, schoolID, yearID, userID).Scan(&periodID))
	must(pool.QueryRow(ctx, `INSERT INTO admission_tracks (school_id, period_id, name) VALUES ($1, $2, 'R') RETURNING id`, schoolID, periodID).Scan(&trackID))

	periods := NewService(pool)
	svc := NewApplicantService(pool, pii.DevKeyring(), periods)
	p := Period{ID: periodID}
	tr := Track{ID: trackID}
	const n = 50
	ids := make([]string, n)
	for i := range ids {
		a, _, err := svc.CreateDraft(ctx, schoolID, draftForm(p, tr), "")
		must(err)
		a = fill(t, svc, schoolID, a.ID)
		// NIK berbeda per pendaftar.
		_, err = svc.SaveStep(ctx, schoolID, a.ID, StepChild, map[string]string{"full_name": "Anak", "gender": "P", "birth_date": "2019-01-01",
			"nik": fmt.Sprintf("32010101011%05d", i), "kk_number": childKK, "birth_place": "Bandung", "religion": "01"})
		must(err)
		ids[i] = a.ID
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := svc.Submit(ctx, schoolID, id)
			errs <- err
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent submit: %v", err)
		}
	}
	var distinct, maxSeq int
	must(pool.QueryRow(ctx, `SELECT count(DISTINCT registration_number) FROM applicants WHERE school_id = $1 AND registration_number IS NOT NULL`, schoolID).Scan(&distinct))
	must(pool.QueryRow(ctx, `SELECT last_seq FROM admission_counters WHERE school_id = $1`, schoolID).Scan(&maxSeq))
	if distinct != n || maxSeq != n {
		t.Errorf("distinct numbers = %d, last_seq = %d, want %d", distinct, maxSeq, n)
	}
}

// publicClient menjalankan alur formulir publik dengan cookie sesi.
type publicClient struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
}

func newPublicServer(t *testing.T, e *env) *publicClient {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, _ := fs.Sub(web.FS, "templates")
	static, _ := fs.Sub(web.FS, "static")
	rd, err := render.New(templates, static, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewPublicHandler(e.tx, e.svc, NewApplicantService(e.tx, pii.DevKeyring(), e.svc), NewDocumentService(e.tx, store, pii.DevKeyring()), sm, rd, logger)
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	h.Routes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &publicClient{t: t, srv: srv, client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *publicClient) get(path string) (int, string) {
	c.t.Helper()
	res, err := c.client.Get(c.srv.URL + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (c *publicClient) post(path string, form url.Values) (int, string, string) {
	c.t.Helper()
	res, err := c.client.PostForm(c.srv.URL+path, form)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header.Get("Location"), string(b)
}

func TestPublicFlow(t *testing.T) {
	e := newEnv(t)
	p, tr := e.openPeriod(t)
	c := newPublicServer(t, e)
	base := "/s/test-adm-a/daftar"

	if code, _ := c.get("/s/tidak-ada/daftar/"); code != http.StatusNotFound {
		t.Errorf("unknown school: %d", code)
	}
	code, body := c.get(base + "/")
	if code != http.StatusOK || !strings.Contains(body, "Mulai pendaftaran") || !strings.Contains(body, "Jalur Reguler") {
		t.Fatalf("start page: %d", code)
	}
	// Tanpa sesi draf: langkah diarahkan ke halaman awal.
	if code, _ := c.get(base + "/isi/calon"); code != http.StatusSeeOther {
		t.Errorf("step without session: %d", code)
	}

	start := url.Values{"choice": {p.ID + "|" + tr.ID}, "full_name": {"Budi Santoso"}, "gender": {"L"}, "birth_date": {"2019-05-17"}, "phone": {"081234567890"}}
	if code, _, body := c.post(base+"/", start); code != http.StatusBadRequest || !strings.Contains(body, "Persetujuan pengolahan data") {
		t.Errorf("without consent: %d", code)
	}
	start.Set("consent", "on")
	code, loc, _ := c.post(base+"/", start)
	if code != http.StatusSeeOther || !strings.HasSuffix(loc, "/isi/calon") {
		t.Fatalf("create draft: %d %s", code, loc)
	}
	code, body = c.get(base + "/isi/calon")
	m := regexp.MustCompile(`Kode lanjut Anda: <strong>([A-Z0-9]{6})</strong>`).FindStringSubmatch(body)
	if code != http.StatusOK || m == nil {
		t.Fatalf("step page without resume code: %d", code)
	}
	resumeCode := m[1]
	if _, body := c.get(base + "/isi/calon"); strings.Contains(body, "Kode lanjut Anda") {
		t.Error("resume code shown twice")
	}

	steps := []struct {
		step string
		form url.Values
		next string
	}{
		{"calon", url.Values{"full_name": {"Budi Santoso"}, "gender": {"L"}, "birth_date": {"2019-05-17"}, "nik": {childNIK}, "kk_number": {childKK}, "birth_place": {"Bandung"}, "religion": {"01"}, "special_needs": {"01"}}, "alamat"},
		{"alamat", url.Values{"street": {"Jl. Melati 5"}, "village": {"Sukajadi"}, "district": {"Sukasari"}, "residence_type": {"01"}, "transport_mode": {"02"}, "has_kip": {"on"}, "kip_number": {"KIP1"}}, "orangtua"},
		{"orangtua", url.Values{"ibu.full_name": {"Sari"}, "ibu.nik": {motherNIK}, "ayah.full_name": {"Joko"}, "ayah.occupation": {"98"}}, "sekolah"},
		{"sekolah", url.Values{"previous_school": {"TK Melati"}, "email": {"budi@contoh.id"}}, "dokumen"},
		{"dokumen", url.Values{}, "kirim"},
	}
	for _, s := range steps {
		code, loc, body := c.post(base+"/isi/"+s.step, s.form)
		if code != http.StatusSeeOther || !strings.HasSuffix(loc, "/isi/"+s.next) {
			t.Fatalf("step %s: %d %s\n%s", s.step, code, loc, body)
		}
	}
	// Error validasi: isian ditampilkan ulang.
	if code, _, body := c.post(base+"/isi/calon", url.Values{"full_name": {"Budi"}, "gender": {"L"}, "birth_date": {"2019-05-17"}, "nik": {"12"}}); code != http.StatusBadRequest || !strings.Contains(body, "NIK harus 16 digit") {
		t.Errorf("invalid step: %d", code)
	}
	if code, _, _ := c.post(base+"/isi/calon", steps[0].form); code != http.StatusSeeOther {
		t.Fatalf("resave child step: %d", code)
	}
	code, body = c.get(base + "/isi/kirim")
	if code != http.StatusOK || !strings.Contains(body, "Kirim pendaftaran") {
		t.Fatalf("review: %d\n%s", code, body)
	}

	// Melanjutkan dari "HP lain" (klien baru) dengan HP + kode.
	other := c.clone(t)
	if code, _, _ := other.post(base+"/lanjut", url.Values{"phone": {"081234567890"}, "code": {"XXXXXX"}}); code != http.StatusUnauthorized {
		t.Errorf("wrong resume code: %d", code)
	}
	if code, loc, _ := other.post(base+"/lanjut", url.Values{"phone": {"0812 3456 7890"}, "code": {resumeCode}}); code != http.StatusSeeOther || !strings.HasSuffix(loc, "/isi/calon") {
		t.Errorf("resume: %d %s", code, loc)
	}

	code, loc, _ = c.post(base+"/kirim", url.Values{})
	if code != http.StatusSeeOther || !strings.HasSuffix(loc, "/selesai") {
		t.Fatalf("submit: %d %s", code, loc)
	}
	code, body = c.get(base + "/selesai")
	if code != http.StatusOK || !regexp.MustCompile(`SPMB-2027-\d{4}`).MatchString(body) {
		t.Errorf("done page: %d", code)
	}
	// Setelah kirim, langkah tidak bisa dibuka lagi.
	if code, _ := c.get(base + "/isi/calon"); code != http.StatusSeeOther {
		t.Errorf("step after submit: %d", code)
	}

	// Honeypot: tidak membuat draf.
	var before, after int
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM applicants WHERE school_id = $1`, e.schoolA).Scan(&before)
	bot := c.clone(t)
	start.Set("website", "http://spam")
	bot.post(base+"/", start)
	_ = e.tx.QueryRow(context.Background(), `SELECT count(*) FROM applicants WHERE school_id = $1`, e.schoolA).Scan(&after)
	if after != before {
		t.Error("honeypot submission created a draft")
	}
}

// clone membuat klien baru (cookie terpisah) ke server yang sama.
func (c *publicClient) clone(t *testing.T) *publicClient {
	jar, _ := cookiejar.New(nil)
	return &publicClient{t: t, srv: c.srv, client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func TestPIIColumnsRotate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	old := pii.DevKeyring()
	svc := NewApplicantService(e.tx, old, e.svc)
	a, _, err := svc.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
	if err != nil {
		t.Fatal(err)
	}
	fill(t, svc, e.schoolA, a.ID)
	newKeys, err := pii.NewKeyring(map[byte][]byte{1: bytes.Repeat([]byte{0x11}, pii.KeySize), 2: bytes.Repeat([]byte{0x55}, pii.KeySize)}, bytes.Repeat([]byte{0x22}, pii.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range PIIColumns() {
		if _, err := pii.Rotate(ctx, e.tx, newKeys, c, 10); err != nil {
			t.Fatalf("rotate %s: %v", c.AADName, err)
		}
	}
	got, err := NewApplicantService(e.tx, newKeys, e.svc).Load(ctx, e.schoolA, a.ID)
	if err != nil || got.Values["nik"] != childNIK || got.Guardians["ibu"]["nik"] != motherNIK {
		t.Errorf("after rotation: %v %v", got.Values["nik"], err)
	}
}
